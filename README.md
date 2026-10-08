# qianjue CLI

千谲 AI 平台的官方命令行客户端：提交图片 / 视频生成任务，查询与等待结果，按幂等键恢复未知结果。

推荐一行安装（自动选平台、校验 SHA256、处理 PATH 与 macOS 隔离属性）：

```bash
# macOS / Linux
curl -fsSL https://github.com/zriyox/qianjue-cli/releases/latest/download/install.sh | sh
```

```powershell
# Windows
irm https://github.com/zriyox/qianjue-cli/releases/latest/download/install.ps1 | iex
```

**升级就是重跑同一条命令**（覆写旧二进制，凭证不用重登）。「更新完还是老版本」通常是机器上并存了两份，按脚本结尾的提示跑 `--clean` 清掉。卸载：`--uninstall`。

手动装 / 从源码构建：

```bash
curl -fsSL -o qianjue https://github.com/zriyox/qianjue-cli/releases/latest/download/qianjue-darwin-arm64
chmod +x qianjue && sudo mv qianjue /usr/local/bin/
# 或：go install github.com/zriyox/qianjue-cli/cmd/qianjue@latest
```

Windows PowerShell（一行安装）：

```powershell
$d="$env:LOCALAPPDATA\qianjue";ni $d -Force -ItemType Directory|Out-Null;curl.exe -fL "https://github.com/zriyox/qianjue-cli/releases/latest/download/qianjue-windows-amd64.exe" -o "$d\qianjue.exe";$p=[Environment]::GetEnvironmentVariable("Path","User");if(($p -split ';') -notcontains $d){[Environment]::SetEnvironmentVariable("Path",(($p,$d)-join ';'),"User")};& "$d\qianjue.exe" version
```

执行后请重新打开 PowerShell，即可直接使用 `qianjue`。

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

## 版本更新提示

每次命令收尾时，CLI 会顺带确认一次有没有更新版本，**发现新版本只往 stderr 打一行提示**：
不改 stdout 的 JSON、不改退出码，`--quiet` 时不打。结果缓存在本地 —— 24 小时内不再请求，
查不到时 1 小时内不重试，所以正常调用几乎没有额外延迟。

```bash
qianjue version --check                  # 主动确认（同步查询）
qianjue version --check --output json    # data.checkStatus: UPDATE_AVAILABLE / UP_TO_DATE / UNAVAILABLE / UNKNOWN
```

默认查询 `https://github.com/zriyox/qianjue-cli/releases/latest`（只读发布页的重定向，不带任何凭证，
也不占 GitHub API 配额）。关闭方式二选一：

- 环境变量 `QIANJUE_NO_UPDATE_CHECK=1`
- `config.toml` 里写 `update_check = false`

分发渠道不是公开仓库时，用 `QIANJUE_RELEASE_URL` 指向自己的发布页，无需重新编译。

## AI skill（随 CLI 分发，可由 AI 自助安装）

本 CLI 内置一个**面向使用者**的 AI skill `qianjue-cli`，教 AI 助手怎么用 `qianjue` 生成图片/视频、等任务、处理幂等与恢复。

skill 是**分层**的：[`skill/SKILL.md`](skill/SKILL.md) 保持精简（每次加载都要读完），
各能力的参数表放 [`skill/references/`](skill/references/)，按需打开：

```
skill/
  SKILL.md                    铁律 / 工作流 / 环境 / 安装
  references/image-tasks.md   20 个图片 type + inputImages 字段
  references/video-tasks.md   6 种 sourceType × 12 modelCode + volcanoConfig
  references/recovery.md      退出码 / 失败处理 / 未知结果恢复
```

整棵树通过 `go:embed` 打进二进制，`skill install` 会完整还原（含 `references/`）。

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

# 2. Device Flow 登录：终端会显示验证码（如 BKTW-QZHM），在浏览器授权页输入它确认授权，凭证进系统凭证库。
#    不要把授权链接或验证码发给别人——对方输入后就能用你的账号和积分。
qianjue auth login

# 3. 创建图片任务（Idempotency-Key 自动生成并先落本地请求日志）
qianjue image create --request examples/txt2img.json --wait --output json

# 4. 查询 / 取消
qianjue image get <taskId>
qianjue image cancel <taskId>
```

PAT 方式（令牌在网页「个人中心 → 命令行与 API」生成）：

```bash
qianjue auth import-token --type pat        # 终端里：运行后按提示粘贴令牌，输入不显示
some-secret-tool | qianjue auth import-token --type pat --stdin   # 脚本 / CI：从管道传入
```

令牌永远不作为命令参数传入（会进 shell 历史与进程列表）。

## 命令树

```text
qianjue
├── version  [--check]
├── config   profile create|use|list · show · path
├── auth     login · import-token · status · logout
├── image    create · get · wait · cancel · request-status · resume
├── video    create · edit · upscale · gesture-replica · resume
├── task     get · list · wait · cancel        # <domain> 必填：image | video
├── asset    upload                            # 批量直传，拿公网 URL 喂给带图任务
├── catalog  models                            # 可用模型/比例/分辨率，提交前查，别硬编码
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
