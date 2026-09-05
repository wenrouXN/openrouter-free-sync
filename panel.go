package main

import (
	"fmt"
	"strings"
	"time"
)

// renderPanel returns the HTML for the plugin's web panel.
func renderPanel(pluginID string) string {
	return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>OpenRouter Free Sync</title>
<style>
:root{--bg:#0f1115;--card:#181b22;--border:#2a2d37;--text:#e0e0e0;--muted:#8b8d97;--accent:#4f9cf9;--green:#3fb950;--red:#f85149;--yellow:#d29922}
*{box-sizing:border-box;margin:0;padding:0}
body{font:14px/1.5 -apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:var(--bg);color:var(--text);padding:20px;max-width:1100px;margin:0 auto}
h1{font-size:20px;margin-bottom:4px}
.sub{color:var(--muted);font-size:13px;margin-bottom:20px}
.card{background:var(--card);border:1px solid var(--border);border-radius:8px;padding:16px;margin-bottom:16px}
.card h2{font-size:15px;margin-bottom:12px}
.row{display:flex;gap:12px;align-items:center;margin-bottom:8px}
.row label{width:220px;font-size:13px;color:var(--muted)}
.row input,.row select{flex:1;background:#11141a;border:1px solid var(--border);color:var(--text);border-radius:4px;padding:6px 10px;font-size:13px}
.btn{background:var(--accent);color:#fff;border:none;border-radius:4px;padding:8px 16px;cursor:pointer;font-size:13px}
.btn:hover{opacity:0.85}
.btn:disabled{opacity:0.4;cursor:not-allowed}
.badge{display:inline-block;padding:2px 8px;border-radius:10px;font-size:12px;white-space:nowrap}
.badge.ok{background:rgba(63,185,80,.15);color:var(--green)}
.badge.fail{background:rgba(248,81,73,.15);color:var(--red)}
.badge.warn{background:rgba(210,153,34,.15);color:var(--yellow)}
.badge.muted{background:rgba(139,141,151,.15);color:var(--muted)}
table{width:100%;border-collapse:collapse;font-size:12.5px}
th{text-align:left;color:var(--muted);padding:6px 8px;border-bottom:1px solid var(--border);font-weight:500;white-space:nowrap}
td{padding:6px 8px;border-bottom:1px solid var(--border);vertical-align:top}
td.mono{font-family:ui-monospace,Menlo,monospace;font-size:12px}
.sync-info{font-size:13px;color:var(--muted);margin-bottom:12px}
.stat{display:inline-block;margin-right:18px}
.stat b{color:var(--text);font-size:16px}
.stat span{color:var(--muted);font-size:12px}
.spinner{display:inline-block;width:14px;height:14px;border:2px solid var(--border);border-top-color:var(--accent);border-radius:50%;animation:spin .6s linear infinite;margin-right:6px;vertical-align:-2px}
@keyframes spin{to{transform:rotate(360deg)}}
.tabs{display:flex;gap:4px;margin-bottom:12px}
.tab{background:transparent;border:1px solid var(--border);border-radius:4px;padding:5px 14px;cursor:pointer;color:var(--muted);font-size:13px}
.tab.active{background:var(--accent);border-color:var(--accent);color:#fff}
.hidden{display:none}
.toolbar{display:flex;justify-content:space-between;align-items:center;margin-bottom:10px}
.log-count{color:var(--muted);font-size:12px}
tr.dead td{opacity:0.45}
.qr{color:var(--red);font-size:11.5px;margin-top:2px}
</style>
</head>
<body>
<h1>🔄 OpenRouter Free Sync</h1>
<p class="sub">Auto-sync free OpenRouter models into CPA openai-compatibility provider · v0.5.0</p>

<div class="card">
  <h2>Management Key</h2>
  <div class="row">
    <label>CPA Management Key</label>
    <input type="password" id="mgmtKeyInput" placeholder="paste CPA management key (saved to localStorage)">
    <button class="btn" onclick="saveKey()">Save</button>
    <span id="keyStatus"></span>
  </div>
</div>

<div class="card">
  <h2>Sync Status</h2>
  <div id="stats" class="sync-info">Loading…</div>
  <button class="btn" id="refreshBtn" onclick="doRefresh()">🔄 Sync Now</button>
</div>

<div class="card">
  <div class="tabs">
    <button class="tab active" onclick="switchTab('models',this)">Models</button>
    <button class="tab" onclick="switchTab('audit',this)">Audit Log</button>
    <button class="tab" onclick="switchTab('config',this)">Config</button>
  </div>

  <div id="tab-models">
    <div class="toolbar">
      <span class="log-count" id="modelToolbarInfo"></span>
      <span>
        <button class="btn" onclick="doPreview()" style="margin-right:6px">🔍 Preview Filters</button>
        <a class="btn" href="/v0/management/plugins/openrouter-free-sync/audit?format=csv&limit=1000" style="text-decoration:none;display:inline-block">⬇ Export CSV</a>
      </span>
    </div>
    <div id="previewBox" class="hidden" style="background:#11141a;border:1px solid var(--border);border-radius:6px;padding:10px;margin-bottom:10px;font-size:12.5px"></div>
    <table id="modelTable">
      <thead><tr>
        <th>Status</th><th>Model ID</th><th>Context</th><th>Modality</th>
        <th>Price in/out ($/M)</th><th>Tools</th><th>Probe</th><th>Added</th><th></th>
      </tr></thead>
      <tbody></tbody>
    </table>
  </div>

  <div id="tab-audit" class="hidden">
    <div class="toolbar">
      <span class="log-count" id="auditCount"></span>
      <span>
        <select id="auditFilter" onchange="loadAudit()" style="background:#11141a;border:1px solid var(--border);color:var(--text);border-radius:4px;padding:4px 8px;font-size:12px">
          <option value="">all events</option>
          <option value="model_added">model_added</option>
          <option value="model_removed">model_removed</option>
          <option value="model_quarantined">model_quarantined</option>
          <option value="model_recovered">model_recovered</option>
          <option value="model_readded">model_readded</option>
          <option value="sync">sync</option>
          <option value="config_updated">config_updated</option>
        </select>
      </span>
    </div>
    <table id="auditTable">
      <thead><tr><th style="width:160px">Time</th><th style="width:150px">Type</th><th>Model</th><th>Detail</th></tr></thead>
      <tbody></tbody>
    </table>
  </div>

  <div id="tab-config" class="hidden">
    <div id="configForm">Loading…</div>
    <div style="margin-top:12px"><button class="btn" onclick="saveConfig()">Save Config</button></div>
  </div>
</div>

<script>
const PLUGIN = "openrouter-free-sync";
const BASE = "/v0/management/plugins/" + PLUGIN;
let mgmtKey = "";
try { mgmtKey = localStorage.getItem("mgmtKey") || localStorage.getItem("management_key") || localStorage.getItem("managementKey") || ""; } catch(e) {}

function loadKeyUI() {
  let inp = document.getElementById("mgmtKeyInput");
  let st = document.getElementById("keyStatus");
  if (document.activeElement !== inp) inp.value = mgmtKey;
  st.innerHTML = mgmtKey ? '<span class="badge ok">key set</span>' : '<span class="badge fail">no key — API calls will 401</span>';
}
function saveKey() {
  mgmtKey = document.getElementById("mgmtKeyInput").value.trim();
  try { localStorage.setItem("mgmtKey", mgmtKey); } catch(e) {}
  loadKeyUI();
  loadStatus(); loadModels(); loadAudit(); loadConfig();
}

function headers() {
  let h = {"Content-Type":"application/json"};
  if (mgmtKey) h["Authorization"] = "Bearer " + mgmtKey;
  return h;
}
async function api(method, path, body) {
  let opts = {method, headers: headers()};
  if (body) opts.body = JSON.stringify(body);
  let r = await fetch(BASE + path, opts);
  if (r.status === 401) throw new Error("401 unauthorized — paste the CPA management key above and Save");
  if (!r.ok) throw new Error(r.status + " " + (await r.text()).slice(0,200));
  return r.json();
}
function esc(s){return (s==null?"":String(s)).replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;")}
function ctxFmt(n){ if(!n) return "-"; if(n>=1e6) return (n/1e6).toFixed(1)+"M"; return Math.round(n/1e3)+"K"; }
function priceFmt(p){ return p==="0"?"free":p; }
function tFmt(iso){ try{return new Date(iso).toLocaleString();}catch(e){return iso||"-";} }

function switchTab(name, btn) {
  document.querySelectorAll(".tab").forEach(b=>b.classList.remove("active"));
  btn.classList.add("active");
  ["models","audit","config"].forEach(t=>document.getElementById("tab-"+t).classList.toggle("hidden", t!==name));
}

async function loadStatus() {
  try {
    let d = await api("GET", "/status");
    let ls = d.last_sync_meta || {};
    let badge = ls.success===true ? '<span class="badge ok">OK</span>'
              : ls.error ? '<span class="badge fail">Error</span>' : '<span class="badge muted">Never synced</span>';
    let when = ls.time ? tFmt(ls.time) : "-";
    let nextLine = d.next_sync_at ? '<br><span class="log-count">next sync: ' + tFmt(d.next_sync_at) + '</span>' : '';
    document.getElementById("stats").innerHTML =
      badge + ' <span class="log-count">last: '+when+(ls.error? ' · '+esc(ls.error):'')+'</span>'
      + '<br><br>'
      + '<span class="stat"><b>'+d.models_active+'</b> <span>active</span></span>'
      + '<span class="stat"><b>'+(d.models_quarantined||0)+'</b> <span>quarantined</span></span>'
      + '<span class="stat"><b>'+(d.models_total||0)+'</b> <span>tracked total</span></span>'
      + '<span class="stat"><b>'+(d.audit_count||0)+'</b> <span>audit events</span></span>' + nextLine;
  } catch(e) {
    document.getElementById("stats").innerHTML = '<span class="badge fail">Error</span> ' + esc(e.message);
  }
}

async function loadModels() {
  try {
    let d = await api("GET", "/models");
    let tb = document.querySelector("#modelTable tbody");
    let models = d.models || [];
    models.sort((a,b)=> (b.context_length||0)-(a.context_length||0));
    tb.innerHTML = models.map(m => {
      let status, cls="";
      if (!m.active) status = '<span class="badge muted">removed</span>';
      else if (m.quarantine_reason) { status = '<span class="badge fail">quarantined</span>'; cls=' class="dead"'; }
      else status = '<span class="badge ok">active</span>';
      let probe = "-";
      if (m.last_probe_at) {
        let pb;
        if (m.last_probe_ok) pb = '<span class="badge ok">OK</span>';
        else if (m.last_probe_status===401||m.last_probe_status===403) pb = '<span class="badge muted">restricted</span>';
        else pb = '<span class="badge warn">FAIL</span>';
        probe = pb
          + '<br><span class="log-count">'+(m.last_probe_status||'')+' · '+(m.last_probe_latency_ms||0)+'ms · '+tFmt(m.last_probe_at)+'</span>';
        if (!m.last_probe_ok && m.last_probe_error) probe += '<div class="qr">'+esc(m.last_probe_error.slice(0,120))+'</div>';
      }
      let failInfo = m.fail_count>0 ? '<div class="qr">fails: '+m.fail_count+'</div>' : '';
      let hist = '';
      if (m.probe_history && m.probe_history.length) {
        let dots = m.probe_history.slice(-12).map(h => h.ok ? '<span style="color:var(--green)">●</span>' : '<span style="color:var(--yellow)">●</span>').join('');
        let lastH = m.probe_history[m.probe_history.length-1];
        hist = '<div class="log-count" title="recent probes (oldest→newest)">'+dots+' '+lastH.latency_ms+'ms</div>';
      }
      let probeBtn = m.active ? '<button class="btn" style="padding:3px 8px;font-size:11px" onclick="probeOne(\''+m.id+'\')">probe</button>' : '';
      return '<tr'+cls+'><td>'+status+failInfo+hist+'</td>'
        + '<td class="mono">'+esc(m.id)+(m.display_name?'<br><span class="log-count">'+esc(m.display_name)+'</span>':'')+'</td>'
        + '<td>'+ctxFmt(m.context_length)+'</td>'
        + '<td class="mono">'+esc(m.modality||"-")+'</td>'
        + '<td>'+priceFmt(m.input_price)+' / '+priceFmt(m.output_price)+'</td>'
        + '<td>'+(m.tools?'<span class="badge ok">tools</span>':'<span class="badge muted">no</span>')+'</td>'
        + '<td>'+probe+'</td>'
        + '<td class="log-count">'+tFmt(m.added_at)+'</td>'
        + '<td>'+probeBtn+'</td></tr>';
    }).join("") || '<tr><td colspan="9" class="log-count">no models tracked yet — run a sync</td></tr>';
  } catch(e) {
    document.querySelector("#modelTable tbody").innerHTML = '<tr><td colspan="9"><span class="badge fail">Error</span> '+esc(e.message)+'</td></tr>';
  }
}

async function loadAudit() {
  try {
    let f = document.getElementById("auditFilter").value;
    let d = await api("GET", "/audit?limit=300");
    let log = (d.log||[]).filter(e => !f || e.type===f).reverse();
    document.getElementById("auditCount").textContent = d.count + " events (showing " + log.length + ", newest first)";
    document.querySelector("#auditTable tbody").innerHTML = log.map(e => {
      let cls = e.type==="model_quarantined"||e.type==="model_removed" ? '<span class="badge fail">'
              : e.type==="model_added"||e.type==="model_recovered"||e.type==="model_readded" ? '<span class="badge ok">'
              : e.type==="sync" ? '<span class="badge muted">' : '<span class="badge warn">';
      return '<tr><td class="log-count">'+tFmt(e.time)+'</td><td>'+cls+esc(e.type)+'</span></td>'
        + '<td class="mono">'+esc(e.model||"-")+'</td><td class="log-count">'+esc(e.detail||"")+'</td></tr>';
    }).join("") || '<tr><td colspan="4" class="log-count">no events</td></tr>';
  } catch(e) {
    document.querySelector("#auditTable tbody").innerHTML = '<tr><td colspan="4"><span class="badge fail">Error</span> '+esc(e.message)+'</td></tr>';
  }
}

const CFG_FIELDS = [
  ["refresh_interval","text"],["min_context_length","number"],["pricing_filter","text"],
  ["excluded_providers","text"],["require_text_output","checkbox"],["require_tools_support","checkbox"],
  ["openrouter_api_key","password"],["openrouter_base_url","text"],["provider_name","text"],
  ["management_key","password"],["cpa_base_url","text"],
  ["availability_check","checkbox"],["availability_fail_threshold","number"],
  ["probe_interval_ms","number"],["probe_cooldown_min","number"],
  ["audit_sync_always","checkbox"],["audit_max_entries","number"],["prune_after_days","number"],
  ["exclude_models","text"],["force_include","text"],
  ["alias_prefix","text"],["alias_overrides","text"],
  ["require_input_modality","text"],["require_params","text"],["probe_history_size","number"],
  ["state_path","text"]
];
async function loadConfig() {
  try {
    let d = await api("GET", "/settings");
    document.getElementById("configForm").innerHTML = CFG_FIELDS.map(([k,t]) => {
      let v = d[k];
      if (t==="checkbox") return '<div class="row"><label>'+k+'</label><input type="checkbox" id="cfg_'+k+'" '+(v?'checked':'')+'></div>';
      let type = t==="password"?"password":(t==="number"?"number":"text");
      return '<div class="row"><label>'+k+'</label><input type="'+type+'" id="cfg_'+k+'" value="'+esc(v==null?"":v)+'"></div>';
    }).join("");
  } catch(e) {
    document.getElementById("configForm").innerHTML = '<span class="badge fail">Error</span> '+esc(e.message);
  }
}
async function saveConfig() {
  let body = {};
  CFG_FIELDS.forEach(([k,t]) => {
    let el = document.getElementById("cfg_"+k);
    if (t==="checkbox") body[k] = el.checked;
    else if (t==="number") body[k] = parseInt(el.value)||0;
    else body[k] = el.value;
  });
  body["excluded_providers"] = body["excluded_providers"].split(",").map(s=>s.trim()).filter(Boolean);
  try {
    await api("PUT", "/settings", body);
    alert("Config saved.");
    loadStatus(); loadAudit();
  } catch(e) { alert("Save failed: " + e.message); }
}

async function doRefresh() {
  let btn = document.getElementById("refreshBtn");
  btn.disabled = true;
  btn.innerHTML = '<span class="spinner"></span> Syncing…';
  try {
    let d = await api("POST", "/refresh");
    if (d.success) alert("Synced "+d.model_count+" models (added="+d.added+" removed="+d.removed+" quarantined="+d.quarantined+" recovered="+d.recovered+" probed="+d.probed+")");
    else alert("Sync failed: " + (d.error||"unknown"));
  } catch(e) { alert("Refresh failed: " + e.message); }
  btn.disabled = false; btn.innerHTML = "🔄 Sync Now";
  loadStatus(); loadModels(); loadAudit();
}

async function doPreview() {
  let box = document.getElementById("previewBox");
  box.classList.remove("hidden");
  box.innerHTML = '<span class="spinner"></span> Previewing…';
  // send current form values (if config tab was edited) so preview reflects unsaved edits
  let overrides = {};
  CFG_FIELDS.forEach(([k,t]) => {
    let el = document.getElementById("cfg_"+k);
    if (!el) return;
    if (t==="checkbox") overrides[k] = el.checked;
    else if (t==="number") overrides[k] = parseInt(el.value)||0;
    else overrides[k] = el.value;
  });
  overrides["excluded_providers"] = (overrides["excluded_providers"]||"").split(",").map(s=>s.trim()).filter(Boolean);
  overrides["exclude_models"] = (overrides["exclude_models"]||"").split(",").map(s=>s.trim()).filter(Boolean);
  overrides["force_include"] = (overrides["force_include"]||"").split(",").map(s=>s.trim()).filter(Boolean);
  overrides["require_params"] = (overrides["require_params"]||"").split(",").map(s=>s.trim()).filter(Boolean);
  try {
    let r = await fetch(BASE + "/preview", {method:"POST", headers: headers(), body: JSON.stringify(overrides)});
    if (!r.ok) throw new Error(r.status + " " + (await r.text()).slice(0,150));
    let d = await r.json();
    let add = (d.would_add||[]), rem = (d.would_remove||[]);
    box.innerHTML = '<b>'+(d.would_sync||[]).length+'</b> of '+d.total_catalog+' catalog models would sync'
      + ' · <span style="color:var(--green)">+'+add.length+' to add</span>'
      + ' · <span style="color:var(--red)">-'+rem.length+' to remove</span>'
      + (add.length ? '<br><span class="log-count">add: '+esc(add.join(", "))+'</span>' : '')
      + (rem.length ? '<br><span class="log-count">remove: '+esc(rem.join(", "))+'</span>' : '')
      + '<br><span class="log-count">(dry-run only — nothing changed)</span>';
  } catch(e) {
    box.innerHTML = '<span class="badge fail">Error</span> ' + esc(e.message);
  }
}

async function probeOne(id) {
  if (!confirm("Probe " + id + " now?")) return;
  try {
    let d = await api("POST", "/probe", {model: id});
    alert((d.ok ? "OK " : "FAIL ") + d.status + " · " + d.latency_ms + "ms" + (d.error ? "\n" + d.error : ""));
    loadModels(); loadAudit();
  } catch(e) { alert("Probe failed: " + e.message); }
}

loadKeyUI(); loadStatus(); loadModels(); loadAudit(); loadConfig();
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
		Body:       []byte(html),
	}
}

// errorResponse builds a management error response.
func errorResponse(code int, msg string) managementResponse {
	body, _ := jsonMarshal(map[string]string{"error": msg})
	return managementResponse{
		StatusCode: code,
		Headers:    map[string][]string{"Content-Type": {"application/json"}},
		Body:       body,
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
