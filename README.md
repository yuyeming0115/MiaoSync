# MiaoSync 🐱

> 你的 Win / Mac / 移动硬盘三端同步备份猫

MiaoSync 是一个开源的、跨平台的个人数据同步备份工具，专注于在三端之间保持一致：
**Windows 个人电脑 · macOS 个人电脑 · 移动硬盘**。

不依赖云服务，数据始终在你自己的设备上流转 —— 插上硬盘就能同步，拔掉也能各自工作。

---

## ✨ 特性（草案）

- 🖥️ **三端同步**：Windows、macOS、移动硬盘（exFAT / FAT32 / APFS / NTFS）
- 🔁 **双向同步 + 单向备份**两种模式可切换
- ⚡ **增量同步**：基于文件哈希 / 修改时间，只传变化的部分
- 🤝 **冲突检测与解决**：同名文件两端都改了？自动保留冲突副本，不悄无声息覆盖
- 🔒 **加密备份**（规划中）：本地静态加密，硬盘丢了也不慌
- 🧰 **命令行优先**，未来提供图形界面 / 托盘
- 📦 **单二进制分发**：Go 编译，无运行时依赖，Win / Mac 一个 exe 搞定

---

## 🧭 支持平台

| 平台 | 状态 | 备注 |
|---|---|---|
| Windows 10/11 | 🟡 规划 | 路径 `D:\Sync` 等 |
| macOS 12+ | 🟡 规划 | 路径 `/Users/you/Sync` |
| 移动硬盘 | 🟡 规划 | exFAT 不支持符号链接/权限，需特殊处理 |

> 🟡 = 骨架阶段，尚未实现。

---

## 🚀 快速开始（设计阶段）

> 当前为仓库骨架，二进制尚未发布。以下为预期用法。

```bash
# 1. 初始化配置（按提示填入你的三端路径）
miaosync init

# 2. 预览将发生的同步（dry-run，不改动文件）
miaosync sync --dry-run

# 3. 真正同步
miaosync sync

# 4. 查看各端状态
miaosync status
```

---

## 🏗️ 架构概览

详见 [docs/design.md](docs/design.md)。

```
        ┌─────────────┐
        │  CLI (cmd)  │
        └──────┬──────┘
               │
        ┌──────▼──────┐
        │  sync 引擎   │  比对各端快照 → 生成动作计划 → 应用
        └──────┬──────┘
     ┌─────────┼─────────┐
   ┌─▼─┐    ┌──▼──┐   ┌──▼──┐
   Win  │    │ macOS│   │ HDD │   ← 三个 Node（根目录）
   └───┘    └─────┘   └─────┘
```

---

## 📁 目录结构

```
MiaoSync/
├── README.md                 # 本文件
├── LICENSE                   # MIT
├── go.mod                    # Go 模块（技术栈以 Go 为例）
├── .gitignore
├── cmd/
│   └── miaosync/
│       └── main.go           # 入口（占位）
├── internal/
│   ├── sync/                 # 同步引擎（核心算法）
│   ├── fs/                   # 跨平台文件系统差异封装
│   ├── config/               # 配置加载
│   └── cli/                  # 命令行解析
├── pkg/                      # 可对外复用的包
├── config/
│   └── miaosync.example.yaml # 示例配置
├── docs/
│   ├── design.md             # 详细设计草案
│   └── roadmap.md            # 里程碑路线图
└── scripts/
    └── build.sh              # 跨平台构建脚本
```

---

## 🗺️ 路线图

见 [docs/roadmap.md](docs/roadmap.md)。当前处于 **Phase 0：骨架**。

---

## 🤝 贡献

欢迎 Issue / PR。项目早期，任何想法都先开 Issue 讨论。

## 📄 许可证

[MIT](LICENSE) © MiaoSync contributors
