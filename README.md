# MiaoSync 🐱

> 你的 Win / Mac / 移动硬盘三端同步备份猫

MiaoSync 是一个开源的、跨平台的个人数据同步备份工具，专注于在三端之间保持一致：
**Windows 个人电脑 · macOS 个人电脑 · 移动硬盘**。

不依赖云服务，数据始终在你自己的设备上流转 —— 插上硬盘就能同步，拔掉也能各自工作。

---

## ✨ 当前状态

> 🟢 **可信内核 v0.1.0（内置 Web GUI）**。开箱即用，无需命令行。
>
> ⚠️ **数据安全警示（v0.1.0）**：同步内核已完成数据安全重构（单路径决策矩阵、原子写、失败不提交快照、全局运行锁），并通过故障注入验收测试。**但仍建议仅用于有额外备份的数据，并始终先预览**；待里程碑 2（真实文件系统验证矩阵）完成后摘除本警示。

本仓库最初是骨架（Phase 0），现已完成第一版**可用 MVP**，并完成「可信内核」数据安全重构：

- ✅ **内置 Web 图形界面**：启动后自动打开浏览器，点点点就能配节点、预览、同步
- ✅ **三端同步引擎**：任意 2~3 个目录之间两两同步（Win / Mac / 硬盘）
- ✅ **双向同步 + 单向备份**：`mirror` / `backup` 两种模式
- ✅ **增量同步**：基于 SHA-256 内容哈希 + 修改时间，只动变化的部分
- ✅ **冲突处理**：同名文件两端都改了？自动保留冲突副本（`.conflict-<时间戳>`），**永不静默覆盖**
- ✅ **危险操作保护**：某端「几乎被清空」时暂停并二次确认（防误触）
- ✅ **单二进制分发**：Go 编译，无运行时依赖，一个 `miaosync.exe` 搞定

**可信内核（2026-10 重构，验收通过）**：

- ✅ **单路径单决策**：每个文件一次运行只得到一个最终动作（删除场景不再同时产生"恢复复制+同步删除"）
- ✅ **原子写入**：临时文件 → fsync → hash 校验 → 原子 rename；任何失败不破坏已有目标文件
- ✅ **失败不提交快照**：任一动作失败时快照保持上次成功基线，不固化半截状态
- ✅ **全局运行锁**：同一时间只允许一个同步任务（并发请求返回 409）
- ✅ **不可变计划**：预览生成带指纹的计划，执行时校验文件状态，预览后文件变化即拒绝执行（消除 TOCTOU）
- ✅ **路径合法性校验**：拒绝相同/嵌套根目录、系统根目录、符号链接越界、大小写冲突
- ✅ **冲突副本归档**：冲突备份统一存入 `.miaosync/conflicts/`，不再散落业务目录

> 加密备份、插盘自动触发、定时同步等为后续阶段（见路线图）。

---

## 🚀 快速开始（MVP，GUI 版）

### 方式一：直接运行（推荐给验收/测试）

1. 拿到构建产物 `miaosync.exe`（Windows）或对应平台的二进制。
2. **插上你的移动硬盘**，记下它的同步目录（如 `E:\Sync`）。
3. 双击 `miaosync.exe`。
   - 它会在同目录自动生成一份 `miaosync.yaml`（如不存在）。
   - 自动打开浏览器，访问 `http://127.0.0.1:7480/`。
4. 在网页里：
   - 把每个节点的路径改成你**真实的同步目录**（如 `D:\Sync`、`E:\Sync`）。
   - 勾选要参与本次同步的节点（至少 2 个）。
   - 选择模式（`mirror` 双向 / `backup` 单向）与冲突策略。
   - 点 **预览**，确认计划无误。
   - 点 **开始同步**，实时看进度与日志。

> 💡 第一次跑建议先用 `mirror` + **预览**，确认计划里只有你期望的复制/冲突动作，再正式执行。

### 方式二：从源码构建

```bash
git clone https://github.com/yuyeming0115/MiaoSync.git
cd MiaoSync
./scripts/build.sh          # 产物在 dist/（Windows / macOS 双架构）
# 或直接跑：
go run ./cmd/miaosync       # 开发态，自动开浏览器
```

可选启动参数：

```bash
miaosync -config my.yaml    # 指定配置文件（默认 miaosync.yaml）
miaosync -port 9000         # 修改监听端口（默认 7480）
miaosync -no-open           # 不自动打开浏览器
```

---

## 🧭 支持平台

| 平台 | 状态 | 备注 |
|---|---|---|
| Windows 10/11 | 🟢 可用 | 路径如 `D:\Sync`；`miaosync.exe` 直接跑 |
| macOS 12+ | 🟢 可用 | 路径如 `/Users/you/Sync`；交叉编译产物 `miaosync-darwin-*` |
| 移动硬盘 | 🟢 可用 | exFAT/FAT32/APFS/NTFS 均可；依赖内容哈希而非 mtime |

---

## 🧩 同步行为说明

### 模式

| 模式 | 说明 | 适用 |
|---|---|---|
| `mirror`（双向） | 两端互相补齐，删除也互相同步 | 三端互为镜像 |
| `backup`（单向） | 仅从 source → target 复制，target 的删除不回灌 source | 硬盘只作备份盘 |

### 冲突策略（同名文件两端都改了）

| 策略 | 行为 |
|---|---|
| `rename`（默认） | 保留较新一方，旧版本归档到 `.miaosync/conflicts/` 目录，数据不丢 |
| `keep` | 两端各自保留，旧版归档到 `.miaosync/conflicts/` |
| `newer` | 仅保留 mtime 较新者，旧者直接删除 |

> 原则：**永不静默覆盖**。任何冲突都留下可追溯的副本。

### 危险操作保护

`mirror` 模式下，当某次同步计划要删除的文件数超过安全阈值（**绝对 > 50 个，或相对 > 当前文件总数的 30%**）时，引擎会**中止并要求确认**，不会擅自清空你的目录。确认无误后在界面点击「确认危险操作」再执行。

---

## 🗂️ 配置（`miaosync.yaml`）

首次启动会在程序同目录生成默认配置。也可复制示例手动改：

```bash
cp config/miaosync.example.yaml miaosync.yaml
```

```yaml
nodes:
  - name: win-pc
    win-path: "D:\\Sync"      # Windows 下用这个
    mac-path: ""              # 不在 Mac 上出现可留空
    role: peer
  - name: hdd
    win-path: "E:\\Sync"      # 移动硬盘在 Windows 下的盘符
    mac-path: "/Volumes/Backup/Sync"
    role: peer

mode: mirror                  # mirror | backup
conflict: rename             # rename | keep | newer
ignore:
  - ".DS_Store"
  - "Thumbs.db"
  - ".git"
  - ".miaosync"              # 快照目录自身不参同步
```

> `maiosync.yaml` / `config/miaosync.yaml` 已被 `.gitignore` 忽略，含真实路径，**不会误提交**。

---

## 🏗️ 架构

MVP 采用 **「单二进制 + 内嵌 Web」** 方案：Go 编译出一个无依赖的可执行文件，内置静态前端（HTML/CSS/JS）与 REST/SSE 后端，启动后自动开浏览器。这样「插硬盘直接测」零安装成本。

```
        ┌──────────────────────────────┐
        │    miaosync.exe (单二进制)    │
        │  ┌──────────┐  ┌───────────┐  │
        │  │ Web GUI  │  │  sync 引擎 │  │
        │  │(embed静态)│  │ scan/plan/ │  │
        │  └────┬─────┘  │  apply     │  │
        │       │ REST/SSE│           │  │
        └───────┼────────┴─────┬─────┘  │
                │              │
        浏览器(127.0.0.1:7480) │
                                │
        ┌─────────┼─────────┐
      ┌─▼─┐    ┌──▼──┐   ┌──▼──┐
      Win │    │ macOS│   │ HDD │   ← 三个 Node（根目录）
      └───┘    └─────┘   └─────┘
```

目录结构：

```
MiaoSync/
├── README.md
├── LICENSE                   # MIT
├── go.mod / go.sum
├── cmd/miaosync/main.go      # 入口：启动 Web 服务 + 自动开浏览器
├── internal/
│   ├── sync/                 # 同步引擎（scan/plan/apply/快照/哈希/测试）
│   ├── fs/                   # 跨平台路径/忽略封装
│   ├── config/               # YAML 配置加载校验
│   └── web/                  # 内嵌 Web GUI（server.go / api.go / static/）
├── config/miaosync.example.yaml
├── docs/{design.md,roadmap.md}
└── scripts/build.sh          # 跨平台交叉编译
```

---

## 📌 已知限制（MVP 阶段）

- **无加密**：硬盘丢了数据可被直接读取（加密备份在路线图）。
- **无定时 / 插盘自动触发**：当前需手动在界面点「开始同步」。
- **删除同步需谨慎**：`mirror` 模式会同步删除，已有危险保护但仍建议先预览。
- **大文件未分块**：当前整文件复制，超大文件（数十 GB）无断点续传。
- **仅本地运行**：服务监听 `127.0.0.1`，不对外暴露，数据不出本机。

---

## 🗺️ 路线图

见 [docs/roadmap.md](docs/roadmap.md)。

---

## 🤝 贡献

欢迎 Issue / PR。MVP 刚落地，任何想法先开 Issue 讨论。

## 📄 许可证

[MIT](LICENSE) © MiaoSync contributors
