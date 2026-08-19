# 失败处理与退出码

## 退出码（稳定契约，必须判）

| 码 | 含义 | 该做什么 |
| --- | --- | --- |
| 0 | 成功 | 继续 |
| 2 | 参数/用法错误 | 按报错改命令；后端消息通常写明缺哪个字段 |
| 3 | 认证失败 | 重新登录 |
| 4 | 权限不足（缺 Scope） | 重新登录并申请对应 scope（上传需 `asset.write`） |
| 5 | 资源不存在 | 核对 taskId 与 domain |
| 6 | 幂等键冲突 | 同 Key 发了不同请求体；换新 Key 或改回原请求体 |
| 7 | 仍在进行 / 本地等待超时 | **任务还在服务端跑**，稍后 `task get` |
| 8 | 需要人工恢复 | **停止，上报用户**，不要再尝试任何创建 |
| 9 | 终态失败（含余额不足） | 上报，不重试 |
| 10 | 任务失败 | 读 message 上报 |
| 11 | 任务已取消 | 上报 |
| 12 | 网络/传输错误 | **创建类走 `resume`，不要重发** |
| 13 | 服务端错误 | 查询类可稍后重试；创建类走 `resume` |
| 14 | 本地凭证库不可用 | 修系统凭证库，**不要用明文绕过** |
| 130 | Ctrl-C | 仅停本地，服务端任务仍在跑 |

## 创建请求结果未知时（最重要）

网络中断、超时、连接重置 —— **绝不换新 Idempotency-Key 重试，也不要重跑 create**，那会重复创建、重复扣费。

```bash
qianjue image resume --idempotency-key <原来那个 Key>
qianjue video resume --idempotency-key <原来那个 Key>
```

Key 在失败输出的 `data.idempotencyKey` 里。找不到时用
`qianjue image request-status --idempotency-key <Key>` 查服务端的脱敏幂等状态。

**退出码 8（RECOVERY_REQUIRED）意味着服务端也不确定结果，只能人工介入 —— 必须停下并上报，任何"再试一次"都是错的。**

## 任务失败但不是你的错的情况

- **素材与任务不匹配**：字幕擦除给了无字幕视频、翻译给了无人声视频、图生视频给了非公网图
- **余额不足**：退出码 9，任务不会创建
- **供应商侧失败**：任务会 FAILED 且**积分自动退回**，message 里有说明

这些都如实转告用户，不要自作主张改参数重试 —— 生成要花用户的钱。

## 等待与轮询

```bash
qianjue task wait video <taskId> --wait-timeout 10m
```

- `Ctrl-C` 与等待超时**只停本地**，服务端任务照常跑完，不会退款
- 超时后用 `qianjue task get <domain> <taskId>` 继续查
- 结果媒体统一在 `data.media.resultMediaList[]`
