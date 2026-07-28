# MiaoSync 设计草案（Design Draft）

> 状态：草案 / 待评审。骨架阶段，以下为预期设计，实现时可能调整。

## 1. 问题定义

用户在三处保存个人数据：

- **Win 电脑**（如 `D:\Sync`）
- **Mac 电脑**（如 `/Users/you/Sync`）
- **移动硬盘**（插到哪台就是哪台的 `E:\Sync` 或 `/Volumes/Backup/Sync`）

目标：让这三处中的指定目录保持一致，且**不依赖任何云服务商**。

典型场景：

- 在家 Mac 上改了文档 → 插硬盘同步 → 公司 Win 上插硬盘同步 → 拿到最新版。
- 移动硬盘作为「中转 + 备份」，断网也能工作。

---

## 2. 核心模型

### 2.1 Node（同步端点）

```text
Node {
  name: string   // 端点名，如 "win-pc" / "mac-pc" / "hdd"
  path: string   // 根目录绝对路径
  role: enum     // "peer"（对等）| "source" | "target"
}
```

移动硬盘的 `path` 是**动态**的：在 Win 下是盘符，在 Mac 下是挂载点。配置里允许为同一 Node 指定多平台路径（见配置示例）。

### 2.2 FileState（文件快照）

每个 Node 定期扫描，得到一份文件状态表：

```text
FileState {
  path:  string   // 相对根目录的路径（统一用 "/" 分隔）
  size:  int64
  mtime: int64    // 修改时间（纳秒）
  hash:  string   // 内容哈希（用于增量比对，BLAKE3 / xxHash）
}
```

快照存于各 Node 根目录下的 `.miaosync/snapshot.json`（或 SQLite），用于下次增量比对，避免全量扫描。

### 2.3 Action（同步动作）

引擎比对两端快照后，产出动作列表：

```text
Action {
  kind: enum  // "copy" | "delete" | "rename-conflict"
  from: string
  to:   string
}
```

---

## 3. 同步策略

| 模式 | 说明 | 适用 |
|---|---|---|
| `mirror`（双向） | 两端互相补齐，删除也互相同步 | 三端互为镜像 |
| `backup`（单向） | 仅从 source → target 复制，target 的删除不影响 source | 硬盘做备份盘 |

默认推荐 `mirror`，但 `backup` 模式对「硬盘只作备份」更安全（避免误删从备份回灌）。

---

## 4. 冲突处理

当同一文件在**两端都发生变化**（哈希不同、mtime 都晚于上次快照）：

| 策略 | 行为 |
|---|---|
| `rename`（默认） | 保留较新的一方，另一方重命名为 `file.conflict-<时间戳>.<ext>`，不丢数据 |
| `keep` | 保留两端各自版本，新端加 `.conflict` 后缀 |
| `newer` | 仅保留 mtime 较新者，旧者进回收站/备份 |

> 原则：**永不静默覆盖**。任何冲突都留下可追溯的副本。

删除冲突（一端改、一端删）：按 `mode` 决定 —— `mirror` 下删除胜出；`backup` 下修改胜出。

---

## 5. 扫描与比对流程

```text
for each Node:
    scan() → 当前 FileState 表
    load 上次快照 snapshot.json
    diff(当前, 快照) → 变化集

比对两两 Node 的变化集 → Action 计划
dry-run 打印计划
用户确认（或 --yes）→ Apply 执行
执行后写回各 Node 新快照
```

---

## 6. 跨平台差异（坑点）

| 问题 | Win | macOS | 移动硬盘(exFAT) | 处理 |
|---|---|---|---|---|
| 路径分隔符 | `\` | `/` | `/` | 内部统一 `/`，落盘再转 |
| 大小写敏感 | 不敏感 | 默认不敏感（可区分） | 不敏感 | 以「不敏感」为准，避免同名不同写 |
| 符号链接 | 支持 | 支持 | **不支持** | exFAT 忽略/展开符号链接 |
| 文件权限 | ACL | POSIX | **不支持** | 不同步权限位 |
| 隐藏文件 | `Thumbs.db` | `.DS_Store` | 两者皆有可能 | 默认 ignore |
| 时间精度 | 100ns | ns | 2s（exFAT） | 比对用 hash 兜底，不只看 mtime |

> exFAT 的 2 秒时间精度是增量同步的大坑 —— **必须依赖内容哈希而非仅靠 mtime**。

---

## 7. 配置格式（YAML）

见 `config/maosync.example.yaml`。要点：

- `nodes`：三端定义，每端可带 `win-path` / `mac-path` 双路径。
- `mode`：`mirror` | `backup`
- `ignore`：glob 忽略列表
- `conflict`：冲突策略

---

## 8. CLI 设计

```text
miaosync init                 # 交互式生成配置
miaosync sync [--dry-run] [--yes]
miaosync status               # 打印三端快照差异概览
miaosync restore <file>       # 从快照/冲突副本恢复
miaosync version
```

---

## 9. 安全

- **加密（规划）**：静态加密用 `age` 或 `gpg`，密钥本地保管；适合移动硬盘怕丢的场景。
- **校验**：每个 Action 执行后校验目标 hash == 源 hash。
- **危险操作保护**：`mirror` 模式若检测到某端「几乎全删」，暂停并要求确认（防误触清空）。

---

## 10. 技术栈建议

- **语言**：Go 1.22+（单二进制、交叉编译简单、无运行时依赖）。
- **哈希**：BLAKE3（快）或 xxHash（更快，非加密）。
- **快照存储**：先 JSON，量大改 SQLite（`modernc.org/sqlite` 纯 Go）。
- **CLI**：`spf13/cobra` 或标准 `flag`。
- **配置**：`gopkg.in/yaml.v3`。

> 若想换 Python / Rust / Node，目录结构可平移，核心算法不变。
