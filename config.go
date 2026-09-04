package main

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// PluginConfig holds all configuration for openrouter-free-sync.
type PluginConfig struct {
	RefreshInterval     string   `yaml:"refresh_interval"`
	MinContextLength    int      `yaml:"min_context_length"`
	PricingFilter       string   `yaml:"pricing_filter"`
	ExcludedProviders   []string `yaml:"excluded_providers"`
	RequireTextOutput   bool     `yaml:"require_text_output"`
	RequireToolsSupport bool     `yaml:"require_tools_support"`
	OpenRouterAPIKey    string   `yaml:"openrouter_api_key"`
	OpenRouterBaseURL   string   `yaml:"openrouter_base_url"`
	ProviderName        string   `yaml:"provider_name"`
	ManagementKey       string   `yaml:"management_key"`
	CPABaseURL          string   `yaml:"cpa_base_url"`
	// v0.2.0: availability probing, audit log, state persistence
	AvailabilityCheck         bool   `yaml:"availability_check"`
	AvailabilityFailThreshold int    `yaml:"availability_fail_threshold"`
	ProbeIntervalMS           int    `yaml:"probe_interval_ms"`
	AuditMaxEntries           int    `yaml:"audit_max_entries"`
	StatePath                 string `yaml:"state_path"`
}

func defaultConfig() PluginConfig {
	return PluginConfig{
		RefreshInterval:           "24h",
		MinContextLength:          512000,
		PricingFilter:             "free",
		ExcludedProviders:         []string{"openai/", "anthropic/", "google/"},
		RequireTextOutput:         true,
		RequireToolsSupport:       false,
		OpenRouterBaseURL:         "https://openrouter.ai/api/v1",
		ProviderName:              "openrouter",
		CPABaseURL:                "http://localhost:8317",
		AvailabilityCheck:         true,
		AvailabilityFailThreshold: 3,
		ProbeIntervalMS:           4000,
		AuditMaxEntries:           500,
	}
}

func parseConfig(yamlBytes []byte) (PluginConfig, error) {
	cfg := defaultConfig()
	if len(yamlBytes) > 0 {
		if err := yaml.Unmarshal(yamlBytes, &cfg); err != nil {
			return cfg, err
		}
	}
	return cfg, nil
}

func (c PluginConfig) RefreshDuration() time.Duration {
	d, err := time.ParseDuration(c.RefreshInterval)
	if err != nil || d == 0 {
		return 24 * time.Hour
	}
	return d
}

func (c PluginConfig) effectiveManagementKey() string {
	if c.ManagementKey != "" {
		return c.ManagementKey
	}
	return os.Getenv("MANAGEMENT_PASSWORD")
}

func (c PluginConfig) failThreshold() int {
	if c.AvailabilityFailThreshold < 1 {
		return 1
	}
	return c.AvailabilityFailThreshold
}

// configFields returns CPA ConfigFields for web rendering.
func configFields() []map[string]interface{} {
	return []map[string]interface{}{
		{"Name": "refresh_interval", "Type": "string", "Description": "Auto-sync interval (e.g. 24h, 6h, 1h, 30m)"},
		{"Name": "min_context_length", "Type": "integer", "Description": "Minimum context length filter"},
		{"Name": "pricing_filter", "Type": "enum", "EnumValues": []string{"free", "any"}, "Description": "free = $0 input + $0 output; any = include all"},
		{"Name": "excluded_providers", "Type": "string", "Description": "Comma-separated provider prefixes to exclude"},
		{"Name": "require_text_output", "Type": "boolean", "Description": "Exclude models without text output modality"},
		{"Name": "require_tools_support", "Type": "boolean", "Description": "Exclude models without function-calling support"},
		{"Name": "openrouter_api_key", "Type": "string", "Description": "OpenRouter API key"},
		{"Name": "openrouter_base_url", "Type": "string", "Description": "OpenRouter API base URL"},
		{"Name": "provider_name", "Type": "string", "Description": "CPA openai-compatibility provider name to sync into"},
		{"Name": "management_key", "Type": "string", "Description": "CPA management API key (empty = MANAGEMENT_PASSWORD env)"},
		{"Name": "cpa_base_url", "Type": "string", "Description": "CPA base URL for management API"},
		{"Name": "availability_check", "Type": "boolean", "Description": "Probe each model with a 1-token request each sync; quarantine after N consecutive failures"},
		{"Name": "availability_fail_threshold", "Type": "integer", "Description": "Consecutive probe failures before a model is quarantined (removed from CPA)"},
		{"Name": "probe_interval_ms", "Type": "integer", "Description": "Delay between per-model probes in ms (default 4000, keeps under OpenRouter 20/min free limit)"},
		{"Name": "audit_max_entries", "Type": "integer", "Description": "Max audit log entries kept"},
		{"Name": "state_path", "Type": "string", "Description": "Path to persist sync state + audit log (empty = memory only)"},
	}
}
