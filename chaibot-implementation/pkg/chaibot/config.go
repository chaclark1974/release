package chaibot

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// ConfigFromFile loads Chaibot configuration from a YAML file
type ConfigFromFile struct {
	Enabled           bool            `yaml:"enabled"`
	MonitoredChannels []ChannelConfig `yaml:"monitored_channels"`
	Analysis          AnalysisConfig  `yaml:"analysis"`
	RateLimiting      RateLimitConfig `yaml:"rate_limiting"`
}

// AnalysisConfig contains analysis-specific settings
type AnalysisConfig struct {
	Timeout        int    `yaml:"timeout"`
	AIProvider     string `yaml:"ai_provider"`
	MCPEndpoint    string `yaml:"mcp_endpoint"`
	PromptTemplate string `yaml:"prompt_template"`
}

// RateLimitConfig contains rate limiting settings
type RateLimitConfig struct {
	MaxAnalysesPerHour int `yaml:"max_analyses_per_hour"`
	CooldownSeconds    int `yaml:"cooldown_seconds"`
}

// LoadConfig loads Chaibot configuration from file and environment
func LoadConfig(configPath string) (*Config, error) {
	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var fileConfig ConfigFromFile
	if err := yaml.Unmarshal(data, &fileConfig); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// Get MCP token from environment
	mcpToken := os.Getenv("SHIP_HELP_MCP_TOKEN")
	if mcpToken == "" {
		return nil, fmt.Errorf("SHIP_HELP_MCP_TOKEN environment variable not set")
	}

	// Build final config
	config := &Config{
		Enabled:            fileConfig.Enabled,
		MCPEndpoint:        fileConfig.Analysis.MCPEndpoint,
		MCPToken:           mcpToken,
		MonitoredChannels:  fileConfig.MonitoredChannels,
		AnalysisTimeout:    time.Duration(fileConfig.Analysis.Timeout) * time.Second,
		MaxAnalysesPerHour: fileConfig.RateLimiting.MaxAnalysesPerHour,
		PromptTemplate:     fileConfig.Analysis.PromptTemplate,
	}

	// Validate configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration: %w", err)
	}

	return config, nil
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if !c.Enabled {
		return fmt.Errorf("chaibot is not enabled")
	}

	if c.MCPEndpoint == "" {
		return fmt.Errorf("MCP endpoint not configured")
	}

	if c.MCPToken == "" {
		return fmt.Errorf("MCP token not provided")
	}

	if len(c.MonitoredChannels) == 0 {
		return fmt.Errorf("no monitored channels configured")
	}

	if c.AnalysisTimeout == 0 {
		c.AnalysisTimeout = 120 * time.Second // Default
	}

	if c.MaxAnalysesPerHour == 0 {
		c.MaxAnalysesPerHour = 100 // Default
	}

	return nil
}
