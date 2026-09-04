package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// OpenRouter model catalog types
type orModel struct {
	ID               string            `json:"id"`
	Name             string            `json:"name"`
	ContextLength    int               `json:"context_length"`
	Architecture     orArchitecture   `json:"architecture"`
	Pricing          orPricing         `json:"pricing"`
	SupportedParams  []string          `json:"supported_parameters"`
}

type orArchitecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

type orPricing struct {
	Prompt     string `json:"prompt"`
	Completion string `json:"completion"`
}

type orModelsResponse struct {
	Data []orModel `json:"data"`
}

// CPA openai-compatibility types
type cpaModelEntry struct {
	Name  string `json:"name"`
	Alias string `json:"alias,omitempty"`
}

type cpaProvider struct {
	Name          string         `json:"name"`
	Disabled      bool           `json:"disabled,omitempty"`
	BaseURL       string         `json:"base-url"`
	APIKeyEntries []cpaAPIKey    `json:"api-key-entries,omitempty"`
	Models        []cpaModelEntry `json:"models"`
	Headers       map[string]string `json:"headers,omitempty"`
}

type cpaAPIKey struct {
	APIKey   string `json:"api-key"`
	ProxyURL string `json:"proxy-url,omitempty"`
}

type cpaCompatResponse []cpaProvider

// SyncResult holds the outcome of a sync operation.
type SyncResult struct {
	Success    bool         `json:"success"`
	Error      string       `json:"error,omitempty"`
	SyncedAt   time.Time    `json:"synced_at"`
	ModelCount int          `json:"model_count"`
	Models     []cpaModelEntry `json:"models"`
}

var (
	syncMu      sync.Mutex
	lastSync    SyncResult
)

// isFree checks if a model has $0 prompt + $0 completion pricing.
func isFree(p orPricing) bool {
	return p.Prompt == "0" && p.Completion == "0"
}

// hasTextOutput checks if model supports text output.
func hasTextOutput(a orArchitecture) bool {
	for _, m := range a.OutputModalities {
		if m == "text" {
			return true
		}
	}
	return false
}

// hasToolsSupport checks if model supports function calling.
func hasToolsSupport(params []string) bool {
	for _, p := range params {
		if p == "tools" {
			return true
		}
	}
	return false
}

// isExcluded checks if a model ID starts with any excluded prefix.
func isExcluded(id string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// filterModels applies config filters to the OpenRouter model list.
func filterModels(models []orModel, cfg PluginConfig) []cpaModelEntry {
	var result []cpaModelEntry
	for _, m := range models {
		if m.ContextLength < cfg.MinContextLength {
			continue
		}
		if cfg.PricingFilter == "free" && !isFree(m.Pricing) {
			continue
		}
		if isExcluded(m.ID, cfg.ExcludedProviders) {
			continue
		}
		if cfg.RequireTextOutput && !hasTextOutput(m.Architecture) {
			continue
		}
		if cfg.RequireToolsSupport && !hasToolsSupport(m.SupportedParams) {
			continue
		}
		result = append(result, cpaModelEntry{
			Name:  m.ID,
			Alias: m.ID,
		})
	}
	return result
}

// fetchOpenRouterModels retrieves the model catalog from OpenRouter.
func fetchOpenRouterModels(cfg PluginConfig) ([]orModel, error) {
	headers := map[string][]string{
		"Content-Type": {"application/json"},
	}
	if cfg.OpenRouterAPIKey != "" {
		headers["Authorization"] = []string{"Bearer " + cfg.OpenRouterAPIKey}
	}

	resp, err := hostHTTPDo(httpDoRequest{
		Method:  "GET",
		URL:     cfg.OpenRouterBaseURL + "/models",
		Headers: headers,
	})
	if err != nil {
		return nil, fmt.Errorf("host.http.do: %w", err)
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openrouter returned %d", resp.StatusCode)
	}

	var orResp orModelsResponse
	if err := json.Unmarshal(resp.Body, &orResp); err != nil {
		return nil, fmt.Errorf("parse models: %w", err)
	}
	return orResp.Data, nil
}

// patchCPAProvider updates the openai-compatibility provider's models in CPA.
func patchCPAProvider(cfg PluginConfig, models []cpaModelEntry) error {
	mgmtKey := cfg.effectiveManagementKey()
	if mgmtKey == "" {
		return fmt.Errorf("no management key configured")
	}

	// GET current providers to preserve existing fields
	resp, err := hostHTTPDo(httpDoRequest{
		Method: "GET",
		URL:    cfg.CPABaseURL + "/v0/management/openai-compatibility",
		Headers: map[string][]string{
			"Authorization": {"Bearer " + mgmtKey},
		},
	})
	if err != nil {
		return fmt.Errorf("get providers: %w", err)
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("get providers returned %d", resp.StatusCode)
	}

	var wrapper struct {
		OpenAICompatibility cpaCompatResponse `json:"openai-compatibility"`
	}
	if err := json.Unmarshal(resp.Body, &wrapper); err != nil {
		return fmt.Errorf("parse providers: %w", err)
	}
	providers := wrapper.OpenAICompatibility

	// Find the target provider
	var idx int = -1
	var existing *cpaProvider
	for i, p := range providers {
		if p.Name == cfg.ProviderName {
			idx = i
			existing = &providers[i]
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("provider %q not found in openai-compatibility config", cfg.ProviderName)
	}

	// Update only the models array, preserve everything else
	existing.Models = models

	// PATCH back
	patchBody, _ := json.Marshal(map[string]interface{}{
		"index": idx,
		"value": *existing,
	})

	patchResp, err := hostHTTPDo(httpDoRequest{
		Method: "PATCH",
		URL:    cfg.CPABaseURL + "/v0/management/openai-compatibility",
		Headers: map[string][]string{
			"Authorization": {"Bearer " + mgmtKey},
			"Content-Type":  {"application/json"},
		},
		Body: patchBody,
	})
	if err != nil {
		return fmt.Errorf("patch providers: %w", err)
	}
	if patchResp.StatusCode != 200 {
		return fmt.Errorf("patch returned %d: %s", patchResp.StatusCode, string(patchResp.Body))
	}

	return nil
}

// runSync executes a full sync cycle: fetch → filter → patch.
func runSync(cfg PluginConfig) SyncResult {
	result := SyncResult{SyncedAt: time.Now()}

	hostLog("info", "openrouter-free-sync: starting sync")

	models, err := fetchOpenRouterModels(cfg)
	if err != nil {
		result.Error = err.Error()
		hostLog("error", "openrouter-free-sync: fetch failed: "+err.Error())
		return result
	}

	filtered := filterModels(models, cfg)
	result.Models = filtered
	result.ModelCount = len(filtered)

	if err := patchCPAProvider(cfg, filtered); err != nil {
		result.Error = err.Error()
		hostLog("error", "openrouter-free-sync: patch failed: "+err.Error())
		return result
	}

	result.Success = true
	hostLog("info", fmt.Sprintf("openrouter-free-sync: synced %d models", len(filtered)))
	return result
}

// getLastSync returns the last sync result (thread-safe).
func getLastSync() SyncResult {
	syncMu.Lock()
	defer syncMu.Unlock()
	return lastSync
}

// setLastSync sets the last sync result (thread-safe).
func setLastSync(r SyncResult) {
	syncMu.Lock()
	defer syncMu.Unlock()
	lastSync = r
}
