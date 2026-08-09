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

---

## 七、登录/登出接口压测（login/logout，2026-08-09）

> 上一章只压了 `/heartbeat`（不经过业务 filter/interceptor）。本章针对**真正走完整安全/鉴权链路**的 `/api/login` 与 `/api/logout` 接口做并发压测。
> 压测对象：`agentService`（`bin/agentService.exe`，已含 Lua 会话修复与日志修复后的最新代码）。

### 测试方法
- 每个并发 worker 使用**独立测试账号**（`bench_0000`~`bench_0999`，密码同为 `123456`，压测后已从 `t_account` 清理）。
  - 为什么必须独立账号：登录会通过 `LuaCreateOrReplace` 把 `token_<user>` 替换为新 token；同一账号并发登录会让旧 token 失效，logout 即返回 `401 / respCode=3017 Token无效`。这是**正确的会话语义**，不是缺陷。用单账号（`d/123456`）实测：8 线程并发下 logout 几乎全部 401。
- 每次迭代 = 一次完整周期：`POST /api/login?NONCE=<唯一>`（取 `data.token`）→ `POST /api/logout`（`AUTHORIZATION` 头携带 token，logout 在 `deSecurityUrl`，无需 NONCE）。
- **NONCE 注意**：`SecurityFilter` 会对 NONCE 做防重放（写入 Redis，2 分钟窗口），因此**每次 login 必须使用全局唯一的 NONCE**，重复即 `403 / respCode=1006 ConstNonceInvalid`。
- **临时放行限流**：`TrafficFilter` 硬编码「每 IP 每 API 2 分钟内 50 次」（`IPAPITrafficFilterLimits=50`），会把压测几乎全部打 403。为测得真实吞吐，压测期间在 `service_main.go` 临时把 `/api/login`、`/api/logout` 加入 `UnTrafficUrls`；**压测完成后已还原该改动并重新构建**。
- 每档前重启服务（起点一致），窗口 10s，客户端超时 20s，Schannel 跳过证书校验。

### 结果汇总

| 并发 | 周期总数 | 成功周期 | 失败 | 错误率 | login 失败 | logout 失败 | 超时 | 周期/秒 | p50 | p99 |
|-----:|--------:|--------:|-----:|-------:|----------:|----------:|-----:|--------:|----:|----:|
| 100 | 300 | 300 | 0 | 0.00% | 0 | 0 | 0 | 30.0 | 3.63 s | 4.26 s |
| 200 | 400 | 399 | 1 | 0.25% | 1 | 0 | 0 | 40.0 | 7.95 s | 8.63 s |
| 300 | 300 | 300 | 0 | 0.00% | 0 | 0 | 0 | 30.0 | 11.98 s | 12.64 s |
| 500 | 501 | 179 | 322 | 64.27% | 322 | 0 | 321 | 50.1 | 10.96 s | 13.76 s |
| 1000 | 1607 | 0 | 1607 | 100.00% | 1607 | 0 | 1094 | 160.7 | 6.03 s | 14.04 s |

> 周期/秒为「总周期数 ÷ 10s」的名义值；高并发下大量请求超时/被丢弃，真实吞吐远低于名义值，**健康时真实吞吐恒定约 30~40 周期/秒**（见下分析）。

### 逐档详细结果

#### 并发 100（10s）
```
Total cycles  : 300
Success       : 300
Failed        : 0
Error rate    : 0.0000%
  failLogin   : 0 / failLogout 0 / timeout 0 / traffic403 0
Cycles/sec    : 30.00
Latency mean  : 3644.805 ms   p50 3629.112  p95 3986.950  p99 4260.418  max 4288.131
```

#### 并发 200（10s）
```
Total cycles  : 400
Success       : 399
Failed        : 1
Error rate    : 0.2500%
  failLogin   : 1 / failLogout 0 / timeout 0 / traffic403 0
Cycles/sec    : 40.00
Latency mean  : 7867.708 ms   p50 7954.920  p95 8392.928  p99 8631.580  max 8773.230
```

#### 并发 300（10s）
```
Total cycles  : 300
Success       : 300
Failed        : 0
Error rate    : 0.0000%
  failLogin   : 0 / failLogout 0 / timeout 0 / traffic403 0
Cycles/sec    : 30.00
Latency mean  : 11982.763 ms   p50 11979.481  p95 12571.942  p99 12635.955  max 12786.794
```

#### 并发 500（10s）
```
Total cycles  : 501
Success       : 179
Failed        : 322
Error rate    : 64.2715%
  failLogin   : 322 / failLogout 0 / timeout 321 / traffic403 0
Cycles/sec    : 50.10
Latency mean  : 11618.200 ms   p50 10960.133  p95 13675.304  p99 13758.750  max 13981.127
```

#### 并发 1000（10s）
```
Total cycles  : 1607
Success       : 0
Failed        : 1607
Error rate    : 100.0000%
  failLogin   : 1607 / failLogout 0 / timeout 1094 / traffic403 0
Cycles/sec    : 160.70
Latency mean  : 7748.708 ms   p50 6030.881  p95 13771.490  p99 14039.900  max 14362.506
```

### 关键发现与分析

1. **login/logout 链路真实吞吐恒定在 ~30~40 周期/秒，与并发无关**：c=100→30/s、c=200→40/s、c=300→30/s。这是**串行/资源瓶颈**的典型特征（Little's law：并发÷吞吐≈单周期耗时，c=100 时 ≈3.3s，与实测 p50 3.6s 吻合）。
2. **单周期服务端耗时高（数百 ms ~ 秒级）**：与 `/heartbeat`（~0.2ms）形成巨大反差。差异根源是该链路**每个周期约 8 次到远程 Redis 的往返**：
   - 非重放：`GET nonce` + `SET nonce`（2 次）
   - 节流：`GET login_lock_*` + `DEL`（2 次）
   - 会话：`EVAL LuaCreateOrReplace`、`EVAL LuaGetEx`、`EVAL LuaRefresh`、`EVAL LuaRevoke`（4 次）
   - 实测到 `35.78.178.26:6379` 的 Redis **PING RTT ≈ 70ms**（66~75ms），TCP 建连峰值 1074ms。
   - 单请求串行约 8×70ms≈560ms 起步；受 `poolSize=20` 限制，并发高时排队加剧。
3. **c≥500 开始超时，c=1000 全部失败**：客户端 20s 超时 + Redis 池耗尽（服务端 `readTimeout=3s/writeTimeout=3s` 报错）。日志观测到大量 `[Redis] connection established`（连接池饱和重建）以及 `/api/login` 返回 `500 / code=1008 internal server error`。**未再出现旧“~251 僵死”缺陷**——服务可恢复，只是极端过载。
4. **两个被测到的既有约束**（非缺陷，属正确行为）：
   - `TrafficFilter`：每 IP 每 API 2 分钟 50 次（`IPAPITrafficFilterLimits=50`），压测需临时放行；**生产上这正是防刷限流**。
   - 单账号并发：旧 token 的 logout 返回 `401 respCode=3017 Token无效`（token 已被新登录替换），会话模型符合预期。
5. **优化建议（针对瓶颈）**：
   - 就近部署 Redis（当前为远程 AWS EC2，RTT ~70ms）或改用本地/内网 Redis，可显著降低每次往返延迟；
   - 适当提高 `poolSize`（当前 20）与 `readTimeout/writeTimeout`，降低排队概率；
   - 合并 Redis 往返：如 NONCE 防重放用 `SET NX EX` 一次完成、节流用 Lua 原子计数，可把每周期往返从 ~8 次降到 ~4 次；
   - 账号校验（SQLite/GORM 读 `t_account`）与 Redis 会话读写可考虑缓存账号信息，减少热点查询。

### 附注
- 压测脚本：`%TEMP%\cclaw_bench\loadtest_loginlogout.ps1`（独立账号模式 `-UserPrefix bench_`）、`run_login_bench.ps1`。
- 压测后已**还原** `service_main.go` 限流放行改动、**清理** `bench_*` 测试账号，并重新构建/重启 `bin/agentService.exe`（8443 正常监听）。
- 压测过程中 Redis 产生的 `nonce_*`（2min TTL）、`user_*`/`token_*`（30min TTL）会话键会自动过期，无需手工清理。

---

### poolSize 对比验证（20 → 500，2026-08-09）

针对上一节定位的「远程 Redis 延迟 × 小连接池」瓶颈，将 `[memoryDb] poolSize` 从 **20 改为 500** 后，用相同方法（独立账号、唯一 NONCE、临时放行限流、10s/档、客户端超时 20s）重跑全部并发档，结果如下：

| 并发 | pool20 吞吐 | pool500 吞吐 | 提升 | pool20 错误率 | pool500 错误率 | pool20 p50 | pool500 p50 |
|-----:|----------:|----------:|----:|-------------:|-------------:|----------:|----------:|
| 100 | 30.0/s | 135.2/s | **4.5x** | 0.00% | 0.00% | 3.63 s | 0.62 s |
| 200 | 40.0/s | 233.8/s | **5.8x** | 0.25% | 0.00% | 7.95 s | 0.86 s |
| 300 | 30.0/s | 306.0/s | **10.2x** | 0.00% | 0.00% | 11.98 s | 0.91 s |
| 500 | 50.1/s¹ | 528.8/s | **~10.6x** | 64.27% | 0.00% | 10.96 s | 0.86 s |
| 1000 | 160.7/s¹ | 604.1/s | **~3.7x** | 100.00% | 0.00% | 6.03 s | 1.70 s |

> ¹ pool20 在 c≥500 时大量失败/超时，名义 RPS 不代表真实吞吐，故只作参考。

#### 结论
- **poolSize=500 大幅提升并发吞吐**：100~500 并发下吞吐提升 **4.5~10x**，延迟下降 **5~13x**；c≥500 的错误率从 64%~100% 降为 **0%**。
- **单周期延迟地板不变**：p50 稳定在 ~0.6~0.9s（c≤500），仍是「8 次远程 Redis 往返 × ~70ms RTT + SQLite 账号查询」决定，调池不能消除该地板。
- **c=1000 时池再次成为弱约束**：1000 并发 × 8 次往返 ≈ 8000 在途操作超过 500 池容量，p50 回升到 1.7s。若需压到 1000+ 并发，可再调大池或就近部署 Redis。
- **进一步优化方向**（不再靠调池）：
  - 就近/本地部署 Redis（消除 ~70ms 远程 RTT）；
  - 合并往返：NONCE 防重放用 `SET NX EX` 一次完成、节流计数用 Lua 原子化，把每周期往返从 ~8 次降到 ~4 次；
  - 账号信息（`t_account`）缓存，减少 SQLite/GORM 热点查询。

> 该对比验证后，`poolSize` 已按用户意图保留为 **500**（源码与 `bin/config` 一致）；`service_main.go` 临时限流放行已还原并重建重启。
