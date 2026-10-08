# 视频任务参数速查

线上 9 个视频工具里，**出片这一步全部走同一个端点**：`qianjue video create --request <json>`，
区别在 `sourceType`（业务场景）× `modelCode`（用哪个模型/供应商）。

> 以下组合均经真实提交验证：后端校验通过、计费通过、**供应商真实接单**。

## 顶层字段

| 字段 | 约束 |
| --- | --- |
| `sourceType` | `VIDEO_TASK` / `VIDEO_REVERSE_PROMPT` / `GESTURE_DANCE_REPLICA` / `TVC_ADS` / `VIDEO_STUDIO_KOUBO` / `VIDEO_STUDIO_SMART_MIX` |
| `modelCode` | 见下表，默认 `SEEDANCE_2_0` |
| `items[]` | **1~20** 条，批量就多放几条 |
| `groupId` | ≤64，可选分组 |
| `templateMaterialId` | 可选 |

**不要传 `subjectType` / `subjectId`**（后端按登录态判定）。`clientRequestId` 由 CLI 自动生成，不用手写。

> **提交前先查目录**：`qianjue catalog video-models` 返回每个模型的
> `allowedDurations` / `allowedAspectRatios` / `maxInputImages` / `requiresPrompt` /
> `supportsInputImages`，比本文档的静态表新。
>
> **但要分清两件事**（实测确认）：目录里的 `allowedDurations` 是**平台推荐/前端选择器的候选值**，
> **不是硬校验**——给 `SEEDANCE_2_0_MINI` 传目录里没有的 `durationSeconds=7` 依然会被接受。
> 真正会拒你的是 DTO 约束（通用 1~30、gesture-replica 4~15 等，见下）。
> 所以：**按目录挑值最稳妥**，但别把目录当校验规则去预判报错。

## modelCode 全表

**先跑 `qianjue catalog video-models`，用它返回的清单，不要用下表。**
下表只帮你认名字，**已知会过时**：模型上下架由后台控制，本文档不会跟着改。
本表与 catalog 冲突时 **无条件信 catalog** —— 下表列出而 catalog 没返回的模型，
在当前环境就是不可用，提交必失败。

| modelCode | 用途 |
| --- | --- |
| `SEEDANCE_2_0` / `SEEDANCE_2_5` / `SEEDANCE_2_0_FAST` / `SEEDANCE_2_0_MINI` | 图生视频主力 |
| `kling-v3-omni` | 可灵（走中台） |
| `MINIMAX_H3` | MiniMax |
| `GEMINI_OMNI_FLASH` | Gemini（**常被下架，必须先查 catalog**） |
| `GROK_IMAGINE_1_5` | Grok（**常被下架，必须先查 catalog**） |
| `VOLCANO_SUBTITLE_ERASE` | 字幕擦除 |
| `VOLCANO_TRANSLATE` | 视频翻译 |
| `VOLCANO_AD_AUDIT` | 广告审核 |
| `VECTCUT_KOUBO_TEMPLATE` | VectCut 口播模板剪辑 |
| `VECTCUT_SMART_MIX` | VectCut 智能混剪（**不在 `video create` 的模型目录里**，走 `qianjue video-studio smart-mix submit`，见 [video-studio.md](video-studio.md)）|

**比例默认值别照抄任何静态说明**：从 catalog 的 `defaultAspectRatio` /
`allowedAspectRatios` 取。当前实际情况是除 `VECTCUT_KOUBO_TEMPLATE` 是 `9:16` 外，
其余模型 `defaultAspectRatio` 都是 `16:9` —— 用户要竖屏就显式传 `9:16`，
别以为默认就是竖的。

**`requiresPrompt` 按模型不同**：catalog 里该字段为 `true` 的模型（Seedance 全系 /
可灵 / MiniMax）`prompt` 必填；火山系与 VectCut 为 `false`。别一律当必填或一律当选填。

## items[] 字段

「必填」一列写的是 **DTO 层是否必填**；标「按模型」的要去 catalog 查该模型的字段
（`requiresPrompt` / `allowedDurations`），不要一律当必填或一律当选填。

| 字段 | 必填 | 约束 |
| --- | --- | --- |
| `inputImageUrl` / `inputImageOosKey` | 图生视频必填其一 | ≤1024 / ≤512，图生视频主图 |
| `inputImages[]` | 否 | ≤20，素材列表。**Seedance 系与 `kling-v3-omni` 由 CLI 自动映射**：首张 → 主图，其余 → `seedanceConfig.referenceImages` / `omniConfig.imageList`，见下方「多图输入」 |
| `referenceVideoUrl` / `referenceVideoOosKey` | 火山系 / 复刻类必填其一 | 参考视频 / 待处理源视频 |
| `durationSeconds` | **按模型** | DTO 放到 **1~30**，但**各模型可选值不同，必须取 catalog 的 `allowedDurations`**。已知差异：`SEEDANCE_2_5` 实际 **4~30**（不是 1~30）、`MINIMAX_H3` 4~15、Seedance 其余三档只有 5/10/15、火山系 1/5/10/15/20 |
| `aspectRatio` | 否（有默认） | ≤16 字符。**默认值取 catalog 的 `defaultAspectRatio`**，当前除 VectCut 口播是 `9:16` 外其余都是 `16:9`；要竖屏必须显式传。⚠️ **`SEEDANCE_2_5` 会忽略它**，见下方警告 |
| `quality` | 否 | ≤16，如 `480p` / `720p`。省钱就取该模型最低档 |
| `prompt` | **按模型** | ≤5000。catalog 里 `requiresPrompt=true` 的模型必填（Seedance 全系 / 可灵 / MiniMax），火山系与 VectCut 为 `false` |
| `negativePrompt` | 否 | ≤2000 |
| `extendFromTaskId` | 否 | ≤128，续写 |
| `seedanceConfig` | 否 | Seedance 专用，如 `{"resolution":"480p"}`。内含 `generateAudio`（默认 `true`）—— **它只是允许模型出声，不保证有口播，见 SKILL.md 铁律 10** |
| `omniConfig` | 否 | 可灵专用。多图时其余参考图由 CLI 写入 `imageList`，见下方「多图输入」 |
| `volcanoConfig` | 火山系必填 | 火山专用，见下节；字幕擦除必须带 `eraseType` |

顶层 `sourceType` **必填**（见上面的顶层字段表），不传后端无法判断这是哪条链路。

### 多图输入（Seedance / 可灵 Omni）：CLI 会自动映射，别只在 `inputImages` 里堆图

后端真正下发给供应商的只有两处：`items[].inputImageUrl`（主图，1 张）和**各模型自己的参考图字段**
（其余参考图）。**`inputImages[]` 只用于详情页展示、审核与主体识别**——只往它里面堆图，
供应商那边就只有主图一张。Web 端一直是按下面这样拆的，CLI 现在也一致：

| 模型 | 其余参考图去哪 |
| --- | --- |
| Seedance 系 | `seedanceConfig.referenceImages[]` |
| `kling-v3-omni` | `omniConfig.imageList[]` |

两种模型的主图映射相同，其余参考图都来自 `inputImages[1..]`：

| 你写在请求里的 | CLI 提交给平台的 |
| --- | --- |
| `inputImages[0]` | `inputImageUrl` / `inputImageOosKey`（**仅当你两个都没写时**才回填） |
| `inputImages[1..]` | 上表对应字段（**仅当它原本为空时**才填） |

规则细节：

- **只对 `video create` 生效**，且只对多图模型生效：Seedance 系（`SEEDANCE` / `SEEDANCE_2_0` /
  `SEEDANCE_2_5` / `SEEDANCE_2_0_FAST` / `SEEDANCE_2_0_MINI`）与 `kling-v3-omni`。
  其余模型只取首张，请直接写 `inputImageUrl`，CLI 不会替你造参考图字段。
- **`edit` / `upscale` / `gesture-replica` 一律不动** —— 那几个端点是后端自己从 `inputImages`
  拼参考图的，塞 `seedanceConfig` / `omniConfig` 反而会坏。
- **已有值不覆盖**：显式写了 `inputImageUrl` 或目标参考图字段就按你写的走；同一请求重复执行也不会叠加。
- `kling-v3-omni` 的 `omniConfig.imageList` 条目按 Web 端形状写 `{"imageOosKey","imageUrl"}`；
  只给 `imageOosKey` 也行，后端会先换成 URL 再提交。
- ⚠️ `kling-v3-omni` 的 `omniConfig.imageList` 后端硬上限 **9 张**（DTO `@Size(max=9)`，且入口是
  `@Valid`）。主图不计入，所以 `inputImages` 最多能放 **10** 张；11 张起会直接被后端 400 拒掉。
  CLI 不做截断（与 Web 一致），超了就是超了。
- 命中映射时 CLI 会把结果说明写到 **stderr**（说明里带具体字段名；`--quiet` 时不写），
  stdout 的 JSON 不受影响。
- 想完全自己控制，就显式写目标参考图字段；要「只提交一张图」也照此显式写。
- 幂等重放不受影响：升级前提交过的 key 仍按日志里的原文重放（CLI 会在 stderr 说明）。

**排查「多张素材只提交了一张」**：先看后台供应商日志里的 `媒体数据` 条数 —— 那里显示的就是真正
下发给供应商的图。若只有 1 张，按模型检查请求里 `seedanceConfig.referenceImages` /
`omniConfig.imageList` 是否为空，以及 CLI 是否是旧版本（`qianjue version --check`）。

> ⚠️ **`SEEDANCE_2_5` + 只给一张首帧图时，你传的 `aspectRatio` 会被忽略** —— 上游按
> `adaptive` 处理，**成片跟随输入图的比例**。实测：传 `aspectRatio=9:16` + 一张 1:1 方图，
> 出来的是 `640×640` 方视频，而任务详情里 `aspectRatio` 仍显示 `9:16`。
>
> **所以只看任务 JSON 会误判。** 想要竖屏成片，三选一：
> ① 输入图本身就是竖图（最可靠，比例跟着图走）；
> ② 换用别的模型（Seedance 2.0 系 / 可灵 / MiniMax 不走这条 adaptive 规则）；
> ③ 不给输入图做纯文生视频。
>
> 同理，`SEEDANCE_2_5` 带参考视频做 `edit` / `extend` 时比例与时长都跟随参考视频。
> **回报给用户前请以实际文件为准**（`ffprobe` 或看首帧），别照抄请求参数说"已按竖屏生成"。

**所有 URL 必须公网可达** —— 本地文件先 `qianjue asset upload`。

## 场景 → 参数组合

| 线上功能 | sourceType | modelCode | 关键字段 |
| --- | --- | --- | --- |
| 视频生成 | `VIDEO_TASK` | Seedance / Kling / MiniMax（Gemini / Grok 先查 catalog 在不在） | `inputImageUrl` + `prompt` + `durationSeconds` |
| 口播视频生成 | `VIDEO_REVERSE_PROMPT` | 同上 | 同上（脚本先在 Web 端解析产出） |
| 营销视频生成 | `TVC_ADS` | 同上 | 商品图 + 分镜 prompt |
| 模特 / 商品替换 | `GESTURE_DANCE_REPLICA` | 同上 | 也可用专用端点 `video gesture-replica` |
| 字幕擦除 | `VIDEO_TASK` | `VOLCANO_SUBTITLE_ERASE` | `referenceVideoUrl` + `volcanoConfig` |
| 视频翻译 | `VIDEO_TASK` | `VOLCANO_TRANSLATE` | `referenceVideoUrl` + `volcanoConfig` |

## volcanoConfig（字幕擦除 / 视频翻译）

| 字段 | 说明 |
| --- | --- |
| `skillType` | `Erase`（擦除）/ `AITranslation`（翻译）/ `AdAudit` |
| `targetLanguage` | **翻译必填**，如 `en` |
| `sourceLanguage` | 可空，火山自动识别 |
| `translationTypeList` | `SubtitleTranslation` / `VoiceTranslation` / `FacialTranslation`，**字幕翻译必选** |
| `subtitleSource` | `OCR` / `ASR` |
| `hardSubtitle` | 是否把字幕烧进画面 |
| `eraseSourceSubtitle` | 是否擦掉原字幕 |
| `subtitleFontSize` | 硬字幕字号，像素 [1,80] |
| `subtitleMarginL/R/V` | 硬字幕边距比例 [0,1)，**开硬字幕时必填** |
| `eraseMode` | **`Auto` / `Manual`**（首字母大写）。`Auto` 自动找字幕，`Manual` 按你给的框擦 |
| `eraseType` | **`Subtitle` / `Text`**（首字母大写）。只在 `eraseMode=Auto` 下有意义：`Subtitle` 只擦字幕，`Text` 擦画面里所有文字 |
| `eraseLocations[]` | 擦除区域，`eraseMode=Manual` 时用；坐标是**相对视频宽高的 0~1 比例**，不是像素 |
| `clipFilterMode` | **`Selected` / `Skip`**（首字母大写），配 `clipFilterClips[]` 指定只处理 / 跳过哪些时间段（单位秒） |

> **这些值大小写敏感**，全是首字母大写的驼峰：`Subtitle` 不是 `subtitle`、`Auto` 不是 `auto`。
> 传小写会被上游拒。`skillType` 同理：`Erase` / `AITranslation` / `AdAudit`。

**素材要匹配任务**：字幕擦除要给**画面里真有硬字幕**的视频；翻译要给**真有语音或字幕**的视频。
给一段无字幕无人声的片子，参数再对也会失败。

## 各模型的取值限制

| modelCode | durationSeconds | aspectRatio | 备注 |
| --- | --- | --- | --- |
| `VOLCANO_*` | 1/5/10/15/20 | **默认 `16:9`**（不是 9:16，以 catalog 为准） | 只发 `referenceVideo*` + `volcanoConfig`，**不发输入图** |
| `GEMINI_OMNI_FLASH` | 3~10（默认 10） | 仅 `16:9` / `9:16` | **参考视频与 `extendFromTaskId` 互斥** |
| Seedance 系 | **以 catalog `allowedDurations` 为准**（2.5 是 4~30，其余三档只有 5/10/15） | 较自由 | 配 `seedanceConfig.resolution` |

## 其它视频端点

```bash
qianjue video upscale          --request x.json   # {inputVideoUrl} 或 {inputVideoOosKey}
qianjue video edit             --request x.json   # {config:{clips[],backgroundAudio,textTracks[],aspectRatio,canvas,exportOptions}}，config 必填
qianjue video gesture-replica  --request x.json   # 模特 / 商品替换，字段见下
```

### `video gesture-replica`（模特 / 商品替换）完整字段

**注意它跟 `video create` 的取值范围不一样** —— `durationSeconds` 这里是 **4~15**（`SEEDANCE_2_5` 可到 30），不是 1~30：

| 字段 | 约束 |
| --- | --- |
| `inputImageUrl` / `inputImageOosKey` | 主体图，≤1024 / ≤512 |
| `inputImages[]` | ≤20 |
| `referenceVideoUrl` / `referenceVideoOosKey` | 参考视频 |
| `referenceVideoSourceUrl` | ≤2048，短视频原始链接（抖音等） |
| `extraPrompt` | ≤500 |
| `replicaMode` | `NON_SPEECH`（默认）/ `SPEECH` |
| `scene` | `MODEL_PRODUCT_REPLACEMENT` / `DANCE_REPLICA` |
| `inputSubjectType` | `MODEL` / `PRODUCT` / `FACE` / `BACKGROUND` / `CLOTHING` |
| `inputSubjectDescription` | **≤10 个字符**（很短，别写整句） |
| `modelCode` | 只收 Seedance 四个：`SEEDANCE_2_0` / `SEEDANCE_2_5` / `SEEDANCE_2_0_FAST` / `SEEDANCE_2_0_MINI` |
| `durationSeconds` | **4~15**；仅 `modelCode=SEEDANCE_2_5` 时上限放宽到 **30** |
| `aspectRatio` | 默认 `9:16` |
| `seedanceResolution` | `480p` / `720p`，留空用环境默认 |
| `runningHubEnhanceEnabled` | 默认 false |

## 示例：字幕擦除

```json
{
  "sourceType": "VIDEO_TASK",
  "modelCode": "VOLCANO_SUBTITLE_ERASE",
  "items": [{
    "referenceVideoUrl": "https://…/source.mp4",
    "durationSeconds": 5,
    "aspectRatio": "9:16",
    "volcanoConfig": { "skillType": "Erase", "eraseMode": "Auto", "eraseType": "Subtitle" }
  }]
}
```

## 示例：视频翻译成英文

```json
{
  "sourceType": "VIDEO_TASK",
  "modelCode": "VOLCANO_TRANSLATE",
  "items": [{
    "referenceVideoUrl": "https://…/source.mp4",
    "durationSeconds": 5,
    "aspectRatio": "9:16",
    "volcanoConfig": {
      "skillType": "AITranslation",
      "targetLanguage": "en",
      "translationTypeList": ["SubtitleTranslation"],
      "subtitleSource": "OCR"
    }
  }]
}
```

创建返回批次 `{batchId, totalCount, taskIds[], status}` —— **后续查询用 `taskIds[0]`，不是 batchId**。

---

# 口播视频生成（两步）

线上「口播视频生成」= **解析一条视频拿到分镜与口播脚本 → 再拿脚本出片**。CLI 两步对应两条命令。

## 第一步：解析

```bash
qianjue reverse-prompt create --video-url 'https://…/源视频.mp4' --output json
# → {"result":{"result":{"sessionId":2090045238100922369,"status":"RUNNING", ...}}}

qianjue reverse-prompt get <sessionId> --output json   # 轮询直到终态
# → {"sessionId":…,"status":"COMPLETED","rawResult":"…","structuredContent":"[…]","thinkingContent":"…"}
```

- **异步**：创建立刻返回 `sessionId`，脚本要等解析完才有，用 `get` 轮询。
- **真实状态值**：创建后是 `RUNNING`，成功终态是 **`COMPLETED`**（不是 `SUCCESS`）。实测一条 15 秒视频约 30~45 秒出结果。
- `structuredContent` 是一段 **JSON 字符串**，解析后是数组，每项 `{"content": "镜号N｜景别｜起止秒\n画面：…\n台词：…"}`；
  `rawResult` 是同样内容的纯文本版。要喂给下游出片，从 `structuredContent` 里取单个镜号的 `content` 当 prompt。
- 需要参考图 / 音频 / 增强描述等完整参数时改用 `--request x.json`，字段见
  `CreateVideoReversePromptRequestDTO`：`videoUrl`(必填,≤1024)、`mediaType`、`videoSourceType`、
  `fileName`(≤255)、`enhancementEnabled`、`referenceImages[]`、`referenceDescription`(≤2000)、
  `audio`、`deepThinkingEnabled`。
- 会话详情字段：`status` / `rawResult`（原始文本）/ `structuredContent`（结构化脚本）/ `thinkingContent`。

## 第二步：出片

拿到脚本后按普通视频提交，**`sourceType` 用 `VIDEO_REVERSE_PROMPT`**：

```json
{
  "sourceType": "VIDEO_REVERSE_PROMPT",
  "modelCode": "SEEDANCE_2_0_MINI",
  "items": [{
    "inputImageUrl": "https://…/首帧.png",
    "prompt": "<把解析出来的脚本填这里>",
    "durationSeconds": 5,
    "seedanceConfig": { "resolution": "480p" }
  }]
}
```

之后照常 `qianjue task wait video <taskId>`。

**结果未知时**：不要换新 Key 重试，用同一个 `--idempotency-key` 重放即可安全恢复。

---

# 爆款视频策划（一次成稿）

上传商品图 + 说清平台/人群/卖点，**一次调用拿到完整脚本**，再决定要不要拿去出片。

```bash
qianjue viral-plan create --request plan.json --output json
```

```json
{
  "businessType": "VIRAL_PLAN",
  "initialQuery": "夏季男士POLO衫套装，抖音，25-35岁男性，主打透气和显瘦",
  "files": [
    { "type": "image", "url": "https://…/商品图1.png" },
    { "type": "image", "url": "https://…/商品图2.png" }
  ]
}
```

- **`files` 必填且至少 1 张**（DTO 只有 `@NotNull`，张数上限没有真正的校验；按 1~9 张用即可），
  先 `qianjue asset upload` 换成公网 URL
- `initialQuery` ≤2000 字：说清平台、人群、卖点，**信息越全脚本越准**
- **`answer` 是一段 JSON 字符串，要先 `json.loads` 再用**，解析后是
  `{"scripts":[{"title":"…","content":"…"}]}`。`content` 是整篇 Markdown 分镜稿。
  **`scripts` 可能只有 1 条**，别假设一定是多条。
- `chargedCredits` 是本次实际扣费，`conversationId` 可回 Web 端继续追问，
  另有 `supportNo` / `pricingRuleSetCode` / `pricingRuleVersion`
- 实测一轮约 25 秒返回

**信息不足时**后端会在 `answer` 里说明还缺什么 —— 补全后**重新提交**（用新 Key），
不要在命令行里模拟多轮对话；Web 端才有「AI 追问 + 从多条脚本里挑」的完整体验。

**一轮就要扣积分**（实测 `chargedCredits=2`；Web 页面上的标价与这里的实扣可能不同，
**以返回的 `chargedCredits` 为准**，别照页面数字向用户报数）。结果未知时**绝不要换新 Key 重试**，
用同一个 `--idempotency-key` 重放会回放历史结果，不会二次扣费。

拿到脚本后出片：把脚本填进 `prompt`，`sourceType` 用 `VIDEO_REVERSE_PROMPT` 或 `VIDEO_TASK`。
