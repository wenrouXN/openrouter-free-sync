package main

import (
	"testing"
	"time"
)

func TestAutoAlias(t *testing.T) {
	cases := map[string]string{
		"dots-studio/dots-3-note-preview:free": "dots-3-note-preview",
		"minimax/minimax-m3:free":              "minimax-m3",
		"nvidia/nemotron-3.5-lightning:free":   "nemotron-3.5-lightning",
		"nousresearch/hermes-agent:free":       "hermes-agent",
	}
	for id, want := range cases {
		if got := autoAlias(id); got != want {
			t.Errorf("autoAlias(%q) = %q, want %q", id, got, want)
		}
	}
}

func orModelForTest(id string, ctx int) orModel {
	return orModel{
		ID:              id,
		Name:            id,
		ContextLength:   ctx,
		Architecture:    orArchitecture{Modality: "text->text", OutputModalities: []string{"text"}},
		Pricing:         orPricing{Prompt: "0", Completion: "0"},
		SupportedParams: []string{"tools"},
	}
}

func TestFilterModelsAliasCollision(t *testing.T) {
	models := []orModel{
		orModelForTest("vendor-a/model-x:free", 1000000),
		orModelForTest("vendor-b/model-x:free", 1000000),
		orModelForTest("vendor-c/unique:free", 1000000),
	}
	cfg := defaultConfig()
	cfg.AutoAlias = true
	out := filterModels(models, cfg)
	if len(out) != 3 {
		t.Fatalf("expected 3 models, got %d", len(out))
	}
	aliases := map[string]string{}
	for _, e := range out {
		aliases[e.Name] = e.Alias
	}
	if aliases["vendor-a/model-x:free"] != "vendor-a/model-x:free" {
		t.Errorf("colliding alias must fall back to full ID, got %q", aliases["vendor-a/model-x:free"])
	}
	if aliases["vendor-b/model-x:free"] != "vendor-b/model-x:free" {
		t.Errorf("colliding alias must fall back to full ID, got %q", aliases["vendor-b/model-x:free"])
	}
	if aliases["vendor-c/unique:free"] != "unique" {
		t.Errorf("non-colliding model should keep short alias, got %q", aliases["vendor-c/unique:free"])
	}
}

func TestFilterModelsContextAndPricing(t *testing.T) {
	paid := orModelForTest("c/paid:free", 1000000)
	paid.Pricing = orPricing{Prompt: "0.1", Completion: "0"}
	models := []orModel{
		orModelForTest("a/ok:free", 512000),
		orModelForTest("b/small:free", 256000),
		paid,
	}
	cfg := defaultConfig()
	out := filterModels(models, cfg)
	if len(out) != 1 || out[0].Name != "a/ok:free" {
		t.Fatalf("expected only a/ok:free to pass, got %+v", out)
	}
}

func TestMaskSecret(t *testing.T) {
	if got := maskSecret(""); got != "" {
		t.Errorf("empty should stay empty, got %q", got)
	}
	if got := maskSecret("short"); got != "****" {
		t.Errorf("short secret should be ****, got %q", got)
	}
	secret := "sk-or-v1-abcdefgh12345678"
	got := maskSecret(secret)
	if got == secret {
		t.Error("secret not masked")
	}
	if len(got) >= len(secret) {
		t.Errorf("masked (%q) should be shorter than input", got)
	}
}

func TestPruneInactiveRecords(t *testing.T) {
	stateEnsure("")
	stateMu.Lock()
	st.Models = map[string]*ModelRecord{
		"old/dead:free":    {ID: "old/dead:free", Active: false, RemovedAt: time.Now().AddDate(0, 0, -40).Format(time.RFC3339)},
		"new/dead:free":    {ID: "new/dead:free", Active: false, RemovedAt: time.Now().AddDate(0, 0, -5).Format(time.RFC3339)},
		"legacy/dead:free": {ID: "legacy/dead:free", Active: false, RemovedAt: ""},
		"alive/ok:free":    {ID: "alive/ok:free", Active: true},
	}
	st.AuditLog = []AuditEvent{}
	cfg := defaultConfig()
	cfg.PruneAfterDays = 30
	res := &SyncResult{}
	pruneInactiveRecords(cfg, res)
	_, oldExists := st.Models["old/dead:free"]
	_, newExists := st.Models["new/dead:free"]
	_, legacyExists := st.Models["legacy/dead:free"]
	_, aliveExists := st.Models["alive/ok:free"]
	stateMu.Unlock()

	if oldExists {
		t.Error("40-day-old inactive record should be pruned")
	}
	if !newExists {
		t.Error("5-day-old inactive record should survive")
	}
	if !legacyExists {
		t.Error("legacy record without RemovedAt should be stamped first, not pruned")
	}
	if !aliveExists {
		t.Error("active record must never be pruned")
	}
	if res.Pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", res.Pruned)
	}
}

func TestAuditCap(t *testing.T) {
	stateEnsure("")
	stateMu.Lock()
	st.AuditLog = []AuditEvent{}
	cfgMu.Lock()
	saved := cfg
	cfg.AuditMaxEntries = 10
	cfgMu.Unlock()
	for i := 0; i < 25; i++ {
		auditAddLocked("sync", "n", "d")
	}
	n := len(st.AuditLog)
	cfgMu.Lock()
	cfg = saved
	cfgMu.Unlock()
	stateMu.Unlock()
	if n != 10 {
		t.Errorf("audit log should be capped at 10, got %d", n)
	}
}

func TestApplyConfigFieldsMaskedEcho(t *testing.T) {
	c := defaultConfig()
	c.OpenRouterAPIKey = "sk-or-v1-realkey123456"
	incoming := map[string]interface{}{
		"openrouter_api_key": maskSecret(c.OpenRouterAPIKey), // panel echoes masked value back
		"min_context_length": float64(1000000),
	}
	changed := applyConfigFields(&c, incoming)
	if c.OpenRouterAPIKey != "sk-or-v1-realkey123456" {
		t.Error("masked echo must not overwrite the real key")
	}
	if c.MinContextLength != 1000000 {
		t.Errorf("min_context_length should be applied, got %d", c.MinContextLength)
	}
	for _, k := range changed {
		if k == "openrouter_api_key" {
			t.Error("masked echo must not be reported as a change")
		}
	}
}

func TestOverlayExcludesSecrets(t *testing.T) {
	existing := map[string]interface{}{"min_context_length": 512000}
	incoming := map[string]interface{}{
		"min_context_length": 1000000,
		"openrouter_api_key": "sk-or-v1-NEWKEY",
		"management_key":     "newkey",
		"state_path":         "/tmp/x.json",
	}
	ov := overlayFromIncoming(existing, incoming)
	for _, k := range []string{"openrouter_api_key", "management_key", "state_path"} {
		if _, ok := ov[k]; ok {
			t.Errorf("%s must not be persisted into overlay", k)
		}
	}
	if ov["min_context_length"] != 1000000 {
		t.Error("non-secret fields should merge into overlay")
	}
}
