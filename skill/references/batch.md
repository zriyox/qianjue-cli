# 批量提交图片任务

```bash
qianjue image batch --request items.json --idempotency-key my-batch-1
```

请求文件是 **JSON 数组**，每一项就是一次 `image create` 的请求体：

```json
[
  { "type": "UPSCALE", "inputImages": [{ "type": "image", "url": "https://…/a.png" }] },
  { "type": "BACKGROUND_REMOVAL", "inputImages": [{ "type": "image", "url": "https://…/b.png" }] }
]
```

## 整批可安全重放（关键）

每一项的 Idempotency-Key 由「批次 Key + 序号」推导，不是每项现取随机值。所以：

- 中途失败、超时、结果未知时，**重跑同一条命令**即可安全续上
- 已建成功的任务会原样返回（`historical: true`），**不会重复创建、不会重复扣费**
- **绝不要换新 `--idempotency-key` 重试** —— 那才会真的建出第二批

实测：第 2 项提交超时（结果未知），按提示重跑同一命令，两项都返回原 taskId 且
`historical=true`。

## 其他标志

| 标志 | 作用 |
| --- | --- |
| `--continue-on-error` | 单项失败后继续提交剩余项（默认遇错即停） |
| `--wait` | 提交后逐个等待终态；单个任务失败不中断整批 |
| `--wait-timeout` | 整批等待上限 |

传输类失败会**先打印已成功的项**再抛错，半截批次不用猜哪些已经建出来了。
