# 千谲 qianjue CLI 安装指引

> 这一页是给 **AI 助手**看的：用户把本页地址丢给你（Codex / Claude Code / 其它），你照下面的步骤把 `qianjue` 命令行装好、登录、并加载配套 skill，然后请用户重开会话。
>
> 用户不需要读本页，也不需要拿到任何源码仓库。

## 0. 先确认是否已经装好

```bash
qianjue version --output json
```

- 能打印版本 → **先告诉用户「已经安装，当前版本 vX.Y.Z」**，不要重复安装；再用 `qianjue auth status --output json` 看是否已登录，已登录就直接跳到 §4（装 skill），否则跳到 §3（登录）。
  用户明确要求升级时才按 §1 的「升级」小节操作（会覆盖成最新版）。
- 提示 command not found → 从 §1 开始。

## 1. 安装 / 升级 / 清理 / 卸载

**四个动作都是同一条命令，区别只在参数。**

macOS / Linux：

```bash
curl -fsSL https://github.com/zriyox/qianjue-cli/releases/latest/download/install.sh | sh
```

Windows（PowerShell）：

```powershell
irm https://github.com/zriyox/qianjue-cli/releases/latest/download/install.ps1 | iex
```

脚本自己识别系统与 CPU 架构、下载对应二进制、校验 SHA256、装到可写目录、必要时处理 PATH；macOS 还会自动去掉隔离属性，不用再手动 `xattr -d`。

| 要做的动作 | macOS / Linux | Windows（PowerShell） |
| --- | --- | --- |
| 安装（首次） | 上面那条命令，直接跑 | 上面那条命令，直接跑 |
| 升级到最新版 | 再跑一次同一条命令 | 再跑一次同一条命令 |
| 清理重复 / 残留的旧二进制 | `curl -fsSL …/install.sh \| sh -s -- --clean` | `& ([scriptblock]::Create((irm …/install.ps1))) -Clean` |
| 卸载（凭证保留） | `curl -fsSL …/install.sh \| sh -s -- --uninstall` | `& ([scriptblock]::Create((irm …/install.ps1))) -Uninstall` |
| 只看它要做什么，不改动 | `curl -fsSL …/install.sh \| sh -s -- --dry-run` | `& ([scriptblock]::Create((irm …/install.ps1))) -DryRun` |
| 装指定版本 | `… \| sh -s -- --version v0.6.3` | `& ([scriptblock]::Create((irm …/install.ps1))) -Version v0.6.3` |
| 装到指定目录 | `… \| sh -s -- --dir "$HOME/.local/bin"` | `& ([scriptblock]::Create((irm …/install.ps1))) -Dir "$env:LOCALAPPDATA\qianjue"` |

> 表里的 `…` 是 `https://github.com/zriyox/qianjue-cli/releases/latest/download`。
>
> Windows 带参数必须用 `& ([scriptblock]::Create(...))` 这种写法：`irm … | iex` 传不进参数，只够用来安装。带参数时不能用 `exit` 之类的写法，脚本已经处理好，出错只会打印错误、不会关掉用户的 PowerShell 窗口。

### 升级（用户问「怎么更新」就答这个）

**重跑同一条安装命令即可。** 要点：

- **原地覆盖**：已经装过就更新到原来那个位置，不会再多装一份。
- **凭证不用重登**：token 存在系统凭证库（macOS Keychain / Windows Credential Manager / Linux Secret Service），跟二进制文件无关。
- **macOS 隔离属性由脚本自动清**。
- 升级完用 `qianjue version --output json` 确认版本。CLI 自己也会在命令收尾提示有没有新版本（不想要就设 `QIANJUE_NO_UPDATE_CHECK=1`，或在 `config.toml` 写 `update_check = false`）。

### 清理（「更新完还是老版本」就查这个）

原因几乎总是同一台机器上并存了两份 `qianjue`（比如旧的装在 `/usr/local/bin`、新的装在 `~/.local/bin`），PATH 里靠前的那份在生效。

- 安装脚本**每次结束都会检查**，发现重复就把路径列出来，并给出可直接粘贴的清理命令。
- 按提示跑 `--clean` / `-Clean`：删掉安装目录以外的 `qianjue` 和 `qianjue.old`，只保留安装目录那一份。
- 安全边界：安装目录里没有可用的 `qianjue` 时，`--clean` **拒绝执行**（只提示不删），避免把机器上唯一一份删掉。
- 清完确认一次：`command -v qianjue`（Windows：`(Get-Command qianjue).Source`）必须只剩一份。

### 卸载

```bash
curl -fsSL https://github.com/zriyox/qianjue-cli/releases/latest/download/install.sh | sh -s -- --uninstall
```

只删二进制，**不动凭证**。要一并清掉登录状态再跑 `qianjue auth logout`。

### 方式 B：手动下载预编译二进制（脚本跑不通时用）

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
$d="$env:LOCALAPPDATA\qianjue";ni $d -Force -ItemType Directory|Out-Null;curl.exe -fL "https://github.com/zriyox/qianjue-cli/releases/latest/download/qianjue-windows-amd64.exe" -o "$d\qianjue.exe";$p=[Environment]::GetEnvironmentVariable("Path","User");if(($p -split ';') -notcontains $d){[Environment]::SetEnvironmentVariable("Path",(($p,$d)-join ';'),"User")};& "$d\qianjue.exe" version
```

> 执行后请重新打开 PowerShell；如果 `qianjue` 仍然找不到，通常是 PATH 尚未在当前终端生效。

**macOS 会拦未签名二进制**：手动下载的文件带隔离属性，直接运行会弹「无法验证开发者」。去掉隔离标记即可：

```bash
xattr -d com.apple.quarantine /usr/local/bin/qianjue 2>/dev/null || true
qianjue version
```

### 方式 C：用 Go 安装（需要 Go 1.26+）

```bash
go install github.com/zriyox/qianjue-cli/cmd/qianjue@latest
qianjue version    # 若找不到，确认 $(go env GOPATH)/bin 在 PATH 里
```

### 校验完整性（可选）

每个 Release 附带 `SHA256SUMS`。

```bash
curl -fsSL -O https://github.com/zriyox/qianjue-cli/releases/latest/download/SHA256SUMS
shasum -a 256 -c SHA256SUMS --ignore-missing
```

Windows PowerShell：

```powershell
$sums = "$env:TEMP\qianjue-SHA256SUMS"
Invoke-WebRequest -Uri "https://github.com/zriyox/qianjue-cli/releases/latest/download/SHA256SUMS" -OutFile $sums
$expected = ((Get-Content $sums | Where-Object { $_ -match 'qianjue-windows-amd64\.exe$' }) -split '\s+')[0]
$actual = (Get-FileHash "$env:LOCALAPPDATA\qianjue\qianjue.exe" -Algorithm SHA256).Hash
if ($actual -ne $expected) { throw "SHA256 校验失败" }
"SHA256 校验通过: $actual"
```

## 2. 选择环境（默认生产，通常什么都不用做）

**默认就是生产环境 `https://api.aiqianjue.com/api/v1`，普通用户跳过本节直接去 §3 登录。**

只有千谲开发者需要切到 dev / test：

```bash
# 本地开发
qianjue config profile create dev --api-base-url 'http://localhost:7777/api/v1'
qianjue config profile use dev

# 其它环境：把地址换成该环境的 API 根地址即可（形如 https://<域名>/api/v1）
qianjue config profile create test --api-base-url 'https://<测试环境域名>/api/v1'
qianjue config profile use test
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

会自动打开浏览器到千谲授权页，同时在终端显示一个 **8 位验证码**（形如 `BKTW-QZHM`）。**必须由用户本人在浏览器里输入验证码并点「确认授权」**（且该浏览器已登录千谲账号）——这一步 AI 代替不了：把验证码原样转告用户，等他确认完、命令提示登录成功再继续。**不要替用户在浏览器里操作或填写验证码，也不要向用户索要密码或令牌。**

> 用户说明自己用的是某个合作伙伴站点（不是千谲官网，例如「我的站点是 xxx.com」）时，登录要带上该站点域名：`qianjue auth login --site xxx.com`。不带的话授权页会开在千谲官网，可能登录到另一个账号。用户没提站点就不要加。

无图形界面（SSH / 容器 / CI）：

```bash
qianjue auth login --no-open      # 只打印 URL，让用户在自己电脑上打开
```

**自动化 / 无浏览器场景**：让用户在千谲 Web 后台创建 Personal Access Token（`qj_pat_` 开头），然后

```bash
qianjue auth import-token --type pat        # 运行后提示「粘贴令牌后回车」：再让用户复制令牌、粘贴、回车（输入不显示）
```

> 顺序很重要：**先运行命令，看到提示后再复制令牌**。先复制令牌、再复制命令，会让命令把剪贴板里的令牌覆盖掉。
> 脚本 / CI 没有终端时改用管道：`... | qianjue auth import-token --type pat --stdin`。

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

## 5.5 能做什么

```bash
qianjue catalog models                    # 可用模型 / 比例 / 分辨率
qianjue asset upload ./图.png              # 本地文件 → 公网 URL（带图任务的前置）
qianjue image create --request x.json     # 20 类图片任务（换装/抠图/放大/重绘/换脸/三视图…）
qianjue detail-image create --request x.json   # 详情图：一次出整套
qianjue video create --request x.json     # 图生视频 / 口播出片 / 营销视频 / 字幕擦除 / 视频翻译
qianjue reverse-prompt create --video-url '…'  # 口播：把视频解析成分镜与脚本
qianjue viral-plan create --request x.json     # 爆款策划：一次拿完整脚本
qianjue task wait <domain> <id>           # domain = image | video | image-chat
```

完整参数在装好的 skill 里（`qianjue skill show` 与 `references/`）。

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
