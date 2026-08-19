---
name: qianjue-cli
description: 用 qianjue 命令行调用千谲 AI 平台生成图片和视频、查询与等待任务、处理幂等与失败恢复。Activates whenever the user wants to generate an image or video through 千谲 / qianjue, mentions the qianjue CLI, a qianjue task id, Integration Access Token / PAT, Idempotency-Key, or asks to check, wait for, cancel, or recover a qianjue task.
---

# 千谲 qianjue CLI 使用指南

本文写给 AI 助手：照此执行即可替用户用 `qianjue` 生成图片/视频并跟踪任务。**每条命令、每个字段都以本文为准，不要凭印象发明参数。** 不确定时先跑 `qianjue <命令> --help` 看真实签名。

`qianjue` 是千谲 AI 平台的官方命令行客户端：提交图片/视频生成任务、查询与等待结果、失败后按幂等键恢复。它只是 HTTP API 的客户端，**任务在服务端异步跑，生成要花钱（扣积分）**。

## 0. 铁律（先读这 6 条，违反会造成重复扣费或丢任务）

1. **创建请求超时/网络中断/结果未知时，绝不换一个新的 Idempotency-Key 重试**，也不要重新跑一次 `create`——那会重复创建任务、重复扣费。唯一正确动作是 `resume`（见 §6）。
2. **必须判进程退出码**，不要只看 stdout 有没有内容。退出码是稳定契约（见 §7）。
3. `--output json` 时 stdout **只有一个 JSON**，进度与提示都在 stderr。要机器解析就固定加 `--output json`。
4. **生成类命令会扣用户积分**。批量、循环、重试前先跟用户确认数量。
5. 用户没要求就**不要取消任务**。`Ctrl-C` 和等待超时只停止本地等待，服务端任务照常在跑，不会退款。
6. **本地图片不能直接喂给任务**——必须先 `qianjue asset upload` 换成公网 URL（见 §3.5）。

## 1. 检查是否已安装

```bash
qianjue version --output json
```

- 命令不存在 → 按安装页指引装（见本文末「安装」），装完再继续。
- 能跑通 → 进入 §2。

## 2. 一次性配置：Profile + 登录

### 2.1 选择环境（默认生产，通常不用配）

**不配置任何东西时默认打生产 `https://api.aiqianjue.com/api/v1`**，普通用户直接跳到 2.2 登录。

只有千谲开发者需要切环境：

```bash
qianjue config profile create dev --api-base-url 'http://localhost:7777/api/v1'
qianjue config profile use dev
# 或单次覆盖：--api-base-url '...' / 环境变量 QIANJUE_API_BASE_URL
```

**动手前先确认在打哪个环境**（尤其是要花积分的生成类命令）：

```bash
qianjue config show --output table
# API base URL  https://api.aiqianjue.com/api/v1  (内置默认：生产)  ← 带这个标记就是在打生产
```

- 优先级：`--api-base-url` > `QIANJUE_API_BASE_URL` > Profile > 内置生产默认。
- 非 localhost 地址**强制 HTTPS**；凭证按 Profile 隔离，切 Profile 要重新登录。

### 2.2 登录（两条路，选一条）

**A. Device Flow（有浏览器的桌面环境，推荐）**

```bash
qianjue auth login
```

- **会自动打开浏览器**到授权页（macOS `open` / Windows `rundll32` / Linux `xdg-open`）。
- **需要用户本人在浏览器里点「确认授权」**，且该浏览器已登录千谲 Web。这一步 AI 代替不了——把 URL 给用户，请他点完再回来。
- 无图形界面（SSH/CI/容器）加 `--no-open`，只打印 URL，让用户在别的机器上打开。
- 默认申请 `task.create` / `task.read` / `task.cancel`，可用 `--scope` 重复指定。

**B. Personal Access Token（无浏览器 / 自动化）**

用户在千谲 Web 后台创建 PAT（`qj_pat_` 开头），然后：

```bash
qianjue auth import-token --stdin --type pat < /path/to/token.txt
```

- **只能从 stdin 导入**，没有 `--token` 参数，别把明文 token 写进命令行（会进 shell 历史）。
- 一次性场景也可用环境变量 `QIANJUE_TOKEN=qj_pat_xxx`，优先级最高。

### 2.3 确认登录状态

```bash
qianjue auth status --output json     # 只读本地元数据，不调后端
```

凭证存在系统凭证库（macOS Keychain / Windows Credential Manager / Linux Secret Service）。**没有明文文件回退**——凭证库不可用会直接失败（退出码 14），不要试图绕过。

## 3. 生成图片

把请求写成 JSON 文件再提交（`--request -` 表示从 stdin 读）：

```bash
cat > /tmp/req.json <<'EOF'
{
  "type": "TXT2IMG",
  "prompt": "白底马克杯，商业棚拍",
  "modelCode": "GPT_IMAGE_2",
  "aspectRatio": "1:1",
  "outputResolution": "1K",
  "outputCount": 1
}
EOF

qianjue image create --request /tmp/req.json --wait --output json
```

- `--wait` 会一直轮询到终态（完成/失败/取消），不加则立刻返回 taskId 由你自己等。
- `--wait-timeout 10m` 覆盖等待上限；**超时只是本地不等了，任务仍在服务端跑**（退出码 7），随后可用 `qianjue task get image <taskId>` 继续查。
- `--idempotency-key` 一般不用手动给，省略时自动生成并在发 HTTP 前落本地日志。

## 3.5 带输入图的任务（图生图 / 编辑 / 换装 / 放大）

**关键约束：`inputImages[].url` 必须是服务端能访问的公网地址，本地路径一律无效。** 手上是本地文件时，先上传拿 URL：

```bash
# 上传（支持批量；文件直传对象存储，不经过千谲服务器）
qianjue asset upload ./原图.png --output json
# → [{"file":"./原图.png","objectKey":"...","url":"https://.../xxx.png","size":123}]

# 整个目录
qianjue asset upload --dir ./素材 --glob '*.png' --output json
```

拿到 `url` 后填进请求：

```bash
cat > /tmp/edit.json <<'EOF'
{
  "type": "IMG2IMG",
  "prompt": "把人物脚上的黑色袜子改成红色，其余部分保持不变",
  "modelCode": "GPT_IMAGE_2",
  "aspectRatio": "1:1",
  "outputResolution": "1K",
  "outputCount": 1,
  "inputImages": [
    { "type": "image", "url": "https://<上一步拿到的 url>" }
  ]
}
EOF

qianjue image create --request /tmp/edit.json --wait --output json
```

- `inputImages[].type` 按任务而定：图生图 / 放大用 `image`；换装类用 `model` + `inner_top` / `outer_top` / `pants` / `whole_body` 等部位名；换脸用 `model` + `face`。**不确定时问用户，别猜**。
- 一次最多上传 20 个文件；上传本身**不扣积分**（扣费发生在后面的 `image create`）。
- 上传失败退出码 12（传输错误）；文件类型不支持或超限退出码 2（用法错误，消息里写明原因）。

## 4. 生成视频

四种视频任务，各一个子命令，请求体形状不同：

| 命令 | 用途 |
| --- | --- |
| `qianjue video create` | 普通/参考视频生成（批量，1~20 条） |
| `qianjue video edit` | 视频编辑 |
| `qianjue video upscale` | 视频高清放大 |
| `qianjue video gesture-replica` | 手势舞一键复刻 |

图生视频示例（`items` 是数组，一次可提交多条）：

```bash
cat > /tmp/video.json <<'EOF'
{
  "sourceType": "VIDEO_TASK",
  "modelCode": "SEEDANCE_2_0_MINI",
  "items": [
    {
      "inputImageUrl": "https://<公网可访问的图片地址>.png",
      "prompt": "产品在白色桌面上缓慢旋转，商业棚拍质感",
      "durationSeconds": 5,
      "seedanceConfig": { "resolution": "480p" }
    }
  ]
}
EOF

qianjue video create --request /tmp/video.json --output json
```

- 图生视频的 `inputImageUrl` **必须是服务端能访问的公网地址**，本地路径无效——本地文件先走 `qianjue asset upload`（§3.5）。
- 返回的是批次：`{batchId, totalCount, taskIds[], status}`，**后续查询用 `taskIds[0]`，不是 batchId**。
- 四类共用同一个幂等作用域，所以**同一个 Idempotency-Key 不能用于不同类型的视频请求**（会因指纹不同报冲突，退出码 6）。

## 5. 查询 / 等待 / 取消任务

统一命令跨业务域，`<domain>` 取 `image` 或 `video`，**必填**（各域任务 ID 不互通，无法自动推断）：

```bash
qianjue task get    video <taskId> --output json
qianjue task list   video --status PROCESSING --page 1 --size 10 --output json
qianjue task wait   video <taskId> --wait-timeout 10m --output json
qianjue task cancel video <taskId> --output json
```

- `--status` 可选 `PENDING` / `PROCESSING` / `COMPLETED` / `FAILED` / `CANCELLED`。
- 结果媒体在 `data.media.resultMediaList[]`（图片和视频都归一到这里）。
- `qianjue image get|wait|cancel <taskId>` 与 `task ... image <taskId>` 等价，两种都可用。
- 取消只对未开始的任务有意义，**已完成不会退款**；取消失败不自动重试。

## 6. 创建失败怎么办（最重要）

判断依据是**退出码**：

| 情况 | 退出码 | 正确动作 |
| --- | --- | --- |
| 网络中断 / 超时 / 结果未知 | 12 | **`resume`，不要重新 create** |
| 幂等键冲突（同 Key 不同请求体） | 6 | 换个新 Key，或改回原来的请求体 |
| 需要人工恢复 | 8 | **停下来告诉用户**，不要再尝试任何创建 |
| 余额不足 / 业务终态失败 | 9 | 告知用户，不要重试 |

`resume` 用本地请求日志按**原 Key、原 JSON** 重放，不会重复创建：

```bash
qianjue image resume --idempotency-key <原来那个 Key>
qianjue video resume --idempotency-key <原来那个 Key>
```

失败输出里就有那个 Key（`data.idempotencyKey`）。找不到时用 `qianjue image request-status --idempotency-key <Key>` 查服务端的脱敏幂等状态。

**退出码 8（RECOVERY_REQUIRED）意味着服务端也不确定结果，只能人工介入——此时 AI 必须停止并上报，任何"再试一次"都是错的。**

## 7. 退出码（稳定契约，必须判）

| 码 | 含义 | 建议动作 |
| --- | --- | --- |
| 0 | 成功 | 继续 |
| 2 | 参数/用法错误 | 修正命令 |
| 3 | 认证失败 | 重新登录（§2.2） |
| 4 | 权限不足（缺 Scope） | 重新登录并申请对应 scope |
| 5 | 资源不存在 | 核对 taskId 与 domain |
| 6 | 幂等键冲突 | 见 §6 |
| 7 | 仍在进行 / 本地等待超时 | 任务还在跑，稍后 `task get` |
| 8 | 需要人工恢复 | **停止，上报用户** |
| 9 | 终态失败（含余额不足） | 上报，不重试 |
| 10 | 任务失败 | 读 message 上报 |
| 11 | 任务已取消 | 上报 |
| 12 | 网络/传输错误 | 见 §6，创建类走 `resume` |
| 13 | 服务端错误 | 可稍后重试**查询**类；创建类走 `resume` |
| 14 | 本地凭证库/文件不可用 | 检查系统凭证库，别用明文绕过 |
| 130 | 被 Ctrl-C 中断 | 仅本地停止，服务端任务仍在跑 |

## 8. 输出契约

`--output json` 时 stdout 是单个 JSON：

```json
{ "schemaVersion": "1", "command": "video.create", "ok": true,
  "data": { }, "meta": { "profile": "default", "traceId": "..." } }
```

失败时 `ok:false` 且带 `error.kind`（如 `AUTH` / `TRANSPORT` / `RECOVERY_REQUIRED`）。排查问题时把 `meta.traceId` 给用户，服务端可凭它查链路。加 `--trace` 可打印请求阶段。

不带 `--output json` 时是给人看的表格；**要解析就必须显式加 `--output json`**，别去解析表格。

## 9. 环境变量

| 变量 | 作用 |
| --- | --- |
| `QIANJUE_TOKEN` | 直接提供凭证，优先级最高（CI 常用） |
| `QIANJUE_API_BASE_URL` | 覆盖 API 根地址 |
| `QIANJUE_PROFILE` | 选择 Profile |
| `QIANJUE_OUTPUT` | 默认输出格式 |
| `QIANJUE_HTTP_TIMEOUT` / `QIANJUE_TASK_WAIT_TIMEOUT` | 单次 HTTP 超时 / 任务等待上限 |

## 10. 不要做的事

- ❌ 创建超时后换新 Key 重试（重复扣费）——用 `resume`
- ❌ 退出码 8 之后继续尝试创建
- ❌ 把 token 写进命令行参数、脚本、日志或提交进 Git（只走 stdin / 环境变量 / 凭证库）
- ❌ 解析非 JSON 输出、或只看 stdout 不判退出码
- ❌ 把本地文件路径当成 `url` / `inputImageUrl` 传给后端（服务端读不到，必须先 `asset upload`）
- ❌ 用户没要求就取消任务、或批量刷生成（花的是用户的钱）
- ❌ 凭证库不可用时改用明文文件绕过

## 安装与更新本 skill

本 skill 的内容随 `qianjue` 二进制分发，用下面任一方式取出：

```bash
qianjue skill show                 # 把 SKILL.md 打印到 stdout —— 你自己决定写到哪个 skills 目录
qianjue skill path --output json   # 看候选目录及其是否存在（不写文件）
qianjue skill install              # 便捷方式：只写入已存在的 skills 目录
qianjue skill install --dir <路径>  # 显式指定目录（会创建）
```

**放置位置由你（AI）判断**：你比 CLI 更清楚当前是哪个助手、用的哪套 skills 目录约定、该放全局还是项目级。`skill install` 只是便捷方式，**它不会创建助手的配置根目录**——没探测到已存在的 skills 目录时会失败并让你用 `--dir` 或 `show`，这是刻意的，避免弄坏用户的助手配置。

CLI 升级后重跑一次即可更新本 skill。**装完/更新后需让用户重新打开会话才会加载**（会话启动时才扫描 skills 目录）。CLI 本身的安装方式见千谲官方安装页。
