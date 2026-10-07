# Hookcheck

用可重复的场景，验证 Webhook 在重复、乱序、并发和超时情况下的业务结果。

[English](README.md)

HTTP 返回成功后，权益仍可能重复发放，订单状态也可能被旧事件覆盖。Hookcheck 先编排回调，再查询业务结果，给出能用于 CI 的退出码和诊断报告。

## 快速运行

需要 Go 1.25+，核心 Go 项目没有第三方依赖。第一版支持 macOS、Linux。

直接安装命令行工具：

```sh
go install github.com/ljn-forge/hookcheck/cmd/hookcheck@latest
```

运行完整示例时，先克隆 [GitHub 仓库](https://github.com/ljn-forge/hookcheck)：

```sh
git clone https://github.com/ljn-forge/hookcheck.git
cd hookcheck
```

```sh
make build
export HOOKCHECK_DEMO_SECRET=example-only-secret
./bin/payment-demo --mode broken
```

另开终端，进入项目目录：

```sh
export HOOKCHECK_DEMO_SECRET=example-only-secret
./bin/hookcheck run \
  --scenario examples/payment/scenario.json \
  --base-url http://127.0.0.1:8080 \
  --report broken-report.json
```

六次 HTTP 请求都成功，但业务检查失败：支付事件导致四次发放，旧事件把状态改回 pending，退出码为 1。

停止示例服务，改用 `--mode fixed` 启动后执行相同命令，退出码为 0：同一事件重复发送、同一订单出现不同支付事件，都只发放一次。示例只建模 pending / paid 两种状态，paid 是终态；`version` 表示已观察到的最高事件版本。

`make smoke` 使用编译后的 CLI 和独立示例进程，自动验证缺陷版本、修复版本和超时场景。只有这个测试脚本需要 Python 3。

## 第一版能力

- 严格、带版本的 JSON 场景：拒绝未知字段和多个 JSON 文档。
- 按步骤串行推进；每个步骤支持重复事件、并发上限和固定种子的打乱顺序。
- 每次 HTTP 请求都有超时，覆盖连接、响应头和响应正文。
- 显式重复才增加发送次数；POST 的 HTTP 传输层自动重放也被关闭。
- 查询接口验证业务结果，可设置有截止时间的 GET 轮询。
- 对实际发送的正文进行通用 HMAC-SHA256 签名，密钥从环境变量读取。
- 输出文本摘要和原子写入的 JSON 报告，取消时保留部分结果。
- 复杂 HTTP 断言可调用已有 Hurl，避免再造 JSONPath 和通用测试语言。

场景格式、默认值和示例参见 [英文配置说明](README.md#configuration-reference)。事件正文和 `fields` 预期值都是 JSON。`fields` 比较顶层指定字段的完整结构：对象键顺序无关、数组顺序有关，缺失和 null 不同，大整数不会转换为浮点数；数字 token 精确比较，所以 1 和 1.0 不相等。

固定种子复现的是发送计划。并发情况下，网络到达顺序、耗时和目标系统的状态仍然可能变化。需要固定乱序事件到达关系时，使用串行步骤。

## 超时后的不确定结果

使用 `./bin/payment-demo --mode fixed --ack-delay 200ms` 启动示例，再运行 `examples/payment/timeout.json`。服务先提交状态，再延迟响应：客户端 20ms 后超时，查询接口仍能观察到一次权益发放。

这次运行仍然失败，并在报告中保留 timeout：查询到正确状态不会改变回调响应超时这一事实。工具不会擅自补发。

## Hurl 集成

安装 [Hurl](https://github.com/Orange-OpenSource/hurl) 后，用 `examples/payment/scenario-hurl.json` 并添加 `--allow-hurl`。Hurl 文件路径相对场景文件解析，工具传入 `base_url` 变量，整体执行限时 30 秒。macOS/Linux 下取消会终止进程组。

Hurl 输出可能包含敏感正文，所以集成模式只返回元数据；详细错误可直接运行对应 `.hurl` 文件查看。Hurl 可以访问其他主机、配置自己的重试和输出文件，因此需要先检查文件再开启。原生场景的发送边界不约束 Hurl 内部请求。网络故障可组合使用 [Toxiproxy](https://github.com/Shopify/toxiproxy)。

## 退出码和报告

| 退出码 | 含义 |
| --- | --- |
| 0 | 请求和业务检查全部通过 |
| 1 | 请求、业务断言或 Hurl HTTP/断言失败 |
| 2 | 配置、依赖、外部进程或输出错误 |
| 130 | 执行取消或整体上下文截止 |

报告包含场景/事件/检查名称、计划编号、重复编号、HTTP 状态和时间信息，不记录请求正文、响应正文、URL、请求头及密钥。名称属于用户提供的元数据，也需要避免填写敏感内容。报告不能覆盖场景或 Hurl 输入文件。

场景是会主动发请求的测试输入，请对有权限的测试环境执行。复现需要同时保留场景、二进制版本、目标环境和种子。

## 验证与贡献

```sh
make check
make fuzz
make smoke
```

参见 [本地验证记录](docs/verification.md)、[设计边界](docs/superpowers/specs/2026-10-07-hookcheck-design.md) 和 [贡献指南](CONTRIBUTING.md)。项目附带 MIT 协议，远端结果可在 [GitHub Actions](https://github.com/ljn-forge/hookcheck/actions) 查看。
