# 对话生图参数速查

`qianjue image-chat create --request <json>` —— 用提示词加参考图直接出图。

**它不是 `image create` 的一个 type**，走另一套内核，请求字段也不同：参考图是
`images[]`（`alias` + `oosUrl`），不是 `inputImages[].url`。查询与等待回到统一命令：
`qianjue task get image-chat <taskId>` / `qianjue task wait image-chat <taskId>`。

## 顶层字段

| 字段 | 约束 | 说明 |
| --- | --- | --- |
| `prompt` | **必填**，≤8000 字符 | 比 `image create` 的 200 字符宽得多 |
| `displayPrompt` | ≤8000 | 回显原文（含 `{{图片N}}` 占位符）；只做程序化提交可不传 |
| `modelCode` | 见下表 | 不传由平台选默认 |
| `aspectRatio` | `auto` `1:1` `16:9` `9:16` `4:3` `3:4` `3:2` `2:3` `4:5` `5:4` `21:9` | |
| `outputResolution` | `1k` `2k` `4k` | 受模型二次约束，见下表 |
| `outputCount` | 1~**5** | |
| `images[]` | 张数上限按模型，见下表 | 参考图 |

**不要传 `subjectType` / `subjectId`** —— 后端按登录态判定归属。

## modelCode 与它的二次约束

`aspectRatio` / `outputResolution` / 参考图张数三项，模型会在通用范围之上再收紧一次：

| modelCode | 用户可见名 | 参考图上限 | 输出分辨率 | 宽高比 |
| --- | --- | --- | --- | --- |
| `NANO_BANANA_2` | 千谲快绘 | 14 | 1k / 2k / 4k | 全部 |
| `NANO_BANANA_PRO` | 千谲质感 Pro | 14 | 1k / 2k / 4k | 全部 |
| `GPT_IMAGE_2` | 千谲灵感 Max | **8** | **2k / 4k** | 全部 |
| `SEEDREAM_5_0_PRO` | 千谲网感 Pro | 10 | **2k / 4k** | 不支持 `4:5` `5:4` |
| `SEEDREAM_4_5` | 千谲网感 4.5 | 14 | **2k / 4k** | 不支持 `4:5` `5:4` |

超限报错是明文的，如「千谲灵感 Max 最多支持上传 8 张参考图」「千谲网感 Pro 仅支持 2k、4k 输出分辨率」。

## images[] 字段

| 字段 | 约束 | 说明 |
| --- | --- | --- |
| `alias` | **必填**，非空 | 提示词里引用这张图时用的名字，如 `图1` |
| `oosUrl` | **必填**，公网可达 | 本地文件先 `qianjue asset upload` 拿 URL |
| `oosKey` | 可选 | 有就传，没有不影响 |
| `sort` | 1 起、不重复 | **CLI 通道可以整体省略**，见下 |
| `thumbnailUrl` | 可选 | 纯前端展示用 |
| `imageWidth` / `imageHeight` | 可选，≥1 | 只对按首图比例换算像素的供应商有意义 |

### sort 可以不传，但要么全不传要么全传

`sort` 在校验器里是硬要求（非空、≥1、不重复），网页端由前端逐张写死。
自动化调用方发的本来就是**有序数组**，再手写一遍序号是重复劳动，所以 Integration 通道
会在**整个数组都没传 `sort`** 时按下标补成 1、2、3……

**一旦有任意一张显式带了 `sort`，就不再自动补**：那说明调用方在自己排序，
替它猜剩下的会造出重复序号，报「图片顺序不能重复」，比如实报错更难查。
所以混着传是错误用法 —— 要么全省略，要么每张都给。

## 最小可用示例

```json
{
  "prompt": "参考图1的配色，画一只趴在窗台上的橘猫",
  "images": [
    { "alias": "图1", "oosUrl": "https://.../a.png" }
  ],
  "outputCount": 1
}
```

```bash
# 本地图先上传拿公网 URL
qianjue asset upload ./ref.png --output json    # stdout 是纯 JSON，进度走 stderr

qianjue image-chat create --request ./req.json --output json
qianjue task wait image-chat <taskId> --wait-timeout 15m --output json
```

## 排查

| 报错 | 原因 |
| --- | --- |
| `图片顺序必须大于等于 1` | 混着传了 `sort`（部分张有、部分张无），自动补被跳过 |
| `图片顺序不能重复` | 显式传的 `sort` 撞号了 |
| `图片别名不能为空` / `图片 OOS URL 不能为空` | `alias` / `oosUrl` 漏了 |
| `图像对话模型不受支持` | `modelCode` 不在上表 |
| `对话生图输出张数最多支持 5 张` | `outputCount` > 5 |

被内容审核拦住（退出码 15）时走 `qianjue moderation`，见 [identity.md](identity.md) 与主 SKILL.md 的审核一节。
