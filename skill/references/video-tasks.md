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

| modelCode | 用途 |
| --- | --- |
| `SEEDANCE_2_0` / `SEEDANCE_2_5` / `SEEDANCE_2_0_FAST` / `SEEDANCE_2_0_MINI` | 图生视频主力 |
| `kling-v3-omni` | 可灵（走中台） |
| `GEMINI_OMNI_FLASH` | Gemini |
| `GROK_IMAGINE_1_5` | Grok |
| `VOLCANO_SUBTITLE_ERASE` | 字幕擦除 |
| `VOLCANO_TRANSLATE` | 视频翻译 |
| `VOLCANO_AD_AUDIT` | 广告审核 |
| `VECTCUT_KOUBO_TEMPLATE` / `VECTCUT_SMART_MIX` | VectCut 模板剪辑 / 智能混剪 |

## items[] 字段

| 字段 | 约束 |
| --- | --- |
| `inputImageUrl` / `inputImageOosKey` | ≤1024 / ≤512，图生视频主图 |
| `inputImages[]` | ≤20，多图展示列表 |
| `referenceVideoUrl` / `referenceVideoOosKey` | 参考视频 / 待处理源视频 |
| `durationSeconds` | **1~30**（火山系前端夹到 1~20） |
| `aspectRatio` | ≤16 字符，如 `9:16` |
| `quality` | ≤16，如 `480p` / `720p` |
| `prompt` | **≤5000** |
| `negativePrompt` | ≤2000 |
| `extendFromTaskId` | ≤128，续写 |
| `seedanceConfig` | Seedance 专用，如 `{"resolution":"480p"}` |
| `omniConfig` | 可灵专用 |
| `volcanoConfig` | 火山专用，见下节 |

**所有 URL 必须公网可达** —— 本地文件先 `qianjue asset upload`。

## 场景 → 参数组合

| 线上功能 | sourceType | modelCode | 关键字段 |
| --- | --- | --- | --- |
| 视频生成 | `VIDEO_TASK` | Seedance / Kling / Gemini / Grok | `inputImageUrl` + `prompt` + `durationSeconds` |
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
| `eraseMode` / `eraseType` / `eraseLocations[]` | 擦除模式 / 类型 / 位置框 |

**素材要匹配任务**：字幕擦除要给**画面里真有硬字幕**的视频；翻译要给**真有语音或字幕**的视频。
给一段无字幕无人声的片子，参数再对也会失败。

## 各模型的取值限制

| modelCode | durationSeconds | aspectRatio | 备注 |
| --- | --- | --- | --- |
| `VOLCANO_*` | 1~20 | 默认 `9:16` | 只发 `referenceVideo*` + `volcanoConfig`，**不发输入图** |
| `GEMINI_OMNI_FLASH` | 3~10（默认 10） | 仅 `16:9` / `9:16` | **参考视频与 `extendFromTaskId` 互斥** |
| Seedance 系 | 1~30 | 较自由 | 配 `seedanceConfig.resolution` |

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
    "volcanoConfig": { "skillType": "Erase", "eraseType": "subtitle" }
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
