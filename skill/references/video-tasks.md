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
qianjue video upscale          --request x.json   # {inputVideoUrl}
qianjue video edit             --request x.json   # {config:{clips[],backgroundAudio,textTracks[],aspectRatio,canvas,exportOptions}}
qianjue video gesture-replica  --request x.json   # {inputImageUrl, referenceVideoUrl, referenceVideoSourceUrl, extraPrompt≤500, replicaMode:NON_SPEECH|SPEECH}
```

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
