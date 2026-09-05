package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ModelRecord tracks one model across syncs, with metadata + availability.
type ModelRecord struct {
	ID               string `json:"id"`
	DisplayName      string `json:"display_name,omitempty"`
	ContextLength    int    `json:"context_length"`
	Modality         string `json:"modality,omitempty"`
	InputPrice       string `json:"input_price,omitempty"`
	OutputPrice      string `json:"output_price,omitempty"`
	Tools            bool   `json:"tools"`
	Active           bool   `json:"active"`
	AddedAt          string `json:"added_at,omitempty"`
	FailCount        int    `json:"fail_count"`
	LastProbeAt      string `json:"last_probe_at,omitempty"`
	LastProbeOK      bool   `json:"last_probe_ok"`
	LastProbeStatus  int    `json:"last_probe_status,omitempty"`
	LastProbeLatency int64  `json:"last_probe_latency_ms,omitempty"`
	LastProbeError   string `json:"last_probe_error,omitempty"`
	QuarantineReason string `json:"quarantine_reason,omitempty"`
	RemovedAt        string `json:"removed_at,omitempty"` // when the model left the desired set
	Restricted       bool   `json:"restricted"`            // 401/403 agent-harness restriction
	ProbeHistory     []ProbeEntry `json:"probe_history,omitempty"` // bounded ring of recent probes
}

// ProbeEntry is one availability probe outcome in a model's history.
type ProbeEntry struct {
	Time      string `json:"time"`
	OK        bool   `json:"ok"`
	Status    int    `json:"status"`
	LatencyMS int64  `json:"latency_ms"`
}

// AuditEvent is one auditable occurrence (model added/removed, sync result, config change).
type AuditEvent struct {
	Time   string `json:"time"`
	Type   string `json:"type"`
	Model  string `json:"model,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// LastSyncMeta summarizes the most recent sync.
type LastSyncMeta struct {
	Time        string `json:"time"`
	Success     bool   `json:"success"`
	Error       string `json:"error,omitempty"`
	ActiveCount int    `json:"active_count"`
}

// SyncState is the persisted plugin state.
type SyncState struct {
	SchemaVersion int                     `json:"schema_version"`
	Models        map[string]*ModelRecord `json:"models"`
	AuditLog      []AuditEvent            `json:"audit_log"`
	LastSync      LastSyncMeta            `json:"last_sync"`
	// ConfigOverlay holds panel-applied config changes (secrets excluded),
	// re-applied on top of config.yaml at plugin start so web edits
	// survive CPA restarts.
	ConfigOverlay map[string]interface{} `json:"config_overlay,omitempty"`
}

const stateSchemaVersion = 1

var (
	stateMu   sync.Mutex
	st        *SyncState
	statePath string
)

// stateEnsure loads state from disk once; reloads only if the path changed.
func stateEnsure(path string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	if st != nil && statePath == path {
		return
	}
	statePath = path
	fresh := &SyncState{SchemaVersion: stateSchemaVersion, Models: map[string]*ModelRecord{}, AuditLog: []AuditEvent{}}
	if path == "" {
		st = fresh
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		st = fresh
		return
	}
	var loaded SyncState
	if err := json.Unmarshal(data, &loaded); err != nil {
		hostLog("warn", "openrouter-free-sync: state load failed, starting fresh: "+err.Error())
		st = fresh
		return
	}
	if loaded.Models == nil {
		loaded.Models = map[string]*ModelRecord{}
	}
	if loaded.AuditLog == nil {
		loaded.AuditLog = []AuditEvent{}
	}
	if loaded.SchemaVersion == 0 {
		loaded.SchemaVersion = stateSchemaVersion // forward migration: stamp legacy files
	}
	st = &loaded
}

// stateSaveLocked persists state atomically. Caller must hold stateMu.
func stateSaveLocked() {
	if statePath == "" || st == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(statePath), 0o755); err != nil {
		hostLog("warn", "openrouter-free-sync: state dir create failed: "+err.Error())
		return
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return
	}
	tmp := statePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		hostLog("warn", "openrouter-free-sync: state save failed: "+err.Error())
		return
	}
	_ = os.Rename(tmp, statePath)
}

// auditAddLocked appends an audit event. Caller must hold stateMu.
func auditAddLocked(eventType, model, detail string) {
	if st == nil {
		return
	}
	st.AuditLog = append(st.AuditLog, AuditEvent{
		Time:   time.Now().Format(time.RFC3339),
		Type:   eventType,
		Model:  model,
		Detail: detail,
	})
	if maxA := cfg.AuditMaxEntries; maxA > 0 && len(st.AuditLog) > maxA {
		st.AuditLog = st.AuditLog[len(st.AuditLog)-maxA:]
	}
}

// appendProbeHistory appends one probe outcome, keeping at most max entries.
func appendProbeHistory(rec *ModelRecord, e ProbeEntry, max int) {
	if max <= 0 {
		max = 20
	}
	rec.ProbeHistory = append(rec.ProbeHistory, e)
	if len(rec.ProbeHistory) > max {
		rec.ProbeHistory = rec.ProbeHistory[len(rec.ProbeHistory)-max:]
	}
}
