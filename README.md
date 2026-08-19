# qianjue CLI

千谲 AI 平台的官方命令行客户端：提交图片 / 视频生成任务，查询与等待结果，按幂等键恢复未知结果。

```bash
# 预编译二进制（无需 Go）
curl -fsSL -o qianjue https://github.com/zriyox/qianjue-cli/releases/latest/download/qianjue-darwin-arm64
chmod +x qianjue && sudo mv qianjue /usr/local/bin/

# 或用 Go
go install github.com/zriyox/qianjue-cli/cmd/qianjue@latest
```

- **完整安装指引（也是给 AI 助手读的）**：[`docs/install.md`](docs/install.md)
- **内置 AI skill**：`qianjue skill show`（源文件 [`skill/SKILL.md`](skill/SKILL.md)）
- 默认连生产环境 `https://api.aiqianjue.com/api/v1`；dev / test 需显式配置

## 构建与安装

要求 Go 1.26+。

```bash
cd cli
make build          # 产出 bin/qianjue（ldflags 注入 version/commit/buildDate）
make install        # 装二进制到 /usr/local/bin（PREFIX 可覆盖）
make install-skill  # 可选：装 AI skill（只写已存在的 skills 目录）
make test           # 全部单元 + HTTP mock 集成测试（离线，无付费调用）
make lint           # gofmt + go vet
```

## AI skill（随 CLI 分发，可由 AI 自助安装）

本 CLI 内置一个**面向使用者**的 AI skill `qianjue-cli`，教 AI 助手怎么用 `qianjue` 生成图片/视频、等任务、处理幂等与恢复。内容通过 `go:embed` 打进二进制，源文件是 [`cli/skill/SKILL.md`](skill/SKILL.md)。

> 它与仓库内的 `zriyo-qianjue-cli`（**开发本 CLI 源码的护栏**）是两个不同的 skill：前者随二进制发给用户，后者只在 qianjue-parent 仓库里用，不随二进制分发。

取用方式（跨 Windows / macOS / Linux，用 `os.UserHomeDir` 定位）：

```bash
qianjue skill show                 # 打印 SKILL.md 到 stdout，自行重定向到合适的 skills 目录
qianjue skill path --output json   # 列出候选目录及其是否存在（不写文件）
qianjue skill install              # 便捷方式：只写入【已存在】的 skills 目录
qianjue skill install --dir <路径>  # 显式指定目录（会创建）
```

**设计取向：放置位置交给 AI 判断，CLI 不替用户改助手配置。** `skill install` 绝不创建 `~/.codex` / `~/.claude` 这类助手配置根目录——那些路径可能是符号链接、被统一管理，或该机器上助手根本按别的约定放 skill。一个候选目录都不存在时它会直接失败并提示用 `--dir` 或 `show`，而不是硬造目录。因此 `make install`（装二进制）**不会**顺手装 skill；`make install-skill` 是独立的可选目标。

**生效**：skills 在会话启动时扫描加载，**装完必须重新打开 Codex / Claude Code 会话**，运行中新增不生效。

## 快速开始

```bash
# 1. 选环境：默认就是生产 https://api.aiqianjue.com/api/v1，普通用户跳过这步。
#    只有开发者需要切 dev/test（非 localhost 强制 HTTPS；config show 会标注是否为内置默认）
qianjue config profile create local --api-base-url 'http://localhost:7777/api/v1'
qianjue config profile use local

# 2. Device Flow 登录（浏览器确认授权，凭证进系统凭证库）
qianjue auth login

# 3. 创建图片任务（Idempotency-Key 自动生成并先落本地请求日志）
qianjue image create --request examples/txt2img.json --wait --output json

# 4. 查询 / 取消
qianjue image get <taskId>
qianjue image cancel <taskId>
```

PAT 方式（CI/自动化，只允许 stdin 导入）：

```bash
read -rsp 'PAT: ' P; printf '%s' "$P" | qianjue auth import-token --type pat --stdin; unset P
```

## 命令树

```text
qianjue
├── version
├── config   profile create|use|list · show · path
├── auth     login · import-token · status · logout
├── image    create · get · wait · cancel · request-status · resume
├── video    create · edit · upscale · gesture-replica · resume
├── task     get · list · wait · cancel        # <domain> 必填：image | video
└── skill    show · install · path
```

`task` 是跨业务域的统一读/等/取消面（`qianjue task get video <id>`）；`image get|wait|cancel <id>` 与 `task ... image <id>` 等价。新增业务域只要接入统一查询投影 + 一个 create 适配器，读/等/取消零改动白拿。

全局参数：`--profile` `--api-base-url` `--output table|json` `--quiet` `--no-color` `--http-timeout` `--trace`。非 TTY 默认 JSON 输出，stdout 永远只有一个 JSON Document。

## 本地文件

| 数据 | 位置 | 权限 |
| --- | --- | --- |
| 配置 | `${XDG_CONFIG_HOME:-~/.config}/qianjue/config.toml` | 0600（目录 0700） |
| 幂等请求日志 | `${XDG_STATE_HOME:-~/.local/state}/qianjue/requests/<profile>/` | 0600 |
| Refresh 进程锁 | `${XDG_STATE_HOME:-~/.local/state}/qianjue/locks/` | — |
| 凭证 | 系统凭证库（macOS Keychain / WinCred / Secret Service），service `com.zriyo.qianjue.cli` | — |

Token 永不写入配置文件或请求日志；系统凭证库不可用时 CLI 直接失败（exit 14），不退化明文存储。

## 未知结果恢复

创建遇网络超时/连接重置时，CLI 用**原 Key + 原 JSON** 自动恢复（查幂等状态 → 404 重放 → PROCESSING 退避 → SUCCEEDED 回查任务）。`RECOVERY_REQUIRED` 时停止自动创建并输出 `requestId`，按 `docs/integration/admin-recovery-runbook.md` 人工处理。手动恢复：

```bash
qianjue image resume --idempotency-key '<key>' --wait
```

## 退出码

`0` OK · `2` USAGE · `3` AUTH · `4` FORBIDDEN · `5` NOT_FOUND · `6` IDEMPOTENCY_CONFLICT(2018) · `7` IN_PROGRESS(2019)/等待超时 · `8` RECOVERY_REQUIRED(2020) · `9` FINAL_FAILURE(2021/3007) · `10` 任务 FAILED/TIMEOUT · `11` 任务 CANCELLED · `12` TRANSPORT · `13` SERVER · `14` LOCAL_STORAGE · `130` Ctrl-C。

完整映射与语义以 `docs/integration/cli-contract.md` §23/§24 为准；退出码是长期自动化契约，不得重排。
