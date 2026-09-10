# 营销视频（TVC 高级广告）

对应线上「营销视频生成」。产品图一路到成片的**聚合任务**：一条任务串起分镜生成、
分镜绑定、视频绑定几个阶段，产物按阶段挂在任务上。

## 读取必须用它自己的接口

`UnifiedTaskDomain` 里没有 TVC 域，也没有统一查询 provider —— **`task get video <id>`
对 TVC 任务返回 NOT_FOUND**（实测确认）。轮询只能走：

```bash
qianjue tvc-ads get  <taskId>
qianjue tvc-ads wait <taskId> --wait-timeout 30m
```

## 创建

```bash
qianjue tvc-ads create --request req.json
```

```json
{
  "productImageOosKey": "third-party/images/2026/05/30/<uid>/<hash>.png",
  "productImageUrl": "https://…/<hash>.png",
  "extraRequirement": "可选：额外创意要求",
  "storyboardPrompt": "可选：分镜提示词",
  "videoPrompt": "可选：视频提示词"
}
```

只有 `productImageOosKey` 必填。注意它要的是**对象存储 key**，不是 URL ——
先用 `qianjue asset upload` 直传拿到 key。

同一 `--idempotency-key` 重放返回同一 taskId 且标注「未重复创建」，不会重复扣费。
