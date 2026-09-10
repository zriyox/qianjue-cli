# 视频剪辑（口播成片 / 智能混剪）

对应线上「视频剪辑」。两条链路都**必须先查模板**：`templateId` 提交时必填、模板由后台配置且会变，
硬编码迟早失效。产出的是视频任务，用 `qianjue task get/wait video <taskId>` 轮询。

```bash
qianjue video-studio koubo templates          # 口播模板
qianjue video-studio smart-mix templates      # 混剪字幕模板
qianjue video-studio smart-mix voices         # 混剪音色（voiceCode 从这里取）
```

## 口播成片 `koubo submit`

```json
{
  "videoUrl": "https://…/source.mp4",
  "templateId": "xijing-lv-shuangyu",
  "title": "标题（必填，留空直接被拒）",
  "durationSeconds": 15,
  "kongjingUrls": ["https://…/broll1.jpg"],
  "textContent": "可选：覆盖识别出的文案"
}
```

`videoUrl` / `templateId` / `title` / `durationSeconds` 四个必填。

## 智能混剪 `smart-mix submit`

`inputMode` 决定哪些字段**必须有、哪些必须没有**，组合错了会报
「智能混剪输入模式与主输入、字幕模板或音色字段不匹配」。规则如下（来自
`SmartMixInputModeRules`，实测确认）：

| `inputMode` | textContents | sourceAudio | sourceVideo | targetDurationSeconds | templateId | voiceCode |
| --- | --- | --- | --- | --- | --- | --- |
| `TEXT` | ✅ 必须 | ❌ | ❌ | ❌ **不能传** | ✅ 必须 | ✅ 必须 |
| `AUDIO` | ❌ | ✅ 必须 | ❌ | ❌ | ✅ 必须 | ❌ 不能传 |
| `VIDEO` | ❌ | ❌ | ✅ 必须 | ❌ | ✅ 必须 | ❌ 不能传 |
| `BROLL_ONLY` | ❌ | ❌ | ❌ | ✅ 必须 | ❌ 不能传 | ❌ 不能传 |

其余约束（都是实测撞出来的）：

- `renderMode` 只能是 `STANDARD` 或 `IDLE`，传 `DRAFT` 会被拒
- `title` 必填
- `kongjingItems[]` 每项**必须带 `mediaType`**（`IMAGE` 或 `VIDEO`），只给 url 会报
  「空镜素材#1媒体类型仅支持 IMAGE 或 VIDEO」
- 草稿生成费与云渲染导出费分别冻结、各阶段独立结算

`TEXT` 模式最小可用请求：

```json
{
  "inputMode": "TEXT",
  "title": "标题",
  "renderMode": "STANDARD",
  "templateId": "smart-mix-gaoji-hei",
  "voiceCode": "fish-smart-mix-official-demo",
  "textContents": [{ "text": "要念的文案" }],
  "kongjingItems": [{ "mediaType": "IMAGE", "url": "https://…/a.png" }]
}
```
