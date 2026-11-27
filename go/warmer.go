package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginID       = "multi-quota-warmer"
	pluginName     = "Multi-Provider Five-Hour Quota Warmer"
	pluginVersion  = "0.3.1"
	maxLogsDefault = 200

	providerCodex       = "codex"
	providerAntigravity = "antigravity"

	defaultCodexModel        = "gpt-5.6-luna"
	defaultCodexPrompt       = "Reply with exactly: quota window activated"
	defaultAntigravityModel  = "gemini-3-flash"
	defaultAntigravityPrompt = "Explain how HTTP/2 stream multiplexing works in detail with concrete examples. Write at least 600 tokens across several paragraphs without summarizing early."
	defaultTimezone          = "Asia/Singapore"
	defaultLogPageSize       = 20
	maxLogPageSize           = 200

	methodHostAuthModels          = "host.auth.models"
	antigravityGenerateDailyURL   = "https://daily-cloudcode-pa.googleapis.com/v1internal:generateContent"
	antigravityEnvelopeUserAgent  = "antigravity"
	antigravityWarmupUserAgent    = "antigravity/hub/2.29.0 (aidev_client; os_type=darwin; arch=arm64; cl=1013060670)"
	antigravityLegacyHubUserAgent = "antigravity/hub/2.9.1 darwin/arm64"
)

var globalWarmer = newWarmer()

type providerWarmupConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	Model   string `yaml:"model" json:"model"`
	Prompt  string `yaml:"prompt" json:"prompt"`
}

type pluginConfig struct {
	Enabled       bool                 `yaml:"enabled" json:"enabled"`
	Codex         providerWarmupConfig `yaml:"codex" json:"codex"`
	Antigravity   providerWarmupConfig `yaml:"antigravity" json:"antigravity"`
	Timezone      string               `yaml:"timezone" json:"timezone"`
	Schedules     []schedule           `yaml:"schedules" json:"schedules"`
	MaxLogEntries int                  `yaml:"max_log_entries" json:"max_log_entries"`
	StateFile     string               `yaml:"state_file" json:"state_file"`
}

type rawPluginConfig struct {
	Enabled       bool               `yaml:"enabled"`
	Codex         *rawProviderConfig `yaml:"codex"`
	Antigravity   *rawProviderConfig `yaml:"antigravity"`
	Model         string             `yaml:"model"`
	Prompt        string             `yaml:"prompt"`
	Timezone      string             `yaml:"timezone"`
	Schedules     []schedule         `yaml:"schedules"`
	MaxLogEntries int                `yaml:"max_log_entries"`
	StateFile     string             `yaml:"state_file"`
}

type rawProviderConfig struct {
	Enabled *bool  `yaml:"enabled"`
	Model   string `yaml:"model"`
	Prompt  string `yaml:"prompt"`
}

type schedule struct {
	ID      string `yaml:"id" json:"id"`
	At      string `yaml:"at" json:"at"`
	Enabled *bool  `yaml:"enabled" json:"enabled,omitempty"`
}

type scheduledRun struct {
	ID   string    `json:"id"`
	At   string    `json:"at"`
	Time time.Time `json:"time"`
}

type warmResult struct {
	Provider   string `json:"provider,omitempty"`
	AuthID     string `json:"auth_id"`
	Label      string `json:"label,omitempty"`
	Model      string `json:"model"`
	StatusCode int    `json:"status_code,omitempty"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

type logEntry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Event   string         `json:"event"`
	Message string         `json:"message"`
	Fields  map[string]any `json:"fields,omitempty"`
}

type logPage struct {
	Logs       []logEntry `json:"logs"`
	Total      int        `json:"total"`
	Page       int        `json:"page"`
	PageSize   int        `json:"page_size"`
	TotalPages int        `json:"total_pages"`
}

type warmerStatus struct {
	Plugin            string               `json:"plugin"`
	Version           string               `json:"version"`
	Running           bool                 `json:"running"`
	Codex             providerWarmupConfig `json:"codex"`
	Antigravity       providerWarmupConfig `json:"antigravity"`
	AntigravityModels []string             `json:"antigravity_models,omitempty"`
	Timezone          string               `json:"timezone"`
	Schedules         []scheduledRun       `json:"schedules"`
	LastRun           *time.Time           `json:"last_run,omitempty"`
	LastSource        string               `json:"last_source,omitempty"`
	LastResults       []warmResult         `json:"last_results"`
	LogCount          int                  `json:"log_count"`
	StateError        string               `json:"state_error,omitempty"`
	Configuration     pluginConfig         `json:"configuration"`
}

type authListResponse struct {
	Files []pluginapi.HostAuthFileEntry `json:"files"`
}

type authModelsResponse struct {
	Credentials []struct {
		ID        string   `json:"id"`
		AuthIndex string   `json:"auth_index"`
		Provider  string   `json:"provider"`
		Models    []string `json:"models"`
	} `json:"credentials"`
}

type hostAuthDocument struct {
	AuthIndex string          `json:"auth_index"`
	Name      string          `json:"name,omitempty"`
	Path      string          `json:"path,omitempty"`
	JSON      json.RawMessage `json:"json"`
}

type hostHTTPRequest struct {
	AuthIndex string      `json:"auth_index,omitempty"`
	Method    string      `json:"Method"`
	URL       string      `json:"URL"`
	Headers   http.Header `json:"Headers,omitempty"`
	Body      []byte      `json:"Body,omitempty"`
}

type hostHTTPResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
}

func (r *hostHTTPResponse) UnmarshalJSON(data []byte) error {
	var raw struct {
		StatusCode      *int        `json:"StatusCode"`
		StatusCodeSnake *int        `json:"status_code"`
		Headers         http.Header `json:"Headers"`
		HeadersLower    http.Header `json:"headers"`
		Body            *string     `json:"Body"`
		BodyLower       *string     `json:"body"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw.StatusCode != nil {
		r.StatusCode = *raw.StatusCode
	} else if raw.StatusCodeSnake != nil {
		r.StatusCode = *raw.StatusCodeSnake
	}
	if raw.Headers != nil {
		r.Headers = raw.Headers
	} else {
		r.Headers = raw.HeadersLower
	}
	body, err := decodeOptionalBase64Body(raw.Body, raw.BodyLower)
	if err != nil {
		return err
	}
	r.Body = body
	return nil
}

func decodeOptionalBase64Body(primary, fallback *string) ([]byte, error) {
	selected := primary
	allowPlain := false
	if selected == nil {
		selected = fallback
		allowPlain = true
	}
	if selected == nil || *selected == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(*selected)
	if err != nil {
		if allowPlain {
			return []byte(*selected), nil
		}
		return nil, fmt.Errorf("decode host HTTP body: %w", err)
	}
	return decoded, nil
}

type selectedAuth struct {
	Provider string
	Entry    pluginapi.HostAuthFileEntry
}

type authSelection struct {
	Total            int
	CodexTotal       int
	AntigravityTotal int
	Disabled         int
	Unavailable      int
	Inactive         int
	MissingID        int
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
}

type managementRequest struct {
	Method  string
	Path    string
	Headers http.Header
	Query   url.Values
	Body    []byte
}

type managementRequestWire struct {
	Method      string          `json:"Method"`
	MethodLower string          `json:"method"`
	Path        string          `json:"Path"`
	PathLower   string          `json:"path"`
	Headers     http.Header     `json:"Headers"`
	QueryRaw    json.RawMessage `json:"Query"`
	QueryLower  json.RawMessage `json:"query"`
	BodyRaw     json.RawMessage `json:"Body"`
	BodyLower   json.RawMessage `json:"body"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type registration struct {
	SchemaVersion uint32                   `json:"schema_version"`
	Metadata      pluginapi.Metadata       `json:"metadata"`
	Capabilities  registrationCapabilities `json:"capabilities"`
}

type registrationCapabilities struct {
	ManagementAPI bool `json:"management_api"`
}

type managementRegistration struct {
	Routes    []managementRoute    `json:"routes,omitempty"`
	Resources []managementResource `json:"resources,omitempty"`
}

type managementRoute struct {
	Method string `json:"Method"`
	Path   string `json:"Path"`
}

type managementResource struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type authListFunc func() ([]pluginapi.HostAuthFileEntry, error)
type warmExecFunc func(providerCfg providerWarmupConfig, provider string, auth pluginapi.HostAuthFileEntry) warmResult
type hostHTTPDoFunc func(req hostHTTPRequest) (hostHTTPResponse, error)
type authMaterialFunc func(auth pluginapi.HostAuthFileEntry) (accessToken string, projectID string, err error)

type warmer struct {
	mu          sync.RWMutex
	config      pluginConfig
	stop        chan struct{}
	done        chan struct{}
	planned     scheduledRun
	running     bool
	lastRun     time.Time
	lastSource  string
	lastResults []warmResult
	logs        []logEntry
	stateFile   string
	stateError  string
	stateLocked bool

	fetchAuths       authListFunc
	executeWarm      warmExecFunc
	fetchAGMaterial  authMaterialFunc
	doHostHTTP       hostHTTPDoFunc
	fallbackModelRun warmExecFunc
}

func newWarmer() *warmer {
	w := &warmer{
		config:           defaultPluginConfig(),
		fetchAuths:       fetchHostAuthFiles,
		fetchAGMaterial:  fetchAntigravityMaterial,
		doHostHTTP:       performHostHTTPDo,
		fallbackModelRun: warmViaHostModelExecute,
	}
	w.executeWarm = w.warmOneCredential
	return w
}

func defaultPluginConfig() pluginConfig {
	return pluginConfig{
		Codex: providerWarmupConfig{
			Enabled: true,
			Model:   defaultCodexModel,
			Prompt:  defaultCodexPrompt,
		},
		Antigravity: providerWarmupConfig{
			Enabled: true,
			Model:   defaultAntigravityModel,
			Prompt:  defaultAntigravityPrompt,
		},
		Timezone:      defaultTimezone,
		MaxLogEntries: maxLogsDefault,
		StateFile:     defaultStateFile(),
	}
}

func parsePluginConfig(raw []byte) (pluginConfig, error) {
	cfg := defaultPluginConfig()
	if len(raw) == 0 {
		return cfg, nil
	}
	var decoded rawPluginConfig
	if errUnmarshal := yaml.Unmarshal(raw, &decoded); errUnmarshal != nil {
		return pluginConfig{}, fmt.Errorf("decode plugin configuration: %w", errUnmarshal)
	}
	cfg.Enabled = decoded.Enabled
	cfg.Schedules = decoded.Schedules
	cfg.MaxLogEntries = decoded.MaxLogEntries
	cfg.StateFile = decoded.StateFile

	if decoded.Codex != nil {
		if decoded.Codex.Enabled != nil {
			cfg.Codex.Enabled = *decoded.Codex.Enabled
		}
		cfg.Codex.Model = strings.TrimSpace(decoded.Codex.Model)
		cfg.Codex.Prompt = strings.TrimSpace(decoded.Codex.Prompt)
	}
	if decoded.Antigravity != nil {
		if decoded.Antigravity.Enabled != nil {
			cfg.Antigravity.Enabled = *decoded.Antigravity.Enabled
		}
		cfg.Antigravity.Model = strings.TrimSpace(decoded.Antigravity.Model)
		cfg.Antigravity.Prompt = strings.TrimSpace(decoded.Antigravity.Prompt)
	}

	// Backward compatibility: migrate legacy top-level model/prompt into codex when omitted.
	if legacyModel := strings.TrimSpace(decoded.Model); legacyModel != "" && (decoded.Codex == nil || cfg.Codex.Model == "") {
		cfg.Codex.Model = legacyModel
	}
	if legacyPrompt := strings.TrimSpace(decoded.Prompt); legacyPrompt != "" && (decoded.Codex == nil || cfg.Codex.Prompt == "") {
		cfg.Codex.Prompt = legacyPrompt
	}

	if errCodex := normalizeProviderConfig(&cfg.Codex, providerCodex, defaultCodexModel, defaultCodexPrompt, decoded.Codex != nil); errCodex != nil {
		return pluginConfig{}, errCodex
	}
	if errAG := normalizeProviderConfig(&cfg.Antigravity, providerAntigravity, defaultAntigravityModel, defaultAntigravityPrompt, decoded.Antigravity != nil); errAG != nil {
		return pluginConfig{}, errAG
	}

	cfg.Timezone = strings.TrimSpace(decoded.Timezone)
	if cfg.Timezone == "" {
		cfg.Timezone = defaultTimezone
	}
	if _, errLocation := time.LoadLocation(cfg.Timezone); errLocation != nil {
		return pluginConfig{}, fmt.Errorf("timezone %q is invalid: %w", cfg.Timezone, errLocation)
	}
	if cfg.MaxLogEntries <= 0 {
		cfg.MaxLogEntries = maxLogsDefault
	}
	if cfg.MaxLogEntries > 2000 {
		return pluginConfig{}, fmt.Errorf("max_log_entries must not exceed 2000")
	}
	cfg.StateFile = strings.TrimSpace(cfg.StateFile)
	if cfg.StateFile == "" {
		cfg.StateFile = defaultStateFile()
	}
	stateFile, errPath := filepath.Abs(cfg.StateFile)
	if errPath != nil {
		return pluginConfig{}, fmt.Errorf("resolve state_file: %w", errPath)
	}
	cfg.StateFile = stateFile
	seen := make(map[string]struct{}, len(cfg.Schedules))
	seenTimes := make(map[string]struct{}, len(cfg.Schedules))
	for index := range cfg.Schedules {
		item := &cfg.Schedules[index]
		item.ID = strings.TrimSpace(item.ID)
		item.At = strings.TrimSpace(item.At)
		if item.ID == "" {
			return pluginConfig{}, fmt.Errorf("schedules[%d].id is required", index)
		}
		if item.At == "" {
			return pluginConfig{}, fmt.Errorf("schedules[%d].at is required", index)
		}
		if _, errParse := time.Parse("15:04", item.At); errParse != nil {
			return pluginConfig{}, fmt.Errorf("schedules[%d].at must use HH:MM: %w", index, errParse)
		}
		if _, duplicate := seen[item.ID]; duplicate {
			return pluginConfig{}, fmt.Errorf("schedule id %q is duplicated", item.ID)
		}
		seen[item.ID] = struct{}{}
		if scheduleEnabled(*item) {
			if _, duplicate := seenTimes[item.At]; duplicate {
				return pluginConfig{}, fmt.Errorf("more than one enabled schedule uses %s", item.At)
			}
			seenTimes[item.At] = struct{}{}
		}
	}
	return cfg, nil
}

func normalizeProviderConfig(cfg *providerWarmupConfig, name, fallbackModel, fallbackPrompt string, explicitlyPresent bool) error {
	cfg.Model = strings.TrimSpace(cfg.Model)
	cfg.Prompt = strings.TrimSpace(cfg.Prompt)
	if !explicitlyPresent {
		if cfg.Model == "" {
			cfg.Model = fallbackModel
		}
		if cfg.Prompt == "" {
			cfg.Prompt = fallbackPrompt
		}
		return nil
	}
	if cfg.Enabled {
		if cfg.Model == "" {
			return fmt.Errorf("%s.model is required when %s is enabled", name, name)
		}
		if cfg.Prompt == "" {
			return fmt.Errorf("%s.prompt is required when %s is enabled", name, name)
		}
		return nil
	}
	if cfg.Model == "" {
		cfg.Model = fallbackModel
	}
	if cfg.Prompt == "" {
		cfg.Prompt = fallbackPrompt
	}
	return nil
}

func scheduleEnabled(item schedule) bool {
	return item.Enabled == nil || *item.Enabled
}

func (w *warmer) Configure(cfg pluginConfig) {
	w.mu.RLock()
	unchanged := w.stateFile == cfg.StateFile && reflect.DeepEqual(w.config, cfg)
	w.mu.RUnlock()
	if unchanged {
		return
	}
	w.Stop()
	w.mu.Lock()
	w.config = cfg
	if w.stateFile != cfg.StateFile {
		state, loaded, errLoad := loadRunState(cfg.StateFile)
		w.stateFile = cfg.StateFile
		w.stateLocked = errLoad != nil
		w.stateError = ""
		if errLoad != nil {
			w.stateError = errLoad.Error()
		} else if loaded {
			w.lastRun = state.LastRun
			w.lastSource = state.LastSource
			w.lastResults = state.LastResults
			w.logs = state.Logs
			if len(w.logs) > cfg.MaxLogEntries {
				w.logs = append([]logEntry(nil), w.logs[len(w.logs)-cfg.MaxLogEntries:]...)
			}
		}
	}
	loadError := w.stateError
	if cfg.Enabled {
		w.stop = make(chan struct{})
		w.done = make(chan struct{})
	}
	stop, done := w.stop, w.done
	w.mu.Unlock()
	if loadError != "" {
		w.logStateFailure(loadError)
	}
	w.recordLog("info", "configuration.updated", "Warm-up configuration updated", map[string]any{
		"schedule_count":      len(cfg.Schedules),
		"timezone":            cfg.Timezone,
		"codex_enabled":       cfg.Codex.Enabled,
		"codex_model":         cfg.Codex.Model,
		"antigravity_enabled": cfg.Antigravity.Enabled,
		"antigravity_model":   cfg.Antigravity.Model,
	})
	if !cfg.Enabled {
		return
	}
	go w.runScheduleLoop(cfg, stop, done)
}

func (w *warmer) Stop() {
	w.mu.Lock()
	stop := w.stop
	done := w.done
	w.stop = nil
	w.done = nil
	w.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	if done != nil {
		<-done
	}
}

func (w *warmer) runScheduleLoop(cfg pluginConfig, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		next, okNext := nextScheduledRun(cfg, time.Now())
		if !okNext {
			select {
			case <-stop:
				return
			case <-time.After(24 * time.Hour):
				continue
			}
		}
		wait := time.Until(next.Time)
		if wait < 0 {
			continue
		}
		w.mu.Lock()
		alreadyPlanned := w.planned.ID == next.ID && w.planned.Time.Equal(next.Time)
		w.planned = next
		w.mu.Unlock()
		if !alreadyPlanned {
			w.recordLog("info", "schedule.planned", "Next quota warm-up scheduled", map[string]any{
				"schedule_id":  next.ID,
				"scheduled_at": next.Time.Format(time.RFC3339),
			})
		}
		timer := time.NewTimer(wait)
		select {
		case <-stop:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			source := "schedule:" + next.ID
			w.recordLog("info", "schedule.triggered", "Quota warm-up schedule triggered", map[string]any{
				"source":       source,
				"scheduled_at": next.Time.Format(time.RFC3339),
			})
			if _, errRun := w.Run(source, "all"); errRun != nil {
				w.recordLog("error", "schedule.failed", "Scheduled quota warm-up failed", map[string]any{
					"source": source,
					"error":  errRun.Error(),
				})
			}
		}
	}
}

func nextScheduledRun(cfg pluginConfig, now time.Time) (scheduledRun, bool) {
	location, errLocation := time.LoadLocation(cfg.Timezone)
	if errLocation != nil {
		return scheduledRun{}, false
	}
	current := now.In(location)
	var next scheduledRun
	found := false
	for _, item := range cfg.Schedules {
		if !scheduleEnabled(item) {
			continue
		}
		clock, errParse := time.Parse("15:04", item.At)
		if errParse != nil {
			continue
		}
		candidate := time.Date(current.Year(), current.Month(), current.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
		if !candidate.After(current) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		if !found || candidate.Before(next.Time) {
			next = scheduledRun{ID: item.ID, At: item.At, Time: candidate}
			found = true
		}
	}
	return next, found
}

func resolveTargetProviders(cfg pluginConfig, targetProvider string) ([]string, error) {
	normalized := strings.ToLower(strings.TrimSpace(targetProvider))
	switch normalized {
	case "", "all":
		providers := make([]string, 0, 2)
		if cfg.Codex.Enabled {
			providers = append(providers, providerCodex)
		}
		if cfg.Antigravity.Enabled {
			providers = append(providers, providerAntigravity)
		}
		return providers, nil
	case providerCodex:
		return []string{providerCodex}, nil
	case providerAntigravity:
		return []string{providerAntigravity}, nil
	default:
		return nil, &hostCallError{
			code:       "invalid_provider",
			message:    fmt.Sprintf("unsupported provider %q (must be all, codex, or antigravity)", targetProvider),
			httpStatus: http.StatusBadRequest,
		}
	}
}

func (w *warmer) Run(source string, targetProvider string) ([]warmResult, error) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil, &hostCallError{code: "warmup_in_progress", message: "a warm-up run is already in progress", httpStatus: http.StatusConflict}
	}
	cfg := cloneConfig(w.config)
	if !cfg.Enabled {
		w.mu.Unlock()
		return nil, &hostCallError{code: "plugin_disabled", message: "the plugin is disabled", httpStatus: http.StatusConflict}
	}
	providers, errProviders := resolveTargetProviders(cfg, targetProvider)
	if errProviders != nil {
		w.mu.Unlock()
		return nil, errProviders
	}
	w.running = true
	w.lastSource = source
	fetchFn := w.fetchAuths
	if fetchFn == nil {
		fetchFn = fetchHostAuthFiles
	}
	execFn := w.executeWarm
	if execFn == nil {
		execFn = w.warmOneCredential
	}
	w.mu.Unlock()

	w.recordLog("info", "warmup.started", "Multi-provider credential warm-up started", map[string]any{
		"source":    source,
		"providers": providers,
	})

	defer func() {
		w.mu.Lock()
		w.running = false
		w.lastRun = time.Now().UTC()
		errPersist := w.persistLocked()
		w.mu.Unlock()
		if errPersist != nil {
			w.logStateFailure(errPersist.Error())
		}
	}()

	if len(providers) == 0 {
		w.mu.Lock()
		w.lastResults = []warmResult{}
		w.mu.Unlock()
		w.recordLog("warn", "warmup.no_providers_enabled", "No warm-up providers are enabled", map[string]any{"source": source})
		w.recordLog("warn", "warmup.finished", "Multi-provider credential warm-up finished", map[string]any{
			"source":           source,
			"credential_count": 0,
			"success_count":    0,
			"failure_count":    0,
		})
		return []warmResult{}, nil
	}

	files, errAuths := fetchFn()
	if errAuths != nil {
		w.recordLog("error", "warmup.auth_list_failed", "Unable to list credentials", map[string]any{"source": source, "error": errAuths.Error()})
		w.recordLog("error", "warmup.finished", "Multi-provider credential warm-up failed", map[string]any{"source": source, "credential_count": 0, "success_count": 0, "failure_count": 0, "error": errAuths.Error()})
		return nil, errAuths
	}

	auths, selection := selectProviderAuths(files, providers)
	w.recordLog("info", "warmup.selection", "Credentials selected for warm-up", map[string]any{
		"source":               source,
		"providers":            providers,
		"auth_total":           selection.Total,
		"codex_total":          selection.CodexTotal,
		"antigravity_total":    selection.AntigravityTotal,
		"selected_count":       len(auths),
		"skipped_disabled":     selection.Disabled,
		"reported_unavailable": selection.Unavailable,
		"reported_inactive":    selection.Inactive,
		"skipped_missing_id":   selection.MissingID,
	})
	if len(auths) == 0 {
		w.recordLog("warn", "warmup.no_credentials", "No eligible credentials for warm-up", map[string]any{"source": source, "providers": providers})
	}
	results := make([]warmResult, 0, len(auths))
	successCount := 0
	for _, item := range auths {
		providerCfg := configForProvider(cfg, item.Provider)
		result := execFn(providerCfg, item.Provider, item.Entry)
		results = append(results, result)
		level := "info"
		message := "Credential warm-up completed"
		if !result.Success {
			level = "warn"
			message = "Credential warm-up failed"
		} else {
			successCount++
		}
		w.recordLog(level, "warmup.credential", message, map[string]any{
			"source":      source,
			"provider":    result.Provider,
			"auth_id":     result.AuthID,
			"label":       result.Label,
			"model":       result.Model,
			"status_code": result.StatusCode,
			"error":       result.Error,
		})
	}
	w.mu.Lock()
	w.lastResults = append([]warmResult(nil), results...)
	w.mu.Unlock()
	level := "info"
	if len(results) == 0 || successCount != len(results) {
		level = "warn"
	}
	w.recordLog(level, "warmup.finished", "Multi-provider credential warm-up finished", map[string]any{
		"source":           source,
		"credential_count": len(results),
		"success_count":    successCount,
		"failure_count":    len(results) - successCount,
	})
	return results, nil
}

func configForProvider(cfg pluginConfig, provider string) providerWarmupConfig {
	switch provider {
	case providerAntigravity:
		return cfg.Antigravity
	default:
		return cfg.Codex
	}
}

func fetchHostAuthFiles() ([]pluginapi.HostAuthFileEntry, error) {
	raw, errCall := callHost(pluginabi.MethodHostAuthList, map[string]any{})
	if errCall != nil {
		return nil, errCall
	}
	var response authListResponse
	if errUnmarshal := json.Unmarshal(raw, &response); errUnmarshal != nil {
		return nil, fmt.Errorf("decode host.auth.list response: %w", errUnmarshal)
	}
	return response.Files, nil
}

func fetchAntigravityModels() []string {
	raw, errCall := callHost(methodHostAuthModels, map[string]any{})
	if errCall != nil || len(raw) == 0 {
		return nil
	}
	var response authModelsResponse
	if errUnmarshal := json.Unmarshal(raw, &response); errUnmarshal != nil {
		return nil
	}
	set := make(map[string]struct{})
	for _, entry := range response.Credentials {
		if canonical, ok := canonicalWarmupProvider(entry.Provider); !ok || canonical != providerAntigravity {
			continue
		}
		for _, m := range entry.Models {
			trimmed := strings.TrimSpace(m)
			if trimmed != "" {
				set[trimmed] = struct{}{}
			}
		}
	}
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for m := range set {
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}

func canonicalWarmupProvider(provider string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case providerCodex:
		return providerCodex, true
	case providerAntigravity, "google-antigravity":
		return providerAntigravity, true
	default:
		return "", false
	}
}

func selectProviderAuths(files []pluginapi.HostAuthFileEntry, targetProviders []string) ([]selectedAuth, authSelection) {
	targetSet := make(map[string]struct{}, len(targetProviders))
	for _, p := range targetProviders {
		targetSet[p] = struct{}{}
	}
	selection := authSelection{Total: len(files)}
	auths := make([]selectedAuth, 0, len(files))
	for _, auth := range files {
		canonical, ok := canonicalWarmupProvider(auth.Provider)
		if !ok {
			continue
		}
		if canonical == providerCodex {
			selection.CodexTotal++
		} else if canonical == providerAntigravity {
			selection.AntigravityTotal++
		}
		if _, wanted := targetSet[canonical]; !wanted {
			continue
		}
		switch {
		case auth.Disabled:
			selection.Disabled++
			continue
		case strings.TrimSpace(auth.ID) == "":
			selection.MissingID++
			continue
		}
		if auth.Unavailable {
			selection.Unavailable++
		}
		if !strings.EqualFold(strings.TrimSpace(auth.Status), "active") {
			selection.Inactive++
		}
		auths = append(auths, selectedAuth{Provider: canonical, Entry: auth})
	}
	sort.Slice(auths, func(i, j int) bool {
		if auths[i].Provider != auths[j].Provider {
			return auths[i].Provider < auths[j].Provider
		}
		return strings.ToLower(auths[i].Entry.ID) < strings.ToLower(auths[j].Entry.ID)
	})
	return auths, selection
}

func (w *warmer) warmOneCredential(providerCfg providerWarmupConfig, provider string, auth pluginapi.HostAuthFileEntry) warmResult {
	if provider == providerAntigravity {
		return w.warmAntigravityCredential(providerCfg, auth)
	}
	if w.fallbackModelRun != nil {
		return w.fallbackModelRun(providerCfg, provider, auth)
	}
	return warmViaHostModelExecute(providerCfg, provider, auth)
}

func (w *warmer) warmAntigravityCredential(providerCfg providerWarmupConfig, auth pluginapi.HostAuthFileEntry) warmResult {
	result := warmResult{
		Provider: providerAntigravity,
		AuthID:   auth.ID,
		Label:    auth.Label,
		Model:    providerCfg.Model,
	}
	fetchMaterial := w.fetchAGMaterial
	if fetchMaterial == nil {
		fetchMaterial = fetchAntigravityMaterial
	}
	doHTTP := w.doHostHTTP
	if doHTTP == nil {
		doHTTP = performHostHTTPDo
	}
	modelFallback := w.fallbackModelRun
	if modelFallback == nil {
		modelFallback = warmViaHostModelExecute
	}

	accessToken, projectID, errMaterial := fetchMaterial(auth)
	if errMaterial != nil || strings.TrimSpace(accessToken) == "" {
		// Fall back to host.model.execute when raw token material is unavailable.
		fb := modelFallback(providerCfg, providerAntigravity, auth)
		if !fb.Success && errMaterial != nil {
			fb.Error = fmt.Sprintf("auth material: %v; fallback: %s", errMaterial, fb.Error)
		}
		return fb
	}

	bodyBytes, errBody := buildAntigravityGenerateBody(projectID, providerCfg.Model, providerCfg.Prompt)
	if errBody != nil {
		result.Error = errBody.Error()
		return result
	}

	authIndex := firstNonEmptyString(auth.AuthIndex, auth.ID, auth.Name)
	resp, errDo := doHTTP(hostHTTPRequest{
		AuthIndex: authIndex,
		Method:    http.MethodPost,
		URL:       antigravityGenerateDailyURL,
		Headers:   antigravityRequestHeaders(accessToken, antigravityWarmupUserAgent),
		Body:      bodyBytes,
	})
	if errDo == nil && resp.StatusCode == http.StatusForbidden {
		legacyResp, legacyErr := doHTTP(hostHTTPRequest{
			AuthIndex: authIndex,
			Method:    http.MethodPost,
			URL:       antigravityGenerateDailyURL,
			Headers:   antigravityRequestHeaders(accessToken, antigravityLegacyHubUserAgent),
			Body:      bodyBytes,
		})
		if legacyErr == nil {
			resp = legacyResp
		}
	}
	if errDo != nil {
		result.Error = errDo.Error()
		return result
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// Stored access_token may be expired; let CPA executor attempt an OAuth refresh + call.
		fb := modelFallback(providerCfg, providerAntigravity, auth)
		if fb.Success {
			return fb
		}
		result.StatusCode = resp.StatusCode
		result.Error = fmt.Sprintf("HTTP 401 (%s); fallback: %s", summarizeUpstreamBody(resp.Body), fb.Error)
		return result
	}

	result.StatusCode = resp.StatusCode
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		if inBandMsg := extractInBandJSONError(resp.Body); inBandMsg != "" {
			result.Error = "upstream in-band error: " + inBandMsg
			return result
		}
		result.Success = true
		return result
	}
	summary := summarizeUpstreamBody(resp.Body)
	if summary != "" {
		result.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, summary)
	} else {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}
	return result
}

func buildAntigravityGenerateBody(projectID, model, prompt string) ([]byte, error) {
	envelope := struct {
		Project     string `json:"project,omitempty"`
		Model       string `json:"model"`
		UserAgent   string `json:"userAgent,omitempty"`
		RequestType string `json:"requestType,omitempty"`
		RequestID   string `json:"requestId,omitempty"`
		Request     struct {
			Contents []struct {
				Role  string `json:"role"`
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"contents"`
			GenerationConfig struct {
				Temperature float64 `json:"temperature,omitempty"`
			} `json:"generationConfig"`
			SessionID string `json:"sessionId,omitempty"`
		} `json:"request"`
	}{
		Project:     strings.TrimSpace(projectID),
		Model:       strings.TrimSpace(model),
		UserAgent:   antigravityEnvelopeUserAgent,
		RequestType: "agent",
		RequestID:   randomAntigravityRequestID(),
	}
	envelope.Request.SessionID = randomAntigravitySessionID()
	envelope.Request.GenerationConfig.Temperature = 0.7
	envelope.Request.Contents = []struct {
		Role  string `json:"role"`
		Parts []struct {
			Text string `json:"text"`
		} `json:"parts"`
	}{{
		Role: "user",
		Parts: []struct {
			Text string `json:"text"`
		}{{Text: prompt}},
	}}
	return json.Marshal(envelope)
}

func antigravityRequestHeaders(accessToken, userAgent string) http.Header {
	return http.Header{
		"Accept":        []string{"application/json"},
		"Authorization": []string{"Bearer " + strings.TrimSpace(accessToken)},
		"Content-Type":  []string{"application/json"},
		"User-Agent":    []string{userAgent},
	}
}

func randomAntigravityRequestID() string {
	var buf [16]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("agent-%x", buf)
}

func randomAntigravitySessionID() string {
	var buf [8]byte
	_, _ = rand.Read(buf[:])
	val := binary.BigEndian.Uint64(buf[:]) & 0x7FFFFFFFFFFFFFFF
	return "-" + strconv.FormatUint(val, 10)
}

func fetchAntigravityMaterial(auth pluginapi.HostAuthFileEntry) (string, string, error) {
	authIndex := firstNonEmptyString(auth.AuthIndex, auth.ID, auth.Name)
	if authIndex == "" {
		return "", "", errors.New("missing auth_index")
	}
	raw, errCall := callHost(pluginabi.MethodHostAuthGet, map[string]string{"auth_index": authIndex})
	if errCall != nil {
		return "", "", errCall
	}
	var doc hostAuthDocument
	if errUnmarshal := json.Unmarshal(raw, &doc); errUnmarshal != nil {
		return "", "", fmt.Errorf("decode host.auth.get: %w", errUnmarshal)
	}
	rawJSON := doc.JSON
	if len(rawJSON) == 0 && strings.TrimSpace(doc.Path) != "" {
		fileBytes, errRead := os.ReadFile(doc.Path)
		if errRead != nil {
			return "", "", fmt.Errorf("read auth file: %w", errRead)
		}
		rawJSON = fileBytes
	}
	token, projectID := extractAntigravityAuthFields(rawJSON)
	if token == "" {
		return "", projectID, errors.New("access_token not found in auth document")
	}
	return token, projectID, nil
}

func extractAntigravityAuthFields(raw json.RawMessage) (string, string) {
	var fields struct {
		AccessToken    string `json:"access_token"`
		Token          string `json:"token"`
		ProjectID      string `json:"project_id"`
		QuotaProjectID string `json:"quota_project_id"`
		Project        string `json:"project"`
		Metadata       struct {
			AccessToken    string `json:"access_token"`
			Token          string `json:"token"`
			ProjectID      string `json:"project_id"`
			QuotaProjectID string `json:"quota_project_id"`
			Project        string `json:"project"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil {
		return "", ""
	}
	accessToken := firstNonEmptyString(
		fields.AccessToken,
		fields.Token,
		fields.Metadata.AccessToken,
		fields.Metadata.Token,
	)
	projectID := firstNonEmptyString(
		fields.ProjectID,
		fields.QuotaProjectID,
		fields.Project,
		fields.Metadata.ProjectID,
		fields.Metadata.QuotaProjectID,
		fields.Metadata.Project,
	)
	return accessToken, projectID
}

func performHostHTTPDo(req hostHTTPRequest) (hostHTTPResponse, error) {
	raw, errCall := callHost(pluginabi.MethodHostHTTPDo, req)
	if errCall != nil {
		return hostHTTPResponse{}, errCall
	}
	var resp hostHTTPResponse
	if errUnmarshal := json.Unmarshal(raw, &resp); errUnmarshal != nil {
		return hostHTTPResponse{}, fmt.Errorf("decode host.http.do response: %w", errUnmarshal)
	}
	return resp, nil
}

func extractInBandJSONError(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" || trimmed[0] != '{' {
		return ""
	}
	var payload struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil || payload.Error == nil {
		return ""
	}
	msg := strings.TrimSpace(payload.Error.Message)
	if msg == "" {
		msg = strings.TrimSpace(payload.Error.Status)
	}
	if msg == "" && payload.Error.Code != 0 {
		msg = fmt.Sprintf("code %d", payload.Error.Code)
	}
	return msg
}

func summarizeUpstreamBody(body []byte) string {
	if msg := extractInBandJSONError(body); msg != "" {
		return truncateString(msg, 300)
	}
	collapsed := strings.Join(strings.Fields(string(body)), " ")
	return truncateString(collapsed, 300)
}

func truncateString(s string, maxRunes int) string {
	if maxRunes <= 0 || len(s) == 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}

func firstNonEmptyString(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func warmViaHostModelExecute(providerCfg providerWarmupConfig, provider string, auth pluginapi.HostAuthFileEntry) warmResult {
	result := warmResult{
		Provider: provider,
		AuthID:   auth.ID,
		Label:    auth.Label,
		Model:    providerCfg.Model,
	}
	body, errBody := json.Marshal(map[string]any{
		"model": providerCfg.Model,
		"messages": []map[string]string{{
			"role":    "user",
			"content": providerCfg.Prompt,
		}},
		"stream": false,
	})
	if errBody != nil {
		result.Error = errBody.Error()
		return result
	}
	forcedProvider := strings.ToLower(strings.TrimSpace(auth.Provider))
	if forcedProvider == "" {
		forcedProvider = provider
	}
	raw, errCall := callHost(pluginabi.MethodHostModelExecute, pluginapi.HostModelExecutionRequest{
		EntryProtocol:  "openai",
		ExitProtocol:   "openai",
		Model:          providerCfg.Model,
		Stream:         false,
		Body:           body,
		Headers:        http.Header{"Content-Type": []string{"application/json"}},
		ForcedProvider: forcedProvider,
		AuthID:         auth.ID,
	})
	if errCall != nil {
		result.Error = errCall.Error()
		var statusCoder interface{ StatusCode() int }
		if errors.As(errCall, &statusCoder) && statusCoder != nil {
			result.StatusCode = statusCoder.StatusCode()
		}
		return result
	}
	var response pluginapi.HostModelExecutionResponse
	if errUnmarshal := json.Unmarshal(raw, &response); errUnmarshal != nil {
		result.Error = fmt.Sprintf("decode host.model.execute response: %v", errUnmarshal)
		return result
	}
	result.StatusCode = response.StatusCode
	result.Success = response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices
	if !result.Success {
		result.Error = fmt.Sprintf("model execution returned HTTP %d", response.StatusCode)
	}
	return result
}

func (w *warmer) Status() warmerStatus {
	w.mu.RLock()
	cfg := cloneConfig(w.config)
	next := nextRuns(cfg, time.Now())
	var lastRun *time.Time
	if !w.lastRun.IsZero() {
		value := w.lastRun
		lastRun = &value
	}
	status := warmerStatus{
		Plugin:        pluginID,
		Version:       pluginVersion,
		Running:       w.running,
		Codex:         cfg.Codex,
		Antigravity:   cfg.Antigravity,
		Timezone:      cfg.Timezone,
		Schedules:     next,
		LastRun:       lastRun,
		LastSource:    w.lastSource,
		LastResults:   append([]warmResult(nil), w.lastResults...),
		LogCount:      len(w.logs),
		StateError:    w.stateError,
		Configuration: cfg,
	}
	w.mu.RUnlock()

	status.AntigravityModels = fetchAntigravityModels()
	return status
}

func nextRuns(cfg pluginConfig, now time.Time) []scheduledRun {
	location, errLocation := time.LoadLocation(cfg.Timezone)
	if errLocation != nil {
		return []scheduledRun{}
	}
	current := now.In(location)
	runs := make([]scheduledRun, 0, len(cfg.Schedules))
	for _, item := range cfg.Schedules {
		if !scheduleEnabled(item) {
			continue
		}
		clock, errParse := time.Parse("15:04", item.At)
		if errParse != nil {
			continue
		}
		candidate := time.Date(current.Year(), current.Month(), current.Day(), clock.Hour(), clock.Minute(), 0, 0, location)
		if !candidate.After(current) {
			candidate = candidate.AddDate(0, 0, 1)
		}
		runs = append(runs, scheduledRun{ID: item.ID, At: item.At, Time: candidate})
	}
	sort.Slice(runs, func(i, j int) bool { return runs[i].Time.Before(runs[j].Time) })
	return runs
}

func (w *warmer) Logs() []logEntry {
	w.mu.RLock()
	defer w.mu.RUnlock()
	logs := make([]logEntry, len(w.logs))
	copy(logs, w.logs)
	return logs
}

// LogsPage returns filtered logs in reverse chronological order (newest first) along with pagination metadata.
// When both page and pageSize are <= 0 and no filters are provided, all logs are returned in chronological order
// for legacy API callers while still populating pagination metadata.
func (w *warmer) LogsPage(page, pageSize int, level, query string) logPage {
	w.mu.RLock()
	snapshot := make([]logEntry, len(w.logs))
	copy(snapshot, w.logs)
	w.mu.RUnlock()

	normLevel := strings.ToLower(strings.TrimSpace(level))
	if normLevel == "all" {
		normLevel = ""
	}
	normQuery := strings.ToLower(strings.TrimSpace(query))

	if page <= 0 && pageSize <= 0 && normLevel == "" && normQuery == "" {
		total := len(snapshot)
		effectiveSize := total
		if effectiveSize == 0 {
			effectiveSize = defaultLogPageSize
		}
		return logPage{
			Logs:       snapshot,
			Total:      total,
			Page:       1,
			PageSize:   effectiveSize,
			TotalPages: 1,
		}
	}

	filtered := make([]logEntry, 0, len(snapshot))
	for i := len(snapshot) - 1; i >= 0; i-- {
		entry := snapshot[i]
		if normLevel != "" && !strings.EqualFold(entry.Level, normLevel) {
			continue
		}
		if normQuery != "" && !logEntryMatches(entry, normQuery) {
			continue
		}
		filtered = append(filtered, entry)
	}

	total := len(filtered)
	if pageSize <= 0 {
		pageSize = defaultLogPageSize
	}
	if pageSize > maxLogPageSize {
		pageSize = maxLogPageSize
	}
	totalPages := 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
	}
	if page <= 0 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}

	start := (page - 1) * pageSize
	if start >= total {
		return logPage{
			Logs:       []logEntry{},
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalPages: totalPages,
		}
	}
	end := min(start+pageSize, total)
	return logPage{
		Logs:       filtered[start:end],
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalPages: totalPages,
	}
}

func logEntryMatches(entry logEntry, needle string) bool {
	if strings.Contains(strings.ToLower(entry.Event), needle) ||
		strings.Contains(strings.ToLower(entry.Message), needle) ||
		strings.Contains(strings.ToLower(entry.Level), needle) {
		return true
	}
	if len(entry.Fields) > 0 {
		encoded, err := json.Marshal(entry.Fields)
		if err == nil && strings.Contains(strings.ToLower(string(encoded)), needle) {
			return true
		}
	}
	return false
}

func (w *warmer) recordLog(level, event, message string, fields map[string]any) {
	w.mu.Lock()
	w.appendLogLocked(level, event, message, fields)
	errPersist := w.persistLocked()
	w.mu.Unlock()
	if errPersist != nil {
		w.logStateFailure(errPersist.Error())
	}
	hostFields := sanitizeLogFields(fields)
	if hostFields == nil {
		hostFields = make(map[string]any)
	}
	hostFields["plugin_id"] = pluginID
	_, _ = callHost(pluginabi.MethodHostLog, map[string]any{
		"level":   level,
		"message": hostLogMessage(event, message, fields),
		"fields":  hostFields,
	})
}

func hostLogMessage(event, message string, fields map[string]any) string {
	switch event {
	case "schedule.planned":
		return fmt.Sprintf("%s schedule_id=%q scheduled_at=%v", message, fields["schedule_id"], fields["scheduled_at"])
	case "schedule.triggered":
		return fmt.Sprintf("%s source=%q scheduled_at=%v", message, fields["source"], fields["scheduled_at"])
	case "warmup.selection":
		return fmt.Sprintf("%s source=%q auth_total=%v codex_total=%v antigravity_total=%v selected=%v skipped_disabled=%v reported_unavailable=%v reported_inactive=%v skipped_missing_id=%v", message, fields["source"], fields["auth_total"], fields["codex_total"], fields["antigravity_total"], fields["selected_count"], fields["skipped_disabled"], fields["reported_unavailable"], fields["reported_inactive"], fields["skipped_missing_id"])
	case "warmup.finished":
		return fmt.Sprintf("%s source=%q credential_count=%v success_count=%v failure_count=%v", message, fields["source"], fields["credential_count"], fields["success_count"], fields["failure_count"])
	case "warmup.started", "warmup.no_credentials", "warmup.no_providers_enabled", "warmup.auth_list_failed", "warmup.credential", "schedule.failed":
		return fmt.Sprintf("%s source=%q", message, fields["source"])
	default:
		return message
	}
}

func (w *warmer) appendLogLocked(level, event, message string, fields map[string]any) {
	entry := logEntry{
		Time:    time.Now().UTC(),
		Level:   level,
		Event:   event,
		Message: message,
		Fields:  sanitizeLogFields(fields),
	}
	w.logs = append(w.logs, entry)
	limit := w.config.MaxLogEntries
	if limit <= 0 {
		limit = maxLogsDefault
	}
	if len(w.logs) > limit {
		w.logs = append([]logEntry(nil), w.logs[len(w.logs)-limit:]...)
	}
}

func sanitizeLogFields(fields map[string]any) map[string]any {
	if len(fields) == 0 {
		return nil
	}
	copyFields := make(map[string]any, len(fields))
	for key, value := range fields {
		if strings.Contains(strings.ToLower(key), "token") || strings.Contains(strings.ToLower(key), "secret") || strings.Contains(strings.ToLower(key), "authorization") {
			continue
		}
		copyFields[key] = value
	}
	return copyFields
}

func cloneConfig(cfg pluginConfig) pluginConfig {
	copyCfg := cfg
	copyCfg.Schedules = append([]schedule(nil), cfg.Schedules...)
	return copyCfg
}

func handleMethod(method string, raw []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var request lifecycleRequest
		if errUnmarshal := json.Unmarshal(raw, &request); errUnmarshal != nil {
			return nil, fmt.Errorf("decode plugin lifecycle request: %w", errUnmarshal)
		}
		cfg, errConfig := parsePluginConfig(request.ConfigYAML)
		if errConfig != nil {
			return nil, errConfig
		}
		globalWarmer.Configure(cfg)
		return okEnvelope(pluginRegistration())
	case pluginabi.MethodPluginQuiesce, pluginabi.MethodPluginShutdown:
		globalWarmer.Stop()
		return okEnvelope(map[string]any{})
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration{
			Routes: []managementRoute{
				{Method: http.MethodGet, Path: "/plugins/" + pluginID + "/status"},
				{Method: http.MethodPost, Path: "/plugins/" + pluginID + "/run"},
				{Method: http.MethodGet, Path: "/plugins/" + pluginID + "/logs"},
			},
			Resources: []managementResource{{
				Path:        "/status",
				Menu:        "Multi Quota Warmer",
				Description: "Schedules and runs Codex and Antigravity credential warm-up requests.",
			}},
		})
	case pluginabi.MethodManagementHandle:
		return handleManagement(raw)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method, 0), nil
	}
}

func pluginRegistration() registration {
	return registration{
		SchemaVersion: pluginabi.SchemaVersion,
		Metadata: pluginapi.Metadata{
			Name:             pluginName,
			Version:          pluginVersion,
			Author:           "wvlike",
			GitHubRepository: "https://github.com/wvlike/multi-quota-warmer",
			ConfigFields: []pluginapi.ConfigField{
				{Name: "codex", Type: pluginapi.ConfigFieldTypeObject, Description: "Codex warm-up settings (enabled, model, prompt)."},
				{Name: "antigravity", Type: pluginapi.ConfigFieldTypeObject, Description: "Antigravity warm-up settings (enabled, model, prompt)."},
				{Name: "timezone", Type: pluginapi.ConfigFieldTypeString, Description: "IANA timezone used by the daily schedules."},
				{Name: "schedules", Type: pluginapi.ConfigFieldTypeArray, Description: "Daily schedule entries with id, at (HH:MM), and enabled."},
				{Name: "max_log_entries", Type: pluginapi.ConfigFieldTypeInteger, Description: "Maximum number of retained warm-up log entries."},
				{Name: "state_file", Type: pluginapi.ConfigFieldTypeString, Description: "Path to the persistent warm-up history JSON file."},
			},
		},
		Capabilities: registrationCapabilities{ManagementAPI: true},
	}
}

func decodeManagementRequest(raw []byte) (managementRequest, error) {
	var wire managementRequestWire
	if errUnmarshal := json.Unmarshal(raw, &wire); errUnmarshal != nil {
		return managementRequest{}, fmt.Errorf("decode management request: %w", errUnmarshal)
	}
	method := strings.TrimSpace(wire.Method)
	if method == "" {
		method = strings.TrimSpace(wire.MethodLower)
	}
	rawPath := strings.TrimSpace(wire.Path)
	if rawPath == "" {
		rawPath = strings.TrimSpace(wire.PathLower)
	}
	cleanPath := rawPath
	query := make(url.Values)
	if basePath, inlineQuery, hasQuery := strings.Cut(rawPath, "?"); hasQuery {
		cleanPath = basePath
		if parsed, errParse := url.ParseQuery(inlineQuery); errParse == nil {
			for k, vals := range parsed {
				query[k] = append(query[k], vals...)
			}
		}
	}
	mergeRawQuery(query, wire.QueryRaw)
	mergeRawQuery(query, wire.QueryLower)

	body, errBody := decodeManagementBody(wire.BodyRaw, wire.BodyLower)
	if errBody != nil {
		return managementRequest{}, errBody
	}
	return managementRequest{
		Method:  method,
		Path:    strings.TrimRight(cleanPath, "/"),
		Headers: wire.Headers,
		Query:   query,
		Body:    body,
	}, nil
}

func mergeRawQuery(dst url.Values, raw json.RawMessage) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return
	}
	if strings.HasPrefix(trimmed, `"`) {
		var str string
		if err := json.Unmarshal(raw, &str); err == nil {
			if parsed, errParse := url.ParseQuery(strings.TrimPrefix(str, "?")); errParse == nil {
				for k, vals := range parsed {
					dst[k] = append(dst[k], vals...)
				}
			}
		}
		return
	}
	var m url.Values
	if err := json.Unmarshal(raw, &m); err == nil {
		for k, vals := range m {
			dst[k] = append(dst[k], vals...)
		}
	}
}

func decodeManagementBody(primary, fallback json.RawMessage) ([]byte, error) {
	raw := primary
	if len(strings.TrimSpace(string(raw))) == 0 || strings.TrimSpace(string(raw)) == "null" {
		raw = fallback
	}
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var b64 string
		if err := json.Unmarshal(raw, &b64); err != nil {
			return nil, fmt.Errorf("decode management body string: %w", err)
		}
		if b64 == "" {
			return nil, nil
		}
		decoded, errDec := base64.StdEncoding.DecodeString(b64)
		if errDec != nil {
			return []byte(b64), nil
		}
		return decoded, nil
	}
	return append([]byte(nil), raw...), nil
}

func handleManagement(raw []byte) ([]byte, error) {
	return handleManagementWithWarmer(globalWarmer, raw)
}

func handleManagementWithWarmer(w *warmer, raw []byte) ([]byte, error) {
	request, errDecode := decodeManagementRequest(raw)
	if errDecode != nil {
		return nil, errDecode
	}
	path := request.Path
	switch {
	case strings.HasSuffix(path, "/status"):
		if strings.HasPrefix(path, "/v0/resource/") {
			return okEnvelope(htmlResponse(http.StatusOK, []byte(statusPageHTML)))
		}
		return okEnvelope(jsonResponse(http.StatusOK, w.Status()))
	case strings.HasSuffix(path, "/logs"):
		page, _ := strconv.Atoi(strings.TrimSpace(request.Query.Get("page")))
		pageSize, _ := strconv.Atoi(strings.TrimSpace(request.Query.Get("page_size")))
		level := request.Query.Get("level")
		queryText := request.Query.Get("q")
		return okEnvelope(jsonResponse(http.StatusOK, w.LogsPage(page, pageSize, level, queryText)))
	case strings.HasSuffix(path, "/run"):
		targetProvider := strings.TrimSpace(request.Query.Get("provider"))
		if targetProvider == "" && len(request.Body) > 0 {
			var bodyReq struct {
				Provider string `json:"provider"`
			}
			if errJSON := json.Unmarshal(request.Body, &bodyReq); errJSON == nil {
				targetProvider = strings.TrimSpace(bodyReq.Provider)
			}
		}
		results, errRun := w.Run("manual", targetProvider)
		if errRun != nil {
			status := http.StatusInternalServerError
			var statusCoder interface{ StatusCode() int }
			if errors.As(errRun, &statusCoder) && statusCoder != nil && statusCoder.StatusCode() > 0 {
				status = statusCoder.StatusCode()
			}
			return okEnvelope(jsonResponse(status, map[string]any{"error": errRun.Error()}))
		}
		return okEnvelope(jsonResponse(http.StatusOK, map[string]any{"results": results, "status": w.Status()}))
	default:
		return okEnvelope(jsonResponse(http.StatusNotFound, map[string]any{"error": "route_not_found"}))
	}
}

func jsonResponse(status int, value any) managementResponse {
	body, errMarshal := json.Marshal(value)
	if errMarshal != nil {
		body = []byte(`{"error":"response_encode_failed"}`)
		status = http.StatusInternalServerError
	}
	return managementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"application/json; charset=utf-8"}},
		Body:       body,
	}
}

func htmlResponse(status int, body []byte) managementResponse {
	return managementResponse{
		StatusCode: status,
		Headers:    http.Header{"Content-Type": []string{"text/html; charset=utf-8"}},
		Body:       body,
	}
}
