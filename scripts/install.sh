#!/bin/sh
# 千谲 qianjue CLI 安装 / 更新 / 卸载（macOS、Linux）
#
#   安装或更新（再跑一遍就是更新，会覆盖旧二进制）：
#     curl -fsSL https://github.com/zriyox/qianjue-cli/releases/latest/download/install.sh | sh
#
#   先看它要做什么，不实际改动：
#     curl -fsSL .../install.sh | sh -s -- --dry-run
#
#   卸载：
#     curl -fsSL .../install.sh | sh -s -- --uninstall
#
#   清理重复 / 残留的旧二进制（「更新完还是老版本」基本都是这个原因）：
#     curl -fsSL .../install.sh | sh -s -- --clean
#
# 参数：
#   --dir <路径>     安装目录（默认 /usr/local/bin；不可写时退回 ~/.local/bin）
#   --version <tag>  指定版本，如 v0.6.3（默认最新）
#   --uninstall      卸载：移除二进制；不动系统凭证库里的登录状态
#   --clean          清理其它目录里重复的 qianjue 和 *.old 残留，只保留安装目录那一份
#   --dry-run        只打印将要执行的动作
#   --no-verify      跳过 SHA256 校验（不建议）
#   -h, --help       帮助
#
# 退出码：0 成功 / 1 参数或环境错误 / 2 下载或校验失败 / 3 安装失败

set -eu

REPO="zriyox/qianjue-cli"
BIN_NAME="qianjue"
GITHUB="https://github.com"
RELEASE_BASE_DEFAULT="${GITHUB}/${REPO}/releases/latest/download"

TARGET_DIR=""
WANT_VERSION=""
UNINSTALL=0
CLEAN=0
DRY_RUN=0
VERIFY=1

usage() {
  if [ -r "$0" ]; then
    sed -n '2,/^$/p' "$0" 2>/dev/null | sed 's/^# \{0,1\}//'
    return 0
  fi
  cat <<'USAGE'
qianjue CLI 安装 / 更新 / 卸载
  --dir <路径>     安装目录
  --version <tag>  指定版本
  --uninstall      卸载
  --clean          清理重复 / 残留的旧二进制
  --dry-run        只打印动作
  --no-verify      跳过 SHA256 校验
  -h, --help       帮助
USAGE
}

die() { printf '错误：%s\n' "$1" >&2; exit "${2:-1}"; }
note() { printf '%s\n' "$1"; }
step() { printf '  → %s\n' "$1"; }

while [ $# -gt 0 ]; do
  case "$1" in
    --dir)        [ $# -ge 2 ] || die "--dir 需要跟一个路径" ; TARGET_DIR="$2"; shift 2 ;;
    --dir=*)      TARGET_DIR="${1#--dir=}"; shift ;;
    --version)    [ $# -ge 2 ] || die "--version 需要跟一个 tag" ; WANT_VERSION="$2"; shift 2 ;;
    --version=*)  WANT_VERSION="${1#--version=}"; shift ;;
    --uninstall)  UNINSTALL=1; shift ;;
    --clean)      CLEAN=1; shift ;;
    --dry-run)    DRY_RUN=1; shift ;;
    --no-verify)  VERIFY=0; shift ;;
    -h|--help)    usage; exit 0 ;;
    *)            die "未知参数：$1（用 -h 看帮助）" ;;
  esac
done

# ---- 平台识别 ---------------------------------------------------------------
detect_platform() {
  os=$(uname -s 2>/dev/null || echo unknown)
  arch=$(uname -m 2>/dev/null || echo unknown)

  case "$os" in
    Darwin) PLATFORM_OS="darwin" ;;
    Linux)  PLATFORM_OS="linux" ;;
    MINGW*|MSYS*|CYGWIN*)
      die "这是 Windows 的 shell。请改用 PowerShell 版：
  irm https://github.com/${REPO}/releases/latest/download/install.ps1 | iex" 1 ;;
    *) die "不支持的平台：$os（只发布 darwin / linux / windows 二进制）" 1 ;;
  esac

  case "$arch" in
    arm64|aarch64) PLATFORM_ARCH="arm64" ;;
    x86_64|amd64)  PLATFORM_ARCH="amd64" ;;
    *) die "不支持的 CPU 架构：$arch" 1 ;;
  esac

  ASSET="${BIN_NAME}-${PLATFORM_OS}-${PLATFORM_ARCH}"
}

# ---- 工具可用性 -------------------------------------------------------------
have() { command -v "$1" >/dev/null 2>&1; }

pick_downloader() {
  if have curl; then DOWNLOADER="curl"
  elif have wget; then DOWNLOADER="wget"
  else die "需要 curl 或 wget 才能下载" 1
  fi
}

download() { # $1=url  $2=输出文件
  case "$DOWNLOADER" in
    curl) curl -fsSL --retry 3 --retry-delay 1 -o "$2" "$1" ;;
    wget) wget -q -O "$2" "$1" ;;
  esac
}

pick_checksummer() {
  if have shasum; then CHECKSUMMER="shasum -a 256"
  elif have sha256sum; then CHECKSUMMER="sha256sum"
  elif have openssl; then CHECKSUMMER="openssl-sha256"
  else CHECKSUMMER=""
  fi
}

sha256_of() {
  case "$CHECKSUMMER" in
    "shasum -a 256") shasum -a 256 "$1" | awk '{print $1}' ;;
    sha256sum)       sha256sum "$1" | awk '{print $1}' ;;
    openssl-sha256)  openssl dgst -sha256 "$1" | awk '{print $NF}' ;;
    *) echo "" ;;
  esac
}

# ---- 安装目录 ---------------------------------------------------------------
# 顺序很重要：已经装过的用户必须原地更新。否则新旧两份二进制并存，
# PATH 顺序决定实际执行哪个，会出现「升级完还是老版本」的假象。
default_dir() {
  if [ -n "${TARGET_DIR}" ]; then echo "$TARGET_DIR"; return; fi

  existing=$(command -v "$BIN_NAME" 2>/dev/null || true)
  if [ -n "$existing" ]; then
    # 常见的是 ~/.local/bin 或 /usr/local/bin 里的真实文件；包装脚本/别名跳过
    if [ -f "$existing" ] && [ -x "$existing" ]; then
      dirname "$existing"
      return
    fi
  fi

  if [ -w /usr/local/bin ] 2>/dev/null; then echo "/usr/local/bin"; return; fi
  # 目录不存在但 /usr/local 可写，也算能用
  if [ ! -d /usr/local/bin ] && [ -w /usr/local ] 2>/dev/null; then echo "/usr/local/bin"; return; fi
  if have sudo && [ -t 0 ] && [ -t 1 ]; then echo "/usr/local/bin"; return; fi

  echo "${HOME}/.local/bin"
}

install_binary() { # $1=源文件  $2=目标目录
  src="$1"; dir="$2"
  dest="${dir}/${BIN_NAME}"

  if [ "$DRY_RUN" = "1" ]; then
    step "mkdir -p ${dir}"
    step "install -m 0755 ${src} ${dest}"
    return 0
  fi

  if [ ! -d "$dir" ]; then
    if [ -w "$(dirname "$dir")" ] 2>/dev/null || [ -w "$dir" ] 2>/dev/null; then
      mkdir -p "$dir" || return 1
    else
      command -v sudo >/dev/null 2>&1 || return 1
      sudo mkdir -p "$dir" || return 1
    fi
  fi

  if [ -w "$dir" ]; then
    chmod 0755 "$src" 2>/dev/null || true
    mv -f "$src" "$dest" || return 1
  else
    command -v sudo >/dev/null 2>&1 || {
      note "没有 ${dir} 的写权限，也没有 sudo。改用 --dir <可写目录>，例如：--dir \$HOME/.local/bin"
      return 1
    }
    note "需要提权写入 ${dir}（可能会提示输入开机密码）"
    sudo install -m 0755 "$src" "$dest" || return 1
  fi
  return 0
}

# ---- 卸载 -------------------------------------------------------------------
do_uninstall() {
  dir=$(default_dir)
  dest="${dir}/${BIN_NAME}"
  printf '卸载 %s\n' "$dest"
  if [ ! -e "$dest" ]; then
    note "该路径下没有 $BIN_NAME，可能装在别处（用 which $BIN_NAME 找一下）。"
    note "凭证与配置不在这个文件里，仍保留。"
    return 0
  fi
  if [ "$DRY_RUN" = "1" ]; then
    step "rm -f ${dest}"
  elif [ -w "$dir" ]; then
    rm -f "$dest"
  else
    sudo rm -f "$dest"
  fi
  note "已移除。凭证仍存在系统凭证库；要一并清理可运行：$BIN_NAME auth logout"
  note "配置文件目录（$HOME/.config/qianjue 或 $HOME/.qianjue）未删除。"
}

# ---- 清理 -------------------------------------------------------------------
# 机器上并存两份 qianjue 时，PATH 靠前的那份生效，「更新完还是老版本」。
# 这里把安装目录以外的重复二进制和 .old 残留列出来删掉。
scan_other_copies() { # $1=要保留的目录
  keep="$1"
  for d in /usr/local/bin /opt/homebrew/bin /opt/local/bin "${HOME}/.local/bin" "${HOME}/bin"; do
    [ -d "$d" ] || continue
    if [ "$d" = "$keep" ]; then continue; fi
    for f in "$d/${BIN_NAME}" "$d/${BIN_NAME}.old"; do
      if [ -e "$f" ]; then printf '%s\n' "$f"; fi
    done
  done
  return 0
}

do_clean() {
  keep=$(default_dir)
  dest="${keep}/${BIN_NAME}"
  others=$(scan_other_copies "$keep")

  if [ -z "$others" ]; then
    note "没有发现重复或残留的 ${BIN_NAME}（保留 ${dest}）。"
    return 0
  fi

  # 保住的那份不在，先不删，否则可能把机器上唯一一份删掉
  if [ ! -e "$dest" ]; then
    note "安装目录 ${keep} 里没有 ${BIN_NAME}，先装好再清理。"
    note "先跑：curl -fsSL ${RELEASE_BASE_DEFAULT}/install.sh | sh"
    return 1
  fi

  note "发现以下重复 / 残留文件（保留 ${dest}）："
  printf '%s\n' "$others" | while IFS= read -r f; do printf '  - %s\n' "$f"; done

  if [ "$DRY_RUN" = "1" ]; then
    step "rm -f <上面这些>"
    note "dry-run：没有改动任何东西。"
    return 0
  fi

  printf '%s\n' "$others" | while IFS= read -r f; do
    if [ -w "$(dirname "$f")" ]; then
      rm -f "$f"
    else
      sudo rm -f "$f"
    fi
    step "已删除 ${f}"
  done
  return 0
}

# ---- 主流程 -----------------------------------------------------------------
main() {
  detect_platform
  pick_downloader
  pick_checksummer

  if [ "$UNINSTALL" = "1" ]; then
    do_uninstall
    exit 0
  fi

  if [ "$CLEAN" = "1" ]; then
    if do_clean; then exit 0; else exit 1; fi
  fi

  if [ -n "$WANT_VERSION" ]; then
    RELEASE_BASE="${GITHUB}/${REPO}/releases/download/${WANT_VERSION}"
  else
    RELEASE_BASE="$RELEASE_BASE_DEFAULT"
  fi

  note "千谲 qianjue CLI 安装 / 更新"
  step "平台：${PLATFORM_OS}-${PLATFORM_ARCH}（资产 ${ASSET}）"
  step "来源：${RELEASE_BASE}"

  # 当前已安装版本（没有就是首次安装）
  CURRENT=""
  if have "$BIN_NAME"; then
    CURRENT=$("$BIN_NAME" version --output json 2>/dev/null | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1 || true)
  fi
  if [ -n "$CURRENT" ]; then
    step "当前版本：${CURRENT}"
  else
    step "当前未安装"
  fi

  if [ "$DRY_RUN" = "1" ]; then
    step "临时目录：$(mktemp -d)"
    step "下载 ${RELEASE_BASE}/${ASSET}"
    [ "$VERIFY" = "1" ] && step "下载 ${RELEASE_BASE}/SHA256SUMS 并校验"
    step "安装到 $(default_dir)/${BIN_NAME}"
    [ "$PLATFORM_OS" = "darwin" ] && step "清除 macOS 隔离属性（xattr -d com.apple.quarantine）"
    note "dry-run：没有改动任何东西。"
    exit 0
  fi

  TMPDIR_QJ=$(mktemp -d)
  # shellcheck disable=SC2064
  trap "rm -rf '${TMPDIR_QJ}'" EXIT INT TERM

  step "下载 ${ASSET}"
  download "${RELEASE_BASE}/${ASSET}" "${TMPDIR_QJ}/${ASSET}" || die "下载失败：${RELEASE_BASE}/${ASSET}" 2

  if [ "$VERIFY" = "1" ]; then
    if [ -n "$CHECKSUMMER" ]; then
      step "校验 SHA256"
      if download "${RELEASE_BASE}/SHA256SUMS" "${TMPDIR_QJ}/SHA256SUMS" 2>/dev/null; then
        want=$(grep " ${ASSET}\$" "${TMPDIR_QJ}/SHA256SUMS" | awk '{print $1}' | head -1 || true)
        if [ -n "$want" ]; then
          got=$(sha256_of "${TMPDIR_QJ}/${ASSET}")
          if [ "$want" != "$got" ]; then
            die "SHA256 校验失败，已中止（期望 ${want}，实际 ${got}）" 2
          fi
          step "校验通过"
        else
          note "  ! SHA256SUMS 里没有 ${ASSET}，跳过校验"
        fi
      else
        note "  ! 取不到 SHA256SUMS，跳过校验"
      fi
    else
      note "  ! 没有 shasum / sha256sum / openssl，跳过校验"
    fi
  fi

  TARGET=$(default_dir)
  step "安装到 ${TARGET}/${BIN_NAME}"
  install_binary "${TMPDIR_QJ}/${ASSET}" "$TARGET" || die "安装失败（可试试 --dir \$HOME/.local/bin）" 3

  DEST="${TARGET}/${BIN_NAME}"
  if [ "$PLATFORM_OS" = "darwin" ] && have xattr; then
    xattr -d com.apple.quarantine "$DEST" 2>/dev/null || true
    step "已清除 macOS 隔离属性"
  fi

  if [ -x "$DEST" ]; then
    NEW=$("$DEST" version --output json 2>/dev/null | sed -n 's/.*"version"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -1 || true)
    if [ -n "$NEW" ]; then
      note "已安装：${NEW}"
    else
      note "已安装：${DEST}"
    fi
  fi

  case ":${PATH}:" in
    *":${TARGET}:"*) ;;
    *) note ""
       note "注意：${TARGET} 不在当前 PATH 里，请加到 PATH，例如："
       note "  echo 'export PATH=\"${TARGET}:\$PATH\"' >> ~/.profile && exec \$SHELL -l" ;;
  esac

  others=$(scan_other_copies "$TARGET")
  if [ -n "$others" ]; then
    note ""
    note "  ! 机器上还有其它位置的 ${BIN_NAME}，PATH 顺序决定实际跑哪个："
    printf '%s\n' "$others" | while IFS= read -r f; do printf '      - %s\n' "$f"; done
    note "    建议清理，只保留 ${TARGET} 这一份："
    note "      curl -fsSL ${RELEASE_BASE_DEFAULT}/install.sh | sh -s -- --clean"
  fi

  note ""
  note "完成。首次使用请登录：${BIN_NAME} auth login"
  note "以后要更新，重跑同一条安装命令即可。"
}

main "$@"
