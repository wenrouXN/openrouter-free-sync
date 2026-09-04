package main

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"
)

// renderPanel returns the HTML for the plugin's web panel.
// The page reads the management key from localStorage and calls
// the plugin's management API routes for config/models/refresh.
func renderPanel(pluginID string) string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>OpenRouter Free Sync</title>
<style>
:root{--bg:#0f1115;--card:#181b22;--border:#2a2d37;--text:#e0e0e0;--muted:#8b8d97;--accent:#4f9cf9;--green:#3fb950;--red:#f85149}
*{box-sizing:border-box;margin:0;padding:0}
body{font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:var(--bg);color:var(--text);padding:20px;max-width:900px;margin:0 auto}
h1{font-size:20px;margin-bottom:4px}
.sub{color:var(--muted);font-size:13px;margin-bottom:20px}
.card{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:16px;margin-bottom:16px}
.card h2{font-size:15px;margin-bottom:12px}
.row{display:flex;gap:12px;align-items:center;margin-bottom:8px}
.row label{width:180px;font-size:13px;color:var(--muted)}
.row input,.row select{flex:1;background:#11141a;border:1px solid var(--border);color:var(--text);border-radius:4px;padding:6px 10px;font-size:13px}
.btn{background:var(--accent);color:#fff;border:none;border-radius:4px;padding:8px 16px;cursor:pointer;font-size:13px}
.btn:hover{opacity:0.85}
.btn:disabled{opacity:0.4;cursor:not-allowed}
.badge{display:inline-block;padding:2px 8px;border-radius:10px;font-size:12px}
.badge.ok{background:rgba(63,185,80,.15);color:var(--green)}
.badge.fail{background:rgba(248,81,73,.15);color:var(--red)}
table{width:100%;border-collapse:collapse;font-size:13px}
th{text-align:left;color:var(--muted);padding:6px 8px;border-bottom:1px solid var(--border);font-weight:500}
td{padding:6px 8px;border-bottom:1px solid var(--border)}
.sync-info{font-size:13px;color:var(--muted);margin-bottom:12px}
.spinner{display:inline-block;width:14px;height:14px;border:2px solid var(--border);border-top-color:var(--accent);border-radius:50%;animation:spin .6s linear infinite;margin-right:6px;vertical-align:-2px}
@keyframes spin{to{transform:rotate(360deg)}}
</style>
</head>
<body>
<h1>🔄 OpenRouter Free Sync</h1>
<p class="sub">Auto-sync free OpenRouter models into CPA openai-compatibility provider</p>

<div class="card">
  <h2>Sync Status</h2>
  <div id="syncInfo" class="sync-info">Loading…</div>
  <button class="btn" id="refreshBtn" onclick="doRefresh()">🔄 Sync Now</button>
</div>

<div class="card">
  <h2>Configuration</h2>
  <div id="configForm">Loading…</div>
  <div style="margin-top:12px">
    <button class="btn" onclick="saveConfig()">Save Config</button>
  </div>
</div>

<div class="card">
  <h2>Synced Models (<span id="modelCount">0</span>)</h2>
  <table id="modelTable">
    <thead><tr><th>#</th><th>Model ID</th></tr></thead>
    <tbody></tbody>
  </table>
</div>

<script>
const PLUGIN = "` + pluginID + `";
const BASE = "/v0/management/plugins/" + PLUGIN;
let mgmtKey = "";

// Read management key from localStorage (same-origin Management Center)
try { mgmtKey = localStorage.getItem("mgmtKey") || localStorage.getItem("management_key") || ""; } catch(e) {}

function headers(json) {
  let h = {"Content-Type":["application/json"]};
  if (mgmtKey) h["Authorization"] = ["Bearer " + mgmtKey];
  return h;
}

async function api(method, path, body) {
  let opts = {method, headers: headers()};
  if (body) opts.body = JSON.stringify(body);
  let r = await fetch(BASE + path, opts);
  if (!r.ok) throw new Error(r.status + " " + await r.text());
  return r.json();
}

async function loadStatus() {
  try {
    let d = await api("GET", "/status");
    let el = document.getElementById("syncInfo");
    if (d.success) {
      el.innerHTML = '<span class="badge ok">OK</span> Last synced: ' + new Date(d.synced_at).toLocaleString() + ' · ' + d.model_count + ' models';
    } else if (d.error) {
      el.innerHTML = '<span class="badge fail">Error</span> ' + d.error;
    } else {
      el.innerHTML = '<span class="badge fail">Never</span> No sync yet';
    }
    document.getElementById("modelCount").textContent = d.model_count || 0;
    let tb = document.querySelector("#modelTable tbody");
    tb.innerHTML = (d.models||[]).map((m,i) => '<tr><td>'+i+'</td><td>'+m.name+'</td></tr>').join("");
  } catch(e) {
    document.getElementById("syncInfo").innerHTML = '<span class="badge fail">Error</span> ' + e.message;
  }
}

async function loadConfig() {
  try {
    let d = await api("GET", "/config");
    let f = document.getElementById("configForm");
    let fields = [
      ["refresh_interval","text"],["min_context_length","number"],["pricing_filter","text"],
      ["excluded_providers","text"],["require_text_output","checkbox"],["require_tools_support","checkbox"],
      ["openrouter_api_key","password"],["openrouter_base_url","text"],["provider_name","text"],
      ["management_key","password"],["cpa_base_url","text"]
    ];
    f.innerHTML = fields.map(([k,t]) => {
      let v = d[k];
      if (t === "checkbox") return '<div class="row"><label>'+k+'</label><input type="checkbox" id="cfg_'+k+'" '+(v?'checked':'')+'></div>';
      let type = t === "password" ? "password" : "text";
      return '<div class="row"><label>'+k+'</label><input type="'+type+'" id="cfg_'+k+'" value="'+(v||'')+'"></div>';
    }).join("");
  } catch(e) {
    document.getElementById("configForm").innerHTML = '<span class="badge fail">Error</span> ' + e.message;
  }
}

async function saveConfig() {
  let fields = ["refresh_interval","min_context_length","pricing_filter","excluded_providers",
    "require_text_output","require_tools_support","openrouter_api_key","openrouter_base_url",
    "provider_name","management_key","cpa_base_url"];
  let body = {};
  fields.forEach(k => {
    let el = document.getElementById("cfg_"+k);
    if (el.type === "checkbox") body[k] = el.checked;
    else if (k === "min_context_length") body[k] = parseInt(el.value)||0;
    else body[k] = el.value;
  });
  // excluded_providers: split by comma
  body["excluded_providers"] = body["excluded_providers"].split(",").map(s=>s.trim()).filter(Boolean);
  try {
    await api("PUT", "/config", body);
    alert("Config saved. Sync will restart with new settings.");
    loadStatus();
  } catch(e) { alert("Save failed: " + e.message); }
}

async function doRefresh() {
  let btn = document.getElementById("refreshBtn");
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> Syncing…';
  try {
    let d = await api("POST", "/refresh");
    if (d.success) {
      alert("Synced " + d.model_count + " models successfully.");
    } else {
      alert("Sync failed: " + (d.error||"unknown"));
    }
    loadStatus();
  } catch(e) {
    alert("Refresh failed: " + e.message);
  }
  btn.disabled = false;
  btn.innerHTML = "🔄 Sync Now";
}

loadStatus();
loadConfig();
</script>
</body>
</html>`
}

// renderPanelResponse builds a management response for the HTML panel.
func renderPanelResponse(pluginID string) managementResponse {
	html := renderPanel(pluginID)
	return managementResponse{
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
		Body:       base64.StdEncoding.EncodeToString([]byte(html)),
	}
}

// jsonResponse builds a management response from a Go value.
func jsonResponse(v interface{}) (managementResponse, error) {
	data, err := jsonMarshal(v)
	if err != nil {
		return managementResponse{StatusCode: 500, Headers: map[string][]string{"Content-Type": {"application/json"}}, Body: base64.StdEncoding.EncodeToString([]byte(`{"error":"` + err.Error() + `"}`))}, err
	}
	return managementResponse{
		StatusCode: 200,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       base64.StdEncoding.EncodeToString(data),
	}, nil
}

// errorResponse builds a management error response.
func errorResponse(code int, msg string) managementResponse {
	body, _ := jsonMarshal(map[string]string{"error": msg})
	return managementResponse{
		StatusCode: code,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       base64.StdEncoding.EncodeToString(body),
	}
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d >= 24*time.Hour {
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
	if d >= time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	if d >= time.Minute {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return d.String()
}

// joinProviders joins excluded providers for display.
func joinProviders(p []string) string {
	return strings.Join(p, ",")
}
