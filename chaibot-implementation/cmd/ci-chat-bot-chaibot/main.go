package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"

	"github.com/openshift/ci-tools/pkg/chaibot"
)

type options struct {
	triageConfigPath string
	enableTriage     bool
	slackBotToken    string
	slackAppToken    string
	logLevel         string
}

func main() {
	opts := parseOptions()

	// Setup logger
	logger := setupLogger(opts.logLevel)

	if !opts.enableTriage {
		logger.Info("Chaibot triage is disabled")
		return
	}

	// Load Chaibot configuration
	config, err := chaibot.LoadConfig(opts.triageConfigPath)
	if err != nil {
		logger.WithError(err).Fatal("Failed to load Chaibot configuration")
	}

	logger.WithFields(logrus.Fields{
		"channels":         len(config.MonitoredChannels),
		"mcp_endpoint":     config.MCPEndpoint,
		"analysis_timeout": config.AnalysisTimeout,
	}).Info("Chaibot configuration loaded")

	// Create Slack client
	slackClient := slack.New(
		opts.slackBotToken,
		slack.OptionAppLevelToken(opts.slackAppToken),
	)

	// Create Chaibot analyzer
	analyzer, err := chaibot.NewAnalyzer(config, slackClient, logger.WithField("component", "chaibot"))
	if err != nil {
		logger.WithError(err).Fatal("Failed to create Chaibot analyzer")
	}
	defer analyzer.Close(context.Background())

	logger.Info("Chaibot analyzer initialized")

	// Setup Socket Mode client
	socketClient := socketmode.New(
		slackClient,
	)

	// Handle events
	go handleSocketEvents(socketClient, analyzer, logger)

	// Run Socket Mode client
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle shutdown signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigCh
		logger.Info("Received shutdown signal, stopping Chaibot")
		cancel()
	}()

	logger.Info("Chaibot is running and monitoring channels")

	if err := socketClient.RunContext(ctx); err != nil {
		logger.WithError(err).Fatal("Socket Mode client error")
	}

	logger.Info("Chaibot stopped")
}

func handleSocketEvents(client *socketmode.Client, analyzer *chaibot.Analyzer, logger *logrus.Logger) {
	for evt := range client.Events {
		switch evt.Type {
		case socketmode.EventTypeEventsAPI:
			eventsAPIEvent, ok := evt.Data.(slackevents.EventsAPIEvent)
			if !ok {
				logger.Warn("Unexpected event type")
				continue
			}

			// Acknowledge the event
			client.Ack(*evt.Request)

			// Handle the inner event
			switch ev := eventsAPIEvent.InnerEvent.Data.(type) {
			case *slackevents.MessageEvent:
				// Skip bot messages
				if ev.BotID != "" {
					continue
				}

				// Process message with Chaibot
				ctx := context.Background()
				if err := analyzer.HandleMessage(ctx, &slack.MessageEvent{
					Msg: slack.Msg{
						Type:            ev.Type,
						Channel:         ev.Channel,
						User:            ev.User,
						Text:            ev.Text,
						Timestamp:       ev.TimeStamp,
						ThreadTimestamp: ev.ThreadTimeStamp,
					},
				}); err != nil {
					logger.WithError(err).Error("Failed to handle message")
				}

			default:
				// Ignore other event types
			}

		case socketmode.EventTypeConnectionError:
			logger.Error("Connection error")

		case socketmode.EventTypeInvalidAuth:
			logger.Fatal("Invalid authentication")

		default:
			logger.WithField("type", evt.Type).Debug("Received event")
		}
	}
}

func parseOptions() *options {
	opts := &options{}

	flag.StringVar(&opts.triageConfigPath, "triage-config-path", "/etc/triage-config/triage-config.yaml", "Path to Chaibot triage configuration")
	flag.BoolVar(&opts.enableTriage, "enable-triage", false, "Enable Chaibot test failure triage")
	flag.StringVar(&opts.slackBotToken, "slack-bot-token", os.Getenv("SLACK_BOT_TOKEN"), "Slack bot token")
	flag.StringVar(&opts.slackAppToken, "slack-app-token", os.Getenv("SLACK_APP_TOKEN"), "Slack app token")
	flag.StringVar(&opts.logLevel, "log-level", "info", "Log level (debug, info, warn, error)")

	flag.Parse()

	return opts
}

func setupLogger(level string) *logrus.Logger {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})

	logLevel, err := logrus.ParseLevel(level)
	if err != nil {
		logLevel = logrus.InfoLevel
	}
	logger.SetLevel(logLevel)

	return logger
}
