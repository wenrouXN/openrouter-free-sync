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
	ID              string         `json:"id"`
	Name            string         `json:"name"`
	ContextLength   int            `json:"context_length"`
	Architecture    orArchitecture `json:"architecture"`
	Pricing         orPricing      `json:"pricing"`
	SupportedParams []string       `json:"supported_parameters"`
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

type cpaAPIKey struct {
	APIKey   string `json:"api-key"`
	ProxyURL string `json:"proxy-url,omitempty"`
}

type cpaProvider struct {
	Name          string            `json:"name"`
	Disabled      bool              `json:"disabled,omitempty"`
	BaseURL       string            `json:"base-url"`
	APIKeyEntries []cpaAPIKey       `json:"api-key-entries,omitempty"`
	Models        []cpaModelEntry   `json:"models"`
	Headers       map[string]string `json:"headers,omitempty"`
}

type cpaCompatResponse []cpaProvider

// SyncResult summarizes one sync cycle (auditable counters).
type SyncResult struct {
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	SyncedAt    string `json:"synced_at"`
	ModelCount  int    `json:"model_count"`
	Added       int    `json:"added"`
	Removed     int    `json:"removed"`
	Quarantined int    `json:"quarantined"`
	Recovered   int    `json:"recovered"`
	Probed      int    `json:"probed"`
}

var (
	syncMu   sync.Mutex
	lastSync SyncResult
)

// syncGate serializes sync cycles; concurrent triggers are skipped.
var syncGate sync.Mutex

func runSyncSerialized(cfg PluginConfig) SyncResult {
	if !syncGate.TryLock() {
		hostLog("info", "openrouter-free-sync: sync already in progress, skipping")
		return getLastSync()
	}
	defer syncGate.Unlock()
	return runSync(cfg)
}

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
		result = append(result, cpaModelEntry{Name: m.ID, Alias: m.ID})
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

// probeModel sends a minimal chat completion to check model availability.
func probeModel(cfg PluginConfig, modelID string) (ok bool, status int, latencyMs int64, errMsg string) {
	start := time.Now()
	payload, _ := json.Marshal(map[string]interface{}{
		"model":      modelID,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 1,
	})
	headers := map[string][]string{
		"Content-Type": {"application/json"},
	}
	if cfg.OpenRouterAPIKey != "" {
		headers["Authorization"] = []string{"Bearer " + cfg.OpenRouterAPIKey}
	}
	resp, err := hostHTTPDo(httpDoRequest{
		Method:  "POST",
		URL:     cfg.OpenRouterBaseURL + "/chat/completions",
		Headers: headers,
		Body:    payload,
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		return false, 0, latency, err.Error()
	}
	if resp.StatusCode == 200 {
		return true, 200, latency, ""
	}
	detail := string(resp.Body)
	if len(detail) > 200 {
		detail = detail[:200]
	}
	return false, resp.StatusCode, latency, fmt.Sprintf("HTTP %d: %s", resp.StatusCode, detail)
}

// patchCPAProvider updates the openai-compatibility provider's models in CPA.
func patchCPAProvider(cfg PluginConfig, models []cpaModelEntry) error {
	mgmtKey := cfg.effectiveManagementKey()
	if mgmtKey == "" {
		return fmt.Errorf("no management key configured")
	}
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

	idx := -1
	for i := range providers {
		if providers[i].Name == cfg.ProviderName {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("provider %q not found in openai-compatibility config", cfg.ProviderName)
	}

	providers[idx].Models = models

	patchBody, _ := json.Marshal(map[string]interface{}{
		"index": idx,
		"value": providers[idx],
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

// runSync executes a full sync cycle: fetch → filter → probe → audit → patch.
func runSync(cfg PluginConfig) SyncResult {
	stateEnsure(cfg.StatePath)
	result := SyncResult{SyncedAt: time.Now().Format(time.RFC3339)}
	hostLog("info", "openrouter-free-sync: starting sync")

	models, err := fetchOpenRouterModels(cfg)
	if err != nil {
		result.Error = err.Error()
		hostLog("error", "openrouter-free-sync: fetch failed: "+err.Error())
		finishSync(result)
		return result
	}

	filtered := filterModels(models, cfg)
	orByID := map[string]orModel{}
	for _, m := range models {
		orByID[m.ID] = m
	}
	desired := map[string]bool{}
	for _, m := range filtered {
		desired[m.Name] = true
	}

	// Phase 1: update state (add/remove records) under lock
	stateMu.Lock()
	now := time.Now().Format(time.RFC3339)
	for _, m := range filtered {
		rec, exists := st.Models[m.Name]
		if !exists {
			om := orByID[m.Name]
			st.Models[m.Name] = &ModelRecord{
				ID:            m.Name,
				DisplayName:   om.Name,
				ContextLength: om.ContextLength,
				Modality:      om.Architecture.Modality,
				InputPrice:    om.Pricing.Prompt,
				OutputPrice:   om.Pricing.Completion,
				Tools:         hasToolsSupport(om.SupportedParams),
				Active:        true,
				AddedAt:       now,
			}
			auditAddLocked("model_added", m.Name,
				fmt.Sprintf("ctx=%d modality=%s price=%s/%s", om.ContextLength, om.Architecture.Modality, om.Pricing.Prompt, om.Pricing.Completion))
			result.Added++
		} else {
			om := orByID[m.Name]
			rec.DisplayName = om.Name
			rec.ContextLength = om.ContextLength
			rec.Modality = om.Architecture.Modality
			rec.InputPrice = om.Pricing.Prompt
			rec.OutputPrice = om.Pricing.Completion
			rec.Tools = hasToolsSupport(om.SupportedParams)
			if !rec.Active {
				rec.Active = true
				auditAddLocked("model_readded", m.Name, "matches filters again")
				result.Added++
			}
		}
	}
	var probeTargets []string
	nowT := time.Now()
	for id, rec := range st.Models {
		if rec.Active && !desired[id] {
			rec.Active = false
			rec.QuarantineReason = ""
			rec.FailCount = 0
			auditAddLocked("model_removed", id, "no longer matches filters or left OpenRouter catalog")
			result.Removed++
		}
		if !rec.Active {
			continue
		}
		if rec.QuarantineReason == "" {
			probeTargets = append(probeTargets, id)
			continue
		}
		// Quarantined models are re-probed at most every 30 min to save quota.
		if last := rec.LastProbeAt; last != "" {
			if t, err := time.Parse(time.RFC3339, last); err == nil && nowT.Sub(t) < 30*time.Minute {
				continue
			}
		}
		probeTargets = append(probeTargets, id)
	}
	stateMu.Unlock()

	// Phase 2: probe availability WITHOUT holding the lock
	type probeRes struct {
		ok      bool
		status  int
		latency int64
		errMsg  string
	}
	probeResults := map[string]probeRes{}
	if cfg.AvailabilityCheck && cfg.OpenRouterAPIKey != "" {
		interval := time.Duration(cfg.ProbeIntervalMS) * time.Millisecond
		if interval <= 0 {
			interval = 4 * time.Second
		}
		for i, id := range probeTargets {
			if i > 0 {
				time.Sleep(interval) // stay under OpenRouter free-models-per-min limit
			}
			ok, status, latency, errMsg := probeModel(cfg, id)
			probeResults[id] = probeRes{ok, status, latency, errMsg}
			result.Probed++
		}
	}

	// Phase 3: apply probe results + build final list under lock
	stateMu.Lock()
	for _, id := range probeTargets {
		rec := st.Models[id]
		pr, probed := probeResults[id]
		if !probed {
			continue
		}
		rec.LastProbeAt = time.Now().Format(time.RFC3339)
		rec.LastProbeOK = pr.ok
		rec.LastProbeStatus = pr.status
		rec.LastProbeLatency = pr.latency
		rec.LastProbeError = pr.errMsg
		if pr.status == 401 || pr.status == 403 {
			// auth / agent-harness restriction, not an availability failure
			if rec.QuarantineReason != "" {
				rec.QuarantineReason = ""
				rec.FailCount = 0
				auditAddLocked("model_recovered", id, "401/403 restriction no longer counted as failure")
				result.Recovered++
			}
			continue
		}
		if pr.ok {
			if rec.QuarantineReason != "" {
				rec.QuarantineReason = ""
				auditAddLocked("model_recovered", id, "probe OK, restored to CPA provider")
				result.Recovered++
			}
			rec.FailCount = 0
			continue
		}
		rec.FailCount++
		if pr.status == 404 {
			if rec.QuarantineReason == "" {
				rec.QuarantineReason = "model not found (404)"
				auditAddLocked("model_quarantined", id, rec.QuarantineReason)
				result.Quarantined++
			}
			continue
		}
		if rec.FailCount >= cfg.failThreshold() && rec.QuarantineReason == "" {
			rec.QuarantineReason = fmt.Sprintf("%d consecutive probe failures (last: %s)", rec.FailCount, pr.errMsg)
			auditAddLocked("model_quarantined", id, rec.QuarantineReason)
			result.Quarantined++
		}
	}
	var final []cpaModelEntry
	for _, m := range filtered {
		if rec := st.Models[m.Name]; rec != nil && rec.QuarantineReason == "" {
			final = append(final, cpaModelEntry{Name: m.Name, Alias: m.Name})
		}
	}
	result.ModelCount = len(final)
	stateSaveLocked()
	stateMu.Unlock()

	// Phase 4: PATCH CPA
	if err := patchCPAProvider(cfg, final); err != nil {
		result.Error = err.Error()
		hostLog("error", "openrouter-free-sync: patch failed: "+err.Error())
		finishSync(result)
		return result
	}

	result.Success = true
	hostLog("info", fmt.Sprintf("openrouter-free-sync: synced %d models (added=%d removed=%d quarantined=%d recovered=%d probed=%d)",
		len(final), result.Added, result.Removed, result.Quarantined, result.Recovered, result.Probed))
	finishSync(result)
	return result
}

// finishSync records the sync outcome in state + audit log.
func finishSync(result SyncResult) {
	stateMu.Lock()
	if st != nil {
		st.LastSync = LastSyncMeta{
			Time:        result.SyncedAt,
			Success:     result.Success,
			Error:       result.Error,
			ActiveCount: result.ModelCount,
		}
		detail := fmt.Sprintf("models=%d added=%d removed=%d quarantined=%d recovered=%d probed=%d",
			result.ModelCount, result.Added, result.Removed, result.Quarantined, result.Recovered, result.Probed)
		if result.Error != "" {
			detail += " error=" + result.Error
		}
		auditAddLocked("sync", "", detail)
		stateSaveLocked()
	}
	stateMu.Unlock()

	syncMu.Lock()
	lastSync = result
	syncMu.Unlock()
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
