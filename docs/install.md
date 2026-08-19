# 千谲 qianjue CLI 安装指引

> 这一页是给 **AI 助手**看的：用户把本页地址丢给你（Codex / Claude Code / 其它），你照下面的步骤把 `qianjue` 命令行装好、登录、并加载配套 skill，然后请用户重开会话。
>
> 用户不需要读本页，也不需要拿到任何源码仓库。

## 0. 先确认是否已经装好

```bash
qianjue version --output json
```

- 能打印版本 → 跳到 §3（登录）或 §4（装 skill）。
- 提示 command not found → 从 §1 开始。

## 1. 安装二进制

### 方式 A：下载预编译二进制（推荐，无需 Go 环境）

按用户的操作系统和 CPU 架构选一个。不确定架构时先跑 `uname -sm`（Windows PowerShell：`$env:PROCESSOR_ARCHITECTURE`）。

**macOS / Linux**

```bash
# 把 <PLATFORM> 换成 darwin-arm64 / darwin-amd64 / linux-amd64 / linux-arm64
curl -fsSL -o /tmp/qianjue "https://github.com/zriyox/qianjue-cli/releases/latest/download/qianjue-<PLATFORM>"
chmod +x /tmp/qianjue
sudo mv /tmp/qianjue /usr/local/bin/qianjue      # 无 sudo 权限时改放 ~/.local/bin 并确保它在 PATH
qianjue version
```

**Windows（PowerShell）**

```powershell
$dir = "$env:LOCALAPPDATA\qianjue"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
Invoke-WebRequest -Uri "https://github.com/zriyox/qianjue-cli/releases/latest/download/qianjue-windows-amd64.exe" -OutFile "$dir\qianjue.exe"
# 加入 PATH（当前用户，永久）
[Environment]::SetEnvironmentVariable("Path", $env:Path + ";$dir", "User")
& "$dir\qianjue.exe" version
```

> 装完若 `qianjue` 仍然找不到，多半是 PATH 没生效——让用户开一个新终端再试。

**macOS 会拦未签名二进制**：下载来的文件带隔离属性，直接运行会弹「无法验证开发者」。去掉隔离标记即可：

```bash
xattr -d com.apple.quarantine /usr/local/bin/qianjue 2>/dev/null || true
qianjue version
```

**校验完整性（可选）**：每个 Release 附带 `SHA256SUMS.txt`。

```bash
curl -fsSL -O https://github.com/zriyox/qianjue-cli/releases/latest/download/SHA256SUMS.txt
shasum -a 256 -c SHA256SUMS.txt --ignore-missing
```

### 方式 B：用 Go 安装（需要 Go 1.26+）

```bash
go install github.com/zriyox/qianjue-cli/cmd/qianjue@latest
qianjue version    # 若找不到，确认 $(go env GOPATH)/bin 在 PATH 里
```

## 2. 选择环境（默认生产，通常什么都不用做）

**默认就是生产环境 `https://api.aiqianjue.com/api/v1`，普通用户跳过本节直接去 §3 登录。**

只有千谲开发者需要切到 dev / test：

```bash
# 测试环境
qianjue config profile create test --api-base-url '{{TEST_API_BASE_URL}}'
qianjue config profile use test

# 本地开发
qianjue config profile create dev --api-base-url 'http://localhost:7777/api/v1'
qianjue config profile use dev
```

也可以不建 Profile，单次覆盖：`--api-base-url '...'` 或环境变量 `QIANJUE_API_BASE_URL`。

随时确认自己在打哪个环境：

```bash
qianjue config show --output table
# API base URL  https://api.aiqianjue.com/api/v1  (内置默认：生产)   ← 带这个标记 = 没切环境，正在打生产
```

> 优先级：`--api-base-url` > `QIANJUE_API_BASE_URL` > Profile 配置 > 内置生产默认。非 localhost 地址强制 HTTPS。凭证按 Profile 隔离，切 Profile 后需要在该 Profile 下重新登录。

## 3. 登录

**有浏览器的桌面机器（推荐）**

```bash
qianjue auth login
```

会自动打开浏览器到千谲授权页。**必须由用户本人在浏览器里点「确认授权」**（且该浏览器已登录千谲账号）——这一步 AI 代替不了，把 URL 给用户，等他点完再继续。

无图形界面（SSH / 容器 / CI）：

```bash
qianjue auth login --no-open      # 只打印 URL，让用户在自己电脑上打开
```

**自动化 / 无浏览器场景**：让用户在千谲 Web 后台创建 Personal Access Token（`qj_pat_` 开头），然后

```bash
qianjue auth import-token --stdin --type pat        # 从 stdin 读，不回显、不进 shell 历史
```

确认状态：

```bash
qianjue auth status --output json
```

> 凭证只存系统凭证库（macOS Keychain / Windows Credential Manager / Linux Secret Service），**没有明文文件回退**。若报退出码 14，说明凭证库不可用，请解决凭证库问题，不要试图绕过。

## 4. 安装配套 AI skill（让你以后会用这个 CLI）

CLI 内置一份使用说明 skill（`qianjue-cli`）。**放到哪个目录由你判断**——你比 CLI 更清楚自己是哪个助手、用的哪套 skills 目录约定。

先看候选位置：

```bash
qianjue skill path --output json
```

然后任选一种：

```bash
# (a) 便捷：只写入【已存在】的 skills 目录
qianjue skill install

# (b) 显式指定目录（会创建）
qianjue skill install --dir <你的 skills 目录>

# (c) 自己放：把内容打印出来，重定向到你认为正确的位置
qianjue skill show > <你的 skills 目录>/qianjue-cli/SKILL.md
```

> `skill install` **不会**创建 `~/.codex`、`~/.claude` 这类助手配置根目录——那些路径可能是符号链接或被统一管理，硬造会弄坏用户配置。一个候选目录都不存在时它会直接失败，此时请用 (b) 或 (c)。

## 5. 请用户重开会话

skills 只在会话启动时扫描加载。装完后**必须让用户重新打开 Codex / Claude Code 会话**，新 skill 才会生效。请明确地告诉用户这一点。

## 6. 冒烟验证（可选）

```bash
qianjue task list image --page 1 --size 1 --output json
```

退出码 0 即链路通。退出码 3 = 未登录/凭证失效，回 §3。

## 附：装完之后

后续怎么用（生成图片/视频、等任务、幂等恢复、退出码语义）都写在刚装的 skill 里；重开会话后你会自动加载到。也可以随时用

```bash
qianjue skill show
```

直接查看完整使用说明。

---

## 维护者填写清单

发布本页前必须把下列占位符替换成真实值：

| 占位符 | 含义 |
| --- | --- |
| `https://github.com/zriyox/qianjue-cli/releases/latest/download` | 预编译二进制的下载目录地址（各平台文件同目录，命名如 `qianjue-darwin-arm64`、`qianjue-windows-amd64.exe`） |
| `{{TEST_API_BASE_URL}}` | 测试环境 API 根地址（仅开发者用；生产地址已内置为默认，无需填） |
