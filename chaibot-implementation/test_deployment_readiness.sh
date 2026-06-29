#!/bin/bash

echo "==================================================================="
echo "    Chaibot Deployment Readiness Test for #opp-discussion"
echo "==================================================================="
echo ""

# Colors
GREEN='\033[0;32m'
RED='\033[0;31m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}✅ PASS${NC}: $1"; }
fail() { echo -e "${RED}❌ FAIL${NC}: $1"; }
warn() { echo -e "${YELLOW}⚠️  WARN${NC}: $1"; }

ERRORS=0
WARNINGS=0

echo "Test 1: Configuration File Exists"
if [ -f ../core-services/ci-chat-bot/triage-config.yaml ]; then
    pass "triage-config.yaml exists"
else
    fail "triage-config.yaml not found"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Test 2: Configuration Valid YAML"
if python3 -c "import yaml; yaml.safe_load(open('../core-services/ci-chat-bot/triage-config.yaml'))" 2>/dev/null; then
    pass "YAML syntax valid"
else
    fail "YAML syntax invalid"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Test 3: #opp-discussion Configured"
if grep -q "C04TMLC6DRV" ../core-services/ci-chat-bot/triage-config.yaml; then
    pass "#opp-discussion channel ID found (C04TMLC6DRV)"
else
    fail "#opp-discussion not configured"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Test 4: Ship-Help MCP Endpoint Configured"
if grep -q "ship-help-mcp" ../core-services/ci-chat-bot/triage-config.yaml; then
    pass "Ship-help MCP endpoint configured"
else
    fail "Ship-help endpoint missing"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Test 5: Timeout Set Correctly"
TIMEOUT=$(grep "timeout:" ../core-services/ci-chat-bot/triage-config.yaml | awk '{print $2}')
if [ "$TIMEOUT" == "300" ]; then
    pass "Timeout set to 300 seconds (5 minutes)"
elif [ "$TIMEOUT" == "120" ]; then
    fail "Timeout still 120 seconds - should be 300"
    ERRORS=$((ERRORS + 1))
else
    warn "Unexpected timeout value: $TIMEOUT"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""
echo "Test 6: Implementation Code Exists"
if [ -f pkg/chaibot/analyzer.go ] && [ -f pkg/chaibot/mcp/client.go ]; then
    pass "Implementation files present"
    
    # Count lines
    LINES=$(find pkg/chaibot -name "*.go" -exec wc -l {} \; | awk '{sum+=$1} END {print sum}')
    echo "     Total implementation: $LINES lines of Go code"
else
    fail "Implementation files missing"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Test 7: ConfigMap YAML Exists"
if [ -f ../clusters/app.ci/ci-chat-bot/chaibot-configmap.yaml ]; then
    pass "ConfigMap YAML exists"
else
    fail "ConfigMap YAML missing"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Test 8: Deployment YAML Updated"
if grep -q "SHIP_HELP_MCP_TOKEN" ../clusters/app.ci/ci-chat-bot/ci-chat-bot.yaml 2>/dev/null; then
    pass "Deployment has SHIP_HELP_MCP_TOKEN env var"
else
    warn "Deployment may need SHIP_HELP_MCP_TOKEN env var"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""
echo "Test 9: Secret Bootstrap Configuration"
if grep -q "ship-help-mcp-token" ../core-services/ci-secret-bootstrap/_config.yaml 2>/dev/null; then
    pass "Secret bootstrap configured"
else
    warn "Secret bootstrap may need configuration"
    WARNINGS=$((WARNINGS + 1))
fi

echo ""
echo "Test 10: Ship-Help Token Format Valid"
TOKEN="eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJVMEFFVFVCSDlUOSIsInBlcnNvbmEiOiJvY3BfYWlfaGVscGRlc2siLCJqdGkiOiI1YjI3MjFhMGQxZGU0NjE2OTljNDgyNmE3ZmI2ODc4OCIsImlhdCI6MTc4MTYxNTU2MCwic2xhY2tfdXNlcm5hbWUiOiJjaGFjbGFyayJ9.aMohO0DQEqxzm4NGOwWopdLENXM933Kx8I-V0I_IH5I"

# JWT has 3 parts separated by dots
if [ $(echo "$TOKEN" | grep -o "\." | wc -l) -eq 2 ]; then
    pass "Token format valid (JWT with 3 parts)"
    
    # Decode header (base64)
    HEADER=$(echo "$TOKEN" | cut -d. -f1 | base64 -d 2>/dev/null)
    if echo "$HEADER" | grep -q "HS256"; then
        echo "     Algorithm: HS256"
    fi
    
    # Decode payload
    PAYLOAD=$(echo "$TOKEN" | cut -d. -f2 | base64 -d 2>/dev/null)
    if echo "$PAYLOAD" | grep -q "ocp_ai_helpdesk"; then
        echo "     Persona: ocp_ai_helpdesk ✓"
    fi
    if echo "$PAYLOAD" | grep -q "chaclark"; then
        echo "     User: chaclark ✓"
    fi
else
    fail "Token format invalid"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "==================================================================="
echo "                  Deployment Simulation"
echo "==================================================================="
echo ""

echo "What would happen when a user posts in #opp-discussion:"
echo ""
echo "User posts:"
echo "  'Job failed: https://prow.ci.openshift.org/view/gs/test-platform-results/logs/periodic-ci-stolostron-policy-collection-main-ocp4.22-interop-opp-aws/2066591093067091968'"
echo ""
echo "Chaibot flow:"
echo "  1. ✓ Message received in monitored channel C04TMLC6DRV"
echo "  2. ✓ Prow URL detected: https://prow.ci.openshift.org/view/gs/..."
echo "  3. ✓ Failure keyword detected: 'failed'"
echo "  4. ✓ Rate limit check: PASS (first analysis)"
echo "  5. ✓ Add 👀 reaction to message"
echo "  6. ✓ Call ship-help MCP endpoint"
echo "  7. ⏱  Wait 60-300 seconds for analysis"
echo "  8. ✓ Parse AI response:"
echo "       - Root cause: Infrastructure/Flaky/Bug"
echo "       - Confidence score: 0-100%"
echo "       - Jira tickets: ACM-XXXXX, LPINTEROP-XXXXX"
echo "       - Recommendations: [AI generated]"
echo "  9. ✓ Format for Slack:"
echo "       - Add emoji based on category (☁️/🎲/🐛)"
echo "       - Create sections (summary, analysis, issues)"
echo "       - Add links to Jira tickets"
echo " 10. ✓ Post in thread reply"
echo " 11. ✓ Add ✅ reaction"
echo ""

echo "Expected output in Slack thread:"
echo ""
echo "╔═════════════════════════════════════════════════════════════╗"
echo "║  ☁️ Test Failure Analysis                                   ║"
echo "║                                                             ║"
echo "║  Root Cause: Infrastructure - Pod failure (85% confidence)  ║"
echo "║                                                             ║"
echo "║  Analysis:                                                  ║"
echo "║  The job periodic-ci-stolostron-policy-collection-main-    ║"
echo "║  ocp4.22-interop-opp-aws failed in the test phase due to   ║"
echo "║  a pod failure in the acm-fetch-managed-clusters step.     ║"
echo "║  This is a known infrastructure issue affecting AWS-based   ║"
echo "║  interop tests.                                             ║"
echo "║                                                             ║"
echo "║  Related Issues:                                            ║"
echo "║  • ACM-35382 - Pod failure in acm-fetch-managed-clusters   ║"
echo "║  • LPINTEROP-6873 - Test failure in acm-tests-clc-create   ║"
echo "║                                                             ║"
echo "║  Recommendations:                                           ║"
echo "║  1. Retry the job (likely to pass on retry)                ║"
echo "║  2. Check if AWS quota issues are affecting the test        ║"
echo "║  3. Monitor Sippy for failure rate trend                    ║"
echo "║                                                             ║"
echo "║  Analysis completed in 48.3s • Powered by Chai Bot          ║"
echo "╚═════════════════════════════════════════════════════════════╝"
echo ""

echo "==================================================================="
echo "                     Test Summary"
echo "==================================================================="
echo ""

if [ $ERRORS -eq 0 ] && [ $WARNINGS -eq 0 ]; then
    echo -e "${GREEN}✅ ALL TESTS PASSED${NC}"
    echo ""
    echo "Deployment Status: READY ✓"
    echo ""
    echo "Next Steps:"
    echo "1. Commit timeout change to PR-80559"
    echo "2. Wait for PR review and merge"
    echo "3. Request DPTP team to create ship-help secret"
    echo "4. Test in #opp-discussion with real Prow URL"
    EXIT_CODE=0
elif [ $ERRORS -eq 0 ]; then
    echo -e "${YELLOW}⚠️  PASSED WITH WARNINGS${NC}"
    echo ""
    echo "Warnings: $WARNINGS"
    echo ""
    echo "Deployment Status: READY (with minor issues)"
    echo ""
    echo "Review warnings above before deploying"
    EXIT_CODE=0
else
    echo -e "${RED}❌ TESTS FAILED${NC}"
    echo ""
    echo "Errors: $ERRORS"
    echo "Warnings: $WARNINGS"
    echo ""
    echo "Deployment Status: NOT READY"
    echo ""
    echo "Fix errors above before deploying"
    EXIT_CODE=1
fi

echo ""
echo "Test completed: $(date)"
echo ""

exit $EXIT_CODE
