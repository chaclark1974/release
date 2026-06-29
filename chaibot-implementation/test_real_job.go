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
	// Configuration
	endpoint := "https://ship-help-mcp-continuous-release-tooling--ship-help-bot.apps.gpc.ocp-hub.prod.psi.redhat.com/personas/ocp_ai_helpdesk/mcp"
	token := "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJVMEFFVFVCSDlUOSIsInBlcnNvbmEiOiJvY3BfYWlfaGVscGRlc2siLCJqdGkiOiI1YjI3MjFhMGQxZGU0NjE2OTljNDgyNmE3ZmI2ODc4OCIsImlhdCI6MTc4MTYxNTU2MCwic2xhY2tfdXNlcm5hbWUiOiJjaGFjbGFyayJ9.aMohO0DQEqxzm4NGOwWopdLENXM933Kx8I-V0I_IH5I"
	jobURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-stolostron-policy-collection-main-ocp4.22-interop-opp-aws/2066591093067091968"

	// Setup logger
	logger := logrus.New()
	logger.SetLevel(logrus.InfoLevel)
	logger.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})
	logEntry := logger.WithField("component", "test")

	fmt.Println("=== Chaibot Real Job Test ===")
	fmt.Printf("Job URL: %s\n", jobURL)
	fmt.Printf("MCP Endpoint: %s\n", endpoint)
	fmt.Println()

	// Create MCP client
	client := mcp.NewClient(endpoint, token, logEntry)

	// Initialize session
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fmt.Println("Step 1: Initializing MCP session...")
	if err := client.Initialize(ctx); err != nil {
		fmt.Printf("❌ Failed to initialize: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ MCP session initialized")
	fmt.Println()

	// Build analysis prompt
	prompt := fmt.Sprintf(`Analyze this failed Prow CI job: %s

Please provide a comprehensive failure analysis:

1. **Which step(s) failed?**
2. **Root cause:** Product bug, test issue, or infrastructure problem?
3. **Related Jira tickets:** Duplicates, auto-filed tickets
4. **Pass rate:** Last 14 days if available
5. **Recommended fixes:** Prioritized options
6. **Next steps:** Who to escalate to, what action to take

Format with clear headings and Jira links: [TICKET-123](https://redhat.atlassian.net/browse/TICKET-123)`, jobURL)

	// Analyze failure
	fmt.Println("Step 2: Requesting analysis from ship-help...")
	fmt.Println("(This will take 30-90 seconds - ship-help is analyzing the job)")
	fmt.Println()

	analysisCtx, analysisCancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer analysisCancel()

	startTime := time.Now()
	analysis, err := client.AskPersona(analysisCtx, prompt)
	duration := time.Since(startTime)

	if err != nil {
		fmt.Printf("❌ Analysis failed: %v\n", err)
		os.Exit(1)
	}

	// Display results
	fmt.Println("✅ Analysis completed!")
	fmt.Printf("⏱️  Duration: %.1f seconds\n", duration.Seconds())
	fmt.Println()
	fmt.Println("=== Ship-Help Analysis ===")
	fmt.Println()
	fmt.Println(analysis)
	fmt.Println()
	fmt.Println("=== Test Complete ===")

	// Close session
	if err := client.Close(context.Background()); err != nil {
		fmt.Printf("Warning: Failed to close session: %v\n", err)
	}
}
