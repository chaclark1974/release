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
	jobURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-stolostron-policy-collection-main-ocp4.22-interop-opp-aws/2066591093067091968"

	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})
	logEntry := logger.WithField("component", "test")

	fmt.Println("=== Prow Job Metadata Test ===")
	fmt.Printf("Job URL: %s\n", jobURL)
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

	// Ask for just metadata, not full analysis
	fmt.Println("Requesting job information (not full log analysis)...")
	fmt.Println("Timeout: 120 seconds")
	fmt.Println()

	questionCtx, questionCancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer questionCancel()

	// Simpler question that doesn't require downloading all logs
	question := fmt.Sprintf(`Look at this Prow job: %s

Without analyzing all the logs, can you tell me:
1. What is the job name?
2. What release/version is it testing?
3. Based on the job name, what component is being tested?

Keep the answer brief (2-3 sentences).`, jobURL)

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
