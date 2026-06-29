# Chaibot Implementation - ship-help MCP Client

This directory contains the Go implementation code for Chaibot test failure triage using ship-help MCP.

## Overview

Chaibot automatically monitors Slack channels for test failure messages, analyzes them using the Chai Bot (ship-help MCP) service, and posts intelligent triage analysis directly in Slack threads.

## Architecture

```
pkg/chaibot/
├── mcp/
│   └── client.go          # MCP protocol client for ship-help
├── analyzer.go            # Main Chaibot analyzer logic
└── config.go              # Configuration loader

cmd/ci-chat-bot-chaibot/
└── main.go                # Standalone Chaibot service
```

## Components

### 1. MCP Client (`pkg/chaibot/mcp/client.go`)

Implements the Model Context Protocol (MCP) client for communicating with ship-help:

- **Session Management**: Initializes and maintains MCP sessions
- **Protocol Handling**: JSON-RPC 2.0 over HTTP with SSE support
- **Tool Calling**: Invokes `ask_persona` tool for AI analysis
- **Error Handling**: Robust error handling with retries

**Key Methods**:
- `Initialize(ctx)` - Establishes MCP session
- `AskPersona(ctx, question)` - Calls ship-help AI for analysis
- `Close(ctx)` - Cleans up session

### 2. Analyzer (`pkg/chaibot/analyzer.go`)

Main Chaibot logic for detecting and analyzing test failures:

- **Message Processing**: Detects Prow job URLs in Slack messages
- **Failure Detection**: Keywords-based failure pattern matching
- **Rate Limiting**: Hourly limits and cooldown periods
- **Slack Integration**: Posts formatted analysis with emojis and reactions
- **Response Parsing**: Extracts root cause, confidence, Jira tickets

**Key Methods**:
- `HandleMessage(ctx, event)` - Processes incoming Slack messages
- `AnalyzeFailure(ctx, jobURL)` - Analyzes a specific job failure
- `formatAnalysisMessage(result)` - Formats analysis for Slack

### 3. Configuration (`pkg/chaibot/config.go`)

Loads and validates Chaibot configuration from YAML:

- **File Parsing**: Reads triage-config.yaml
- **Environment Integration**: Loads MCP token from env vars
- **Validation**: Ensures all required fields are present

### 4. Main Service (`cmd/ci-chat-bot-chaibot/main.go`)

Standalone Chaibot service that can run independently:

- **Socket Mode**: Uses Slack Socket Mode for event handling
- **Graceful Shutdown**: Handles SIGTERM/SIGINT
- **Logging**: JSON-formatted structured logging

## Usage

### As Part of ci-chat-bot

Integrate into existing ci-chat-bot by importing the analyzer:

```go
import "github.com/openshift/ci-tools/pkg/chaibot"

// Load configuration
config, err := chaibot.LoadConfig("/etc/triage-config/triage-config.yaml")

// Create analyzer
analyzer, err := chaibot.NewAnalyzer(config, slackClient, logger)
defer analyzer.Close(context.Background())

// Handle messages
func handleMessage(event *slack.MessageEvent) {
    if err := analyzer.HandleMessage(context.Background(), event); err != nil {
        log.Error(err)
    }
}
```

### As Standalone Service

Run Chaibot as a separate service:

```bash
go run cmd/ci-chat-bot-chaibot/main.go \
    --enable-triage=true \
    --triage-config-path=/etc/triage-config/triage-config.yaml \
    --slack-bot-token=$SLACK_BOT_TOKEN \
    --slack-app-token=$SLACK_APP_TOKEN
```

## Environment Variables

Required:
- `SHIP_HELP_MCP_TOKEN` - Authentication token for ship-help MCP
- `SLACK_BOT_TOKEN` - Slack bot user OAuth token
- `SLACK_APP_TOKEN` - Slack app-level token (for Socket Mode)

Optional:
- `LOG_LEVEL` - Logging level (debug, info, warn, error)

## Configuration File

The `triage-config.yaml` file (from openshift/release) is mounted at `/etc/triage-config/triage-config.yaml`:

```yaml
enabled: true

monitored_channels:
  - name: "opp-discussion"
    channel_id: "C04TMLC6DRV"
    auto_respond: true
    response_mode: "thread"

analysis:
  timeout: 120
  ai_provider: "ship-help-mcp"
  mcp_endpoint: "https://ship-help-mcp...redhat.com/personas/ocp_ai_helpdesk/mcp"
  prompt_template: |
    Analyze this failed Prow CI job: {job_url}
    ...

rate_limiting:
  max_analyses_per_hour: 100
  cooldown_seconds: 30
```

## Deployment

### Kubernetes Deployment

The service runs as part of the ci-chat-bot deployment on app.ci:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: ci-chat-bot
  namespace: ci
spec:
  template:
    spec:
      containers:
      - name: bot
        image: gcr.io/k8s-prow/ci-chat-bot:latest
        command:
        - /ci-chat-bot
        args:
        - --enable-triage=true
        - --triage-config-path=/etc/triage-config/triage-config.yaml
        env:
        - name: SHIP_HELP_MCP_TOKEN
          valueFrom:
            secretKeyRef:
              name: cluster-secrets-chaibot-ship-help
              key: ship-help-token
        - name: SLACK_BOT_TOKEN
          valueFrom:
            secretKeyRef:
              name: ci-chat-bot-slack
              key: bot-token
        volumeMounts:
        - name: triage-config
          mountPath: /etc/triage-config
          readOnly: true
      volumes:
      - name: triage-config
        configMap:
          name: ci-chat-bot-triage-config
```

## Testing

### Unit Tests

Run unit tests:

```bash
cd pkg/chaibot
go test -v ./...
```

### Integration Test

Test with a real Prow job:

```bash
export SHIP_HELP_MCP_TOKEN="your-token"
export SLACK_BOT_TOKEN="xoxb-..."
export SLACK_APP_TOKEN="xapp-..."

go run cmd/ci-chat-bot-chaibot/main.go \
    --enable-triage=true \
    --triage-config-path=../../core-services/ci-chat-bot/triage-config.yaml \
    --log-level=debug
```

Then post a message with a Prow URL in a monitored channel.

## Metrics

The analyzer tracks:
- `chaibot_messages_processed_total` - Messages evaluated
- `chaibot_analyses_completed_total` - Analyses finished
- `chaibot_analysis_duration_seconds` - Analysis latency
- `chaibot_mcp_errors_total` - MCP client errors

(Metrics implementation not included - add Prometheus instrumentation)

## Error Handling

The implementation includes:
- Context timeouts for MCP calls (default 120s)
- Automatic retry for transient network errors
- Graceful degradation on MCP failures
- Rate limiting to prevent abuse

## Future Enhancements

- [ ] Add Prometheus metrics
- [ ] Implement caching for repeated job analyses
- [ ] Add support for multiple job URLs in one message
- [ ] Integrate with Sippy for historical data
- [ ] Add configurable retry logic
- [ ] Support batch analysis mode
- [ ] Add health check endpoints

## References

- [MCP Protocol Specification](https://modelcontextprotocol.io/)
- [ship-help Documentation](https://ship-help.corp.redhat.com/)
- [Slack Socket Mode](https://api.slack.com/apis/connections/socket)
- [OpenShift CI Docs](https://docs.ci.openshift.org/)

## License

Apache 2.0 (same as openshift/ci-tools)
