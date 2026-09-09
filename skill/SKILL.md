---
name: qianjue-cli
description: 用 qianjue 命令行调用千谲 AI 平台生成图片和视频（换装、抠图、放大、重绘、印花提取、模特三视图、图生视频、字幕擦除、视频翻译等）、查询与等待任务、处理幂等与失败恢复。Activates whenever the user wants to generate or edit an image or video through 千谲 / qianjue, mentions the qianjue CLI, a qianjue task id, Integration Access Token / PAT, Idempotency-Key, or asks to check, wait for, cancel, or recover a qianjue task.
---

# 千谲 qianjue CLI

`qianjue` 是千谲 AI 平台的官方命令行客户端。它只是 HTTP API 的客户端，**任务在服务端异步执行，生成会扣用户积分**。

本文写给 AI 助手：照此执行即可替用户完成生成任务。**参数以本文与 `references/` 为准，不要凭印象发明字段**；不确定时先跑 `qianjue <命令> --help`。

## 铁律（先读这 7 条）

1. **创建请求超时 / 中断 / 结果未知时，绝不换新 Idempotency-Key 重试**，也不要重跑 `create` —— 那会重复创建、重复扣费。唯一正确动作是 `resume`（见 [references/recovery.md](references/recovery.md)）。
2. **必须判进程退出码**，别只看 stdout 有没有内容。
3. `--output json` 时 stdout 只有一个 JSON，进度都在 stderr。要机器解析就固定加 `--output json`。
4. **生成类命令花用户的钱**。批量、循环、重试前先跟用户确认数量。
5. 用户没要求就**不要取消任务**。`Ctrl-C` 和等待超时只停本地，服务端仍在跑，不退款。
6. **本地文件不能直接当参数** —— 所有图片/视频 URL 必须公网可达，先 `qianjue asset upload`。
7. **遇到退出码 15（`MODERATION_HOLD`）立刻停手，交给用户**。任务被内容审核拦住了，不是失败、不要重试、不要改提示词绕过、**更不要自己去申请审核或确认继续** —— 那是需要用户本人做的决定。照第 7 节转述给用户。

## 1. 检查安装

```bash
qianjue version --output json
```

命令不存在 → 见文末「安装」。

## 2. 选环境（默认生产，通常不用配）

**不配置就是生产 `https://api.aiqianjue.com/api/v1`**，普通用户直接去登录。

开发者切环境：

```bash
qianjue config profile create dev --api-base-url 'http://localhost:7777/api/v1'
qianjue config profile use dev
qianjue config show --output table    # 带「(内置默认：生产)」标记 = 正在打生产
```

优先级：`--api-base-url` > `QIANJUE_API_BASE_URL` > Profile > 内置生产默认。非 localhost 强制 HTTPS。

## 3. 登录

```bash
qianjue auth login             # Device Flow：自动开浏览器，需用户本人点「确认授权」
qianjue auth login --no-open   # 无图形界面时只打印 URL
qianjue auth status --output json
```

无浏览器 / 自动化用 PAT（用户在 Web 后台创建）：

```bash
qianjue auth import-token --stdin --type pat < token.txt
```

只能 stdin 导入，没有 `--token` 参数。凭证只进系统凭证库，**没有明文回退**（凭证库不可用 = 退出码 14）。

> 上传素材需要 `asset.write` scope；老凭证可能没有，报退出码 4 就重新登录并申请。

## 4. 核心工作流

```bash
# ① 本地文件 → 公网 URL（批量，直传对象存储，不经过平台服务器）
qianjue asset upload ./a.png ./b.png --output json
qianjue asset upload --dir ./素材 --glob '*.png' --concurrency 4 --output json

# ② 提交任务
qianjue image create        --request req.json --wait --output json
qianjue video create        --request req.json --output json
qianjue detail-image create --request req.json --output json   # 详情图：一次出整套
qianjue reverse-prompt create --video-url '…'  --output json   # 口播：先解析出脚本
qianjue viral-plan    create --request plan.json --output json  # 爆款策划：一次出完整脚本

# ③ 查询 / 等待 / 取消（domain = image | video | image-chat，必填）
qianjue task get    video <taskId> --output json
qianjue task wait   video <taskId> --wait-timeout 10m
qianjue task list   image --status PROCESSING --page 1 --size 10
qianjue task cancel video <taskId>

# ④ 提交前查能力（别把模型和比例硬编码）
qianjue catalog models       --output json   # 图片模型：比例/分辨率/画布尺寸
qianjue catalog video-models --output json   # 视频模型：可选时长/比例/输入图上限/是否必须 prompt
```

结果媒体统一在 `data.media.resultMediaList[]`。视频创建返回批次，**后续查询用 `taskIds[0]` 而不是 `batchId`**。

## 5. 参数速查（按需打开）

| 要做什么 | 看这里 |
| --- | --- |
| 图片：换装 / 抠图 / 放大 / 重绘 / 换背景 / 印花提取 / 换脸 / 三视图 / 文生图 / 图生图 / **详情图生成** | [references/image-tasks.md](references/image-tasks.md) |
| 视频：图生视频 / 口播 / 营销视频 / 模特商品替换 / 字幕擦除 / 视频翻译 / 剪辑 / 放大 | [references/video-tasks.md](references/video-tasks.md) |
| 退出码、失败处理、未知结果恢复 | [references/recovery.md](references/recovery.md) |

**最容易搞错的一条**：`modelCode` 只对换装 / 换装 Pro / 细节增强 / 模特三视图 4 类生效，
`TXT2IMG` / `IMG2IMG` 传了会被丢弃 —— 别向用户承诺「用某某模型生成」。

## 6. 输出契约

```json
{ "schemaVersion": "1", "command": "video.create", "ok": true,
  "data": { }, "meta": { "profile": "default", "traceId": "…" } }
```

失败时 `ok:false` 且带 `error.kind`。排查问题把 `meta.traceId` 给用户，服务端可凭它查链路；加 `--trace` 可看请求阶段。**要解析就必须显式 `--output json`**，别去解析表格。

## 7. 内容审核拦截（退出码 15）

任务提交后如果素材命中平台内容审核，任务**不会失败**：它停在 `PENDING`，积分**冻结但未扣除**，等一个人来决定。`task wait` 会立刻返回退出码 15，不会一直等下去。

```json
{ "ok": false, "error": { "kind": "MODERATION_HOLD",
  "message": "任务 draw/8812 被内容审核拦截：图2：疑似含受限内容。任务仍保留、积分已冻结未扣除，未在 2026-09-10T14:22:00 前处理将自动取消并退积分。需要你本人决定：qianjue moderation submit-review 41207（申请人工审核）或 qianjue moderation cancel 41207（放弃并退积分）" } }
```

**你（AI 助手）该做的**：停止当前任务流 → 把拦截原因、命中的素材、记录号原样转述给用户 → 把下面两条命令交给用户**自己执行**。

**你不该做的**：
- ❌ 自行执行 `moderation submit-review` 或 `moderation confirm`
- ❌ 给这两个命令加 `--i-understand-the-risk`（这个标志是留给用户在脚本里表达本人意愿的，不是给你绕过用的）
- ❌ 换个说法重写提示词再提交一次（同样会被拦，且再冻一笔积分）
- ❌ 把退出码 15 当成 `TASK_FAILED` 去重试

用户可用的命令：

```bash
qianjue moderation list                    # 列出所有待决定的拦截（跨会话恢复用）
qianjue moderation status <recordId>       # 查审核进度
qianjue moderation submit-review <recordId>  # 申请人工审核（只入队，不放行）
qianjue moderation confirm <recordId>      # 平台通过后，知悉风险并继续生成
qianjue moderation cancel <recordId>       # 放弃，积分立即退回
```

流程是：**拦截 → 用户申请人工审核 → 平台审核 → 通过后用户确认继续**。注意 CLI 提交的任务不会自动进审核队列（`reviewStatus` 为空 = 还没申请），必须用户显式 `submit-review`；干等只会等到超时自动取消。

审核通常跨小时，你的会话多半撑不到。等用户回来时，让他跑 `qianjue moderation list` 接着处理即可 —— 你不需要守着。`cancel` 是安全动作（退积分、不产出），用户明确说不要了的话你可以代为执行。

## 8. 环境变量

| 变量 | 作用 |
| --- | --- |
| `QIANJUE_TOKEN` | 直接提供凭证，优先级最高（CI 常用；**也是绕开系统凭证库的唯一方式**，见下） |
| `QIANJUE_API_BASE_URL` | 覆盖 API 根地址 |
| `QIANJUE_PROFILE` | 选择 Profile |
| `QIANJUE_OUTPUT` | 默认输出格式 |
| `QIANJUE_HTTP_TIMEOUT` / `QIANJUE_TASK_WAIT_TIMEOUT` | 单次 HTTP 超时 / 任务等待上限 |

> **凭证库超时（退出码 14）怎么办**：命令报「系统凭证库读取超时（10s）；macOS 钥匙串可能正在等待授权点击」时，是钥匙串在等一个没人会点的授权弹窗 —— 换了新编译的二进制、或换了安装路径都会触发。**不要重试**，重试一样超时。让用户用 `QIANJUE_TOKEN=<token>` 提供凭证，或让用户在他自己的交互式终端里跑一次同样的命令并点「允许」，之后就正常了。

## 9. 不要做的事

- ❌ 创建超时后换新 Key 重试（重复扣费）—— 用 `resume`
- ❌ 退出码 8 之后继续尝试创建
- ❌ 把本地路径当 `url` / `inputImageUrl` 传给后端
- ❌ 把 token 写进命令行、脚本、日志或提交进 Git
- ❌ 解析非 JSON 输出，或只看 stdout 不判退出码
- ❌ 用户没要求就取消任务、或批量刷生成
- ❌ 退出码 15 时自行申请审核 / 确认继续 / 加 `--i-understand-the-risk` / 改写提示词重提
- ❌ 退出码 14「凭证库超时」后反复重试 —— 换 `QIANJUE_TOKEN` 或让用户在交互式终端授权一次

## 安装与更新

```bash
qianjue skill show                 # 打印本文到 stdout
qianjue skill path --output json   # 看候选 skills 目录及其是否存在
qianjue skill install              # 便捷：写入【已存在】的 skills 目录（含 references/）
qianjue skill install --dir <路径>  # 显式指定目录
```

**放哪由你（AI）判断** —— 你比 CLI 更清楚当前是哪个助手、用哪套 skills 目录。
`skill install` **不会创建 `~/.codex`、`~/.claude` 这类配置根目录**（可能是符号链接或被统一管理，硬造会弄坏用户配置）；一个候选都不存在时它会失败并提示用 `--dir` 或 `show`。

**装完/更新后必须让用户重新打开会话**才会加载（会话启动时才扫描 skills 目录）。
CLI 本身的安装方式见千谲官方安装页。
