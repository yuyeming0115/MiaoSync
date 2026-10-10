// MiaoSync 前端逻辑（原生 JS，无框架）。
(function () {
  "use strict";

  const state = {
    config: null,
    participating: new Set(),
    planId: null, // P1-2：预览返回的计划 ID，执行时回传以消除 TOCTOU
  };

  const $ = (sel) => document.querySelector(sel);
  const $$ = (sel) => Array.from(document.querySelectorAll(sel));

  // ---------- API ----------
  async function getJSON(url) {
    const r = await fetch(url);
    if (!r.ok) throw new Error(await r.text());
    return r.json();
  }
  async function postJSON(url, body) {
    const r = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
    const data = await r.json().catch(() => ({}));
    if (!r.ok) throw new Error(data.error || r.statusText);
    return data;
  }

  function toast(msg, isErr) {
    const t = $("#toast");
    t.textContent = msg;
    t.className = "toast show" + (isErr ? " err" : "");
    setTimeout(() => (t.className = "toast"), 3200);
  }

  // ---------- 渲染：节点编辑 ----------
  function renderNodes() {
    const wrap = $("#nodeList");
    wrap.innerHTML = "";
    (state.config.nodes || []).forEach((n, i) => {
      const div = document.createElement("div");
      div.className = "node";
      div.innerHTML = `
        <button class="del" data-i="${i}" title="删除节点">✕</button>
        <div class="row">
          <input class="nm" data-i="${i}" value="${esc(n.name || "")}" placeholder="节点名（如 win-pc）" />
          <select class="role" data-i="${i}">
            <option value="peer"${n.role === "peer" ? " selected" : ""}>peer</option>
            <option value="source"${n.role === "source" ? " selected" : ""}>source</option>
            <option value="target"${n.role === "target" ? " selected" : ""}>target</option>
          </select>
        </div>
        <div class="row path-row">
          <input class="wp" data-i="${i}" value="${esc(n["win-path"] || "")}" placeholder="Windows 路径，如 D:\\Sync" />
          <button class="browse" type="button" data-i="${i}" data-field="win-path" title="选择文件夹">📁</button>
        </div>
        <div class="row path-row">
          <input class="mp" data-i="${i}" value="${esc(n["mac-path"] || "")}" placeholder="macOS 路径，如 /Users/you/Sync" />
          <button class="browse" type="button" data-i="${i}" data-field="mac-path" title="选择文件夹">📁</button>
        </div>`;
      wrap.appendChild(div);
    });
    bindNodeInputs();
    renderParticipate();
  }

  function bindNodeInputs() {
    $$("#nodeList .nm").forEach((el) =>
      el.addEventListener("input", (e) => {
        state.config.nodes[+e.target.dataset.i].name = e.target.value;
      })
    );
    $$("#nodeList .role").forEach((el) =>
      el.addEventListener("change", (e) => {
        state.config.nodes[+e.target.dataset.i].role = e.target.value;
      })
    );
    $$("#nodeList .wp").forEach((el) =>
      el.addEventListener("input", (e) => {
        state.config.nodes[+e.target.dataset.i]["win-path"] = e.target.value;
      })
    );
    $$("#nodeList .mp").forEach((el) =>
      el.addEventListener("input", (e) => {
        state.config.nodes[+e.target.dataset.i]["mac-path"] = e.target.value;
      })
    );
    $$("#nodeList .browse").forEach((el) =>
      el.addEventListener("click", (e) => {
        const i = +e.target.dataset.i;
        const field = e.target.dataset.field;
        const cls = field === "win-path" ? ".wp" : ".mp";
        const input = document.querySelector(`#nodeList ${cls}[data-i="${i}"]`);
        if (!input) return;
        e.target.disabled = true;
        e.target.textContent = "…";
        postJSON("/api/pick-folder", {})
          .then((data) => {
            if (data.path) {
              input.value = data.path;
              state.config.nodes[i][field] = data.path;
            }
          })
          .catch((err) => toast("选择文件夹失败：" + err.message, true))
          .finally(() => {
            e.target.disabled = false;
            e.target.textContent = "📁";
          });
      })
    );
    $$("#nodeList .del").forEach((el) =>
      el.addEventListener("click", (e) => {
        const i = +e.target.dataset.i;
        const name = state.config.nodes[i].name;
        state.config.nodes.splice(i, 1);
        state.participating.delete(name);
        renderNodes();
      })
    );
  }

  function renderParticipate() {
    const wrap = $("#participate");
    wrap.innerHTML = "";
    (state.config.nodes || []).forEach((n) => {
      const on = state.participating.has(n.name);
      const label = document.createElement("label");
      label.className = "chk" + (on ? " on" : "");
      label.innerHTML = `<input type="checkbox" ${on ? "checked" : ""}/> ${esc(n.name || "(未命名)")}`;
      label.querySelector("input").addEventListener("change", (e) => {
        const nm = n.name;
        if (e.target.checked) state.participating.add(nm);
        else state.participating.delete(nm);
        label.className = "chk" + (e.target.checked ? " on" : "");
      });
      wrap.appendChild(label);
    });
  }

  // ---------- 渲染：状态 ----------
  function renderStatus() {
    const wrap = $("#statusList");
    wrap.innerHTML = "";
    getJSON("/api/status")
      .then((data) => {
        // P3：显示运行状态
        const running = data.running;
        const btnSync = $("#btnSync");
        if (btnSync) btnSync.disabled = running;
        if (running) {
          const banner = document.createElement("div");
          banner.className = "status running";
          banner.innerHTML = `<div class="name">🔄 同步进行中…</div>`;
          wrap.appendChild(banner);
        }
        (data.nodes || []).forEach((s) => {
          const div = document.createElement("div");
          div.className = "status";
          const sync = s.lastSync ? new Date(s.lastSync).toLocaleString() : "从未同步";
          div.innerHTML = `
            <div>
              <div class="name">${esc(s.name)}</div>
              <div class="meta">${esc(s.path)}</div>
            </div>
            <div class="meta">
              ${s.accessible ? `<span class="ok">${s.fileCount} 个文件</span>` : `<span class="bad">不可访问</span>`}
              <br/>${sync}
            </div>`;
          wrap.appendChild(div);
        });
      })
      .catch(() => {
        wrap.innerHTML = `<p class="empty">获取状态失败</p>`;
      });
  }

  // ---------- 预览 ----------
  function renderPreview(actions) {
    const counts = { copy: 0, delete: 0, "rename-conflict": 0 };
    actions.forEach((a) => (counts[a.kind] = (counts[a.kind] || 0) + 1));
    $("#previewSummary").innerHTML =
      `<span class="stat stat-copy">复制 ${counts.copy || 0}</span>` +
      `<span class="stat stat-del">删除 ${counts.delete || 0}</span>` +
      `<span class="stat stat-conf">冲突 ${counts["rename-conflict"] || 0}</span>`;

    const list = $("#previewList");
    list.innerHTML = "";
    if (!actions.length) {
      list.innerHTML = `<p class="empty">✅ 两端已经一致，无需同步。</p>`;
      return;
    }
    actions.slice(0, 500).forEach((a) => {
      const el = document.createElement("div");
      el.className = "act " + a.kind;
      const label = a.kind === "copy" ? "COPY" : a.kind === "delete" ? "DELETE" : "CONFLICT";
      el.innerHTML = `<span class="kind">${label}</span><span class="rel">${esc(a.relPath)}</span><span class="reason">${esc(a.reason || "")}</span>`;
      list.appendChild(el);
    });
    if (actions.length > 500) {
      const more = document.createElement("p");
      more.className = "empty";
      more.textContent = `… 还有 ${actions.length - 500} 个动作`;
      list.appendChild(more);
    }
  }

  // ---------- 日志 ----------
  function addLog(msg, cls) {
    const log = $("#log");
    const div = document.createElement("div");
    div.className = cls || "";
    div.textContent = msg;
    log.appendChild(div);
    log.scrollTop = log.scrollHeight;
  }
  function setProgress(done, total) {
    const pct = total > 0 ? Math.round((done / total) * 100) : (done > 0 ? 100 : 0);
    $("#progressBar").style.width = pct + "%";
  }

  // ---------- 操作 ----------
  function gatherConfig() {
    return {
      nodes: state.config.nodes,
      mode: $("#mode").value,
      conflict: $("#conflict").value,
      ignore: $("#ignore").value.split("\n").map((s) => s.trim()).filter(Boolean),
    };
  }

  function selectedNodes() {
    const names = Array.from(state.participating).filter((n) =>
      (state.config.nodes || []).some((x) => x.name === n)
    );
    return names;
  }

  async function loadConfig() {
    state.config = await getJSON("/api/config");
    if (state.config.nodes) {
      state.participating = new Set(state.config.nodes.map((n) => n.name));
    }
    $("#mode").value = state.config.mode || "mirror";
    $("#conflict").value = state.config.conflict || "rename";
    $("#ignore").value = (state.config.ignore || []).join("\n");
    renderNodes();
  }

  async function saveConfig() {
    try {
      await postJSON("/api/config", gatherConfig());
      toast("配置已保存");
      await loadConfig();
      renderStatus();
    } catch (e) {
      toast("保存失败：" + e.message, true);
    }
  }

  async function previewSync() {
    const names = selectedNodes();
    if (names.length < 2) return toast("请至少选择 2 个参与同步的节点", true);
    try {
      const data = await postJSON("/api/plan", {
        nodes: names,
        mode: $("#mode").value,
        conflict: $("#conflict").value,
      });
      state.planId = data.planId; // P1-2：保存计划 ID 供执行时回传
      renderPreview(data.actions);
      addLog(`预览完成：复制 ${data.copied} / 删除 ${data.deleted} / 冲突 ${data.conflicts}`, "ok");
    } catch (e) {
      toast("预览失败：" + e.message, true);
    }
  }

  async function runSync() {
    const names = selectedNodes();
    if (names.length < 2) return toast("请至少选择 2 个参与同步的节点", true);
    // P1-2：必须先预览获取计划 ID，执行时回传以消除 TOCTOU
    if (!state.planId) return toast("请先预览同步计划", true);
    const btn = $("#btnSync");
    btn.disabled = true;
    $("#log").innerHTML = "";
    setProgress(0, 0);
    addLog("开始同步：" + names.join(" ↔ "), "ok");
    const confirmDanger = !!window.__confirmDanger;
    window.__confirmDanger = false;
    try {
      const resp = await fetch("/api/sync", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          nodes: names,
          mode: $("#mode").value,
          conflict: $("#conflict").value,
          confirmDangerous: confirmDanger,
          planId: state.planId,
        }),
      });
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({}));
        // 计划失效/过期/不存在时清除本地 planId，提示重新预览
        if (err.error && (err.error.indexOf("计划") >= 0 || err.error.indexOf("预览") >= 0)) {
          state.planId = null;
        }
        throw new Error(err.error || resp.statusText);
      }
      await streamEvents(resp);
    } catch (e) {
      addLog("错误：" + e.message, "err");
      toast("同步出错：" + e.message, true);
    } finally {
      btn.disabled = false;
      renderStatus();
    }
  }

  async function streamEvents(resp) {
    const reader = resp.body.getReader();
    const decoder = new TextDecoder();
    let buf = "";
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const parts = buf.split("\n\n");
      buf = parts.pop();
      for (const part of parts) {
        const line = part.split("\n").find((l) => l.startsWith("data:"));
        if (!line) continue;
        let evt;
        try {
          evt = JSON.parse(line.slice(5).trim());
        } catch {
          continue;
        }
        handleEvent(evt);
      }
    }
  }

  function handleEvent(evt) {
    if (evt.type === "progress") {
      setProgress(evt.done, evt.total);
      let cls = "";
      if (evt.msg && evt.msg.indexOf("失败") >= 0) cls = "err";
      else if (evt.msg && evt.msg.indexOf("删除") >= 0) cls = "warn";
      else if (evt.stage === "apply") cls = "ok";
      addLog(`[${evt.stage}] ${evt.done}/${evt.total} ${evt.msg || ""}`, cls);
    } else if (evt.type === "done") {
      const s = evt.summary;
      setProgress(1, 1);
      addLog(
        `✅ ${s.message}：复制 ${s.copied} / 删除 ${s.deleted} / 冲突 ${s.conflicts}` +
          (s.errors ? ` / 错误 ${s.errors}` : ""),
        "ok"
      );
      if (s.requiresConfirm) {
        addLog("⚠️ " + s.dangerousInfo, "warn");
        toast("存在危险操作，请确认后重试", true);
      } else {
        // P1-2：同步完成后计划已消费，清除本地 planId
        state.planId = null;
        toast("同步完成");
      }
      showReport(s);
    } else if (evt.type === "error") {
      addLog("❌ " + evt.message, "err");
    }
  }

  // ---------- 同步完成报告 ----------
  const MODE_LABEL = { mirror: "双向镜像", backup: "单向备份" };
  const CONFLICT_LABEL = { rename: "重命名备份", keep: "两端保留", newer: "仅留较新" };
  const SEG_DEFS = [
    { key: "copied", label: "复制", color: "#38bdf8" },
    { key: "deleted", label: "删除", color: "#f87171" },
    { key: "conflicts", label: "冲突", color: "#fbbf24" },
    { key: "skipped", label: "跳过", color: "#94a3b8" },
  ];
  const C = 2 * Math.PI * 68; // 环形周长（与 SVG r=68 对应）

  function formatBytes(n) {
    if (!n) return "0 B";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0, x = n;
    while (x >= 1024 && i < u.length - 1) { x /= 1024; i++; }
    return (i === 0 ? x : x.toFixed(x < 10 ? 2 : 1)) + " " + u[i];
  }
  function formatDuration(ms) {
    if (!ms) return "—";
    if (ms < 1000) return ms + " ms";
    return (ms / 1000).toFixed(1) + " s";
  }

  function showReport(s) {
    const modal = $("#reportModal");
    const head = $("#reportHead");
    const names = selectedNodes();

    let icon = "✅", title = "同步完成", cls = "";
    if (s.requiresConfirm) { icon = "⚠️"; title = "需要确认危险操作"; cls = "is-warn"; }
    else if (s.errors > 0) { icon = "⚠️"; title = "同步完成，有 " + s.errors + " 个错误"; cls = "is-warn"; }
    const when = new Date().toLocaleString();

    // 元数据徽标
    const metaHtml =
      `<span class="m">⏱ 耗时 <b>${formatDuration(s.durationMs)}</b></span>` +
      `<span class="m">📦 传输 <b>${formatBytes(s.bytesCopied)}</b></span>` +
      `<span class="m">🔄 模式 <b>${MODE_LABEL[$("#mode").value] || $("#mode").value}</b></span>` +
      `<span class="m">⚡ 冲突 <b>${CONFLICT_LABEL[$("#conflict").value] || $("#conflict").value}</b></span>`;

    head.className = "report-head " + cls;
    head.innerHTML =
      `<div class="report-title-block">` +
      `<span class="ico">${icon}</span>` +
      `<div><p class="title">${title}</p>` +
      `<p class="sub">${esc(names.join(" ↔ "))} · ${when}</p></div></div>` +
      `<div class="report-meta-row">${metaHtml}</div>`;

    // 环形图
    const segs = $("#reportSegments");
    segs.innerHTML = "";
    const total = SEG_DEFS.reduce((a, d) => a + (s[d.key] || 0), 0);
    let acc = 0;
    const circles = [];
    SEG_DEFS.forEach((d) => {
      const val = s[d.key] || 0;
      const len = total > 0 ? (val / total) * C : 0;
      const c = document.createElementNS("http://www.w3.org/2000/svg", "circle");
      c.setAttribute("class", "donut-seg");
      c.setAttribute("cx", "90"); c.setAttribute("cy", "90"); c.setAttribute("r", "68");
      c.setAttribute("stroke", d.color);
      c.setAttribute("stroke-dasharray", "0 " + C);
      c.setAttribute("stroke-dashoffset", String(-acc));
      segs.appendChild(c);
      circles.push({ el: c, len });
      acc += len;
    });
    $("#reportCenter").textContent = total;
    requestAnimationFrame(() => {
      circles.forEach(({ el, len }) => el.setAttribute("stroke-dasharray", len + " " + C));
    });

    // 图例
    const legend = $("#reportLegend");
    legend.innerHTML = "";
    SEG_DEFS.forEach((d) => {
      const val = s[d.key] || 0;
      const pct = total > 0 ? Math.round((val / total) * 100) + "%" : "0%";
      const row = document.createElement("div");
      row.className = "lg";
      row.innerHTML =
        `<span class="dot" style="background:${d.color}"></span>` +
        `<span>${d.label}</span><span class="n">${val}</span><span class="p">${pct}</span>`;
      legend.appendChild(row);
    });

    // 统计卡：2×3 矩阵（复制 / 删除 / 冲突 / 跳过 / 错误 / 总计）
    const tiles = [
      { k: "copied", lbl: "复制", cls: "copy" },
      { k: "deleted", lbl: "删除", cls: "delete" },
      { k: "conflicts", lbl: "冲突", cls: "conflict" },
      { k: "skipped", lbl: "跳过", cls: "skip" },
      { k: "errors", lbl: "错误", cls: "error" },
      { k: null, lbl: "总计", cls: "total", val: total },
    ];
    const tileWrap = $("#reportTiles");
    tileWrap.innerHTML = "";
    tiles.forEach((t) => {
      const el = document.createElement("div");
      el.className = "tile " + t.cls;
      const num = t.val != null ? t.val : (s[t.k] || 0);
      el.innerHTML = `<div class="t-num">${num}</div><div class="t-lbl">${t.lbl}</div>`;
      tileWrap.appendChild(el);
    });

    // 传输明细列表
    window.__lastReport = s;
    renderDetail(s);

    // 危险操作横幅 + 确认按钮
    const banner = $("#reportBanner");
    const btnDanger = $("#btnConfirmDanger");
    if (s.requiresConfirm) {
      banner.hidden = false;
      banner.innerHTML = `<b>⚠️ 危险操作</b><br/>${esc(s.dangerousInfo)}<br/>确认无误后点击右下角按钮执行。`;
      btnDanger.hidden = false;
    } else {
      banner.hidden = true;
      btnDanger.hidden = true;
    }

    modal.hidden = false;
  }

  // ---------- 传输明细列表 ----------
  const KIND_ICON = { copy: "→", delete: "✕", conflict: "⇄", skip: "•" };
  const KIND_LABEL = { copy: "复制", delete: "删除", conflict: "冲突", skip: "跳过" };

  function baseName(p) {
    if (!p) return "";
    const i = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
    return i >= 0 ? p.slice(i + 1) : p;
  }

  function renderDetail(s) {
    const wrap = $("#reportDetail");
    const countEl = $("#detailCount");
    const items = s.transferred || [];
    countEl.textContent = items.length;
    const filter = window.__detailFilter || "all";
    wrap.innerHTML = "";
    const list = items.filter((it) => filter === "all" || it.kind === filter);
    if (!list.length) {
      wrap.innerHTML = `<p class="x-empty">${filter === "all" ? "本次同步无文件传输。" : "没有" + (KIND_LABEL[filter] || "") + "操作记录"}</p>`;
      return;
    }
    list.forEach((it) => {
      const row = document.createElement("div");
      row.className = "xrow " + it.kind + (it.status === "error" ? " error" : "");
      const target = it.kind === "delete" ? it.to : it.to || it.from;
      const name = baseName(target) || baseName(it.from || "");
      const path = it.kind === "delete"
        ? it.to || ""
        : (it.from || "") + "  →  " + (it.to || "");
      const sizeTxt = it.status === "error" ? "失败" : formatBytes(it.size);
      row.innerHTML =
        `<div class="x-ico">${KIND_ICON[it.kind] || "•"}</div>` +
        `<div class="x-main"><div class="x-name">${esc(name)}</div><div class="x-path">${esc(path)}</div></div>` +
        `<div class="x-size">${sizeTxt}</div>`;
      wrap.appendChild(row);
    });
  }

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // ---------- 初始化 ----------
  function init() {
    $("#btnAddNode").addEventListener("click", () => {
      state.config.nodes.push({ name: "", "win-path": "", "mac-path": "", role: "peer" });
      renderNodes();
    });
    $("#btnSave").addEventListener("click", saveConfig);
    $("#btnPreview").addEventListener("click", previewSync);
    $("#btnSync").addEventListener("click", runSync);
    $("#btnRefresh").addEventListener("click", renderStatus);

    // 报告弹层：关闭 + 危险操作确认
    $$("#reportModal [data-close]").forEach((el) =>
      el.addEventListener("click", () => ($("#reportModal").hidden = true))
    );
    $("#btnConfirmDanger").addEventListener("click", () => {
      $("#reportModal").hidden = true;
      window.__confirmDanger = true;
      runSync();
    });

    // 传输明细筛选
    $$("#detailFilters .chip").forEach((el) =>
      el.addEventListener("click", () => {
        $$("#detailFilters .chip").forEach((c) => c.classList.remove("active"));
        el.classList.add("active");
        window.__detailFilter = el.dataset.f;
        if (window.__lastReport) renderDetail(window.__lastReport);
      })
    );

    loadConfig()
      .then(renderStatus)
      .catch(() => {
        $("#connStatus").textContent = "未连接";
        $("#connStatus").className = "badge";
        toast("无法连接后端，请确认程序已启动", true);
      });
  }

  document.addEventListener("DOMContentLoaded", init);
})();
