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

extern int cliproxyPluginCall(char*, uint8_t*, size_t, cliproxy_buffer*);
extern void cliproxyPluginFree(void*, size_t);
extern void cliproxyPluginShutdown(void);

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
	"os"
	"sync"
	"time"
	"unsafe"
)

const abiVersion uint32 = 1
const pluginID = "openrouter-free-sync"
const pluginVersion = "0.1.0"

// --- JSON envelope ---

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
	ConfigYAML []byte `json:"config_yaml"` // auto base64 decoded by Go
}

// --- Management API types ---

type managementRequest struct {
	Method  string              `json:"Method"`
	Path    string              `json:"Path"`
	Headers map[string][]string `json:"Headers,omitempty"`
	Query   map[string][]string `json:"Query,omitempty"`
	Body    []byte              `json:"Body,omitempty"` // auto base64 decoded by Go
}

type managementResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers     map[string][]string `json:"Headers,omitempty"`
	Body        []byte              `json:"Body,omitempty"` // auto base64 encoded by Go
}

type managementRoute struct {
	Method string `json:"Method"`
	Path   string `json:"Path"`
}

type resourceRoute struct {
	Path        string `json:"Path"`
	Menu        string `json:"Menu"`
	Description string `json:"Description,omitempty"`
}

type managementRegistrationResponse struct {
	Routes    []managementRoute `json:"routes,omitempty"`
	Resources []resourceRoute    `json:"resources,omitempty"`
}

// --- Host HTTP types ---

type httpDoRequest struct {
	Method  string              `json:"method"`
	URL     string              `json:"url"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    []byte              `json:"body,omitempty"` // auto base64 by Go
}

type httpDoResponse struct {
	StatusCode int                 `json:"StatusCode"`
	Headers     map[string][]string `json:"Headers,omitempty"`
	Body        []byte              `json:"Body,omitempty"` // auto base64 decoded by Go
}

// --- Global state ---

var (
	stateMu   sync.Mutex
	cfg       PluginConfig
	ticker    *time.Ticker
	tickerDone chan struct{}
	cfgLoaded bool
)

func main() {}

//export cliproxy_plugin_init
func cliproxy_plugin_init(host *C.cliproxy_host_api, plugin *C.cliproxy_plugin_api) C.int {
	if plugin == nil {
		return 1
	}
	C.store_host_api(host)
	plugin.abi_version = C.uint32_t(abiVersion)
	plugin.call = C.cliproxy_plugin_call_fn(C.cliproxyPluginCall)
	plugin.free_buffer = C.cliproxy_plugin_free_fn(C.cliproxyPluginFree)
	plugin.shutdown = C.cliproxy_plugin_shutdown_fn(C.cliproxyPluginShutdown)
	return 0
}

//export cliproxyPluginCall
func cliproxyPluginCall(method *C.char, request *C.uint8_t, requestLen C.size_t, response *C.cliproxy_buffer) C.int {
	if response != nil {
		response.ptr = nil
		response.len = 0
	}
	if method == nil {
		writeResponse(response, errorEnvelope("invalid_method", "method is required"))
		return 1
	}

	var requestBytes []byte
	if request != nil && requestLen > 0 {
		requestBytes = C.GoBytes(unsafe.Pointer(request), C.int(requestLen))
	}

	raw, err := handleMethod(C.GoString(method), requestBytes)
	if err != nil {
		writeResponse(response, errorEnvelope("plugin_error", err.Error()))
		return 1
	}
	writeResponse(response, raw)
	return 0
}

//export cliproxyPluginFree
func cliproxyPluginFree(ptr unsafe.Pointer, len C.size_t) {
	if ptr != nil {
		C.free(ptr)
	}
	_ = len
}

//export cliproxyPluginShutdown
func cliproxyPluginShutdown() {
	stopTicker()
}

// --- Method dispatch ---

func handleMethod(method string, requestBytes []byte) ([]byte, error) {
	switch method {
	case "plugin.register", "plugin.reconfigure":
		return handlePluginRegister(method, requestBytes)
	case "management.register":
		return handleManagementRegister()
	case "management.handle":
		return handleManagementHandle(requestBytes)
	default:
		return errorEnvelope("unknown_method", "unknown method: "+method), nil
	}
}

func handlePluginRegister(method string, requestBytes []byte) ([]byte, error) {
	// Parse lifecycle request: {"config_yaml": "<base64 yaml>"}
	var lcReq lifecycleRequest
	var cfgYAML []byte
	if len(requestBytes) > 0 {
		if err := json.Unmarshal(requestBytes, &lcReq); err == nil {
			cfgYAML = lcReq.ConfigYAML
		}
	}

	newCfg, err := parseConfig(cfgYAML)
	if err != nil {
		hostLog("warn", "openrouter-free-sync: config parse failed, using defaults: "+err.Error())
		newCfg = defaultConfig()
	}

	stateMu.Lock()
	cfg = newCfg
	cfgLoaded = true
	stateMu.Unlock()

	// Restart ticker with new config
	stopTicker()
	startTicker(newCfg)

	// Build registration response
	registration := map[string]interface{}{
		"schema_version": 1,
		"metadata": map[string]interface{}{
			"Name":             pluginID,
			"Version":          pluginVersion,
			"Author":           "guofukun",
			"GitHubRepository": "https://github.com/guofukun/openrouter-free-sync",
			"ConfigFields":     configFields(),
		},
		"capabilities": map[string]interface{}{
			"management_api": true,
		},
	}
	return okEnvelope(registration)
}

func handleManagementRegister() ([]byte, error) {
	resp := managementRegistrationResponse{
		Routes: []managementRoute{
			{Method: "POST", Path: "/plugins/" + pluginID + "/refresh"},
			{Method: "GET", Path: "/plugins/" + pluginID + "/status"},
			{Method: "GET", Path: "/plugins/" + pluginID + "/config"},
			{Method: "PUT", Path: "/plugins/" + pluginID + "/config"},
		},
		Resources: []resourceRoute{
			{Path: "/panel", Menu: "OpenRouter Free Sync", Description: "Configure and sync free OpenRouter models into CPA."},
		},
	}
	return okEnvelope(resp)
}

func handleManagementHandle(requestBytes []byte) ([]byte, error) {
	var req managementRequest
	if err := json.Unmarshal(requestBytes, &req); err != nil {
		return errorEnvelope("invalid_request", err.Error()), nil
	}

	// Dispatch based on path
	switch {
	case req.Path == "/v0/resource/plugins/"+pluginID+"/panel":
		return okEnvelope(renderPanelResponse(pluginID))
	case req.Method == "POST" && req.Path == "/v0/management/plugins/"+pluginID+"/refresh":
		return okEnvelope(handleRefresh())
	case req.Method == "GET" && req.Path == "/v0/management/plugins/"+pluginID+"/status":
		return okEnvelope(handleStatus())
	case req.Method == "GET" && req.Path == "/v0/management/plugins/"+pluginID+"/config":
		return okEnvelope(handleGetConfig())
	case req.Method == "PUT" && req.Path == "/v0/management/plugins/"+pluginID+"/config":
		return okEnvelope(handlePutConfig(req))
	default:
		return errorEnvelope("not_found", "no handler for "+req.Method+" "+req.Path), nil
	}
}

// --- Management handlers ---

func handleRefresh() managementResponse {
	stateMu.Lock()
	currentCfg := cfg
	stateMu.Unlock()

	result := runSync(currentCfg)
	setLastSync(result)

	return mgmtFromSync(result)
}

func handleStatus() managementResponse {
	s := getLastSync()
	return mgmtJSON(s)
}

func handleGetConfig() managementResponse {
	stateMu.Lock()
	c := cfg
	stateMu.Unlock()

	// Convert excluded_providers list to comma string for display
	out := map[string]interface{}{
		"refresh_interval":      c.RefreshInterval,
		"min_context_length":    c.MinContextLength,
		"pricing_filter":        c.PricingFilter,
		"excluded_providers":    joinProviders(c.ExcludedProviders),
		"require_text_output":   c.RequireTextOutput,
		"require_tools_support": c.RequireToolsSupport,
		"openrouter_api_key":    c.OpenRouterAPIKey,
		"openrouter_base_url":   c.OpenRouterBaseURL,
		"provider_name":         c.ProviderName,
		"management_key":        c.ManagementKey,
		"cpa_base_url":          c.CPABaseURL,
	}
	return mgmtJSON(out)
}

func handlePutConfig(req managementRequest) managementResponse {
	body := req.Body
	if len(body) == 0 {
		return errorResponse(400, "empty body")
	}

	var incoming map[string]interface{}
	if err := json.Unmarshal(body, &incoming); err != nil {
		return errorResponse(400, "invalid json: "+err.Error())
	}

	stateMu.Lock()
	newCfg := cfg // start from current

	if v, ok := incoming["refresh_interval"].(string); ok {
		newCfg.RefreshInterval = v
	}
	if v, ok := incoming["min_context_length"].(float64); ok {
		newCfg.MinContextLength = int(v)
	}
	if v, ok := incoming["pricing_filter"].(string); ok {
		newCfg.PricingFilter = v
	}
	if v, ok := incoming["excluded_providers"].(string); ok {
		newCfg.ExcludedProviders = splitProviders(v)
	} else if v, ok := incoming["excluded_providers"].([]interface{}); ok {
		newCfg.ExcludedProviders = toStringSlice(v)
	}
	if v, ok := incoming["require_text_output"].(bool); ok {
		newCfg.RequireTextOutput = v
	}
	if v, ok := incoming["require_tools_support"].(bool); ok {
		newCfg.RequireToolsSupport = v
	}
	if v, ok := incoming["openrouter_api_key"].(string); ok {
		newCfg.OpenRouterAPIKey = v
	}
	if v, ok := incoming["openrouter_base_url"].(string); ok {
		newCfg.OpenRouterBaseURL = v
	}
	if v, ok := incoming["provider_name"].(string); ok {
		newCfg.ProviderName = v
	}
	if v, ok := incoming["management_key"].(string); ok {
		newCfg.ManagementKey = v
	}
	if v, ok := incoming["cpa_base_url"].(string); ok {
		newCfg.CPABaseURL = v
	}

	cfg = newCfg
	stateMu.Unlock()

	// Restart ticker
	stopTicker()
	startTicker(newCfg)

	hostLog("info", "openrouter-free-sync: config updated, ticker restarted")

	return mgmtJSON(map[string]string{"status": "ok"})
}

// --- Ticker management ---

func startTicker(c PluginConfig) {
	d := c.RefreshDuration()
	tickerDone = make(chan struct{})
	ticker = time.NewTicker(d)

	go func() {
		hostLog("info", fmt.Sprintf("openrouter-free-sync: ticker started, interval=%s", formatDuration(d)))
		// Run once immediately on start
		result := runSync(c)
		setLastSync(result)

		for {
			select {
			case <-tickerDone:
				return
			case <-ticker.C:
				stateMu.Lock()
				currentCfg := cfg
				stateMu.Unlock()
				result := runSync(currentCfg)
				setLastSync(result)
			}
		}
	}()
}

func stopTicker() {
	if ticker != nil {
		ticker.Stop()
	}
	if tickerDone != nil {
		close(tickerDone)
		tickerDone = nil
	}
	ticker = nil
}

// --- Host callback helpers ---

func callHost(method string, payload []byte) ([]byte, error) {
	cMethod := C.CString(method)
	defer C.free(unsafe.Pointer(cMethod))

	var req *C.uint8_t
	var reqLen C.size_t
	if len(payload) > 0 {
		req = (*C.uint8_t)(C.CBytes(payload))
		defer C.free(unsafe.Pointer(req))
		reqLen = C.size_t(len(payload))
	}

	var response C.cliproxy_buffer
	if C.call_host_api(cMethod, req, reqLen, &response) != 0 {
		return nil, fmt.Errorf("host call failed: %s", method)
	}
	defer C.free_host_buffer(response.ptr, response.len)

	if response.ptr == nil || response.len == 0 {
		return nil, nil
	}

	return C.GoBytes(unsafe.Pointer(response.ptr), C.int(response.len)), nil
}

func callHostJSON(method string, payload interface{}) (json.RawMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	respBytes, err := callHost(method, data)
	if err != nil {
		return nil, err
	}
	if respBytes == nil {
		return nil, nil
	}

	var env envelope
	if err := json.Unmarshal(respBytes, &env); err != nil {
		return nil, fmt.Errorf("parse host response: %w", err)
	}
	if !env.OK {
		if env.Error != nil {
			return nil, fmt.Errorf("host error: %s - %s", env.Error.Code, env.Error.Message)
		}
		return nil, fmt.Errorf("host error: unknown")
	}
	return env.Result, nil
}

func hostHTTPDo(req httpDoRequest) (*httpDoResponse, error) {
	result, err := callHostJSON("host.http.do", req)
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, fmt.Errorf("empty host.http.do response")
	}
	var resp httpDoResponse
	if err := json.Unmarshal(result, &resp); err != nil {
		return nil, fmt.Errorf("parse http response: %w", err)
	}
	return &resp, nil
}

func hostLog(level, message string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"level":   level,
		"message": message,
		"fields":  map[string]string{"plugin": pluginID},
	})
	_, _ = callHost("host.log", payload)
}

// --- Helpers ---

func osGetenv(key string) string {
	return os.Getenv(key)
}

func jsonMarshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// --- Envelope helpers ---

func okEnvelope(v interface{}) ([]byte, error) {
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

// --- Management response helpers ---

func mgmtJSON(v interface{}) managementResponse {
	data, _ := json.Marshal(v)
	return managementResponse{
		StatusCode: 200,
		Headers:   map[string][]string{"Content-Type": {"application/json"}},
		Body:      data,
	}
}

func mgmtFromSync(r SyncResult) managementResponse {
	return mgmtJSON(r)
}

// --- String helpers (in panel.go's domain but here for package cohesion) ---

func splitProviders(s string) []string {
	return splitAndTrim(s, ",")
}

func toStringSlice(v []interface{}) []string {
	result := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}
