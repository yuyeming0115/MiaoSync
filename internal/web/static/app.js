// MiaoSync 前端逻辑（原生 JS，无框架）。
// 布局原则：文件对比是绝对主角，模式场景化驱动，消灭配置与角色冲突，细粒度支持按文件勾选。
(function () {
  "use strict";

  const state = {
    config: null,
    platform: "windows",
    participating: new Set(),
    planId: null, // P1-2：预览返回的计划 ID，执行时回传以消除 TOCTOU
    drives: [],   // 可用驱动器盘符 / 卷列表
    nodeStatusMap: {}, // 节点状态缓存
    modePlanCache: {}, // 模式计划记忆: { mirror: { planId, actions, names, data, time }, backup: { ... } }
    modeSelections: {}, // 模式勾选记忆: { mirror: Set<string>, backup: Set<string> }
    hasPreviewed: false, // 标记用户是否已经执行过预览对比
  };

  function invalidatePlanCache(msg) {
    state.modePlanCache = {};
    state.modeSelections = {};
    state.planId = null;
    state.hasPreviewed = false;
    window.__selectedPaths = null;
    window.__lastPlan = null;
    window.__lastPlanId = null;
    const banner = $("#riskBanner");
    if (banner) banner.hidden = true;
    updateExecutionButton(0, 0);
    const table = $("#cmpTable");
    if (table) {
      table.innerHTML = `<p class="empty">${esc(msg || "配置已变动，请点击「👁 预览对比」重新核对差异。")}</p>`;
    }
    const bulk = $("#cmpBulkBar");
    if (bulk) bulk.hidden = true;
    const chips = $("#cmpChips");
    if (chips) chips.innerHTML = "";
  }

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
    if (!t) return;
    t.textContent = msg;
    t.className = "toast show" + (isErr ? " err" : "");
    setTimeout(() => (t.className = "toast"), 4000);
  }

  function curField() {
    return state.platform === "darwin" ? "mac-path" : "win-path";
  }

  // ---------- 模式意图选择 ----------
  function setMode(mode) {
    if (!state.config) return;
    const prevMode = state.config.mode;
    state.config.mode = mode;
    $("#mode").value = mode;

    const btnMirror = $("#modeMirrorBtn");
    const btnBackup = $("#modeBackupBtn");
    if (btnMirror && btnBackup) {
      if (mode === "mirror") {
        btnMirror.classList.add("active");
        btnBackup.classList.remove("active");
      } else {
        btnBackup.classList.add("active");
        btnMirror.classList.remove("active");
      }
    }
    renderNodes();

    // 模式切换记忆逻辑：
    // 若用户之前已经执行过预览对比（处于对比工作流中），切换模式时自动复用或重新计算，无需用户反复手点
    if (prevMode && prevMode !== mode) {
      const hadAnalysis = state.hasPreviewed || state.planId || Object.keys(state.modePlanCache).length > 0;
      if (hadAnalysis) {
        const cached = state.modePlanCache[mode];
        if (cached && Date.now() - cached.time < 9 * 60 * 1000) {
          // 1. 已有该模式有效缓存：0ms 秒切并恢复用户此模式下的勾选
          state.planId = cached.planId;
          if (!state.modeSelections[mode]) {
            state.modeSelections[mode] = new Set(cached.actions.map((a) => a.relPath));
          }
          window.__selectedPaths = state.modeSelections[mode];
          renderCompare(cached.actions, cached.names);
          addLog(`已切换至「${mode === 'mirror' ? '双向镜像' : '单向备份'}」模式（已记住并复用计划）`, "ok");
        } else {
          // 2. 目标模式暂无缓存：自动触发轻量快速预览（~0.16s），免去手动再次点击
          addLog(`已切换为「${mode === 'mirror' ? '双向镜像' : '单向备份'}」，正在自动核对差异...`);
          previewSync();
        }
      }
    }
  }

  // ---------- 渲染：节点编辑卡片 ----------
  function renderNodes() {
    const wrap = $("#nodeList");
    if (!wrap || !state.config) return;
    wrap.innerHTML = "";
    const cur = curField();
    const oth = cur === "mac-path" ? "win-path" : "mac-path";
    const curLbl = cur === "mac-path" ? "macOS 路径" : "Windows 路径";
    const othLbl = cur === "mac-path" ? "Windows 路径" : "macOS 路径";
    const mode = state.config.mode || "mirror";

    (state.config.nodes || []).forEach((n, i) => {
      const othVal = String(n[oth] || "").trim();
      const isOthSensible = cur === "win-path" ? !/^[a-zA-Z]:/.test(othVal) : othVal.startsWith("/");
      const hasOther = !!(othVal && isOthSensible);
      const st = state.nodeStatusMap[n.name] || {};
      const isBad = st.accessible === false;

      // 友好的人性化角色描述，无需用户手动选 peer/source/target
      let roleLabel = "双向节点";
      let roleCls = "node-role-badge";
      if (mode === "backup") {
        if (i === 0) {
          roleLabel = "源端 (Source 电脑)";
          roleCls = "node-role-badge";
        } else {
          roleLabel = "备份目标 (Target 硬盘)";
          roleCls = "node-role-badge target";
        }
      }

      const div = document.createElement("div");
      div.className = "node";
      div.innerHTML = `
        <div class="node-meta">
          <span class="${roleCls}">${roleLabel}</span>
          <span class="node-health ${isBad ? 'bad' : ''}">
            ${isBad ? '⚠️ 路径不可访问' : (st.fileCount != null ? `● 就绪 · ${st.fileCount} 文件` : '')}
          </span>
        </div>
        <div class="row">
          <input class="nm" data-i="${i}" value="${esc(n.name || "")}" placeholder="节点名称（如 win-pc / hdd）" />
          ${(state.config.nodes.length > 2) ? `<button class="del" data-i="${i}" title="删除节点">✕</button>` : ''}
        </div>
        <div class="row path-row">
          <input class="pp" data-i="${i}" data-f="${cur}" value="${esc(n[cur] || "")}" placeholder="${curLbl}，如 D:\\Sync" />
          <button class="browse" type="button" data-i="${i}" data-f="${cur}" title="选择文件夹 (置顶弹出)">📁</button>
        </div>
        <div class="drives-row" id="drivesRow-${i}"></div>
        <div class="row path-row other-row" data-i="${i}" ${hasOther ? "" : "hidden"}>
          <input class="pp" data-i="${i}" data-f="${oth}" value="${esc(n[oth] || "")}" placeholder="${othLbl}（跨平台时备用）" />
          <button class="browse" type="button" data-i="${i}" data-f="${oth}" title="选择文件夹">📁</button>
        </div>`;
      wrap.appendChild(div);

      // 快速填入可用盘符 chips
      renderDriveChips(div.querySelector(`#drivesRow-${i}`), i, cur);

      if (!hasOther) {
        const link = document.createElement("button");
        link.className = "mini link add-other";
        link.type = "button";
        link.textContent = "＋ " + othLbl + " (高级设置)";
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

  // 渲染盘符快速填充按钮
  function renderDriveChips(container, nodeIndex, field) {
    if (!container || !state.drives || !state.drives.length) return;
    container.innerHTML = `<span style="font-size:11px;color:var(--text-dim);margin-right:2px;">快捷盘符:</span>`;
    state.drives.forEach((d) => {
      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "drive-chip" + (d.removable ? " removable" : "");
      btn.title = d.removable ? "可移动存储盘符" : "本地驱动器";
      btn.textContent = (d.removable ? "💾 " : "") + d.letter;
      btn.addEventListener("click", () => {
        const input = document.querySelector(`#nodeList .pp[data-f="${field}"][data-i="${nodeIndex}"]`);
        if (!input) return;
        const curVal = input.value.trim();
        if (!curVal) {
          input.value = d.path + "Sync";
        } else {
          // 替换前缀盘符
          const m = curVal.match(/^[A-Za-z]:[\\\/](.*)$/);
          if (m) {
            input.value = d.letter + "\\" + m[1];
          } else {
            input.value = d.path + "Sync";
          }
        }
        state.config.nodes[nodeIndex][field] = input.value;
        renderStatus();
      });
      container.appendChild(btn);
    });
  }

  function bindNodeInputs() {
    $$("#nodeList .nm").forEach((el) =>
      el.addEventListener("input", (e) => {
        state.config.nodes[+e.target.dataset.i].name = e.target.value;
      })
    );
    $$("#nodeList .pp").forEach((el) => {
      el.addEventListener("input", (e) => {
        const i = +e.target.dataset.i;
        const f = e.target.dataset.f;
        state.config.nodes[i][f] = e.target.value;
      });
      el.addEventListener("change", () => {
        invalidatePlanCache("路径已修改，请点击「👁 预览对比」重新核对差异。");
        postJSON("/api/config", gatherConfig()).then(renderStatus).catch(() => {});
      });
    });
    $$("#nodeList .browse").forEach((el) =>
      el.addEventListener("click", (e) => {
        const i = +e.currentTarget.dataset.i;
        const field = e.currentTarget.dataset.f;
        const input = document.querySelector(`#nodeList .pp[data-f="${field}"][data-i="${i}"]`);
        if (!input) return;
        const btn = e.currentTarget;
        btn.disabled = true;
        btn.textContent = "…";
        postJSON("/api/pick-folder", { initialPath: input.value.trim() })
          .then((data) => {
            if (data.path) {
              input.value = data.path;
              state.config.nodes[i][field] = data.path;
              invalidatePlanCache("路径已修改，请点击「👁 预览对比」重新核对差异。");
              postJSON("/api/config", gatherConfig()).then(renderStatus).catch(() => {});
            }
          })
          .catch((err) => toast("选择文件夹失败：" + err.message, true))
          .finally(() => {
            btn.disabled = false;
            btn.textContent = "📁";
          });
      })
    );
    $$("#nodeList .del").forEach((el) =>
      el.addEventListener("click", (e) => {
        const i = +e.target.dataset.i;
        const name = state.config.nodes[i].name;
        state.config.nodes.splice(i, 1);
        state.participating.delete(name);
        invalidatePlanCache("节点列表已修改，请点击「👁 预览对比」重新核对差异。");
        renderNodes();
      })
    );
  }

  function renderParticipate() {
    const wrap = $("#participate");
    if (!wrap || !state.config) return;
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

  // ---------- 渲染：节点状态 ----------
  function renderStatus() {
    getJSON("/api/status")
      .then((data) => {
        const btnSync = $("#btnSync");
        if (btnSync && data.running) btnSync.disabled = true;

        const cfgBanner = $("#cfgBanner");
        if (data.configError) {
          cfgBanner.innerHTML =
            `<b>⚠️ 配置提示</b><span class="cfg-sub">${esc(data.configError)}` +
            ` —— 修正路径后点「💾 保存配置」即可。</span>`;
          cfgBanner.hidden = false;
        } else {
          cfgBanner.hidden = true;
        }

        const wrap = $("#nodeBadges");
        if (wrap) {
          wrap.innerHTML = "";
          if (data.running) {
            const b = document.createElement("span");
            b.className = "nbadge run";
            b.textContent = "🔄 同步进行中…";
            wrap.appendChild(b);
          }
          (data.nodes || []).forEach((s) => {
            state.nodeStatusMap[s.name] = s;
            const b = document.createElement("span");
            const sync = s.lastSync ? new Date(s.lastSync).toLocaleString() : "从未同步";
            b.className = "nbadge" + (s.accessible ? "" : " bad");
            b.innerHTML = s.accessible
              ? `${esc(s.name)} · ${s.fileCount} 文件`
              : `${esc(s.name)} · ⚠ 不可访问`;
            b.title = `${s.path}\n上次同步：${sync}`;
            wrap.appendChild(b);
          });
        }
      })
      .catch(() => {
        const cs = $("#connStatus");
        if (cs) {
          cs.textContent = "未连接";
          cs.className = "badge";
        }
      });
  }

  // 加载可用盘符列表
  async function loadDrives() {
    try {
      state.drives = await getJSON("/api/drives");
      renderNodes();
    } catch (_) {}
  }

  // ---------- 文件对比（核心视窗 + 复选框细粒度选择） ----------
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
    window.__lastPlan = { actions, names };
    const curMode = state.config ? (state.config.mode || "mirror") : "mirror";
    if (!state.modeSelections[curMode]) {
      state.modeSelections[curMode] = new Set(actions.map((a) => a.relPath));
    }
    window.__selectedPaths = state.modeSelections[curMode];
    window.__lastPlanId = state.planId;

    const table = $("#cmpTable");
    const bulkBar = $("#cmpBulkBar");

    if (!actions.length) {
      if (bulkBar) bulkBar.hidden = true;
      $("#cmpChips").innerHTML = "";
      table.innerHTML = `<p class="empty ok-big">✅ ${esc(names.join(" ↔ "))} 数据已完全一致，无需同步。</p>`;
      updateExecutionButton(0, 0);
      return;
    }

    if (bulkBar) bulkBar.hidden = false;

    // 各节点根路径映射
    const rootMap = {};
    const f = curField();
    (state.config.nodes || []).forEach((n) => {
      if (names.indexOf(n.name) >= 0) rootMap[n.name] = normPath(n[f] || "");
    });

    const sorted = actions.slice().sort((x, y) => (KIND_RANK[x.kind] ?? 9) - (KIND_RANK[y.kind] ?? 9));
    const filter = window.__cmpFilter || "all";
    const rows = filter === "all" ? sorted : sorted.filter((a) => a.kind === filter);

    let html =
      `<table class="cmp-table"><thead><tr>` +
      `<th class="col-chk"></th>` +
      `<th class="col-file">文件（共 ${actions.length} 项差异）</th>` +
      names.map((n) => `<th>${esc(n)}</th>`).join("") +
      `<th class="col-act">将执行</th></tr></thead><tbody>`;

    rows.slice(0, 500).forEach((a) => {
      const isChecked = window.__selectedPaths.has(a.relPath);
      const cells = names
        .map((nm) => {
          const c = CELL_DEF[cellFor(a, nm, rootMap)] || CELL_DEF.na;
          return `<td><span class="cs ${c.cls}">${c.t}</span></td>`;
        })
        .join("");

      let act;
      if (!isChecked) {
        act = `<span class="act-skip-badge">本次跳过</span>`;
      } else if (a.kind === "copy") {
        const dst = names.find((nm) => cellFor(a, nm, rootMap) === "dst");
        act = `<span class="act-copy">⬆ 复制 → ${esc(dst || "?")}</span>`;
      } else if (a.kind === "delete") {
        act = `<span class="act-del">✕ 删除</span>`;
      } else if (a.kind === "rename-conflict") {
        act = `<span class="act-conf">⚠ 冲突归档</span>`;
      } else {
        act = `<span class="act-skip">跳过</span>`;
      }

      let rowCls = isChecked ? "" : "r-skipped";
      if (a.kind === "rename-conflict") rowCls += " r-conflict";
      else if (a.kind === "delete") rowCls += " r-delete";

      html += `<tr class="${rowCls.trim()}">
        <td class="col-chk">
          <input type="checkbox" class="cmp-chk" data-path="${esc(a.relPath)}" ${isChecked ? "checked" : ""} />
        </td>
        <td class="f">${esc(a.relPath)}</td>
        ${cells}
        <td>${act}</td>
      </tr>`;
    });
    html += "</tbody></table>";
    if (rows.length > 500) {
      html += `<p class="empty">… 还有 ${rows.length - 500} 个差异未显示（可用上方分类筛选）</p>`;
    }
    table.innerHTML = html;

    // 绑定逐项复选框点击
    $$("#cmpTable .cmp-chk").forEach((chk) => {
      chk.addEventListener("change", (e) => {
        const p = e.target.dataset.path;
        const curMode = state.config ? (state.config.mode || "mirror") : "mirror";
        if (e.target.checked) {
          window.__selectedPaths.add(p);
          if (state.modeSelections[curMode]) state.modeSelections[curMode].add(p);
        } else {
          window.__selectedPaths.delete(p);
          if (state.modeSelections[curMode]) state.modeSelections[curMode].delete(p);
        }
        renderCompare(actions, names);
      });
    });

    updateMetricsAndChips(actions);
  }

  // 动态更新指标卡片、全选框与执行按钮
  function updateMetricsAndChips(actions) {
    const selectedPaths = window.__selectedPaths || new Set();
    const selActions = actions.filter((a) => selectedPaths.has(a.relPath));

    const counts = { copy: 0, delete: 0, "rename-conflict": 0 };
    selActions.forEach((a) => (counts[a.kind] = (counts[a.kind] || 0) + 1));

    $("#cmpChips").innerHTML =
      `<span class="stat stat-copy">待复制 ${counts.copy || 0}</span>` +
      `<span class="stat stat-del">待删除 ${counts.delete || 0}</span>` +
      `<span class="stat stat-conf">待冲突归档 ${counts["rename-conflict"] || 0}</span>`;

    // 风险横幅
    const risks = selActions.filter((a) => a.kind === "rename-conflict" || a.kind === "delete");
    const banner = $("#riskBanner");
    if (risks.length) {
      const nc = counts["rename-conflict"] || 0;
      const nd = counts.delete || 0;
      const sample = risks.slice(0, 4).map((a) => `<code>${esc(a.relPath)}</code>`).join("、");
      banner.innerHTML =
        `<b>🛡️ 已勾选项包含 ${nc ? "🔴 " + nc + " 个冲突归档" : ""}${nc && nd ? "，" : ""}${nd ? "🟠 " + nd + " 个删除" : ""}</b>` +
        `<span class="risk-sub">${sample}${risks.length > 4 ? ` 等 ${risks.length} 项` : ""}` +
        ` —— 冲突旧版将安全备份至 .miaosync/conflicts/，若不确定可随时取消勾选。</span>`;
      banner.hidden = false;
    } else {
      banner.hidden = true;
    }

    // 批量控制栏数字
    const selCount = selActions.length;
    const totalCount = actions.length;
    const countEl = $("#bulkSelCount");
    const totalEl = $("#bulkTotalCount");
    if (countEl) countEl.textContent = selCount;
    if (totalEl) totalEl.textContent = totalCount;

    const chkAll = $("#chkSelectAll");
    if (chkAll) {
      chkAll.checked = selCount === totalCount && totalCount > 0;
      chkAll.indeterminate = selCount > 0 && selCount < totalCount;
    }

    updateExecutionButton(selCount, totalCount);
  }

  function updateExecutionButton(selCount, totalCount) {
    const btn = $("#btnSync");
    if (!btn) return;
    if (selCount === 0) {
      btn.disabled = true;
      btn.textContent = "请至少勾选 1 个文件";
    } else {
      btn.disabled = false;
      const skip = totalCount - selCount;
      btn.textContent = `🚀 执行选中的 ${selCount} 项${skip > 0 ? ` (跳过 ${skip} 项)` : ""}`;
    }
  }

  // ---------- 日志 ----------
  function addLog(msg, cls) {
    const log = $("#log");
    if (!log) return;
    const div = document.createElement("div");
    div.className = cls || "";
    div.textContent = msg;
    log.appendChild(div);
    log.scrollTop = log.scrollHeight;
  }
  function setProgress(done, total) {
    const pct = total > 0 ? Math.round((done / total) * 100) : done > 0 ? 100 : 0;
    const p = $("#progressBar");
    if (p) p.style.width = pct + "%";
  }

  // ---------- 配置与操作 ----------
  function gatherConfig() {
    return {
      nodes: state.config.nodes,
      mode: state.config.mode || $("#mode").value || "mirror",
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
    setMode(state.config.mode || "mirror");
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

    const btnPrev = $("#btnPreview");
    if (btnPrev) {
      btnPrev.disabled = true;
      btnPrev.textContent = "⏳ 正在分析对比...";
    }
    $("#cmpTable").innerHTML = `<p class="empty">⏳ 正在扫描并分析两端文件差异，请稍候...</p>`;
    $("#cmpBulkBar").hidden = true;
    $("#cmpChips").innerHTML = "";

    try {
      // 关键体验优化：预览前自动持久化当前界面填写的路径与配置，避免使用旧配置
      await postJSON("/api/config", gatherConfig());
      const mode = state.config.mode || $("#mode").value || "mirror";
      const data = await postJSON("/api/plan", {
        nodes: names,
        mode: mode,
        conflict: $("#conflict").value,
      });
      state.planId = data.planId;
      state.hasPreviewed = true;

      // 写入模式计划缓存
      state.modePlanCache[mode] = {
        planId: data.planId,
        actions: data.actions || [],
        names: names,
        data: data,
        time: Date.now(),
      };

      // 维护模式勾选记忆
      if (!state.modeSelections[mode]) {
        state.modeSelections[mode] = new Set((data.actions || []).map((a) => a.relPath));
      }
      window.__selectedPaths = state.modeSelections[mode];

      renderCompare(data.actions || [], names);
      renderStatus();
      addLog(`预览成功（${mode === 'mirror' ? '双向镜像' : '单向备份'}）：复制 ${data.copied} / 删除 ${data.deleted} / 冲突 ${data.conflicts}`, "ok");
    } catch (e) {
      toast("预览失败：" + e.message, true);
      addLog("预览失败：" + e.message, "err");
      $("#cmpTable").innerHTML = `<p class="empty" style="color:var(--danger)">⚠️ 预览对比失败：${esc(e.message)}</p>`;
    } finally {
      if (btnPrev) {
        btnPrev.disabled = false;
        btnPrev.textContent = "👁 预览对比";
      }
    }
  }

  async function runSync() {
    const names = selectedNodes();
    if (names.length < 2) return toast("请至少选择 2 个参与同步的节点", true);
    if (!state.planId) return toast("请先「预览对比」核对计划", true);

    const selectedPaths = Array.from(window.__selectedPaths || []);
    if (selectedPaths.length === 0) return toast("未勾选任何文件，已取消同步", true);

    const btn = $("#btnSync");
    btn.disabled = true;
    $("#log").innerHTML = "";
    setProgress(0, 0);
    addLog(`开始同步：${names.join(" ↔ ")}（已勾选 ${selectedPaths.length} 个文件）`, "ok");
    const confirmDanger = !!window.__confirmDanger;
    window.__confirmDanger = false;
    try {
      const resp = await fetch("/api/sync", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          nodes: names,
          mode: state.config.mode || $("#mode").value || "mirror",
          conflict: $("#conflict").value,
          confirmDangerous: confirmDanger,
          planId: state.planId,
          selectedPaths: selectedPaths,
        }),
      });
      if (!resp.ok) {
        const err = await resp.json().catch(() => ({}));
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
      addLog(`[${evt.stage}] ${evt.msg} (${evt.done}/${evt.total})`);
    } else if (evt.type === "done") {
      const s = evt.summary || {};
      invalidatePlanCache("已成功同步完毕。可点击「👁 预览对比」重新核对两端状态。");
      addLog(`完成！复制 ${s.copied} / 删除 ${s.deleted} / 冲突 ${s.conflicts}`, "ok");
      renderReportModal(s);
    } else if (evt.type === "error") {
      addLog("失败：" + evt.message, "err");
      toast("同步失败：" + evt.message, true);
    }
  }

  // ---------- 渲染完成报告弹层 ----------
  function renderReportModal(s) {
    window.__lastReport = s;
    const modal = $("#reportModal");
    if (!modal) return;
    const isWarn = !!s.requiresConfirm || (s.errors && s.errors > 0);
    const head = $("#reportHead");
    head.className = "report-head" + (isWarn ? " is-warn" : "");
    head.innerHTML = `
      <div class="report-title-block">
        <span class="ico">${isWarn ? "⚠️" : "🎉"}</span>
        <div>
          <h3 class="title">${isWarn ? "需要二次确认" : "同步完成"}</h3>
          <p class="sub">${esc(s.message || (isWarn ? "检测到大批量操作" : "文件已安全同步"))}</p>
        </div>
      </div>
      <div>耗时 <b>${s.durationMs || 0} ms</b></div>`;

    $("#reportCenter").textContent = s.totalActions || 0;
    $("#reportTiles").innerHTML = `
      <div class="tile"><span>复制写入</span><b>${s.copied || 0}</b></div>
      <div class="tile"><span>冲突归档</span><b>${s.conflicts || 0}</b></div>
      <div class="tile"><span>同步删除</span><b>${s.deleted || 0}</b></div>
      <div class="tile"><span>跳过/保留</span><b>${s.skipped || 0}</b></div>`;

    renderDetail(s);

    const btnConfirm = $("#btnConfirmDanger");
    if (btnConfirm) btnConfirm.hidden = !s.requiresConfirm;
    modal.hidden = false;
  }

  function renderDetail(s) {
    const list = $("#reportDetail");
    const items = s.transferred || [];
    const filter = window.__detailFilter || "all";
    const filtered = filter === "all" ? items : items.filter((it) => it.kind === filter);

    const countEl = $("#detailCount");
    if (countEl) countEl.textContent = filtered.length;

    if (!filtered.length) {
      list.innerHTML = `<p class="x-empty">无相关传输记录。</p>`;
      return;
    }
    list.innerHTML = "";
    filtered.slice(0, 200).forEach((it) => {
      const row = document.createElement("div");
      row.className = "detail-row";
      row.innerHTML = `<span class="detail-path">${esc(it.path)}</span><span class="detail-size">${it.size || 0} B</span>`;
      list.appendChild(row);
    });
  }

  function esc(s) {
    return String(s == null ? "" : s)
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;")
      .replace(/"/g, "&quot;");
  }

  // ---------- 初始化与事件绑定 ----------
  function init() {
    $("#btnAddNode").addEventListener("click", () => {
      state.config.nodes.push({ name: "", "win-path": "", "mac-path": "", role: "peer" });
      invalidatePlanCache("节点列表已修改，请点击「👁 预览对比」重新核对差异。");
      renderNodes();
    });
    $("#btnSave").addEventListener("click", saveConfig);
    $("#btnPreview").addEventListener("click", previewSync);
    $("#btnSync").addEventListener("click", runSync);
    $("#btnRefresh").addEventListener("click", () => {
      renderStatus();
      loadDrives();
      toast("已刷新节点与驱动器状态");
    });

    // 模式切换大卡片
    const btnMirror = $("#modeMirrorBtn");
    const btnBackup = $("#modeBackupBtn");
    if (btnMirror) btnMirror.addEventListener("click", () => setMode("mirror"));
    if (btnBackup) btnBackup.addEventListener("click", () => setMode("backup"));

    // 批量全选 / 仅安全项 / 取消删除
    const chkAll = $("#chkSelectAll");
    if (chkAll) {
      chkAll.addEventListener("change", (e) => {
        if (!window.__lastPlan) return;
        const curList = window.__cmpFilter && window.__cmpFilter !== "all"
          ? window.__lastPlan.actions.filter((a) => a.kind === window.__cmpFilter)
          : window.__lastPlan.actions;
        if (e.target.checked) {
          curList.forEach((a) => window.__selectedPaths.add(a.relPath));
        } else {
          curList.forEach((a) => window.__selectedPaths.delete(a.relPath));
        }
        const curMode = state.config ? (state.config.mode || "mirror") : "mirror";
        state.modeSelections[curMode] = new Set(window.__selectedPaths);
        renderCompare(window.__lastPlan.actions, window.__lastPlan.names);
      });
    }

    const btnSafe = $("#btnSelectSafe");
    if (btnSafe) {
      btnSafe.addEventListener("click", () => {
        if (!window.__lastPlan) return;
        window.__selectedPaths.clear();
        window.__lastPlan.actions.forEach((a) => {
          if (a.kind === "copy") window.__selectedPaths.add(a.relPath);
        });
        const curMode = state.config ? (state.config.mode || "mirror") : "mirror";
        state.modeSelections[curMode] = new Set(window.__selectedPaths);
        renderCompare(window.__lastPlan.actions, window.__lastPlan.names);
      });
    }

    const btnNoDel = $("#btnUncheckDeletions");
    if (btnNoDel) {
      btnNoDel.addEventListener("click", () => {
        if (!window.__lastPlan) return;
        window.__lastPlan.actions.forEach((a) => {
          if (a.kind === "delete") window.__selectedPaths.delete(a.relPath);
        });
        const curMode = state.config ? (state.config.mode || "mirror") : "mirror";
        state.modeSelections[curMode] = new Set(window.__selectedPaths);
        renderCompare(window.__lastPlan.actions, window.__lastPlan.names);
      });
    }

    // 分类筛选 chips
    $$("#cmpFilters .chip").forEach((el) =>
      el.addEventListener("click", () => {
        $$("#cmpFilters .chip").forEach((c) => c.classList.remove("active"));
        el.classList.add("active");
        window.__cmpFilter = el.dataset.f;
        if (window.__lastPlan) renderCompare(window.__lastPlan.actions, window.__lastPlan.names);
      })
    );

    // 迷你日志点击展开/收起
    const logEl = $("#log");
    if (logEl) {
      logEl.addEventListener("click", (e) => {
        e.currentTarget.classList.toggle("expanded");
      });
    }

    // 报告弹层
    $$("#reportModal [data-close]").forEach((el) =>
      el.addEventListener("click", () => ($("#reportModal").hidden = true))
    );
    const btnDanger = $("#btnConfirmDanger");
    if (btnDanger) {
      btnDanger.addEventListener("click", () => {
        $("#reportModal").hidden = true;
        window.__confirmDanger = true;
        runSync();
      });
    }

    // 传输明细筛选
    $$("#detailFilters .chip").forEach((el) =>
      el.addEventListener("click", () => {
        $$("#detailFilters .chip").forEach((c) => c.classList.remove("active"));
        el.classList.add("active");
        window.__detailFilter = el.dataset.f;
        if (window.__lastReport) renderDetail(window.__lastReport);
      })
    );

    // 获取元信息（平台）、驱动器列表、配置与状态
    getJSON("/api/meta")
      .then((meta) => {
        state.platform = meta.platform || "windows";
        return loadConfig();
      })
      .then(loadDrives)
      .then(renderStatus)
      .catch(() => {
        const cs = $("#connStatus");
        if (cs) {
          cs.textContent = "未连接";
          cs.className = "badge";
        }
        toast("无法连接后端，请确认程序已启动", true);
      });

    setInterval(renderStatus, 15000);
  }

  document.addEventListener("DOMContentLoaded", init);
})();
