package main

/*
#include <stdint.h>
#include <stdlib.h>

typedef struct {
	void* ptr;
	size_t len;
} cliproxy_buffer;

typedef int (*cliproxy_host_call_fn)(void*, const char*, const uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_host_free_fn)(void*, size_t);

typedef struct {
	uint32_t abi_version;
	void* host_ctx;
	cliproxy_host_call_fn call;
	cliproxy_host_free_fn free_buffer;
} cliproxy_host_api;

typedef int (*cliproxy_plugin_call_fn)(char*, uint8_t*, size_t, cliproxy_buffer*);
typedef void (*cliproxy_plugin_free_fn)(void*, size_t);
typedef void (*cliproxy_plugin_shutdown_fn)(void);

typedef struct {
	uint32_t abi_version;
	cliproxy_plugin_call_fn call;
	cliproxy_plugin_free_fn free_buffer;
	cliproxy_plugin_shutdown_fn shutdown;
} cliproxy_plugin_api;

extern int HiOnJSONPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void HiOnJSONPluginFree(void*, size_t);
extern void HiOnJSONPluginShutdown(void);

static const cliproxy_host_api* stored_host;

static void store_host_api(const cliproxy_host_api* host) {
	stored_host = host;
}

static int call_host_api(const char* method, const uint8_t* request, size_t request_len, cliproxy_buffer* response) {
	if (stored_host == NULL || stored_host->call == NULL) {
		return 1;
	}
	return stored_host->call(stored_host->host_ctx, method, request, request_len, response);
}

static void free_host_buffer(void* ptr, size_t len) {
	if (stored_host != NULL && stored_host->free_buffer != NULL && ptr != NULL) {
		stored_host->free_buffer(ptr, len);
	}
}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginabi"
	"github.com/router-for-me/CLIProxyAPI/v7/sdk/pluginapi"
	"gopkg.in/yaml.v3"
)

const (
	pluginID           = "hi-on-json"
	pluginVersion      = "0.1.0"
	methodHostAuthList = "host.auth.list"
)

type envelope struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *envelopeError  `json:"error,omitempty"`
}

type envelopeError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type lifecycleRequest struct {
	ConfigYAML []byte `json:"config_yaml"`
	PluginDir  string `json:"plugin_dir,omitempty"`
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
	Resources []managementResource `json:"resources,omitempty"`
}

type managementResource struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description"`
}

type managementRequest struct {
	Method         string      `json:"Method"`
	Path           string      `json:"Path"`
	Headers        http.Header `json:"Headers"`
	Query          url.Values  `json:"Query"`
	Body           []byte      `json:"Body"`
	HostCallbackID string      `json:"host_callback_id,omitempty"`
}

type managementResponse struct {
	StatusCode int         `json:"StatusCode"`
	Headers    http.Header `json:"Headers"`
	Body       []byte      `json:"Body"`
}

type config struct {
	Enabled         bool     `yaml:"enabled"`
	Model           string   `yaml:"model"`
	Prompt          string   `yaml:"prompt"`
	PollIntervalRaw string   `yaml:"poll_interval"`
	SettleDelayRaw  string   `yaml:"settle_delay"`
	IncludeExisting bool     `yaml:"include_existing"`
	Providers       []string `yaml:"providers"`
	NameSuffix      string   `yaml:"name_suffix"`
	EntryProtocol   string   `yaml:"entry_protocol"`
	ExitProtocol    string   `yaml:"exit_protocol"`
	Alt             string   `yaml:"alt"`

	PollInterval time.Duration `yaml:"-"`
	SettleDelay  time.Duration `yaml:"-"`
}


type authFileEntry struct {
	ID        string `json:"id,omitempty"`
	AuthIndex string `json:"auth_index,omitempty"`
	Name      string `json:"name"`
	Provider  string `json:"provider,omitempty"`
	Path      string `json:"path,omitempty"`
	Size      int64  `json:"size,omitempty"`
}
type authListResponse struct {
	Files []authFileEntry `json:"files"`
}

type hostModelExecutionRequest struct {
	pluginapi.HostModelExecutionRequest
	HostCallbackID string `json:"host_callback_id,omitempty"`
}

type chatCompletionRequest struct {
	Model    string        `json:"model"`
	Stream   bool          `json:"stream"`
	Messages []chatMessage `json:"messages"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type runner struct {
	mu         sync.Mutex
	cfg        config
	stop       chan struct{}
	done       chan struct{}
	seen       map[string]struct{}
	lastStatus string
	lastError  string
	lastAsk    time.Time
	asked      int64
}

var state = &runner{}

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(pluginabi.ABIVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.HiOnJSONPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.HiOnJSONPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.HiOnJSONPluginShutdown)
	return 0
}

//export HiOnJSONPluginCall
func HiOnJSONPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 0
	}
	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}
	raw, errHandle := handleMethod(C.GoString(method), requestBytes)
	if errHandle != nil {
		writeResponse(response, errorEnvelope("plugin_error", errHandle.Error()))
		return 0
	}
	writeResponse(response, raw)
	return 0
}

//export HiOnJSONPluginFree
func HiOnJSONPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export HiOnJSONPluginShutdown
func HiOnJSONPluginShutdown() {
	state.stopRunner()
}

func handleMethod(method string, request []byte) ([]byte, error) {
	switch method {
	case pluginabi.MethodPluginRegister, pluginabi.MethodPluginReconfigure:
		var req lifecycleRequest
		if len(request) > 0 {
			if err := json.Unmarshal(request, &req); err != nil {
				return nil, err
			}
		}
		cfg, err := parseConfig(req.ConfigYAML)
		if err != nil {
			return nil, err
		}
		state.start(cfg)
		return okEnvelope(registration{
			SchemaVersion: pluginabi.SchemaVersion,
			Metadata: pluginapi.Metadata{
				Name:             pluginID,
				Version:          pluginVersion,
				Author:           "local",
				GitHubRepository: "https://github.com/router-for-me/CLIProxyAPI",
				ConfigFields: []pluginapi.ConfigField{
					{Name: "enabled", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Enable polling auth JSON records and asking Hi for each new one."},
					{Name: "model", Type: pluginapi.ConfigFieldTypeString, Description: "Model used for the automatic Hi request."},
					{Name: "prompt", Type: pluginapi.ConfigFieldTypeString, Description: "Prompt sent for every newly discovered JSON auth record; default: Hi."},
					{Name: "poll_interval", Type: pluginapi.ConfigFieldTypeString, Description: "Polling interval, Go duration such as 2s."},
					{Name: "settle_delay", Type: pluginapi.ConfigFieldTypeString, Description: "Delay after seeing a new JSON before asking, Go duration such as 3s."},
					{Name: "include_existing", Type: pluginapi.ConfigFieldTypeBoolean, Description: "Ask Hi for existing auth JSON records when the plugin starts."},
					{Name: "providers", Type: pluginapi.ConfigFieldTypeArray, Description: "Optional provider allow-list, e.g. [openai, codex, gemini]. Empty means all providers."},
					{Name: "name_suffix", Type: pluginapi.ConfigFieldTypeString, Description: "Optional filename suffix filter; default .json."},
				},
			},
			Capabilities: registrationCapabilities{ManagementAPI: true},
		})
	case pluginabi.MethodManagementRegister:
		return okEnvelope(managementRegistration{Resources: []managementResource{{Path: "/status", Menu: "Hi on JSON", Description: "Shows automatic Hi-on-new-auth-JSON status."}}})
	case pluginabi.MethodManagementHandle:
		return okEnvelope(statusPage())
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func defaultConfig() config {
	return config{
		Enabled:       true,
		Model:         "gpt-5.5",
		Prompt:        "Hi",
		PollInterval:  2 * time.Second,
		SettleDelay:   3 * time.Second,
		NameSuffix:    ".json",
		EntryProtocol: "openai",
		ExitProtocol:  "openai",
	}
}

func parseConfig(raw []byte) (config, error) {
	cfg := defaultConfig()
	if len(strings.TrimSpace(string(raw))) > 0 {
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("invalid %s config: %w", pluginID, err)
		}
	}
	if cfg.Model = strings.TrimSpace(cfg.Model); cfg.Model == "" {
		cfg.Model = "gpt-5.5"
	}
	if cfg.Prompt == "" {
		cfg.Prompt = "Hi"
	}
	if strings.TrimSpace(cfg.PollIntervalRaw) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(cfg.PollIntervalRaw))
		if err != nil || d <= 0 {
			return cfg, fmt.Errorf("invalid poll_interval %q", cfg.PollIntervalRaw)
		}
		cfg.PollInterval = d
	}
	if strings.TrimSpace(cfg.SettleDelayRaw) != "" {
		d, err := time.ParseDuration(strings.TrimSpace(cfg.SettleDelayRaw))
		if err != nil || d < 0 {
			return cfg, fmt.Errorf("invalid settle_delay %q", cfg.SettleDelayRaw)
		}
		cfg.SettleDelay = d
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 2 * time.Second
	}
	if cfg.SettleDelay < 0 {
		cfg.SettleDelay = 3 * time.Second
	}
	if strings.TrimSpace(cfg.NameSuffix) == "" {
		cfg.NameSuffix = ".json"
	}
	if strings.TrimSpace(cfg.EntryProtocol) == "" {
		cfg.EntryProtocol = "openai"
	}
	if strings.TrimSpace(cfg.ExitProtocol) == "" {
		cfg.ExitProtocol = "openai"
	}
	for i := range cfg.Providers {
		cfg.Providers[i] = strings.ToLower(strings.TrimSpace(cfg.Providers[i]))
	}
	return cfg, nil
}

func (r *runner) start(cfg config) {
	r.stopRunner()
	r.mu.Lock()
	r.cfg = cfg
	r.stop = make(chan struct{})
	r.done = make(chan struct{})
	r.seen = make(map[string]struct{})
	r.lastError = ""
	if cfg.Enabled {
		r.lastStatus = "running"
	} else {
		r.lastStatus = "disabled"
	}
	stop := r.stop
	done := r.done
	r.mu.Unlock()
	if cfg.Enabled {
		go r.loop(cfg, stop, done)
	} else {
		close(done)
	}
}

func (r *runner) stopRunner() {
	r.mu.Lock()
	stop := r.stop
	done := r.done
	if stop != nil {
		select {
		case <-stop:
		default:
			close(stop)
		}
	}
	r.stop = nil
	r.done = nil
	r.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
		}
	}
}

func (r *runner) loop(cfg config, stop <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	if !cfg.IncludeExisting {
		files, err := listAuthFiles()
		if err == nil {
			r.mu.Lock()
			for _, f := range files {
				r.seen[authKey(f)] = struct{}{}
			}
			r.lastStatus = fmt.Sprintf("running; baseline=%d", len(files))
			r.mu.Unlock()
		} else {
			r.setError(err)
		}
	}
	ticker := time.NewTicker(cfg.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			r.setStatus("stopped")
			return
		case <-ticker.C:
			files, err := listAuthFiles()
			if err != nil {
				r.setError(err)
				continue
			}
			for _, f := range files {
				if !cfg.matches(f) {
					continue
				}
				key := authKey(f)
				r.mu.Lock()
				_, exists := r.seen[key]
				if !exists {
					r.seen[key] = struct{}{}
				}
				r.mu.Unlock()
				if exists {
					continue
				}
				go r.askForNewAuth(cfg, f)
			}
			r.setStatus(fmt.Sprintf("running; seen=%d", len(files)))
		}
	}
}

func (cfg config) matches(f authFileEntry) bool {
	name := strings.ToLower(strings.TrimSpace(f.Name))
	path := strings.ToLower(strings.TrimSpace(f.Path))
	if cfg.NameSuffix != "" {
		suffix := strings.ToLower(cfg.NameSuffix)
		if !strings.HasSuffix(name, suffix) && !strings.HasSuffix(path, suffix) {
			return false
		}
	}
	if len(cfg.Providers) > 0 {
		provider := strings.ToLower(strings.TrimSpace(f.Provider))
		matched := false
		for _, p := range cfg.Providers {
			if p != "" && provider == p {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (r *runner) askForNewAuth(cfg config, f authFileEntry) {
	if cfg.SettleDelay > 0 {
		time.Sleep(cfg.SettleDelay)
	}
	msg := cfg.Prompt
	body, _ := json.Marshal(chatCompletionRequest{
		Model:  cfg.Model,
		Stream: false,
		Messages: []chatMessage{{
			Role:    "user",
			Content: msg,
		}},
	})
	_, err := executeModel(hostModelExecutionRequest{HostModelExecutionRequest: pluginapi.HostModelExecutionRequest{
		EntryProtocol: cfg.EntryProtocol,
		ExitProtocol:  cfg.ExitProtocol,
		Model:         cfg.Model,
		Stream:        false,
		Body:          body,
		Headers:       http.Header{},
		Query:         url.Values{},
		Alt:           cfg.Alt,
	}})
	if err != nil {
		r.setError(fmt.Errorf("ask %q for %s failed: %w", cfg.Prompt, displayAuth(f), err))
		_ = logHost("warn", "Hi-on-JSON failed", map[string]any{"auth": displayAuth(f), "error": err.Error()})
		return
	}
	r.mu.Lock()
	r.asked++
	r.lastAsk = time.Now()
	r.lastError = ""
	r.lastStatus = "asked " + cfg.Prompt + " for " + displayAuth(f)
	r.mu.Unlock()
	_ = logHost("info", "Hi-on-JSON asked prompt for new auth JSON", map[string]any{"auth": displayAuth(f), "prompt": cfg.Prompt, "model": cfg.Model})
}

func listAuthFiles() ([]authFileEntry, error) {
	result, err := callHost(methodHostAuthList, map[string]any{})
	if err != nil {
		return nil, err
	}
	var resp authListResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("decode host.auth.list: %w", err)
	}
	return resp.Files, nil
}

func executeModel(req hostModelExecutionRequest) (pluginapi.HostModelExecutionResponse, error) {
	result, err := callHost(pluginabi.MethodHostModelExecute, req)
	if err != nil {
		return pluginapi.HostModelExecutionResponse{}, err
	}
	var resp pluginapi.HostModelExecutionResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return pluginapi.HostModelExecutionResponse{}, fmt.Errorf("decode host.model.execute: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return resp, fmt.Errorf("host.model.execute HTTP %d: %s", resp.StatusCode, truncate(string(resp.Body), 500))
	}
	return resp, nil
}

func callHost(method string, payload any) (json.RawMessage, error) {
	rawPayload, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal host callback payload %s: %w", method, err)
	}
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))
	var response C.cliproxy_buffer
	var requestPtr *C.uint8_t
	if len(rawPayload) > 0 {
		cPayload := C.CBytes(rawPayload)
		if cPayload == nil {
			return nil, fmt.Errorf("allocate host callback payload %s", method)
		}
		defer C.free(cPayload)
		requestPtr = (*C.uint8_t)(cPayload)
	}
	callCode := C.call_host_api(cMethod, requestPtr, C.size_t(len(rawPayload)), &response)
	var rawResponse []byte
	if response.ptr != nil && response.len > 0 {
		rawResponse = C.GoBytes(response.ptr, C.int(response.len))
	}
	if response.ptr != nil {
		C.free_host_buffer(response.ptr, response.len)
	}
	if len(rawResponse) == 0 {
		return nil, fmt.Errorf("host callback %s returned no response, code=%d", method, int(callCode))
	}
	var env envelope
	if err := json.Unmarshal(rawResponse, &env); err != nil {
		return nil, fmt.Errorf("decode host callback envelope %s: %w", method, err)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("%s: %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host callback %s failed", method)
	}
	if callCode != 0 {
		return nil, fmt.Errorf("host callback %s returned code=%d", method, int(callCode))
	}
	return append(json.RawMessage(nil), env.Result...), nil
}

func logHost(level, message string, fields map[string]any) error {
	if fields == nil {
		fields = map[string]any{}
	}
	fields["plugin_id"] = pluginID
	_, err := callHost(pluginabi.MethodHostLog, map[string]any{
		"level":   level,
		"message": message,
		"fields":  fields,
	})
	return err
}

func (r *runner) setError(err error) {
	r.mu.Lock()
	r.lastError = err.Error()
	r.lastStatus = "error"
	r.mu.Unlock()
}

func (r *runner) setStatus(s string) {
	r.mu.Lock()
	r.lastStatus = s
	r.mu.Unlock()
}

func authKey(f authFileEntry) string {
	for _, v := range []string{f.ID, f.AuthIndex, f.Path, f.Name} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return fmt.Sprintf("%s:%s:%d", f.Provider, f.Name, f.Size)
}

func displayAuth(f authFileEntry) string {
	name := f.Name
	if name == "" && f.Path != "" {
		name = filepath.Base(f.Path)
	}
	if name == "" {
		name = f.AuthIndex
	}
	if f.Provider != "" {
		return name + " [" + f.Provider + "]"
	}
	return name
}

func statusPage() managementResponse {
	state.mu.Lock()
	data := map[string]any{
		"plugin":      pluginID,
		"version":     pluginVersion,
		"status":      state.lastStatus,
		"last_error":  state.lastError,
		"asked_count": state.asked,
		"last_ask":    state.lastAsk.Format(time.RFC3339),
		"config":      state.cfg,
	}
	state.mu.Unlock()
	raw, _ := json.MarshalIndent(data, "", "  ")
	return managementResponse{
		StatusCode: 200,
		Headers:    http.Header{"content-type": []string{"application/json; charset=utf-8"}},
		Body:       raw,
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func okEnvelope(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.Marshal(envelope{OK: true, Result: raw})
}

func errorEnvelope(code, message string) []byte {
	raw, _ := json.Marshal(envelope{OK: false, Error: &envelopeError{Code: code, Message: message}})
	return raw
}

func writeResponse(response *C.cliproxy_buffer, raw []byte) {
	if response == nil || len(raw) == 0 {
		return
	}
	ptr := C.CBytes(raw)
	if ptr == nil {
		return
	}
	response.ptr = ptr
	response.len = C.size_t(len(raw))
}

