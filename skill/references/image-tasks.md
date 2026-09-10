# 图片任务参数速查

全部图片功能都走同一个端点：`qianjue image create --request <json>`，区别只在 `type` 与 `inputImages`。

## 通用字段

| 字段 | 约束 | 说明 |
| --- | --- | --- |
| `type` | 必填 | 见下表，决定任务种类 |
| `prompt` | ≤200 字符 | `TXT2IMG` 必填；编辑类用它描述改什么 |
| `aspectRatio` | `auto` `1:1` `9:16` `16:9` `4:3` `3:4` `21:9` `9:21` `3:2` `2:3` `4:5` `5:4` | 默认 `auto` |
| `outputResolution` | `1k` `2k` `4k` | 默认 `2k` |
| `outputCount` | 见下 | 各任务上限不同，默认 1 |
| `inputImages[]` | ≤100 项 | 见下方字段表 |
| `modelCode` | 见下 | **只有 4 类任务生效** |
| `saveToMaterial` | 对象 | 生成后自动存素材库 |

**不要传 `subjectType` / `subjectId`** —— 后端按登录态自动判定归属，传了可能越权失败。

### outputCount 每类上限（实测）

DTO 上那条 `@Max` 只是宽松外壳，真正生效的是各任务自己的上限：

| 任务 | 上限 |
| --- | --- |
| `TXT2IMG` / `IMG2IMG` | **5** |
| `VIRTUAL_TRY_ON_PRO` | 5 |
| `VIRTUAL_TRY_ON` / `MODEL_THREE_VIEW` | 4 |
| 其余任务 | 只支持 1（传别的值直接报错） |

超限报错形如「文生图输出张数最多支持 5 张」。

## modelCode 只对 4 类任务生效

生效范围：`VIRTUAL_TRY_ON`、`VIRTUAL_TRY_ON_PRO`、`DETAIL_ENHANCEMENT`、`MODEL_THREE_VIEW`。
**其余任务（含 `TXT2IMG` / `IMG2IMG`）传了会被后端丢弃**，模型由平台策略决定 —— 别向用户承诺"用某模型生成"。

| modelCode | 用户可见名 |
| --- | --- |
| `NANO_BANANA_2` | 千谲快绘（默认） |
| `NANO_BANANA_PRO` | 千谲质感 Pro |
| `GPT_IMAGE_2` | 千谲灵感 Max |
| `SEEDREAM_5_0_PRO` | 千谲网感 Pro |

可选值以 `qianjue catalog models` 实际返回为准（含每个模型允许的比例与分辨率）。

## 全部 type 与所需输入图

| 功能 | `type` | `inputImages[].type` |
| --- | --- | --- |
| 文生图 | `TXT2IMG` | 不需要 |
| 图生图 | `IMG2IMG` | `image` |
| 换装 | `VIRTUAL_TRY_ON` | `model` + `inner_top`/`outer_top`/`pants`/`whole_body`/`hat`/`shoes`/`bag`/`jewelry`/`accessory_collage`/`background` |
| 精细化换装 | `VIRTUAL_TRY_ON_PRO` | **不走 `inputImages`**，用专属字段，见下节 |
| 姿势参考 | `POSE_TRANSFER` / `POSE_TRANSFER_PRO` | `model` + `pose_reference` |
| 智能重绘 | `IMAGE_CLEANUP` | `image`（**不能传 prompt**） |
| 高清放大 | `UPSCALE`（批量就多传几张） | `image` |
| 图片抠图 | `BACKGROUND_REMOVAL` | `image` |
| 换背景 | `BACKGROUND_REPLACEMENT` | `model`/`image` + `background`/`scene_reference`（**恰好 2 张、不能传 prompt**） |
| 场景提取 | `SCENE_IMAGE_EXTRACTION` | `image` |
| 印花提取 | `PRINT_EXTRACTION` | `image` |
| 模特换脸 | `MODEL_FACE_REPLACEMENT` | `model`/`image` + `face` |
| 智能换头 / 换脸 | `SMART_FACE_RANDOM_HEAD` / `SMART_FACE_RANDOM_FACE` | `model`/`image` |
| 脸部增强 | `FACE_ENHANCEMENT` | `image` |
| 细节增强 | `DETAIL_ENHANCEMENT` | 单张模式：`model` + `mask` + **必填 prompt**；批量模式见下 |
| 模特三视图 | `MODEL_THREE_VIEW` | `model` + `garment_front` + `garment_back` |
| 编辑元素 | `IMAGE_LAYER_SPLIT` | `image` |
| 加噪点 | `IMAGE_GRAIN` | `image` |
| 追色 | `IMAGE_COLOR_RESTORE` | **恰好 `target` + `reference` 各 1 张、不能传 prompt** |
| 换模特和背景 | `MODEL_BACKGROUND_REPLACEMENT` | `model`/`image` |
| 画布类 | `CANVAS_ERASER` / `CANVAS_EXPAND` / `CANVAS_MULTI_ANGLE` / `CANVAS_MOVE_OBJECT` | 各有专属字段，先按最小集提交看报错 |

**当前不可用**：
- ~~对话生图只读不能建~~ **已支持创建**：用独立命令 `qianjue image-chat create`（不是 `image create` 的一个 type）。
  它走另一套内核，请求字段也不同：`images[]` 用 `alias` + `oosUrl`，不是 `inputImages[].url`。
  查询与等待走 `qianjue task get/wait image-chat <taskId>`。
- `POSE_DUPLICATION`（姿势裂变）**后端未实现，已从上表移除**：参数校验会通过、任务也能建出来，
  但提交到 provider 时抛「姿势裂变任务暂未实现」，任务 FAILED、积分退回。
  **不要提交这个 type，也不要向用户承诺这个功能**；用户问起就说该能力尚未开放。

> 详情图生成（`DETAIL_IMAGE_CHAT`）**已经开放**，走独立端点 `qianjue detail-image create`，见本文末节。

## 几类任务的额外硬约束（都是实测撞出来的）

这些不写在 DTO 上，是各自 strategy 里的业务校验，**按最小集提交会被拒**：

| type | 约束 |
| --- | --- |
| `IMAGE_CLEANUP` | 只收 `image` 类型；**传 prompt 直接报错**「洗图任务暂不支持自定义 prompt」；outputCount 只能 1 |
| `BACKGROUND_REPLACEMENT` | **必须恰好 2 张**（主体 `model`/`image` + 背景 `background`/`scene_reference`）；**传 prompt 直接报错**；outputCount 只能 1 |
| `IMAGE_COLOR_RESTORE` | **必须恰好 2 张，且类型必须是 `target` + `reference` 各 1 张**（不是 `image`）；不能传 prompt；outputCount 只能 1 |
| `DETAIL_ENHANCEMENT` | 默认走**单张模式**：必须 `model` + `mask` 两类，**prompt 必填**，且**不许出现** `face`/`hand`/`top`/`pants`/`whole_body` 参考图。要用参考图得走批量模式（`batchMode=true`，`model` 1~40 张） |
| `MODEL_THREE_VIEW` | 需要平台侧配好 `DRAW_MODEL_THREE_VIEW_CN` 计费规则，否则报「计费规则未配置」 |

## inputImages[] 单项字段

| 字段 | 约束 |
| --- | --- |
| `type` | 必填，见上表 |
| `url` | 必填，≤2048，**必须公网可达**（本地文件先 `qianjue asset upload`） |
| `oosKey` / `oosUrl` | 可选 |
| `imageWidth` / `imageHeight` | 可选；`aspectRatio=auto` 时用来按原图换算 |
| `length` | 服装长度，格式 `short-200` / `medium-350` / `long-500`（仅换装类） |
| `placementNote` | ≤200，佩戴方式描述（仅 `hat`/`shoes`/`bag`/`jewelry`） |
| `garmentAttributes` | 对象（仅 `inner_top`/`outer_top`/`pants`/`whole_body`） |

## 示例：换装

```json
{
  "type": "VIRTUAL_TRY_ON",
  "modelCode": "NANO_BANANA_PRO",
  "aspectRatio": "3:4",
  "outputResolution": "2k",
  "inputImages": [
    { "type": "model", "url": "https://…/model.png" },
    { "type": "inner_top", "url": "https://…/tshirt.png", "length": "short-200" }
  ]
}
```

## 示例：精细化换装 `VIRTUAL_TRY_ON_PRO`（**结构和别的 type 完全不同**）

它**不用 `inputImages[]`**，而是一组专属顶层字段；`garmentCrop` 的 8 个子字段全部必填，
且当前被后端**写死**成 9:16 / 2752x4108（传别的值会依次报「仅支持 9:16 服装裁剪」「裁剪尺寸固定为 2752x4108」）：

```json
{
  "type": "VIRTUAL_TRY_ON_PRO",
  "modelImage":   { "url": "https://…/model.png" },
  "garmentImage": { "url": "https://…/garment.png" },
  "garmentCrop": {
    "ratio": "9:16", "size": "2752x4108",
    "cropWidth": 2752, "cropHeight": 4108,
    "offsetX": 0, "offsetY": 0, "scale": 1.0, "rotation": 0
  },
  "shotProfile": "FRONT_FULL"
}
```

可选：`accessoryCollageImage` / `sceneImage` / `poseReferenceImage`（同为 `{url|oosKey|oosUrl}` 对象）、
`poseMaterialId`、`promptAppend`（≤2000）。
`shotProfile` 取值：`FRONT_HALF` `BACK_HALF` `SIDE_HALF` `FRONT_FULL` `BACK_FULL` `SIDE_FULL`。

不确定某类型还需要哪些字段时：**先按最小集提交**，后端参数校验的报错会写明缺什么，按提示补 —— 不要凭猜测编字段名。

---

# 详情图生成（独立端点）

一次提交产出**一整套**图（主图 / 卖点图 / 套餐图），走自己的端点，不是 `image create`。

```bash
qianjue detail-image create --request req.json --output json
qianjue detail-image list --limit 20 --output json   # 轮询：最新在前，每张图的状态与地址在 items 里
```

## 请求字段

| 字段 | 约束 |
| --- | --- |
| `requestId` | **必填**，≤128，业务侧幂等键（与 CLI 的 Idempotency-Key 是两层） |
| `productImageUrls` | **1~3 张**产品图，公网地址 |
| `referenceImageUrls` | 1~10 张参考图，可选 |
| `generationMode` | `MAIN_IMAGE`（主图，出 1 张）/ `AUTO_PACKAGE`（自动套餐）/ `REFERENCE_STYLE`（照参考图版式，每张参考图出 1 张） |
| `mainImageCount` | 主图张数（DTO 上**没有**数值上限校验，别当成硬约束） |
| `detailImageCount` | 详情图张数（同上，无数值上限校验） |
| `mainImageAspectRatios` | 主图比例数组，**最多 7 个** |
| `detailImageAspectRatios` | 详情图比例数组，**最多 12 个** |
| `outline` | 套餐大纲，最多 19 条 |
| `requirement` | 需求描述，≤1000 |
| `language` | 语言，如 `zh-CN` |
| `platformCode` | 平台，如 `TAOBAO` |
| `productInfo` | 商品信息对象，可让平台自动识别 |
| `packageSize` | 套餐规格 |
| `aspectRatio` / `outputResolution` | 可选，整体比例与分辨率 |

> 「上限 7 / 12」是**比例数组**的长度上限，不是张数上限 —— 别把它当成 `mainImageCount` 的校验。

## 读取方式与别处不同

详情图**没有单个 taskId**：一次创建返回 `messageId` + `items[]`，每个 item 自带
`itemId` / `status` / `resultImageUrl` / `errorMessage`。所以轮询用 `detail-image list`，
**不要**用 `task get`。

（`task get image-chat <taskId>` 能查到底层的 ImageChat 任务，但拿不到这套分项状态。）

## 结果未知时

**不要换新 Key 重试**。服务端创建是幂等的：**用同一个 `--idempotency-key` 和同一份请求体重发**，
会回放历史结果，不会生成第二套图。CLI 在传输失败时会把这条命令直接打给你。

## 示例

```json
{
  "requestId": "my-req-001",
  "generationMode": "AUTO_PACKAGE",
  "productImageUrls": ["https://…/product.png"],
  "referenceImageUrls": ["https://…/ref1.png"],
  "mainImageCount": 5,
  "detailImageCount": 5,
  "platformCode": "TAOBAO"
}
```
