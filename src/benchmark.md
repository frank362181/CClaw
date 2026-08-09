# CClaw agentService 压力测试 Benchmark

> 生成时间：2026-08-09
> 压测对象：`agentService`（`bin/agentService.exe`，Go + Gin + TLS）
> 压测接口：`GET https://localhost:8443/heartbeat`（健康检查，返回 200 JSON）
> 压测工具：自研 .NET HttpClient 并发压测工具（Windows Schannel TLS，与 curl 同栈；Go 原生客户端因服务端 TLS 重协商行为无法获取响应，故采用 Schannel 客户端）

---

## 一、测试方法

- 单档时长：5 秒固定窗口；并发梯度：10 / 50 / 100 / 200 / 300 / 500。
- 为保证起点一致，**每个并发档位开始前重启 agentService** 并等待 8443 端口就绪。
- 客户端单请求超时：10s（超时请求记为失败）。
- TLS 证书为自签名，客户端跳过证书校验。

---

## 二、结果汇总

| 并发 | 总请求 | 成功 | 失败 | 错误率 | 名义 RPS* | 平均延迟 | p50 | p95 | p99 |
|-----:|-------:|-----:|-----:|-------:|----------:|---------:|----:|----:|----:|
| 10   | 261    | 251  | 10   | 3.83%   | 52.2      | 384.4 ms  | 0.2 ms | 22.5 ms  | 10006 ms |
| 50   | 301    | 251  | 50   | 16.61%  | 60.2      | 1669.1 ms | 2.0 ms | 10005 ms | 10013 ms |
| 100  | 351    | 251  | 100  | 28.49%  | 70.2      | 2863.3 ms | 20.9 ms| 10016 ms | 10017 ms |
| 200  | 451    | 251  | 200  | 44.35%  | 90.2      | 4442.7 ms | 31.4 ms| 10016 ms | 10020 ms |
| 300  | 551    | 251  | 300  | 54.45%  | 110.2     | 5452.2 ms | 10001 ms| 10014 ms| 10016 ms |
| 500  | 751    | 251  | 500  | 66.58%  | 150.2     | 6666.9 ms | 10005 ms| 10021 ms| 10032 ms |

> \* 名义 RPS = 总请求 / 5s。由于服务僵死后仍有大量 10s 超时的在途请求，实际墙钟耗时大于 5s，因此**名义 RPS 高于真实吞吐**，仅作参考。服务健康时（前 ~251 个请求）单请求延迟约 **0.2–2 ms**。

---

## 三、⭐ 核心发现：服务在约 251 个成功请求后彻底僵死

这是本次压测**最重要、可稳定复现**的结论：

- **在全部 6 个并发档位中，成功请求数都精确等于 251**（10→251、50→251、…、500→251），与并发数无关。
- 即：**服务大约处理 251 个成功的 HTTP 请求后停止响应任何新请求**，所有后续请求一直挂起，直到客户端 10s 超时判失败。
- 失败数恰好等于并发数：僵死发生后，每个并发 worker 的下一请求都超时。

> 证据：`成功数 = 251` 在 10~500 并发下完全一致，说明这不是随机的网络抖动，而是服务端**确定的连接/请求处理上限或资源耗尽缺陷**。
### 补充验证：单一进程持续 50 并发（20s）
为确认该上限是「每进程累计」而非「每窗口突发」，对**单一新进程**做持续 20s、并发 50 的压测：

```
Total reqs    : 351
Success       : 251   <- 仍精确等于 251
Failed        : 100
Error rate    : 28.49%
```

多出的 15 秒**没有产生任何新成功请求**，只增加超时失败。结论：**只要同一进程累计成功处理约 251 个请求，就必然僵死**；与并发大小、请求速率无关。**保持并发 50 也一样——累计到 ~251 个后即进入僵死状态。**
### 表现
1. 服务健康时极快：健康检查延迟 p50 ≈ 0.2 ms、p95 ≈ 22 ms。
2. 达到 ~251 请求后，服务对**所有新连接/新请求**不再返回任何 HTTP 响应（`curl` 亦返回 `HTTP=000` 超时），进程仍在但已完全无响应，需重启才能恢复。
3. `curl -v` 观察到服务端在请求中发起 **TLS 重新协商（remote party requests renegotiation）**，Go 原生客户端在该协商下始终拿不到响应头而超时——疑似服务端 TLS/连接处理存在缺陷。

---

## 四、逐档详细结果

### 并发 10（5s）
```
Total reqs    : 261
Success       : 251
Failed        : 10
Error rate    : 3.8314%
Requests/sec  : 52.20
Latency mean  : 384.417 ms
Latency p50   : 0.206 ms
Latency p95   : 22.491 ms
Latency p99   : 10006.460 ms
Latency max   : 10013.488 ms
```

### 并发 50（5s）
```
Total reqs    : 301
Success       : 251
Failed        : 50
Error rate    : 16.6113%
Requests/sec  : 60.20
Latency mean  : 1669.122 ms
Latency p50   : 2.046 ms
Latency p95   : 10005.275 ms
Latency p99   : 10013.392 ms
Latency max   : 10015.574 ms
```

### 并发 100（5s）
```
Total reqs    : 351
Success       : 251
Failed        : 100
Error rate    : 28.4900%
Requests/sec  : 70.20
Latency mean  : 2863.270 ms
Latency p50   : 20.901 ms
Latency p95   : 10016.436 ms
Latency p99   : 10017.081 ms
Latency max   : 10017.902 ms
```

### 并发 200（5s）
```
Total reqs    : 451
Success       : 251
Failed        : 200
Error rate    : 44.3459%
Requests/sec  : 90.20
Latency mean  : 4442.653 ms
Latency p50   : 31.375 ms
Latency p95   : 10015.845 ms
Latency p99   : 10020.644 ms
Latency max   : 10022.271 ms
```

### 并发 300（5s）
```
Total reqs    : 551
Success       : 251
Failed        : 300
Error rate    : 54.4465%
Requests/sec  : 110.20
Latency mean  : 5452.206 ms
Latency p50   : 10001.335 ms
Latency p95   : 10014.446 ms
Latency p99   : 10015.808 ms
Latency max   : 10017.945 ms
```

### 并发 500（5s）
```
Total reqs    : 751
Success       : 251
Failed        : 500
Error rate    : 66.5779%
Requests/sec  : 150.20
Latency mean  : 6666.863 ms
Latency p50   : 10005.039 ms
Latency p95   : 10020.611 ms
Latency p99   : 10031.568 ms
Latency max   : 10042.598 ms
```

### 并发 5（冒烟，3s，供参考）
```
Total reqs    : 249
Success       : 244
Failed        : 5
Error rate    : 2.0080%
Requests/sec  : 83.00
Latency mean  : 201.773 ms
Latency p50   : 0.205 ms
Latency p95   : 0.404 ms
Latency p99   : 10007.443 ms
Latency max   : 10015.417 ms
```

---

## 五、结论与建议

1. **服务不可持续承受突发流量**：健康检查这类最轻量的接口，突发 ~251 次后服务即完全僵死，必须重启。这是一个必须修复的 P0 级问题（疑似连接/goroutine 泄漏、连接队列或 TLS 重协商处理缺陷）。
2. **建议排查方向**：
   - 检查服务端连接处理是否有资源上限或泄漏（约 251 ≈ 256 附近的某个缓冲/队列容量）；
   - 排查 TLS 重协商逻辑（`curl -v` 已观测到服务端发起 renegotiation），Go 客户端因此无法正常通信；
   - 若为生产预期行为，建议在网关/代理层做限流与熔断，避免把服务打满。
3. 压测工具与脚本位于临时目录（`%TEMP%\cclaw_bench`），如需复测可直接复用。

---

## 六、根因定位与修复后验证（2026-08-09）

### 根因（已定位）
僵死的根因是**日志链路中一个无消费者的缓冲通道写满后阻塞**，导致请求协程永久卡死：

- `tui/engine/engine_wrapper.go`：`logCh = make(chan LogEntry, 256)` —— 日志缓冲通道容量 **256**；
- `tui/engine/engine.go` `LogManager.Log()`：`lm.logCh <- entry` —— **阻塞式发送**，通道满时永久卡住；
- `agentService/main.go`：`GetLogManager().Start()` 被注释，**没有消费者**从 `logCh` 取数据；
- `platformutility/logging/logging_helper.go`：每个日志（含 `/heartbeat` 的响应日志）都会 `GetLogMgr().Log(...)` 写入该通道。

因果链：启动时 ~5 条日志占位 → 每次健康检查 +1 条 → **251 请求 + 5 预置 = 256（满）** → 第 252 个请求在 `Log()` 处永久阻塞 → 服务整体僵死。这精确解释了「成功数恒为 251、与并发无关、必须重启」等所有现象。

### 修复（用户已在 `logging_helper.go` 修改）
`stdoutInstance` 单例直接返回 `nil`（不再绑定 TUI LogManager），日志不再路由到 `logCh`，阻塞点被移除：
```go
   var stdoutInstance = common.GetSingleton(func() *loggingwriter.AsyncLogger {
       // ... 原 ConsoleWriter + GetLogManager() 相关代码被注释
       return nil
   }).Instance()
```
> 建议同时（可选）：在 `tui/engine/engine.go` 的 `Log()` 加 `select { case lm.logCh <- entry: default: }` 兜底，避免任何场景下日志阻塞业务。

### 修复后压测（同一进程，累计 392,804 请求，全程 0 失败，未再僵死）

| 并发 | 总请求 | 成功 | 失败 | 错误率 | RPS | p50 | p95 | p99 |
|-----:|-------:|-----:|-----:|-------:|----:|----:|----:|----:|
| 200 | 160,473 | 160,473 | 0 | 0.00% | 32,094.6 | 3.5 ms | 20.0 ms | 37.0 ms |
| 300 | 114,940 | 114,940 | 0 | 0.00% | 22,988.0 | 7.7 ms | 39.5 ms | 58.0 ms |
| 500 | 117,391 | 117,391 | 0 | 0.00% | 23,478.2 | 11.1 ms | 48.6 ms | 72.5 ms |

- 修复前：~251 个请求即僵死；修复后：**0 失败、无僵死**，吞吐提升约 **3 个数量级**（从 ~50 RPS 到 ~2~3 万 RPS）。
- 修复后服务在累计约 39 万请求后仍正常响应（`curl` 200，~7ms）。

### 修复后逐档详细结果

#### 并发 200（5s）
```
Total reqs    : 160473
Success       : 160473
Failed        : 0
Error rate    : 0.0000%
Requests/sec  : 32094.60
Latency mean  : 6.164 ms
Latency p50   : 3.534 ms
Latency p95   : 20.029 ms
Latency p99   : 36.997 ms
Latency max   : 114.336 ms
```

#### 并发 300（5s）
```
Total reqs    : 114940
Success       : 114940
Failed        : 0
Error rate    : 0.0000%
Requests/sec  : 22988.00
Latency mean  : 12.331 ms
Latency p50   : 7.716 ms
Latency p95   : 39.544 ms
Latency p99   : 57.995 ms
Latency max   : 162.663 ms
```

#### 并发 500（5s）
```
Total reqs    : 117391
Success       : 117391
Failed        : 0
Error rate    : 0.0000%
Requests/sec  : 23478.20
Latency mean  : 17.678 ms
Latency p50   : 11.141 ms
Latency p95   : 48.560 ms
Latency p99   : 72.469 ms
Latency max   : 170.763 ms
```
