# MiaoSync 路线图（Roadmap）

> 当前状态：**Phase 0 — 骨架**。所有后续阶段为计划。

## Phase 0 — 仓库骨架 ✅（当前）
- [x] 命名与定位（MiaoSync 🐱）
- [x] README / 设计草案 / 路线图
- [x] 目录结构与占位代码
- [x] 跨平台构建脚本骨架

## Phase 1 — 单端快照
- [ ] 配置加载（YAML）
- [ ] `scan()` 扫描单 Node，生成 FileState 表
- [ ] 快照落盘（`.miaosync/snapshot.json`）
- [ ] `miaosync status` 基本输出

## Phase 2 — 双端同步 + 冲突
- [ ] 比对两两 Node，生成 Action 计划
- [ ] `mirror` / `backup` 两种模式
- [ ] 冲突检测与 `rename` / `keep` / `newer` 策略
- [ ] `sync --dry-run` 预览

## Phase 3 — 三端 + 移动硬盘
- [ ] 三 Node 链式同步（A↔B↔C）
- [ ] 移动硬盘动态路径（Win 盘符 / Mac 挂载点）
- [ ] exFAT 特殊处理（无符号链接/权限、2s 时间精度 → 依赖 hash）

## Phase 4 — 安全与健壮
- [ ] 静态加密备份（age/gpg）
- [ ] 执行后 hash 校验
- [ ] 危险操作保护（防误清空）
- [ ] 增量传输优化（大文件分块）

## Phase 5 — 体验
- [ ] GUI / 系统托盘（可选）
- [ ] 定时同步 / 插盘自动触发
- [ ] 多语言文档

---

## 发布节奏建议
- 每完成一个 Phase 打一个 tag（`v0.1.0` ...）。
- 早期以 CLI + 文档为主，GUI 放到后期。
