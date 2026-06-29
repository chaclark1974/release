package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/openshift/ci-tools/pkg/chaibot/mcp"
)

func main() {
	endpoint := "https://ship-help-mcp-continuous-release-tooling--ship-help-bot.apps.gpc.ocp-hub.prod.psi.redhat.com/personas/ocp_ai_helpdesk/mcp"
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJVMEFFVFVCSDlUOSIsInBlcnNvbmEiOiJvY3BfYWlfaGVscGRlc2siLCJqdGkiOiI1YjI3MjFhMGQxZGU0NjE2OTljNDgyNmE3ZmI2ODc4OCIsImlhdCI6MTc4MTYxNTU2MCwic2xhY2tfdXNlcm5hbWUiOiJjaGFjbGFyayJ9.aMohO0DQEqxzm4NGOwWopdLENXM933Kx8I-V0I_IH5I"

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	logEntry := logger.WithField("component", "test")

	fmt.Println("=== Simple Ship-Help Test ===")
	fmt.Println()

	client := mcp.NewClient(endpoint, token, logEntry)

	// Initialize
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Initializing session...")
	if err := client.Initialize(ctx); err != nil {
		fmt.Printf("❌ Failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ Session initialized")
	fmt.Println()

	// Simple question
	fmt.Println("Asking simple question (60 second timeout)...")
	questionCtx, questionCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer questionCancel()

	question := "What is your name?"
	startTime := time.Now()

	answer, err := client.AskPersona(questionCtx, question)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ Failed after %.1fs: %v\n", duration.Seconds(), err)
		os.Exit(1)
	}

	fmt.Printf("✅ Success in %.1fs\n", duration.Seconds())
	fmt.Println()
	fmt.Println("=== Answer ===")
	fmt.Println(answer)
	fmt.Println()
	fmt.Println("=== Test Complete ===")
}
