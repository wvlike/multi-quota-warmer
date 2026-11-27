package main

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
)

func TestParsePluginConfigDefaultsAndLegacyMigration(t *testing.T) {
	cfg, err := parsePluginConfig([]byte(`
enabled: true
model: legacy-codex-model
prompt: legacy prompt
timezone: Asia/Shanghai
schedules:
  - id: morning
    at: "08:00"
    enabled: true
`))
	if err != nil {
		t.Fatalf("parsePluginConfig failed: %v", err)
	}
	if !cfg.Codex.Enabled || cfg.Codex.Model != "legacy-codex-model" || cfg.Codex.Prompt != "legacy prompt" {
		t.Fatalf("unexpected codex config after legacy migration: %+v", cfg.Codex)
	}
	if !cfg.Antigravity.Enabled || cfg.Antigravity.Model != defaultAntigravityModel || cfg.Antigravity.Prompt != defaultAntigravityPrompt {
		t.Fatalf("unexpected default antigravity config: %+v", cfg.Antigravity)
	}
}

func TestParsePluginConfigValidation(t *testing.T) {
	_, err := parsePluginConfig([]byte(`
enabled: true
antigravity:
  enabled: true
  model: ""
  prompt: ""
`))
	if err == nil || !strings.Contains(err.Error(), "antigravity.model") {
		t.Fatalf("expected antigravity.model error, got: %v", err)
	}

	cfg, errDisabled := parsePluginConfig([]byte(`
enabled: true
codex:
  enabled: false
  model: ""
  prompt: ""
antigravity:
  enabled: true
  model: gemini-3-flash
  prompt: warm up
`))
	if errDisabled != nil {
		t.Fatalf("disabled provider should not fail empty fields: %v", errDisabled)
	}
	if cfg.Codex.Enabled || cfg.Codex.Model != defaultCodexModel {
		t.Fatalf("expected disabled codex fallback model, got: %+v", cfg.Codex)
	}
}

func TestSelectProviderAuthsAndMultiProviderRun(t *testing.T) {
	w := newWarmer()
	w.config = defaultPluginConfig()
	w.config.Enabled = true
	w.config.StateFile = ""

	w.fetchAuths = func() ([]pluginapi.HostAuthFileEntry, error) {
		return []pluginapi.HostAuthFileEntry{
			{ID: "codex-1", Provider: "codex", Status: "active", Label: "c1@example.com"},
			{ID: "codex-disabled", Provider: "codex", Disabled: true},
			{ID: "ag-1", Provider: "antigravity", Status: "active", Label: "ag1@example.com"},
			{ID: "other-1", Provider: "gemini-cli", Status: "active"},
		}, nil
	}

	var called []string
	w.executeWarm = func(providerCfg providerWarmupConfig, provider string, auth pluginapi.HostAuthFileEntry) warmResult {
		called = append(called, provider+":"+auth.ID+":"+providerCfg.Model)
		return warmResult{
			Provider:   provider,
			AuthID:     auth.ID,
			Label:      auth.Label,
			Model:      providerCfg.Model,
			StatusCode: http.StatusOK,
			Success:    true,
		}
	}

	results, err := w.Run("manual", "all")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if len(called) != 2 || called[0] != "antigravity:ag-1:gemini-3-flash" || called[1] != "codex:codex-1:gpt-5.6-luna" {
		t.Fatalf("unexpected execution calls: %v", called)
	}

	agOnly, errAG := w.Run("manual", "antigravity")
	if errAG != nil || len(agOnly) != 1 || agOnly[0].Provider != "antigravity" {
		t.Fatalf("expected single antigravity result, got err=%v results=%+v", errAG, agOnly)
	}
}

func TestLogsPaginationAndFilter(t *testing.T) {
	w := newWarmer()
	for i := 1; i <= 25; i++ {
		level := "info"
		event := "warmup.credential"
		if i%5 == 0 {
			level = "warn"
			event = "warmup.failed"
		}
		w.appendLogLocked(level, event, "entry", map[string]any{"index": i})
	}

	p1 := w.LogsPage(1, 10, "all", "")
	if p1.Total != 25 || p1.TotalPages != 3 || len(p1.Logs) != 10 {
		t.Fatalf("unexpected page 1 metadata: %+v", p1)
	}
	if idx := p1.Logs[0].Fields["index"]; idx != 25 {
		t.Fatalf("expected newest entry (index=25) first, got %v", idx)
	}

	p3 := w.LogsPage(3, 10, "all", "")
	if len(p3.Logs) != 5 || p3.Logs[4].Fields["index"] != 1 {
		t.Fatalf("unexpected page 3 slice: %+v", p3)
	}

	warns := w.LogsPage(1, 10, "warn", "")
	if warns.Total != 5 || warns.TotalPages != 1 || len(warns.Logs) != 5 {
		t.Fatalf("unexpected warn filter result: %+v", warns)
	}
}

func TestHandleManagementLogsQueryAndRunBody(t *testing.T) {
	w := newWarmer()
	w.config = defaultPluginConfig()
	w.config.Enabled = true
	w.config.StateFile = ""
	w.fetchAuths = func() ([]pluginapi.HostAuthFileEntry, error) {
		return []pluginapi.HostAuthFileEntry{{ID: "ag-1", Provider: "antigravity", Status: "active"}}, nil
	}
	w.executeWarm = func(providerCfg providerWarmupConfig, provider string, auth pluginapi.HostAuthFileEntry) warmResult {
		return warmResult{Provider: provider, AuthID: auth.ID, Model: providerCfg.Model, StatusCode: 200, Success: true}
	}

	runBody := base64.StdEncoding.EncodeToString([]byte(`{"provider":"antigravity"}`))
	runReq, _ := json.Marshal(map[string]any{
		"Method": "POST",
		"Path":   "/v0/management/plugins/multi-quota-warmer/run",
		"Body":   runBody,
	})
	rawResp, err := handleManagementWithWarmer(w, runReq)
	if err != nil {
		t.Fatalf("handleManagement /run failed: %v", err)
	}
	var env envelope
	if err := json.Unmarshal(rawResp, &env); err != nil || !env.OK {
		t.Fatalf("unexpected /run envelope: %s, err=%v", string(rawResp), err)
	}

	logsReq, _ := json.Marshal(map[string]any{
		"Method": "GET",
		"Path":   "/v0/management/plugins/multi-quota-warmer/logs",
		"Query":  "page=1&page_size=2",
	})
	rawLogs, errLogs := handleManagementWithWarmer(w, logsReq)
	if errLogs != nil {
		t.Fatalf("handleManagement /logs failed: %v", errLogs)
	}
	var logsEnv struct {
		OK     bool `json:"ok"`
		Result struct {
			StatusCode int    `json:"StatusCode"`
			Body       []byte `json:"Body"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rawLogs, &logsEnv); err != nil || logsEnv.Result.StatusCode != 200 {
		t.Fatalf("unexpected /logs envelope: %s, err=%v", string(rawLogs), err)
	}
	var page logPage
	if err := json.Unmarshal(logsEnv.Result.Body, &page); err != nil {
		t.Fatalf("decode logPage failed: %v", err)
	}
	if page.PageSize != 2 || len(page.Logs) > 2 || page.Total == 0 {
		t.Fatalf("unexpected paginated logs response: %+v", page)
	}
}
