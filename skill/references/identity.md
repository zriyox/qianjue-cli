# 实名认证（退出码 16）

平台按后台策略要求实名后才能提交任务。被拦时提交接口返回业务码 **2016**，
CLI 映射为 `IDENTITY_REQUIRED` / 退出码 **16**。

## AI 助手必须知道的

实名要**填身份证号并刷脸**，是账号所有者本人的行为。

- ❌ 不要代填姓名 / 身份证号
- ❌ 不要尝试绕过、或改用其他账号提交
- ✅ 把 `qianjue identity verify` 这条命令原样交给用户自己执行

`identity verify` 在非交互式终端下会直接拒绝执行（和 `moderation confirm` 同一条闸）。

## 命令

```bash
qianjue identity status    # 是否已实名、当前是否被强制、提交次数/阈值
qianjue identity verify    # 交互式完成实名
```

`status` 返回示例：

```json
{ "verified": false, "verifyRequired": false, "forceEnabled": false,
  "usageCount": 12, "usageThreshold": 3 }
```

- `verified` 是否已实名
- `verifyRequired` **当前这次提交**是否会被拦（策略关时只有名单用户为真）
- `usageCount` / `usageThreshold` 提交次数与触发阈值

## verify 的流程

与网页端完全一致，只是换了终端：

1. 自动发送短信验证码到账号绑定手机号（1 分钟限 1 次）
2. 交互式询问姓名、身份证号、短信码 —— **建议不要用 `--id-card` 传参**，
   那会把证件号留在 shell 历史里；省略参数走交互输入即可
3. 返回腾讯刷脸页地址，**用手机微信「扫一扫」打开**完成人脸核身
   （在 PC 浏览器直接打开会调电脑摄像头，体验差）
4. 每 3 秒轮询结果，直到 `VERIFIED` / `FAILED` / `EXPIRED`

身份信息只随本次请求发往平台，CLI 不落盘、不写日志、不回显。

超时（默认 10 分钟）只停本地轮询，认证本身仍可在手机上继续完成，
之后用 `qianjue identity status` 查结果。
