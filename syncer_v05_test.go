package main

import (
	"strings"
	"testing"
)

func v05Model(id string, ctx int, params []string, inModalities []string) orModel {
	m := orModelForTest(id, ctx)
	m.SupportedParams = params
	m.Architecture.InputModalities = inModalities
	return m
}

func TestFilterModelsBlacklistBeatsAll(t *testing.T) {
	models := []orModel{v05Model("a/good:free", 1000000, []string{"tools"}, []string{"text"})}
	cfg := defaultConfig()
	cfg.ExcludeModels = []string{"a/good:free"}
	if out := filterModels(models, cfg); len(out) != 0 {
		t.Fatalf("blacklisted model must be excluded even when it passes filters, got %v", out)
	}
}

func TestFilterModelsWhitelistBypassesFilters(t *testing.T) {
	small := v05Model("a/small:free", 256000, []string{"tools"}, []string{"text"}) // fails 512K filter
	models := []orModel{small}
	cfg := defaultConfig()
	cfg.ForceInclude = []string{"a/small:free"}
	out := filterModels(models, cfg)
	if len(out) != 1 || out[0].Name != "a/small:free" {
		t.Fatalf("whitelist must bypass filters, got %v", out)
	}
	// blacklist still wins over whitelist
	cfg.ExcludeModels = []string{"a/small:free"}
	if out := filterModels(models, cfg); len(out) != 0 {
		t.Fatalf("blacklist must beat whitelist, got %v", out)
	}
}

func TestFilterModelsRequireInputModality(t *testing.T) {
	text := v05Model("a/text-only:free", 1000000, []string{"tools"}, []string{"text"})
	vision := v05Model("a/vision:free", 1000000, []string{"tools"}, []string{"text", "image"})
	cfg := defaultConfig()
	cfg.RequireInputModality = "image"
	out := filterModels([]orModel{text, vision}, cfg)
	if len(out) != 1 || out[0].Name != "a/vision:free" {
		t.Fatalf("expected only vision model, got %v", out)
	}
}

func TestFilterModelsRequireParams(t *testing.T) {
	a := v05Model("a/m1:free", 1000000, []string{"tools", "structured_outputs"}, []string{"text"})
	b := v05Model("a/m2:free", 1000000, []string{"tools"}, []string{"text"})
	cfg := defaultConfig()
	cfg.RequireParams = []string{"structured_outputs"}
	out := filterModels([]orModel{a, b}, cfg)
	if len(out) != 1 || out[0].Name != "a/m1:free" {
		t.Fatalf("expected only model with structured_outputs, got %v", out)
	}
}

func TestAliasPrefixAndOverrides(t *testing.T) {
	models := []orModel{
		v05Model("dots-studio/dots-3-note-preview:free", 512000, []string{"tools"}, []string{"text"}),
		v05Model("minimax/minimax-m3:free", 1000000, []string{"tools"}, []string{"text"}),
	}
	cfg := defaultConfig()
	cfg.AutoAlias = true
	cfg.AliasPrefix = "or-"
	cfg.AliasOverrides = "minimax/minimax-m3:free=mm3"

	entries := filterModels(models, cfg)
	got := map[string]string{}
	for _, e := range entries {
		got[e.Name] = e.Alias
	}
	if got["dots-studio/dots-3-note-preview:free"] != "or-dots-3-note-preview" {
		t.Errorf("prefix alias = %q, want or-dots-3-note-preview", got["dots-studio/dots-3-note-preview:free"])
	}
	if got["minimax/minimax-m3:free"] != "mm3" {
		t.Errorf("manual override = %q, want mm3", got["minimax/minimax-m3:free"])
	}
}

func TestAppendProbeHistoryRing(t *testing.T) {
	rec := &ModelRecord{ID: "x/y:free"}
	for i := 0; i < 30; i++ {
		appendProbeHistory(rec, ProbeEntry{Time: "t", OK: i%2 == 0, Status: 200, LatencyMS: int64(i)}, 20)
	}
	if len(rec.ProbeHistory) != 20 {
		t.Fatalf("history should cap at 20, got %d", len(rec.ProbeHistory))
	}
	if rec.ProbeHistory[0].LatencyMS != 10 {
		t.Errorf("oldest kept entry should be #10, got %d", rec.ProbeHistory[0].LatencyMS)
	}
	if rec.ProbeHistory[19].LatencyMS != 29 {
		t.Errorf("newest entry should be #29, got %d", rec.ProbeHistory[19].LatencyMS)
	}
}

func TestCSVEscape(t *testing.T) {
	if got := csvEscape("plain"); got != "plain" {
		t.Errorf("plain = %q", got)
	}
	if got := csvEscape("a,b"); got != `"a,b"` {
		t.Errorf("comma = %q", got)
	}
	if got := csvEscape(`say "hi"`); got != `"say ""hi"""` {
		t.Errorf("quote = %q", got)
	}
	if got := csvEscape("line\nbreak"); !strings.HasPrefix(got, `"line`) {
		t.Errorf("newline = %q", got)
	}
}

func TestParseAliasOverrides(t *testing.T) {
	m := parseAliasOverrides("a/b:free=short, c/d:free = shorter ,bad,=x,")
	if m["a/b:free"] != "short" {
		t.Errorf("a/b:free = %q", m["a/b:free"])
	}
	if m["c/d:free"] != "shorter" {
		t.Errorf("c/d:free = %q (spaces should be trimmed)", m["c/d:free"])
	}
	if len(m) != 2 {
		t.Errorf("malformed pairs must be dropped, got %v", m)
	}
}
