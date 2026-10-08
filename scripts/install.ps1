<#
  千谲 qianjue CLI 安装 / 更新 / 卸载（Windows PowerShell）

  安装或更新（再跑一遍就是更新，会覆盖旧文件）：
    irm https://github.com/zriyox/qianjue-cli/releases/latest/download/install.ps1 | iex

  先看它要做什么，不实际改动：
    & ([scriptblock]::Create((irm https://github.com/zriyox/qianjue-cli/releases/latest/download/install.ps1))) -DryRun

  卸载：
    & ([scriptblock]::Create((irm https://github.com/zriyox/qianjue-cli/releases/latest/download/install.ps1))) -Uninstall

  清理重复 / 残留的旧二进制（「更新完还是老版本」基本都是这个原因）：
    & ([scriptblock]::Create((irm https://github.com/zriyox/qianjue-cli/releases/latest/download/install.ps1))) -Clean

  参数：
    -Dir <路径>      安装目录（默认：已装过就原地更新，否则 %LOCALAPPDATA%\qianjue）
    -Version <tag>   指定版本，如 v0.6.3（默认最新）
    -Uninstall       卸载：移除 exe 并从用户 PATH 里摘掉该目录；不动系统凭证库
    -Clean           清理其它目录里重复的 qianjue.exe 和 .old 残留，只保留安装目录那一份
    -DryRun          只打印将要执行的动作
    -NoVerify        跳过 SHA256 校验（不建议）

  退出码：0 成功 / 1 参数或环境错误 / 2 下载或校验失败 / 3 安装失败
#>
[CmdletBinding()]
param(
  [string]$Dir = '',
  [string]$Version = '',
  [switch]$Uninstall,
  [switch]$Clean,
  [switch]$DryRun,
  [switch]$NoVerify
)

$ErrorActionPreference = 'Stop'
$ProgressPreference = 'SilentlyContinue'

$Repo = 'zriyox/qianjue-cli'
$BinName = 'qianjue'
$Asset = 'qianjue-windows-amd64.exe'   # Windows 只发布 amd64；ARM64 上由系统仿真运行
$ReleaseBaseDefault = "https://github.com/$Repo/releases/latest/download"

# 用 `irm ... | iex` 或 `& ([scriptblock]::Create(...))` 调用时，脚本不是以文件方式运行的。
# 这种场景下绝不能调 exit：exit 会把用户整个 PowerShell 窗口关掉，
# 下载失败时用户连错误信息都看不到。所以只有真的按文件跑才 exit。
$ScriptIsFile = [bool]$MyInvocation.MyCommand.Path

function Finish([int]$code) {
  if ($ScriptIsFile) { exit $code }
  $global:LASTEXITCODE = $code
  if ($code -ne 0) { Write-Host "（以上流程以退出码 $code 结束）" -ForegroundColor Yellow }
}

function Write-Step($text) { Write-Host "  -> $text" }
function Fail-With($text, $code) { Write-Host "错误：$text" -ForegroundColor Red; return $code }

function Get-ExistingDir {
  $cmd = Get-Command $BinName -ErrorAction SilentlyContinue
  if ($cmd -and $cmd.Source -and (Test-Path $cmd.Source)) { return (Split-Path $cmd.Source -Parent) }
  return ''
}

function Resolve-TargetDir {
  if ($Dir) { return $Dir }
  $existing = Get-ExistingDir
  if ($existing) { return $existing }          # 已装过就原地更新，避免新旧两份并存
  $base = $env:LOCALAPPDATA
  if (-not $base) { $base = $env:HOME }        # 非 Windows 上跑时兜底，别拿到 null
  if (-not $base) { $base = '.' }
  return (Join-Path $base $BinName)
}

function Get-CurrentVersion {
  $cmd = Get-Command $BinName -ErrorAction SilentlyContinue
  if (-not $cmd) { return '' }
  try {
    $json = & $cmd.Source version --output json 2>$null | Out-String
    $obj = $json | ConvertFrom-Json
    return $obj.data.version
  } catch { return '' }
}

# 改用户 PATH 失败不应该让一次成功的安装被报成失败，所以这里都吞掉异常并返回状态。
function Add-ToUserPath($directory) {
  try {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($userPath -split ';') -notcontains $directory) {
      [Environment]::SetEnvironmentVariable('Path', "$userPath;$directory", 'User')
      return $true
    }
    return $false
  } catch {
    return $false
  }
}

function Remove-FromUserPath($directory) {
  try {
    $userPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (-not $userPath) { return }
    $parts = ($userPath -split ';') | Where-Object { $_ -and $_ -ne $directory }
    [Environment]::SetEnvironmentVariable('Path', ($parts -join ';'), 'User')
  } catch { }
}

# ---- 清理 -------------------------------------------------------------------
# 机器上并存两份 qianjue 时，PATH 靠前的那份生效，「更新完还是老版本」。
# 这里把安装目录以外的重复 exe 和 .old 残留列出来删掉。
function Join-Safe($base, $child) {
  if (-not $base) { return $null }
  return (Join-Path $base $child)
}

function Get-CandidateDirs {
  @(
    (Join-Safe $env:LOCALAPPDATA $BinName),
    (Join-Safe $env:LOCALAPPDATA "Programs\$BinName"),
    (Join-Safe $env:USERPROFILE 'bin'),
    (Join-Safe $env:USERPROFILE '.local\bin'),
    (Join-Safe $env:ProgramFiles $BinName),
    (Join-Safe $env:APPDATA $BinName)
  ) | Where-Object { $_ -and (Test-Path $_) }
}

function Get-OtherCopies($keep) {
  $found = @()
  foreach ($d in (Get-CandidateDirs)) {
    if ($d -eq $keep) { continue }
    foreach ($n in @("$BinName.exe", "$BinName.exe.old")) {
      $p = Join-Path $d $n
      if (Test-Path $p) { $found += $p }
    }
  }
  return $found
}

function Invoke-Clean {
  $target = Resolve-TargetDir
  $dest = Join-Path $target "$BinName.exe"
  $others = @(Get-OtherCopies $target)

  if ($others.Count -eq 0) {
    Write-Host "没有发现重复或残留的 $BinName（保留 $dest）。"
    return 0
  }

  # 保住的那份不在，先不删，否则可能把机器上唯一一份删掉
  if (-not (Test-Path $dest)) {
    Write-Host "安装目录 $target 里没有 $BinName.exe，先装好再清理。"
    Write-Host "先跑：irm $ReleaseBaseDefault/install.ps1 | iex"
    return 1
  }

  Write-Host "发现以下重复 / 残留文件（保留 $dest）："
  foreach ($f in $others) { Write-Host "  - $f" }

  if ($DryRun) {
    Write-Step "Remove-Item <上面这些>"
    Write-Host "dry-run：没有改动任何东西。"
    return 0
  }

  foreach ($f in $others) {
    try {
      Remove-Item -Force $f
      Write-Step "已删除 $f"
    } catch {
      Write-Host "  ! 删除失败（可能正在运行）：$f"
    }
  }
  return 0
}

# ---- 卸载 -------------------------------------------------------------------
function Invoke-Uninstall {
  $target = Resolve-TargetDir
  $dest = Join-Path $target "$BinName.exe"
  Write-Host "卸载 $dest"
  if (-not (Test-Path $dest)) {
    Write-Host "该路径下没有 $BinName.exe，可能装在别处（用 Get-Command $BinName 找一下）。"
    Write-Host "凭证不在这个文件里，仍保留。"
    return 0
  }
  if ($DryRun) {
    Write-Step "Remove-Item '$dest'"
    Write-Step "从用户 PATH 移除 $target"
    Write-Host "dry-run：没有改动任何东西。"
    return 0
  }
  try {
    Remove-Item -Force $dest
  } catch {
    return (Fail-With "删除失败（$BinName 可能正在运行，请先关掉所有 $BinName 进程）：$($_.Exception.Message)" 3)
  }
  Remove-FromUserPath $target
  Write-Host "已移除。凭证仍存在系统凭据管理器；要一并清理可运行：$BinName auth logout"
  return 0
}

# ---- 安装 / 更新 -------------------------------------------------------------
function Invoke-Install {
  $releaseBase = if ($Version) { "https://github.com/$Repo/releases/download/$Version" } else { $ReleaseBaseDefault }
  $target = Resolve-TargetDir
  $dest = Join-Path $target "$BinName.exe"

  Write-Host "千谲 qianjue CLI 安装 / 更新"
  if ($IsWindows -eq $false) {
    Write-Host "  ! 当前不是 Windows（$($PSVersionTable.Platform)），本脚本装的是 windows-amd64 版本，" -ForegroundColor Yellow
    Write-Host "    仅供测试或跨平台准备；macOS / Linux 请用 install.sh。" -ForegroundColor Yellow
  }
  Write-Step "平台：windows-amd64（资产 $Asset）"
  Write-Step "来源：$releaseBase"
  $current = Get-CurrentVersion
  if ($current) { Write-Step "当前版本：$current" } else { Write-Step "当前未安装" }

  if ($DryRun) {
    Write-Step "下载 $releaseBase/$Asset"
    if (-not $NoVerify) { Write-Step "下载 $releaseBase/SHA256SUMS 并校验" }
    Write-Step "安装到 $dest"
    Write-Step "确保 $target 在用户 PATH 里"
    Write-Host "dry-run：没有改动任何东西。"
    return 0
  }

  # 不要用 $env:TEMP：它在非 Windows 上可能是 null，Join-Path 会直接报错。
  $tmp = Join-Path ([System.IO.Path]::GetTempPath()) ("qianjue-install-" + [Guid]::NewGuid().ToString('N'))
  New-Item -ItemType Directory -Force -Path $tmp | Out-Null
  try {
    $assetPath = Join-Path $tmp $Asset
    Write-Step "下载 $Asset"
    try {
      Invoke-WebRequest -Uri "$releaseBase/$Asset" -OutFile $assetPath -UseBasicParsing
    } catch {
      return (Fail-With "下载失败：$releaseBase/$Asset`n$($_.Exception.Message)" 2)
    }

    if (-not $NoVerify) {
      Write-Step "校验 SHA256"
      $sumsPath = Join-Path $tmp 'SHA256SUMS'
      $haveSums = $true
      try {
        Invoke-WebRequest -Uri "$releaseBase/SHA256SUMS" -OutFile $sumsPath -UseBasicParsing
      } catch {
        $haveSums = $false
      }
      if (-not $haveSums) {
        Write-Host "  ! 取不到 SHA256SUMS，跳过校验"
      } else {
        $line = Get-Content $sumsPath | Where-Object { $_ -match "\s$([regex]::Escape($Asset))\s*$" } | Select-Object -First 1
        if (-not $line) {
          Write-Host "  ! SHA256SUMS 里没有 $Asset，跳过校验"
        } else {
          $want = ($line -split '\s+')[0].ToUpper()
          $got = (Get-FileHash $assetPath -Algorithm SHA256).Hash.ToUpper()
          if ($want -ne $got) { return (Fail-With "SHA256 校验失败，已中止（期望 $want，实际 $got）" 2) }
          Write-Step "校验通过"
        }
      }
    }

    Write-Step "安装到 $dest"
    New-Item -ItemType Directory -Force -Path $target | Out-Null
    # 正在运行的 exe 不能被覆盖，改名备份再删，避免「更新失败但旧文件已没了」
    if (Test-Path $dest) {
      $backup = "$dest.old"
      Remove-Item -Force $backup -ErrorAction SilentlyContinue
      Move-Item -Force $dest $backup
      try {
        Copy-Item -Force $assetPath $dest
        Remove-Item -Force $backup -ErrorAction SilentlyContinue
      } catch {
        Move-Item -Force $backup $dest
        return (Fail-With "写入失败（$BinName 可能正在运行，请关闭后重试）：$($_.Exception.Message)" 3)
      }
    } else {
      Copy-Item -Force $assetPath $dest
    }

    $added = Add-ToUserPath $target
    if ($added) {
      Write-Step "已把 $target 加入用户 PATH（需新开终端生效）"
    } else {
      $inPath = (($env:PATH -split ';') -contains $target)
      if (-not $inPath) {
        Write-Host ""
        Write-Host "注意：$target 可能不在 PATH 里。若新开终端后找不到 $BinName，请把该目录加入用户 PATH。"
      }
    }

    try {
      $new = (& $dest version --output json | Out-String | ConvertFrom-Json).data.version
      Write-Host "已安装：$new"
    } catch {
      Write-Host "已安装：$dest"
    }

    $otherCopies = @(Get-OtherCopies $target)
    if ($otherCopies.Count -gt 0) {
      Write-Host ""
      Write-Host "  ! 机器上还有其它位置的 $BinName，PATH 顺序决定实际跑哪个：" -ForegroundColor Yellow
      foreach ($f in $otherCopies) { Write-Host "      - $f" -ForegroundColor Yellow }
      Write-Host "    建议清理，只保留 $target 这一份：" -ForegroundColor Yellow
      Write-Host "      & ([scriptblock]::Create((irm $ReleaseBaseDefault/install.ps1))) -Clean" -ForegroundColor Yellow
    }

    Write-Host ""
    Write-Host "完成。首次使用请登录：$BinName auth login"
    Write-Host "以后要更新，重跑同一条安装命令即可。"
    return 0
  } finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
  }
}

# ---- 入口 -------------------------------------------------------------------
$code = 0
if ($Uninstall) { $code = Invoke-Uninstall }
elseif ($Clean) { $code = Invoke-Clean }
else { $code = Invoke-Install }
Finish ([int](@($code)[-1]))
