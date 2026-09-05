package main

import (
	"os"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
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
	AutoAlias           bool     `yaml:"auto_alias"`
	// v0.2.0: availability probing, audit log, state persistence
	AvailabilityCheck         bool   `yaml:"availability_check"`
	AvailabilityFailThreshold int    `yaml:"availability_fail_threshold"`
	ProbeIntervalMS           int    `yaml:"probe_interval_ms"`
	AuditMaxEntries           int    `yaml:"audit_max_entries"`
	StatePath                 string `yaml:"state_path"`
	// v0.4.0: data-hygiene + audit constraints
	AuditSyncAlways  bool `yaml:"audit_sync_always"`  // audit every sync even when nothing changed
	PruneAfterDays   int  `yaml:"prune_after_days"`   // delete inactive model records after N days (0 = never)
	ProbeCooldownMin int  `yaml:"probe_cooldown_min"` // skip re-probing healthy models within N minutes
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
		AutoAlias:                 true,
		AvailabilityCheck:         true,
		AvailabilityFailThreshold: 3,
		ProbeIntervalMS:           4000,
		AuditMaxEntries:           500,
		AuditSyncAlways:           false,
		PruneAfterDays:            30,
		ProbeCooldownMin:          10,
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

// parseSchedule detects whether refresh_interval is a cron expression
// (contains whitespace) or a Go duration. Returns (schedule, expr, isCron).
func parseSchedule(s string) (cron.Schedule, string, bool) {
	s = strings.TrimSpace(s)
	if strings.ContainsAny(s, " \t") {
		if sched, err := cron.ParseStandard(s); err == nil {
			return sched, s, true
		}
		hostLog("warn", "openrouter-free-sync: invalid cron expr '"+s+"', falling back to interval mode")
	}
	return nil, s, false
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
		{"Name": "refresh_interval", "Type": "string", "Description": "Go duration (24h) or cron expr — '0 9 * * 1' = every Monday 09:00, container TZ"},
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
		{"Name": "auto_alias", "Type": "boolean", "Description": "Generate short aliases: vendor/model:free → model (default on)"},
		{"Name": "availability_check", "Type": "boolean", "Description": "Probe each model with a 1-token request each sync; quarantine after N consecutive failures"},
		{"Name": "availability_fail_threshold", "Type": "integer", "Description": "Consecutive probe failures before a model is quarantined (removed from CPA)"},
		{"Name": "probe_interval_ms", "Type": "integer", "Description": "Delay between per-model probes in ms (default 4000, keeps under OpenRouter 20/min free limit)"},
		{"Name": "audit_max_entries", "Type": "integer", "Description": "Max audit log entries kept (hard cap, oldest dropped)"},
		{"Name": "state_path", "Type": "string", "Description": "Path to persist state (config.yaml only; not editable from panel overlay)"},
		{"Name": "audit_sync_always", "Type": "boolean", "Description": "Audit every sync even with no changes (default off: only changes/errors)"},
		{"Name": "prune_after_days", "Type": "integer", "Description": "Delete inactive model records from state after N days (0 = never)"},
		{"Name": "probe_cooldown_min", "Type": "integer", "Description": "Skip re-probing healthy models within N minutes of last probe"},
	}
}

// --- v0.4.0: secret masking, config overlay, field application ---

// maskSecret renders a safe-for-display representation of a secret.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 10 {
		return "****"
	}
	return s[:6] + "…" + s[len(s)-4:]
}

// overlayExcluded fields are never persisted into the config overlay:
// secrets stay YAML-only, and changing state_path via the panel would
// orphan the overlay file itself.
var overlayExcluded = map[string]bool{
	"openrouter_api_key": true,
	"management_key":     true,
	"state_path":         true,
}

// applyConfigFields applies present keys from incoming onto c in place.
// Secret fields are skipped when the value is empty or the masked echo of
// the current one. Returns the list of fields that actually changed.
func applyConfigFields(c *PluginConfig, incoming map[string]interface{}) []string {
	var changed []string

	if v, ok := incoming["refresh_interval"].(string); ok && v != c.RefreshInterval {
		c.RefreshInterval = v
		changed = append(changed, "refresh_interval")
	}
	if v, ok := incoming["min_context_length"].(float64); ok && int(v) != c.MinContextLength {
		c.MinContextLength = int(v)
		changed = append(changed, "min_context_length")
	}
	if v, ok := incoming["pricing_filter"].(string); ok && v != c.PricingFilter {
		c.PricingFilter = v
		changed = append(changed, "pricing_filter")
	}
	if v, ok := incoming["excluded_providers"].(string); ok {
		list := splitAndTrim(v, ",")
		if strings.Join(list, ",") != strings.Join(c.ExcludedProviders, ",") {
			c.ExcludedProviders = list
			changed = append(changed, "excluded_providers")
		}
	} else if v, ok := incoming["excluded_providers"].([]interface{}); ok {
		list := toStringSlice(v)
		if strings.Join(list, ",") != strings.Join(c.ExcludedProviders, ",") {
			c.ExcludedProviders = list
			changed = append(changed, "excluded_providers")
		}
	}
	if v, ok := incoming["require_text_output"].(bool); ok && v != c.RequireTextOutput {
		c.RequireTextOutput = v
		changed = append(changed, "require_text_output")
	}
	if v, ok := incoming["require_tools_support"].(bool); ok && v != c.RequireToolsSupport {
		c.RequireToolsSupport = v
		changed = append(changed, "require_tools_support")
	}
	if v, ok := incoming["openrouter_base_url"].(string); ok && v != c.OpenRouterBaseURL {
		c.OpenRouterBaseURL = v
		changed = append(changed, "openrouter_base_url")
	}
	if v, ok := incoming["provider_name"].(string); ok && v != c.ProviderName {
		c.ProviderName = v
		changed = append(changed, "provider_name")
	}
	if v, ok := incoming["cpa_base_url"].(string); ok && v != c.CPABaseURL {
		c.CPABaseURL = v
		changed = append(changed, "cpa_base_url")
	}
	if v, ok := incoming["auto_alias"].(bool); ok && v != c.AutoAlias {
		c.AutoAlias = v
		changed = append(changed, "auto_alias")
	}
	if v, ok := incoming["availability_check"].(bool); ok && v != c.AvailabilityCheck {
		c.AvailabilityCheck = v
		changed = append(changed, "availability_check")
	}
	if v, ok := incoming["availability_fail_threshold"].(float64); ok && int(v) != c.AvailabilityFailThreshold {
		c.AvailabilityFailThreshold = int(v)
		changed = append(changed, "availability_fail_threshold")
	}
	if v, ok := incoming["probe_interval_ms"].(float64); ok && int(v) != c.ProbeIntervalMS {
		c.ProbeIntervalMS = int(v)
		changed = append(changed, "probe_interval_ms")
	}
	if v, ok := incoming["probe_cooldown_min"].(float64); ok && int(v) != c.ProbeCooldownMin {
		c.ProbeCooldownMin = int(v)
		changed = append(changed, "probe_cooldown_min")
	}
	if v, ok := incoming["audit_sync_always"].(bool); ok && v != c.AuditSyncAlways {
		c.AuditSyncAlways = v
		changed = append(changed, "audit_sync_always")
	}
	if v, ok := incoming["audit_max_entries"].(float64); ok && int(v) != c.AuditMaxEntries {
		c.AuditMaxEntries = int(v)
		changed = append(changed, "audit_max_entries")
	}
	if v, ok := incoming["prune_after_days"].(float64); ok && int(v) != c.PruneAfterDays {
		c.PruneAfterDays = int(v)
		changed = append(changed, "prune_after_days")
	}
	if v, ok := incoming["state_path"].(string); ok && v != c.StatePath {
		c.StatePath = v
		changed = append(changed, "state_path")
	}

	// Secrets: skip empty values and masked echoes; apply genuine new values only.
	if v, ok := incoming["openrouter_api_key"].(string); ok && v != "" && v != maskSecret(c.OpenRouterAPIKey) && v != c.OpenRouterAPIKey {
		c.OpenRouterAPIKey = v
		changed = append(changed, "openrouter_api_key")
	}
	if v, ok := incoming["management_key"].(string); ok && v != "" && v != maskSecret(c.ManagementKey) && v != c.ManagementKey {
		c.ManagementKey = v
		changed = append(changed, "management_key")
	}
	return changed
}

// overlayFromIncoming merges a PUT payload onto the existing overlay,
// excluding secrets and state_path so they remain YAML-only.
func overlayFromIncoming(existing map[string]interface{}, incoming map[string]interface{}) map[string]interface{} {
	ov := map[string]interface{}{}
	for k, v := range existing {
		ov[k] = v
	}
	for k, v := range incoming {
		if overlayExcluded[k] {
			continue
		}
		ov[k] = v
	}
	return ov
}
