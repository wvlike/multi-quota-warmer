package main

const statusPageHTML = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>多渠道五小时额度预热</title>
  <style>
    :root { color-scheme: light dark; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; }
    body { margin: 0; background: #f7f8fa; color: #1f2937; }
    main { max-width: 1120px; margin: 0 auto; padding: 28px 18px 48px; }
    h1 { margin: 0; font-size: 28px; } h2 { margin: 0 0 14px; font-size: 18px; } h3 { margin: 0 0 10px; font-size: 16px; }
    .subtitle { color: #5f6b7a; margin: 8px 0 24px; }
    .card { background: #fff; border: 1px solid #e3e7ed; border-radius: 12px; padding: 18px; margin: 16px 0; box-shadow: 0 1px 2px rgba(0,0,0,.03); }
    .subcard { border: 1px solid #e7ebf0; border-radius: 10px; padding: 14px; background: #fafbfd; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(190px, 1fr)); gap: 12px; }
    .provider-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(320px, 1fr)); gap: 14px; margin-top: 14px; }
    .field { display: grid; gap: 6px; } label { font-weight: 600; font-size: 14px; } input, textarea, select, button { font: inherit; }
    input, textarea, select { box-sizing: border-box; width: 100%; padding: 9px 10px; border: 1px solid #c9d1dc; border-radius: 7px; background: #fff; color: #1f2937; }
    input[type="checkbox"] { width: auto; }
    textarea { min-height: 82px; resize: vertical; } button { padding: 8px 12px; border: 1px solid #2867c8; border-radius: 7px; background: #2867c8; color: #fff; cursor: pointer; }
    button.secondary { background: #fff; color: #24579f; } button.danger { border-color: #b42318; background: #fff; color: #b42318; } button:disabled { opacity: .55; cursor: not-allowed; }
    .row { display: flex; gap: 10px; align-items: end; flex-wrap: wrap; } .row-center { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; } .grow { flex: 1 1 220px; }
    .hint, .muted { color: #657181; font-size: 13px; } .notice { margin-top: 12px; min-height: 20px; } .notice.error { color: #b42318; } .notice.success { color: #027a48; }
    table { width: 100%; border-collapse: collapse; font-size: 14px; } th, td { padding: 10px 8px; border-bottom: 1px solid #e7ebf0; text-align: left; vertical-align: top; } th { color: #5f6b7a; font-weight: 600; }
    code { padding: 2px 4px; background: #f2f4f7; border-radius: 4px; } pre { margin: 0; white-space: pre-wrap; word-break: break-word; }
    .status { font-weight: 600; } .ok { color: #027a48; } .failed { color: #b42318; }
    .tag { display: inline-block; border-radius: 100px; padding: 2px 8px; background: #eff4ff; color: #175cd3; font-size: 12px; font-weight: 600; }
    .tag.codex { background: #ecfdf3; color: #027a48; }
    .tag.antigravity { background: #f4f3ff; color: #5925dc; }
    .tag.warn { background: #fffaeb; color: #b54708; }
    .tag.error { background: #fef3f2; color: #b42318; }
    .schedule-row input[type="time"] { width: 130px; } .schedule-row input[type="text"] { width: 180px; }
    .log-toolbar { display: flex; gap: 10px; align-items: center; flex-wrap: wrap; margin-bottom: 12px; }
    .log-toolbar select { width: auto; min-width: 110px; }
    .log-toolbar input[type="search"] { width: 220px; }
    .pagination { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 10px; margin-top: 14px; }
    .pagination-actions { display: flex; gap: 6px; flex-wrap: wrap; }
    .fields-cell { max-width: 360px; word-break: break-all; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: 12px; }
    @media (prefers-color-scheme: dark) {
      body { background: #111827; color: #e5e7eb; }
      .card { background: #182230; border-color: #344054; }
      .subcard { background: #131c28; border-color: #2c374b; }
      input, textarea, select, button.secondary { background: #101828; color: #e5e7eb; border-color: #475467; }
      .subtitle, .muted, .hint, th { color: #a6b1c2; }
      td, th { border-color: #344054; }
      code { background: #293548; }
    }
  </style>
</head>
<body>
<main>
  <h1>多渠道五小时额度预热</h1>
  <p class="subtitle">按每日固定时刻对已启用的 Codex 与 Antigravity 凭证发送预热请求，提前激活 5h 重置窗口。</p>

  <section class="card">
    <h2>管理密钥</h2>
    <div class="row">
      <div class="field grow"><label for="management-key">管理密钥</label><input id="management-key" type="password" autocomplete="current-password" placeholder="用于读取状态、保存日程和手动执行"></div>
      <label class="muted row-center"><input id="remember-key" type="checkbox"> 在此浏览器保存管理密钥</label>
      <button id="reload" type="button" class="secondary">读取数据</button>
    </div>
    <p class="hint">页面资源不携带凭证数据；所有状态和操作均通过受管理密钥保护的接口完成。</p>
    <div id="notice" class="notice" role="status"></div>
  </section>

  <section class="card">
    <div class="row">
      <h2 class="grow">当前状态</h2>
      <div class="row-center">
        <button id="run-codex" type="button" class="secondary">仅预热 Codex</button>
        <button id="run-antigravity" type="button" class="secondary">仅预热 Antigravity</button>
        <button id="run-now" type="button">立即预热全部</button>
      </div>
    </div>
    <div id="summary" class="grid muted">请先输入管理密钥并读取数据。</div>
    <div style="margin-top:16px"><h3>下一次预热</h3><div id="next-runs" class="muted">暂无数据</div></div>
    <div style="margin-top:16px"><h3>最近一轮结果</h3><div id="last-results" class="muted">暂无数据</div></div>
  </section>

  <section class="card">
    <h2>预热设置</h2>
    <div class="grid">
      <div class="field"><label for="timezone">时区</label><input id="timezone" type="text" required placeholder="Asia/Singapore"></div>
      <div class="field"><label for="max-logs">日志条数上限</label><input id="max-logs" type="number" min="1" max="2000"></div>
    </div>

    <div class="provider-grid">
      <div class="subcard">
        <div class="row-center" style="justify-content:space-between;margin-bottom:10px">
          <h3 style="margin:0">Codex 渠道</h3>
          <label class="muted row-center"><input id="codex-enabled" type="checkbox"> 启用定时预热</label>
        </div>
        <div class="field"><label for="codex-model">模型</label><input id="codex-model" type="text" placeholder="gpt-5.6-luna"></div>
        <div class="field" style="margin-top:10px"><label for="codex-prompt">预热消息</label><textarea id="codex-prompt"></textarea></div>
      </div>

      <div class="subcard">
        <div class="row-center" style="justify-content:space-between;margin-bottom:10px">
          <h3 style="margin:0">Antigravity 渠道</h3>
          <label class="muted row-center"><input id="ag-enabled" type="checkbox"> 启用定时预热</label>
        </div>
        <div class="field"><label for="ag-model">模型</label><input id="ag-model" type="text" placeholder="gemini-3-flash"></div>
        <div class="field" style="margin-top:10px"><label for="ag-prompt">预热消息</label><textarea id="ag-prompt"></textarea></div>
        <p class="hint" style="margin:8px 0 0">提示：Gemini 模型通常需要较长输出（建议要求 ≥600 tokens）才能可靠激活 5h 窗口；Claude 模型（如 claude-sonnet-5-5-high）短消息即可。</p>
      </div>
    </div>

    <div style="margin-top:20px">
      <h3>每日固定时刻</h3>
      <p class="hint">每一项每天对所有已启用渠道执行一次。日程保存后会立即重新计算下一次预热时间。</p>
      <div id="schedules"></div>
      <div class="row" style="margin-top:12px">
        <button id="add-schedule" type="button" class="secondary">新增日程</button>
        <button id="save-settings" type="button">保存设置</button>
      </div>
    </div>
  </section>

  <section class="card">
    <div class="row" style="margin-bottom:12px">
      <h2 class="grow" style="margin-bottom:0">运行日志</h2>
      <button id="load-logs" type="button" class="secondary">刷新日志</button>
    </div>
    <div class="log-toolbar">
      <label class="muted row-center">级别
        <select id="log-level">
          <option value="all">全部</option>
          <option value="info">info</option>
          <option value="warn">warn</option>
          <option value="error">error</option>
        </select>
      </label>
      <label class="muted row-center">每页
        <select id="log-page-size">
          <option value="10">10 条</option>
          <option value="20" selected>20 条</option>
          <option value="50">50 条</option>
          <option value="100">100 条</option>
        </select>
      </label>
      <input id="log-query" type="search" placeholder="搜索事件、消息或凭证 ID…">
    </div>
    <div id="logs" class="muted">暂无数据</div>
    <div class="pagination" id="log-pagination" hidden>
      <span id="log-page-info" class="muted"></span>
      <div class="pagination-actions">
        <button id="log-first" type="button" class="secondary">首页</button>
        <button id="log-prev" type="button" class="secondary">上一页</button>
        <button id="log-next" type="button" class="secondary">下一页</button>
        <button id="log-last" type="button" class="secondary">末页</button>
      </div>
    </div>
  </section>
</main>
<script>
(() => {
  const pluginID = "multi-quota-warmer";
  const base = "/v0/management/plugins/" + pluginID;
  const elements = {
    key: document.querySelector("#management-key"),
    remember: document.querySelector("#remember-key"),
    notice: document.querySelector("#notice"),
    summary: document.querySelector("#summary"),
    nextRuns: document.querySelector("#next-runs"),
    results: document.querySelector("#last-results"),
    timezone: document.querySelector("#timezone"),
    maxLogs: document.querySelector("#max-logs"),
    codexEnabled: document.querySelector("#codex-enabled"),
    codexModel: document.querySelector("#codex-model"),
    codexPrompt: document.querySelector("#codex-prompt"),
    agEnabled: document.querySelector("#ag-enabled"),
    agModel: document.querySelector("#ag-model"),
    agPrompt: document.querySelector("#ag-prompt"),
    schedules: document.querySelector("#schedules"),
    logs: document.querySelector("#logs"),
    logLevel: document.querySelector("#log-level"),
    logPageSize: document.querySelector("#log-page-size"),
    logQuery: document.querySelector("#log-query"),
    logPagination: document.querySelector("#log-pagination"),
    logPageInfo: document.querySelector("#log-page-info"),
    logFirst: document.querySelector("#log-first"),
    logPrev: document.querySelector("#log-prev"),
    logNext: document.querySelector("#log-next"),
    logLast: document.querySelector("#log-last"),
    runNow: document.querySelector("#run-now"),
    runCodex: document.querySelector("#run-codex"),
    runAg: document.querySelector("#run-antigravity")
  };
  let configuration = null;
  const logState = { page: 1, pageSize: 20, level: "all", query: "", total: 0, totalPages: 1 };
  let searchTimer = null;

  const storedKey = sessionStorage.getItem(pluginID + ".management-key") || localStorage.getItem(pluginID + ".management-key") || "";
  elements.key.value = storedKey;
  elements.remember.checked = Boolean(localStorage.getItem(pluginID + ".management-key"));

  function managementKey() { return elements.key.value.trim(); }
  function showNotice(text, type) { elements.notice.textContent = text || ""; elements.notice.className = "notice " + (type || ""); }
  function escapeText(value) { return String(value == null ? "" : value); }
  function formatTime(value) { if (!value || String(value).startsWith("0001-01-01")) return "—"; const date = new Date(value); return Number.isNaN(date.getTime()) ? escapeText(value) : date.toLocaleString(); }
  function saveKey() { const key = managementKey(); sessionStorage.removeItem(pluginID + ".management-key"); localStorage.removeItem(pluginID + ".management-key"); if (!key) return; if (elements.remember.checked) localStorage.setItem(pluginID + ".management-key", key); else sessionStorage.setItem(pluginID + ".management-key", key); }
  async function request(path, options = {}) {
    const key = managementKey();
    if (!key) throw new Error("请输入管理密钥");
    saveKey();
    const headers = new Headers(options.headers || {});
    headers.set("Authorization", "Bearer " + key);
    if (options.body) headers.set("Content-Type", "application/json");
    const response = await fetch(path, { ...options, headers, credentials: "same-origin" });
    const payload = await response.json().catch(() => ({}));
    if (!response.ok) throw new Error(payload.message || payload.error || ("请求失败，HTTP " + response.status));
    return payload;
  }
  function cell(text, className) { const node = document.createElement("td"); if (className) node.className = className; node.textContent = escapeText(text); return node; }
  function tagCell(text, tagClass) { const td = document.createElement("td"); const span = document.createElement("span"); span.className = "tag " + (tagClass || ""); span.textContent = escapeText(text); td.append(span); return td; }
  function button(text, className, onClick) { const node = document.createElement("button"); node.type = "button"; node.textContent = text; node.className = className || "secondary"; node.addEventListener("click", onClick); return node; }
  function enabledSchedule(schedule) { return schedule.enabled !== false; }

  function renderSchedules() {
    const list = Array.isArray(configuration && configuration.schedules) ? configuration.schedules : [];
    elements.schedules.replaceChildren();
    if (!list.length) { const empty = document.createElement("p"); empty.className = "muted"; empty.textContent = "尚未设置自动预热时间。"; elements.schedules.append(empty); return; }
    list.forEach((schedule, index) => {
      const row = document.createElement("div"); row.className = "row schedule-row"; row.style.marginTop = "8px";
      const id = document.createElement("input"); id.type = "text"; id.value = schedule.id || ""; id.placeholder = "日程名称";
      const at = document.createElement("input"); at.type = "time"; at.value = schedule.at || "";
      const enabled = document.createElement("input"); enabled.type = "checkbox"; enabled.checked = enabledSchedule(schedule);
      id.addEventListener("input", () => { configuration.schedules[index].id = id.value; });
      at.addEventListener("input", () => { configuration.schedules[index].at = at.value; });
      enabled.addEventListener("change", () => { configuration.schedules[index].enabled = enabled.checked; });
      const enabledLabel = document.createElement("label"); enabledLabel.className = "muted row-center"; enabledLabel.append(enabled, document.createTextNode("启用"));
      row.append(id, at, enabledLabel, button("删除", "danger", () => { configuration.schedules.splice(index, 1); renderSchedules(); }));
      elements.schedules.append(row);
    });
  }

  function fillConfiguration(config) {
    const codex = config.codex || { enabled: true, model: config.model || "gpt-5.6-luna", prompt: config.prompt || "Reply with exactly: quota window activated" };
    const ag = config.antigravity || { enabled: true, model: "gemini-3-flash", prompt: "Explain how HTTP/2 stream multiplexing works in detail with concrete examples. Write at least 600 tokens across several paragraphs without summarizing early." };
    configuration = {
      ...config,
      codex: { enabled: codex.enabled !== false, model: codex.model || "gpt-5.6-luna", prompt: codex.prompt || "Reply with exactly: quota window activated" },
      antigravity: { enabled: ag.enabled !== false, model: ag.model || "gemini-3-flash", prompt: ag.prompt || "" },
      schedules: Array.isArray(config.schedules) ? config.schedules.map(item => ({ ...item })) : []
    };
    elements.timezone.value = configuration.timezone || "Asia/Singapore";
    elements.maxLogs.value = configuration.max_log_entries || 200;
    elements.codexEnabled.checked = configuration.codex.enabled;
    elements.codexModel.value = configuration.codex.model;
    elements.codexPrompt.value = configuration.codex.prompt;
    elements.agEnabled.checked = configuration.antigravity.enabled;
    elements.agModel.value = configuration.antigravity.model;
    elements.agPrompt.value = configuration.antigravity.prompt;
    renderSchedules();
  }

  function renderStatus(status) {
    elements.summary.replaceChildren();
    const codexCfg = status.codex || {};
    const agCfg = status.antigravity || {};
    const fields = [
      ["运行状态", status.running ? "正在预热" : "空闲"],
      ["Codex", (codexCfg.enabled ? "已启用 · " : "已关闭 · ") + (codexCfg.model || "—")],
      ["Antigravity", (agCfg.enabled ? "已启用 · " : "已关闭 · ") + (agCfg.model || "—")],
      ["时区", status.timezone],
      ["最近执行", formatTime(status.last_run)],
      ["执行来源", status.last_source || "—"],
      ["日志数量", status.log_count],
      ["状态保存", status.state_error || "正常"]
    ];
    fields.forEach(([label, value]) => {
      const item = document.createElement("div");
      const strong = document.createElement("strong");
      strong.textContent = label;
      item.append(strong, document.createElement("br"), document.createTextNode(escapeText(value)));
      elements.summary.append(item);
    });
    const runs = Array.isArray(status.schedules) ? status.schedules : [];
    elements.nextRuns.replaceChildren();
    if (!runs.length) elements.nextRuns.textContent = "未设置启用的自动日程。";
    else runs.forEach(run => { const item = document.createElement("div"); item.textContent = run.id + "（" + run.at + "）：" + formatTime(run.time); elements.nextRuns.append(item); });
    renderResults(status.last_results || []);
  }

  function renderResults(results) {
    elements.results.replaceChildren();
    if (!results.length) { elements.results.textContent = "暂无完成的预热记录。"; return; }
    const table = document.createElement("table"), head = document.createElement("thead"), header = document.createElement("tr");
    ["渠道", "凭证", "模型", "结果", "HTTP 状态", "错误"].forEach(value => { const node = document.createElement("th"); node.textContent = value; header.append(node); });
    head.append(header); table.append(head);
    const body = document.createElement("tbody");
    results.forEach(result => {
      const row = document.createElement("tr");
      const providerName = result.provider || "codex";
      const statusCell = cell(result.success ? "成功" : "失败", result.success ? "status ok" : "status failed");
      row.append(
        tagCell(providerName, providerName === "antigravity" ? "antigravity" : "codex"),
        cell(result.label || result.auth_id),
        cell(result.model),
        statusCell,
        cell(result.status_code || "—"),
        cell(result.error || "—")
      );
      body.append(row);
    });
    table.append(body); elements.results.append(table);
  }

  function renderLogs(payload) {
    const entries = Array.isArray(payload.logs) ? payload.logs : [];
    logState.total = typeof payload.total === "number" ? payload.total : entries.length;
    logState.page = typeof payload.page === "number" ? payload.page : 1;
    logState.pageSize = typeof payload.page_size === "number" ? payload.page_size : logState.pageSize;
    logState.totalPages = typeof payload.total_pages === "number" ? Math.max(1, payload.total_pages) : 1;

    elements.logs.replaceChildren();
    if (!entries.length) {
      elements.logs.textContent = logState.total === 0 ? "暂无日志。" : "当前筛选条件下没有匹配日志。";
    } else {
      const table = document.createElement("table"), head = document.createElement("thead"), header = document.createElement("tr");
      ["时间", "级别", "事件", "信息", "字段"].forEach(value => { const node = document.createElement("th"); node.textContent = value; header.append(node); });
      head.append(header); table.append(head);
      const body = document.createElement("tbody");
      entries.forEach(entry => {
        const row = document.createElement("tr");
        const levelClass = entry.level === "error" ? "error" : (entry.level === "warn" ? "warn" : "");
        row.append(
          cell(formatTime(entry.time)),
          tagCell(entry.level, levelClass),
          cell(entry.event),
          cell(entry.message),
          cell(entry.fields ? JSON.stringify(entry.fields) : "—", "fields-cell")
        );
        body.append(row);
      });
      table.append(body); elements.logs.append(table);
    }

    elements.logPagination.hidden = logState.total === 0;
    elements.logPageInfo.textContent = "共 " + logState.total + " 条 · 第 " + logState.page + " / " + logState.totalPages + " 页";
    elements.logFirst.disabled = logState.page <= 1;
    elements.logPrev.disabled = logState.page <= 1;
    elements.logNext.disabled = logState.page >= logState.totalPages;
    elements.logLast.disabled = logState.page >= logState.totalPages;
  }

  function logsURL() {
    const params = new URLSearchParams({
      page: String(logState.page),
      page_size: String(logState.pageSize),
      level: logState.level,
      q: logState.query
    });
    return base + "/logs?" + params.toString();
  }

  async function loadLogs() {
    try {
      const payload = await request(logsURL());
      renderLogs(payload);
    } catch (error) {
      showNotice(error.message, "error");
    }
  }

  async function loadAll() {
    try {
      showNotice("正在读取数据…");
      const [status, config, logs] = await Promise.all([
        request(base + "/status"),
        request(base + "/config"),
        request(logsURL())
      ]);
      renderStatus(status); fillConfiguration(config); renderLogs(logs); showNotice("数据已更新。", "success");
    } catch (error) { showNotice(error.message, "error"); }
  }

  async function saveSettings() {
    try {
      if (!configuration) configuration = {};
      configuration.timezone = elements.timezone.value.trim();
      configuration.max_log_entries = Number(elements.maxLogs.value || 200);
      configuration.codex = {
        enabled: elements.codexEnabled.checked,
        model: elements.codexModel.value.trim(),
        prompt: elements.codexPrompt.value.trim()
      };
      configuration.antigravity = {
        enabled: elements.agEnabled.checked,
        model: elements.agModel.value.trim(),
        prompt: elements.agPrompt.value.trim()
      };
      configuration.schedules = configuration.schedules || [];
      delete configuration.model;
      delete configuration.prompt;
      await request(base + "/config", { method: "PUT", body: JSON.stringify(configuration) });
      showNotice("设置已保存，主程序将重新加载插件配置。", "success");
      window.setTimeout(loadAll, 800);
    } catch (error) { showNotice(error.message, "error"); }
  }

  function setRunButtonsDisabled(disabled) {
    elements.runNow.disabled = disabled;
    elements.runCodex.disabled = disabled;
    elements.runAg.disabled = disabled;
  }

  async function runProvider(provider) {
    try {
      setRunButtonsDisabled(true);
      const label = provider === "codex" ? "Codex" : (provider === "antigravity" ? "Antigravity" : "全部已启用渠道");
      showNotice("正在执行 " + label + " 预热…");
      const payload = await request(base + "/run", { method: "POST", body: JSON.stringify({ provider }) });
      renderResults(payload.results || []);
      renderStatus(payload.status || {});
      showNotice(label + " 预热已完成。", "success");
      logState.page = 1;
      await loadLogs();
    } catch (error) { showNotice(error.message, "error"); }
    finally { setRunButtonsDisabled(false); }
  }

  document.querySelector("#reload").addEventListener("click", loadAll);
  elements.runNow.addEventListener("click", () => runProvider("all"));
  elements.runCodex.addEventListener("click", () => runProvider("codex"));
  elements.runAg.addEventListener("click", () => runProvider("antigravity"));
  document.querySelector("#save-settings").addEventListener("click", saveSettings);
  document.querySelector("#load-logs").addEventListener("click", () => { logState.page = 1; loadLogs(); });
  elements.logLevel.addEventListener("change", () => { logState.level = elements.logLevel.value; logState.page = 1; loadLogs(); });
  elements.logPageSize.addEventListener("change", () => { logState.pageSize = Number(elements.logPageSize.value || 20); logState.page = 1; loadLogs(); });
  elements.logQuery.addEventListener("input", () => {
    window.clearTimeout(searchTimer);
    searchTimer = window.setTimeout(() => { logState.query = elements.logQuery.value.trim(); logState.page = 1; loadLogs(); }, 250);
  });
  elements.logFirst.addEventListener("click", () => { if (logState.page > 1) { logState.page = 1; loadLogs(); } });
  elements.logPrev.addEventListener("click", () => { if (logState.page > 1) { logState.page--; loadLogs(); } });
  elements.logNext.addEventListener("click", () => { if (logState.page < logState.totalPages) { logState.page++; loadLogs(); } });
  elements.logLast.addEventListener("click", () => { if (logState.page < logState.totalPages) { logState.page = logState.totalPages; loadLogs(); } });
  document.querySelector("#add-schedule").addEventListener("click", () => {
    if (!configuration) configuration = { schedules: [] };
    configuration.schedules = configuration.schedules || [];
    const now = new Date();
    const at = String(now.getHours()).padStart(2, "0") + ":" + String(now.getMinutes()).padStart(2, "0");
    configuration.schedules.push({ id: "daily-" + at.replace(":", ""), at, enabled: true });
    renderSchedules();
  });
  if (managementKey()) loadAll();
})();
</script>
</body>
</html>`
