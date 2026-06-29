package chaibot

import (
	"context"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
)

func TestParseAnalysis(t *testing.T) {
	analyzer := &Analyzer{
		logger: logrus.NewEntry(logrus.New()),
		config: &Config{},
	}

	testCases := []struct {
		name     string
		jobURL   string
		analysis string
		want     struct {
			category   string
			rootCause  string
			hasIssues  bool
		}
	}{
		{
			name:   "Infrastructure failure",
			jobURL: "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/job/123",
			analysis: `The job failed due to AWS InsufficientInstanceCapacity error.
This is an infrastructure issue with 85% confidence.

Related tickets: DPTP-1234, OCPBUGS-5678`,
			want: struct {
				category   string
				rootCause  string
				hasIssues  bool
			}{
				category:   "infrastructure",
				rootCause:  "Infrastructure issue",
				hasIssues:  true,
			},
		},
		{
			name:   "Flaky test",
			jobURL: "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/job/456",
			analysis: `This appears to be a flaky test with intermittent failures.
Confidence: 75%

The test passes most of the time but fails occasionally due to race conditions.`,
			want: struct {
				category   string
				rootCause  string
				hasIssues  bool
			}{
				category:   "flaky_test",
				rootCause:  "Flaky test",
				hasIssues:  false,
			},
		},
		{
			name:   "Product bug",
			jobURL: "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/job/789",
			analysis: `This is a product bug in the OAuth server.
90% confidence

Jira: ACM-9876`,
			want: struct {
				category   string
				rootCause  string
				hasIssues  bool
			}{
				category:   "product_bug",
				rootCause:  "Product bug",
				hasIssues:  true,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := analyzer.parseAnalysis(tc.jobURL, tc.analysis)

			if result.Category != tc.want.category {
				t.Errorf("Category = %v, want %v", result.Category, tc.want.category)
			}

			if result.RootCause != tc.want.rootCause {
				t.Errorf("RootCause = %v, want %v", result.RootCause, tc.want.rootCause)
			}

			if tc.want.hasIssues && len(result.RelatedIssues) == 0 {
				t.Errorf("Expected related issues but got none")
			}

			if result.Confidence == 0 {
				t.Errorf("Confidence should be set")
			}
		})
	}
}

func TestContainsFailureKeywords(t *testing.T) {
	analyzer := &Analyzer{
		logger: logrus.NewEntry(logrus.New()),
	}

	testCases := []struct {
		text string
		want bool
	}{
		{"Job failed again", true},
		{"Test failure in e2e-aws", true},
		{"The tests are failing", true},
		{"Flaky test detected", true},
		{"Looks like a regression", true},
		{"Job passed successfully", false},
		{"Everything is working", false},
	}

	for _, tc := range testCases {
		t.Run(tc.text, func(t *testing.T) {
			got := analyzer.containsFailureKeywords(tc.text)
			if got != tc.want {
				t.Errorf("containsFailureKeywords(%q) = %v, want %v", tc.text, got, tc.want)
			}
		})
	}
}

func TestCheckRateLimit(t *testing.T) {
	analyzer := &Analyzer{
		logger: logrus.NewEntry(logrus.New()),
		config: &Config{
			MaxAnalysesPerHour: 2,
		},
		analysisCache: &AnalysisCache{
			recentAnalyses: make(map[string]time.Time),
			hourlyCount:    0,
			hourStart:      time.Now(),
		},
	}

	jobURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/job/123"

	// First analysis should succeed
	if !analyzer.checkRateLimit(jobURL) {
		t.Error("First analysis should be allowed")
	}

	// Second analysis should succeed
	if !analyzer.checkRateLimit(jobURL+"different") {
		t.Error("Second analysis should be allowed")
	}

	// Third analysis should fail (exceeds hourly limit of 2)
	if analyzer.checkRateLimit(jobURL+"another") {
		t.Error("Third analysis should be blocked by hourly limit")
	}
}

func TestBuildAnalysisPrompt(t *testing.T) {
	analyzer := &Analyzer{
		logger: logrus.NewEntry(logrus.New()),
		config: &Config{
			PromptTemplate: "Custom prompt for {job_url}",
		},
	}

	jobURL := "https://prow.ci.openshift.org/view/gs/test-platform-results/logs/job/123"

	prompt := analyzer.buildAnalysisPrompt(jobURL)

	if prompt != "Custom prompt for "+jobURL {
		t.Errorf("buildAnalysisPrompt() = %v, want custom prompt with URL", prompt)
	}
}

func TestFormatAnalysisMessage(t *testing.T) {
	analyzer := &Analyzer{
		logger: logrus.NewEntry(logrus.New()),
	}

	result := &AnalysisResult{
		JobURL:           "https://prow.ci.openshift.org/view/gs/test/123",
		RootCause:        "Infrastructure issue",
		Category:         "infrastructure",
		Confidence:       0.85,
		Evidence:         "AWS capacity error",
		RelatedIssues:    []string{"DPTP-1234"},
		AnalysisDuration: 45 * time.Second,
	}

	message := analyzer.formatAnalysisMessage(result)

	// Check message contains expected elements
	expectedElements := []string{
		"☁️",                  // Infrastructure emoji
		"Infrastructure issue", // Root cause
		"85%",                 // Confidence
		"AWS capacity error",  // Evidence
		"DPTP-1234",          // Related issue
		"45.0s",              // Duration
		"Chai Bot",           // Powered by
	}

	for _, elem := range expectedElements {
		if !containsString(message, elem) {
			t.Errorf("Message missing expected element: %s\nMessage: %s", elem, message)
		}
	}
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > len(substr) && stringContains(s, substr))
}

func stringContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
