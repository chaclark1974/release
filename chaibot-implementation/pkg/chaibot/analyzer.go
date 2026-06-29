package chaibot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"

	"github.com/openshift/ci-tools/pkg/chaibot/mcp"
)

// Analyzer analyzes test failures using ship-help MCP
type Analyzer struct {
	mcpClient     *mcp.Client
	slackClient   *slack.Client
	config        *Config
	logger        *logrus.Entry
	analysisCache *AnalysisCache
}

// Config holds Chaibot configuration
type Config struct {
	Enabled             bool
	MCPEndpoint         string
	MCPToken            string
	MonitoredChannels   []ChannelConfig
	FailurePatterns     []string
	AnalysisTimeout     time.Duration
	MaxAnalysesPerHour  int
	PromptTemplate      string
}

// ChannelConfig defines a monitored Slack channel
type ChannelConfig struct {
	Name         string
	ChannelID    string
	AutoRespond  bool
	ResponseMode string // "thread" or "channel"
}

// AnalysisResult represents the result of a failure analysis
type AnalysisResult struct {
	JobURL           string
	RootCause        string
	Category         string
	Confidence       float64
	Evidence         string
	Recommendations  []string
	RelatedIssues    []string
	AnalysisDuration time.Duration
}

// AnalysisCache provides rate limiting and caching
type AnalysisCache struct {
	recentAnalyses map[string]time.Time
	hourlyCount    int
	hourStart      time.Time
}

var (
	// Regex to match Prow job URLs
	prowJobURLRegex = regexp.MustCompile(`https://prow\.ci\.openshift\.org/view/gs/[^\s]+`)
)

// NewAnalyzer creates a new Chaibot analyzer
func NewAnalyzer(config *Config, slackClient *slack.Client, logger *logrus.Entry) (*Analyzer, error) {
	if !config.Enabled {
		return nil, fmt.Errorf("chaibot is not enabled")
	}

	var mcpClient *mcp.Client

	// Initialize MCP session if endpoint is configured
	if config.MCPEndpoint != "" {
		mcpClient = mcp.NewClient(config.MCPEndpoint, config.MCPToken, logger)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := mcpClient.Initialize(ctx); err != nil {
			logger.WithError(err).Warn("Failed to initialize MCP client, continuing without AI analysis")
			mcpClient = nil
		} else {
			logger.Info("MCP client initialized successfully")
		}
	} else {
		logger.Info("No MCP endpoint configured, running without AI analysis")
	}

	return &Analyzer{
		mcpClient:   mcpClient,
		slackClient: slackClient,
		config:      config,
		logger:      logger,
		analysisCache: &AnalysisCache{
			recentAnalyses: make(map[string]time.Time),
			hourStart:      time.Now(),
		},
	}, nil
}

// HandleMessage processes a Slack message for potential test failures
func (a *Analyzer) HandleMessage(ctx context.Context, event *slack.MessageEvent) error {
	// Check if message is in a monitored channel
	channel := a.findMonitoredChannel(event.Channel)
	if channel == nil {
		return nil // Not a monitored channel
	}

	// Extract Prow job URLs from message
	jobURLs := prowJobURLRegex.FindAllString(event.Text, -1)
	if len(jobURLs) == 0 {
		return nil // No Prow job URLs found
	}

	// Check if message contains failure keywords
	if !a.containsFailureKeywords(event.Text) && !channel.AutoRespond {
		return nil // No failure keywords and auto-respond is disabled
	}

	a.logger.WithFields(logrus.Fields{
		"channel": event.Channel,
		"user":    event.User,
		"jobs":    len(jobURLs),
	}).Info("Detected test failure message")

	// Analyze the first job URL (for now)
	jobURL := jobURLs[0]

	// Check rate limits
	if !a.checkRateLimit(jobURL) {
		a.logger.Warn("Rate limit exceeded, skipping analysis")
		return nil
	}

	// Post "analyzing" reaction
	if err := a.slackClient.AddReaction("eyes", slack.ItemRef{
		Channel:   event.Channel,
		Timestamp: event.Timestamp,
	}); err != nil {
		a.logger.WithError(err).Warn("Failed to add reaction")
	}

	// Perform analysis
	startTime := time.Now()
	result, err := a.AnalyzeFailure(ctx, jobURL)
	if err != nil {
		a.logger.WithError(err).Error("Failed to analyze failure")

		// Post error reaction
		a.slackClient.AddReaction("x", slack.ItemRef{
			Channel:   event.Channel,
			Timestamp: event.Timestamp,
		})

		return err
	}

	result.AnalysisDuration = time.Since(startTime)

	// Post analysis to Slack
	if err := a.postAnalysis(ctx, event, result, channel.ResponseMode); err != nil {
		a.logger.WithError(err).Error("Failed to post analysis")
		return err
	}

	// Post success reaction
	if err := a.slackClient.AddReaction("white_check_mark", slack.ItemRef{
		Channel:   event.Channel,
		Timestamp: event.Timestamp,
	}); err != nil {
		a.logger.WithError(err).Warn("Failed to add success reaction")
	}

	// Remove analyzing reaction
	a.slackClient.RemoveReaction("eyes", slack.ItemRef{
		Channel:   event.Channel,
		Timestamp: event.Timestamp,
	})

	return nil
}

// AnalyzeFailure analyzes a test failure using ship-help MCP
func (a *Analyzer) AnalyzeFailure(ctx context.Context, jobURL string) (*AnalysisResult, error) {
	a.logger.WithField("job_url", jobURL).Info("Analyzing test failure")

	// If MCP is not available, return a basic result
	if a.mcpClient == nil {
		a.logger.Warn("MCP client not available, returning basic analysis")
		return &AnalysisResult{
			JobURL:      jobURL,
			RootCause:   "Analysis unavailable - MCP service not connected",
			Category:    "unknown",
			Confidence:  0.0,
			Evidence:    fmt.Sprintf("ChaiBot detected a failure but AI analysis is currently unavailable. Please review manually: %s", jobURL),
			Recommendations: []string{"Review job logs manually", "Check Sippy for historical data"},
		}, nil
	}

	// Create analysis prompt from template
	prompt := a.buildAnalysisPrompt(jobURL)

	// Call ship-help MCP
	analysisCtx, cancel := context.WithTimeout(ctx, a.config.AnalysisTimeout)
	defer cancel()

	analysis, err := a.mcpClient.AskPersona(analysisCtx, prompt)
	if err != nil {
		a.logger.WithError(err).Warn("MCP analysis failed, returning basic result")
		return &AnalysisResult{
			JobURL:      jobURL,
			RootCause:   "Analysis failed",
			Category:    "unknown",
			Confidence:  0.0,
			Evidence:    fmt.Sprintf("Failed to analyze with MCP: %v. Please review manually: %s", err, jobURL),
			Recommendations: []string{"Review job logs manually", "Check Sippy for historical data"},
		}, nil
	}

	// Parse the analysis response
	result := a.parseAnalysis(jobURL, analysis)

	a.logger.WithFields(logrus.Fields{
		"job_url":     jobURL,
		"category":    result.Category,
		"confidence":  result.Confidence,
	}).Info("Analysis complete")

	return result, nil
}

// buildAnalysisPrompt creates the analysis prompt
func (a *Analyzer) buildAnalysisPrompt(jobURL string) string {
	if a.config.PromptTemplate != "" {
		return strings.ReplaceAll(a.config.PromptTemplate, "{job_url}", jobURL)
	}

	// Default prompt
	return fmt.Sprintf(`Analyze this failed Prow CI job: %s

Please provide a comprehensive failure analysis:

1. **Which step(s) failed?**
2. **Root cause:** Product bug, test issue, or infrastructure problem?
3. **Related Jira tickets:** Duplicates, auto-filed tickets
4. **Pass rate:** Last 14 days if available
5. **Recommended fixes:** Prioritized options
6. **Next steps:** Who to escalate to, what action to take

Format with clear headings and Jira links: [TICKET-123](https://redhat.atlassian.net/browse/TICKET-123)`, jobURL)
}

// parseAnalysis parses the ship-help analysis response
func (a *Analyzer) parseAnalysis(jobURL, analysis string) *AnalysisResult {
	result := &AnalysisResult{
		JobURL: jobURL,
	}

	// Extract root cause
	if strings.Contains(strings.ToLower(analysis), "infrastructure") {
		result.Category = "infrastructure"
		result.RootCause = "Infrastructure issue"
	} else if strings.Contains(strings.ToLower(analysis), "flaky") || strings.Contains(strings.ToLower(analysis), "intermittent") {
		result.Category = "flaky_test"
		result.RootCause = "Flaky test"
	} else if strings.Contains(strings.ToLower(analysis), "bug") {
		result.Category = "product_bug"
		result.RootCause = "Product bug"
	} else {
		result.Category = "unknown"
		result.RootCause = "Unknown"
	}

	// Extract confidence (look for percentage)
	confRegex := regexp.MustCompile(`(\d+)%`)
	if matches := confRegex.FindStringSubmatch(analysis); len(matches) > 1 {
		fmt.Sscanf(matches[1], "%f", &result.Confidence)
		result.Confidence /= 100.0
	} else {
		result.Confidence = 0.75 // Default confidence
	}

	// Extract Jira tickets
	jiraRegex := regexp.MustCompile(`([A-Z]+-\d+)`)
	result.RelatedIssues = jiraRegex.FindAllString(analysis, -1)

	// Full analysis is the evidence
	result.Evidence = analysis

	// Extract recommendations (lines starting with numbers or bullets)
	lines := strings.Split(analysis, "\n")
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "1.") || strings.HasPrefix(trimmed, "2.") ||
			strings.HasPrefix(trimmed, "•") || strings.HasPrefix(trimmed, "-") {
			result.Recommendations = append(result.Recommendations, trimmed)
		}
	}

	return result
}

// postAnalysis posts the analysis to Slack
func (a *Analyzer) postAnalysis(ctx context.Context, event *slack.MessageEvent, result *AnalysisResult, mode string) error {
	// Format the analysis message
	message := a.formatAnalysisMessage(result)

	// Determine where to post
	timestamp := event.Timestamp
	if mode == "thread" {
		timestamp = event.ThreadTimestamp
		if timestamp == "" {
			timestamp = event.Timestamp // Start a new thread
		}
	}

	// Post to Slack
	_, _, err := a.slackClient.PostMessage(
		event.Channel,
		slack.MsgOptionText(message, false),
		slack.MsgOptionTS(timestamp),
	)

	return err
}

// formatAnalysisMessage formats the analysis for Slack
func (a *Analyzer) formatAnalysisMessage(result *AnalysisResult) string {
	var sb strings.Builder

	// Emoji based on category
	emoji := map[string]string{
		"infrastructure": "☁️",
		"flaky_test":     "🎲",
		"product_bug":    "🐛",
		"configuration":  "🔧",
		"unknown":        "❓",
	}

	sb.WriteString(fmt.Sprintf("%s *Test Failure Analysis*\n\n", emoji[result.Category]))
	sb.WriteString(fmt.Sprintf("*Root Cause:* %s (%.0f%% confidence)\n\n", result.RootCause, result.Confidence*100))

	// Evidence/Analysis
	sb.WriteString("*Analysis:*\n")
	sb.WriteString(result.Evidence)
	sb.WriteString("\n\n")

	// Related issues
	if len(result.RelatedIssues) > 0 {
		sb.WriteString("*Related Issues:*\n")
		for _, issue := range result.RelatedIssues {
			sb.WriteString(fmt.Sprintf("• <%s|%s>\n",
				fmt.Sprintf("https://redhat.atlassian.net/browse/%s", issue),
				issue))
		}
		sb.WriteString("\n")
	}

	// Footer
	sb.WriteString(fmt.Sprintf("\n_Analysis completed in %.1fs • Powered by Chai Bot_",
		result.AnalysisDuration.Seconds()))

	return sb.String()
}

// findMonitoredChannel checks if a channel is monitored
func (a *Analyzer) findMonitoredChannel(channelID string) *ChannelConfig {
	for i := range a.config.MonitoredChannels {
		if a.config.MonitoredChannels[i].ChannelID == channelID {
			return &a.config.MonitoredChannels[i]
		}
	}
	return nil
}

// containsFailureKeywords checks if text contains failure-related keywords
func (a *Analyzer) containsFailureKeywords(text string) bool {
	lowerText := strings.ToLower(text)
	keywords := []string{"failed", "failure", "failing", "flaky", "broken", "regression"}

	for _, keyword := range keywords {
		if strings.Contains(lowerText, keyword) {
			return true
		}
	}

	return false
}

// checkRateLimit checks if analysis is allowed based on rate limits
func (a *Analyzer) checkRateLimit(jobURL string) bool {
	now := time.Now()

	// Reset hourly counter if hour has passed
	if now.Sub(a.analysisCache.hourStart) > time.Hour {
		a.analysisCache.hourlyCount = 0
		a.analysisCache.hourStart = now
	}

	// Check hourly limit
	if a.analysisCache.hourlyCount >= a.config.MaxAnalysesPerHour {
		return false
	}

	// Check cooldown for same job
	if lastAnalysis, exists := a.analysisCache.recentAnalyses[jobURL]; exists {
		if now.Sub(lastAnalysis) < 30*time.Second {
			return false
		}
	}

	// Update cache
	a.analysisCache.recentAnalyses[jobURL] = now
	a.analysisCache.hourlyCount++

	return true
}

// Close closes the analyzer and cleans up resources
func (a *Analyzer) Close(ctx context.Context) error {
	if a.mcpClient != nil {
		return a.mcpClient.Close(ctx)
	}
	return nil
}
