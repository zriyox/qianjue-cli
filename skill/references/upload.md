# 上传素材到平台

**平台只认公网可达的 URL**，本地文件路径一律无效。所有带图的任务（换装、抠图、
放大、图生视频…）都要先把本地文件传上去拿到 URL。

```bash
qianjue asset upload ./a.png ./b.jpg        # 支持多文件，一次传完
```

返回每个文件的 `objectKey` 与 `url`：

```json
[{ "file": "./a.png",
   "objectKey": "third-party/images/2026/09/10/<uid>/<hash>.png",
   "url": "https://…cos…/third-party/images/2026/09/10/<uid>/<hash>.png" }]
```

## 两个字段各自用在哪

| 字段 | 用途 |
| --- | --- |
| `url` | 绝大多数任务的 `inputImages[].url`、视频的 `inputImageUrl` |
| `objectKey` | **营销视频 `productImageOosKey`**、**智能搭配师挂素材**要的是它，不是 url |

对话生图两个都要：`images[]` 用 `alias` + `oosUrl`（把 `url` 填进 `oosUrl`）。

## 实现要点

直传对象存储（先向平台换预签名地址，再 PUT 上去），**不经过平台服务器中转**，
所以大文件不会被 API 网关卡住。同一个文件重复上传是安全的。

## 完整闭环示例

```bash
URL=$(qianjue --output json asset upload ./product.png | jq -r '.data[0].url')
cat > req.json <<JSON
{ "type": "UPSCALE", "inputImages": [{ "type": "image", "url": "$URL" }] }
JSON
qianjue image create --request req.json --wait
```
