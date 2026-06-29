package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

// Client is a Model Context Protocol client for ship-help
type Client struct {
	endpoint   string
	token      string
	httpClient *http.Client
	sessionID  string
	logger     *logrus.Entry
}

// NewClient creates a new MCP client
func NewClient(endpoint, token string, logger *logrus.Entry) *Client {
	return &Client{
		endpoint: endpoint,
		token:    token,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
		logger: logger,
	}
}

// MCPRequest represents a JSON-RPC request
type MCPRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      string      `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// MCPResponse represents a JSON-RPC response
type MCPResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      string          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *MCPError       `json:"error,omitempty"`
}

// MCPError represents a JSON-RPC error
type MCPError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// InitializeParams are parameters for the initialize method
type InitializeParams struct {
	ProtocolVersion string     `json:"protocolVersion"`
	ClientInfo      ClientInfo `json:"clientInfo"`
	Capabilities    struct{}   `json:"capabilities"`
}

// ClientInfo contains client metadata
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolCallParams are parameters for calling a tool
type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

// ToolCallResult is the result from a tool call
type ToolCallResult struct {
	Content []ContentBlock `json:"content"`
}

// ContentBlock represents a content block in the response
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// Initialize establishes an MCP session
func (c *Client) Initialize(ctx context.Context) error {
	c.logger.Info("Initializing MCP session with ship-help")

	req := MCPRequest{
		JSONRPC: "2.0",
		ID:      "init-1",
		Method:  "initialize",
		Params: InitializeParams{
			ProtocolVersion: "2024-11-05",
			ClientInfo: ClientInfo{
				Name:    "chaibot",
				Version: "1.0.0",
			},
			Capabilities: struct{}{},
		},
	}

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to initialize MCP session: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("MCP initialize error: %s", resp.Error.Message)
	}

	c.logger.WithField("response", string(resp.Result)).Debug("MCP session initialized")
	return nil
}

// AskPersona calls the ask_persona tool to analyze a question
func (c *Client) AskPersona(ctx context.Context, question string) (string, error) {
	c.logger.WithField("question_length", len(question)).Info("Calling ask_persona tool")

	req := MCPRequest{
		JSONRPC: "2.0",
		ID:      fmt.Sprintf("ask-%d", time.Now().Unix()),
		Method:  "tools/call",
		Params: ToolCallParams{
			Name: "ask_persona",
			Arguments: map[string]interface{}{
				"question": question,
			},
		},
	}

	resp, err := c.sendRequest(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to call ask_persona: %w", err)
	}

	if resp.Error != nil {
		return "", fmt.Errorf("ask_persona error: %s", resp.Error.Message)
	}

	// Parse the result
	var result ToolCallResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return "", fmt.Errorf("failed to parse tool result: %w", err)
	}

	// Extract text from content blocks
	var texts []string
	for _, block := range result.Content {
		if block.Type == "text" {
			texts = append(texts, block.Text)
		}
	}

	if len(texts) == 0 {
		return "", fmt.Errorf("no text content in response")
	}

	analysis := strings.Join(texts, "\n")
	c.logger.WithField("analysis_length", len(analysis)).Info("Received analysis from ship-help")

	return analysis, nil
}

// sendRequest sends an MCP request and returns the response
func (c *Client) sendRequest(ctx context.Context, req MCPRequest) (*MCPResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	httpReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.token))
	if c.sessionID != "" {
		httpReq.Header.Set("MCP-Session-ID", c.sessionID)
	}

	c.logger.WithFields(logrus.Fields{
		"method":   req.Method,
		"endpoint": c.endpoint,
	}).Debug("Sending MCP request")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Check for session ID in response headers
	if sessionID := resp.Header.Get("MCP-Session-ID"); sessionID != "" && c.sessionID == "" {
		c.sessionID = sessionID
		c.logger.WithField("session_id", sessionID).Debug("Received MCP session ID")
	}

	// Handle Server-Sent Events (SSE) response
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return c.parseSSEResponse(resp.Body)
	}

	// Handle regular JSON response
	var mcpResp MCPResponse
	if err := json.NewDecoder(resp.Body).Decode(&mcpResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &mcpResp, nil
}

// parseSSEResponse parses a Server-Sent Events response
func (c *Client) parseSSEResponse(body io.Reader) (*MCPResponse, error) {
	scanner := bufio.NewScanner(body)
	var dataLines []string

	for scanner.Scan() {
		line := scanner.Text()

		// SSE format: "event: message\ndata: {...}"
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			dataLines = append(dataLines, data)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading SSE stream: %w", err)
	}

	if len(dataLines) == 0 {
		return nil, fmt.Errorf("no data received in SSE stream")
	}

	// Parse the last data line as the response
	var mcpResp MCPResponse
	if err := json.Unmarshal([]byte(dataLines[len(dataLines)-1]), &mcpResp); err != nil {
		return nil, fmt.Errorf("failed to parse SSE data: %w", err)
	}

	return &mcpResp, nil
}

// Close closes the MCP session
func (c *Client) Close(ctx context.Context) error {
	if c.sessionID == "" {
		return nil // No active session
	}

	c.logger.Info("Closing MCP session")

	// MCP doesn't have an explicit close method, session expires automatically
	c.sessionID = ""
	return nil
}
