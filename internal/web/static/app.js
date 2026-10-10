// MiaoSync 前端逻辑（原生 JS，无框架）。
// 布局原则：文件对比是主角，风险优先，配置与日志能小则小。
(function () {
  "use strict";

  const state = {
    config: null,
    platform: "windows",
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

  function curField() {
    return state.platform === "darwin" ? "mac-path" : "win-path";
  }

  // ---------- 渲染：节点编辑（紧凑版） ----------
  function renderNodes() {
    const wrap = $("#nodeList");
    wrap.innerHTML = "";
    const cur = curField();
    const oth = cur === "mac-path" ? "win-path" : "mac-path";
    const curLbl = cur === "mac-path" ? "macOS 路径" : "Windows 路径";
    const othLbl = cur === "mac-path" ? "Windows 路径" : "macOS 路径";
    (state.config.nodes || []).forEach((n, i) => {
      const hasOther = !!(n[oth] && String(n[oth]).trim());
      const div = document.createElement("div");
      div.className = "node";
      div.innerHTML = `
        <div class="row">
          <input class="nm" data-i="${i}" value="${esc(n.name || "")}" placeholder="节点名（如 hdd）" />
          <select class="role" data-i="${i}">
            <option value="peer"${n.role === "peer" ? " selected" : ""}>peer</option>
            <option value="source"${n.role === "source" ? " selected" : ""}>source</option>
            <option value="target"${n.role === "target" ? " selected" : ""}>target</option>
          </select>
          <button class="del" data-i="${i}" title="删除节点">✕</button>
        </div>
        <div class="row path-row">
          <input class="pp" data-i="${i}" data-f="${cur}" value="${esc(n[cur] || "")}" placeholder="${curLbl}，如 D:\\Sync" />
          <button class="browse" type="button" data-i="${i}" data-f="${cur}" title="选择文件夹">📁</button>
        </div>
        <div class="row path-row other-row" data-i="${i}"${hasOther ? "" : " hidden"}>
          <input class="pp" data-i="${i}" data-f="${oth}" value="${esc(n[oth] || "")}" placeholder="${othLbl}" />
          <button class="browse" type="button" data-i="${i}" data-f="${oth}" title="选择文件夹">📁</button>
        </div>`;
      wrap.appendChild(div);
      if (!hasOther) {
        const link = document.createElement("button");
        link.className = "mini link add-other";
        link.textContent = "＋ " + othLbl;
        link.addEventListener("click", () => {
          div.querySelector(".other-row").hidden = false;
          link.remove();
        });
        div.appendChild(link);
      }
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
    $$("#nodeList .pp").forEach((el) =>
      el.addEventListener("input", (e) => {
        const i = +e.target.dataset.i;
        const f = e.target.dataset.f;
        state.config.nodes[i][f] = e.target.value;
      })
    );
    $$("#nodeList .browse").forEach((el) =>
      el.addEventListener("click", (e) => {
        const i = +e.currentTarget.dataset.i;
        const field = e.currentTarget.dataset.f;
        const input = document.querySelector(`#nodeList .pp[data-f="${field}"][data-i="${i}"]`);
        if (!input) return;
        e.currentTarget.disabled = true;
        e.currentTarget.textContent = "…";
        postJSON("/api/pick-folder", {})
          .then((data) => {
            if (data.path) {
              input.value = data.path;
              state.config.nodes[i][field] = data.path;
            }
          })
          .catch((err) => toast("选择文件夹失败：" + err.message, true))
          .finally(() => {
            e.currentTarget.disabled = false;
            e.currentTarget.textContent = "📁";
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

  // ---------- 渲染：节点状态（头部小徽标） ----------
  function renderStatus() {
    getJSON("/api/status")
      .then((data) => {
        const btnSync = $("#btnSync");
        if (btnSync) btnSync.disabled = !!data.running;
        // 启动时的配置问题（降级模式）：横幅提示，保存成功后消失
        const cfgBanner = $("#cfgBanner");
        if (data.configError) {
          cfgBanner.innerHTML =
            `<b>⚠️ 配置有问题</b><span class="cfg-sub">${esc(data.configError)}` +
            ` —— 在左侧修正节点 / 规则后点「💾 保存配置」即可。</span>`;
          cfgBanner.hidden = false;
        } else {
          cfgBanner.hidden = true;
        }
        const wrap = $("#nodeBadges");
        wrap.innerHTML = "";
        if (data.running) {
          const b = document.createElement("span");
          b.className = "nbadge run";
          b.textContent = "🔄 同步进行中…";
          wrap.appendChild(b);
        }
        (data.nodes || []).forEach((s) => {
          const b = document.createElement("span");
          const sync = s.lastSync ? new Date(s.lastSync).toLocaleString() : "从未同步";
          b.className = "nbadge" + (s.accessible ? "" : " bad");
          b.innerHTML = s.accessible
            ? `${esc(s.name)} · ${s.fileCount} 个文件`
            : `${esc(s.name)} · ⚠ 不可访问`;
          b.title = `${s.path}\n上次同步：${sync}`;
          wrap.appendChild(b);
        });
      })
      .catch(() => {
        $("#connStatus").textContent = "未连接";
        $("#connStatus").className = "badge";
      });
  }

  // ---------- 文件对比（核心视图） ----------
  const KIND_RANK = { "rename-conflict": 0, delete: 1, copy: 2, skip: 3 };
  const CELL_DEF = {
    src: { t: "● 源", cls: "c-src" },
    dst: { t: "⬆ 更新", cls: "c-dst" },
    del: { t: "✕ 删除", cls: "c-del" },
    conflict: { t: "🔺 已改", cls: "c-conf" },
    na: { t: "—", cls: "c-na" },
  };

  function normPath(p) {
    return String(p || "").replace(/\\/g, "/").replace(/\/+$/, "").toLowerCase();
  }

  // 根据动作的绝对路径推导某节点在此动作中的角色
  function cellFor(a, nodeName, rootMap) {
    const root = rootMap[nodeName] || "";
    const from = normPath(a.from);
    const to = normPath(a.to);
    const fromHere = root && from && (from === root || from.startsWith(root + "/"));
    const toHere = root && to && (to === root || to.startsWith(root + "/"));
    switch (a.kind) {
      case "copy":
        return fromHere ? "src" : toHere ? "dst" : "na";
      case "delete":
        return toHere ? "del" : "na";
      case "rename-conflict":
        return fromHere || toHere ? "conflict" : "na";
      default:
        return "na";
    }
  }

  function renderCompare(actions, names) {
    const counts = { copy: 0, delete: 0, "rename-conflict": 0 };
    actions.forEach((a) => (counts[a.kind] = (counts[a.kind] || 0) + 1));
    $("#cmpChips").innerHTML =
      `<span class="stat stat-copy">复制 ${counts.copy || 0}</span>` +
      `<span class="stat stat-del">删除 ${counts.delete || 0}</span>` +
      `<span class="stat stat-conf">冲突 ${counts["rename-conflict"] || 0}</span>`;

    // 风险横幅：冲突 / 删除永远置顶提示
    const risks = actions.filter((a) => a.kind === "rename-conflict" || a.kind === "delete");
    const banner = $("#riskBanner");
    if (risks.length) {
      const nc = counts["rename-conflict"] || 0;
      const nd = counts.delete || 0;
      const sample = risks
        .slice(0, 4)
        .map((a) => `<code>${esc(a.relPath)}</code>`)
        .join("、");
      banner.innerHTML =
        `<b>⚠️ 本次包含 ${nc ? "🔴 " + nc + " 个冲突" : ""}${nc && nd ? "，" : ""}${nd ? "🟠 " + nd + " 个删除" : ""}</b>` +
        `<span class="risk-sub">${sample}${risks.length > 4 ? ` 等 ${risks.length} 项` : ""}` +
        ` —— 冲突与删除会改动原文件，请核对下方列表后再执行。</span>`;
      banner.hidden = false;
    } else {
      banner.hidden = true;
    }

    const table = $("#cmpTable");
    const filters = $("#cmpFilters");
    window.__lastPlan = { actions, names };

    if (!actions.length) {
      filters.hidden = true;
      table.innerHTML = `<p class="empty ok-big">✅ ${esc(names.join(" ↔ "))} 已完全一致，无需同步。</p>`;
      return;
    }
    filters.hidden = false;

    // 各节点根路径（用于把动作的绝对路径映射到节点）
    const rootMap = {};
    const f = curField();
    (state.config.nodes || []).forEach((n) => {
      if (names.indexOf(n.name) >= 0) rootMap[n.name] = normPath(n[f] || "");
    });

    const sorted = actions.slice().sort((x, y) => (KIND_RANK[x.kind] ?? 9) - (KIND_RANK[y.kind] ?? 9));
    const filter = window.__cmpFilter || "all";
    const rows = filter === "all" ? sorted : sorted.filter((a) => a.kind === filter);

    let html =
      `<table class="cmp-table"><thead><tr><th class="col-file">文件（仅显示有差异的 ${actions.length} 个）</th>` +
      names.map((n) => `<th>${esc(n)}</th>`).join("") +
      `<th class="col-act">将执行</th></tr></thead><tbody>`;
    rows.slice(0, 300).forEach((a) => {
      const cells = names
        .map((nm) => {
          const c = CELL_DEF[cellFor(a, nm, rootMap)];
          return `<td><span class="cs ${c.cls}">${c.t}</span></td>`;
        })
        .join("");
      let act;
      if (a.kind === "copy") {
        const dst = names.find((nm) => cellFor(a, nm, rootMap) === "dst");
        act = `<span class="act-copy">⬆ 复制 → ${esc(dst || "?")}</span>`;
      } else if (a.kind === "delete") {
        act = `<span class="act-del">✕ 删除</span>`;
      } else if (a.kind === "rename-conflict") {
        act = `<span class="act-conf">⚠ 冲突归档</span>`;
      } else {
        act = `<span class="act-skip">跳过</span>`;
      }
      const rowCls =
        a.kind === "rename-conflict" ? "r-conflict" : a.kind === "delete" ? "r-delete" : "";
      html += `<tr class="${rowCls}"><td class="f">${esc(a.relPath)}</td>${cells}<td>${act}</td></tr>`;
    });
    html += "</tbody></table>";
    if (rows.length > 300) {
      html += `<p class="empty">… 还有 ${rows.length - 300} 个差异未显示（可用上方筛选缩小范围）</p>`;
    }
    table.innerHTML = html;
  }

  // ---------- 日志（迷你，可展开） ----------
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
    return Array.from(state.participating).filter((n) =>
      (state.config.nodes || []).some((x) => x.name === n)
    );
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
      renderCompare(data.actions || [], names);
      addLog(`预览完成：复制 ${data.copied} / 删除 ${data.deleted} / 冲突 ${data.conflicts}`, "ok");
    } catch (e) {
      toast("预览失败：" + e.message, true);
    }
  }

  async function runSync() {
    const names = selectedNodes();
    if (names.length < 2) return toast("请至少选择 2 个参与同步的节点", true);
    // P1-2：必须先预览获取计划 ID，执行时回传以消除 TOCTOU
    if (!state.planId) return toast("请先「预览对比」核对计划", true);
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

    // 对比筛选
    $$("#cmpFilters .chip").forEach((el) =>
      el.addEventListener("click", () => {
        $$("#cmpFilters .chip").forEach((c) => c.classList.remove("active"));
        el.classList.add("active");
        window.__cmpFilter = el.dataset.f;
        if (window.__lastPlan) renderCompare(window.__lastPlan.actions, window.__lastPlan.names);
      })
    );

    // 迷你日志点击展开/收起
    $("#log").addEventListener("click", (e) => {
      e.currentTarget.classList.toggle("expanded");
    });

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

    // 元信息（平台）→ 配置 → 状态
    getJSON("/api/meta")
      .then((meta) => {
        state.platform = meta.platform || "windows";
        return loadConfig();
      })
      .then(renderStatus)
      .catch(() => {
        $("#connStatus").textContent = "未连接";
        $("#connStatus").className = "badge";
        toast("无法连接后端，请确认程序已启动", true);
      });

    setInterval(renderStatus, 15000);
  }

  document.addEventListener("DOMContentLoaded", init);
})();
