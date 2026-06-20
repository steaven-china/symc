# 实验报告 · M8 端到端 NATS pub/sub 验证

> **实验 ID**:exp-e2e · 2026-06-20
> **状态**:✅ 跑通
> **产物**:`cmd/e2e/main.go`(结果通过 stdout 显示)
> **测试目标**:验证 M7 `SymcCooperationRequest.publish()` + `subscribe()` 的 NATS pub/sub 模式端到端走得通

---

## 1. 目的

**M7 真实网络 sync**(NATS pub/sub) 只**编译过** —— 实际**没**验证 publisher 发出去、subscriber 收得到。
**M8 e2e** 是补这个洞:

- 启 NATS server(已跑在 127.0.0.1:4222)
- Go client **模拟 Java `SymcCooperationRequest.publish()` 行为**:发 CooperationRequest 风格 JSON 到 subject `symc.cooperation.test`
- Go client **模拟 `subscribe()` 行为**:收消息,验证字段
- 测 100 条

**不真启 Paper** —— paperclip 启 server + MC 客户端连接 + 触发红石事件,链路太长。先验证 NATS pub/sub 模式走得通,Paper 集成留 M8 真阶段。

---

## 2. 方法

### 2.1 NATS server

- 已跑在 `D:/engine/symc/third-party/nats-data`(JetStream 启用,端口 4222)
- 不需要额外配置

### 2.2 Go client(e2e)

- `github.com/nats-io/nats.go` v1.52.0(同 exp1-bus-nats 用的)
- publisher:发 JSON `{"type":"COMPUTATION", "x":0..99, "y":64, "z":64, "event":"redstone_pulse", "payload":"level_change=0→15", "ts":UnixNano}` 到 `symc.cooperation.test`
- subscriber:收消息,记录时间戳,算 RTT

### 2.3 消息格式(模拟 SymcCooperationRequest.CooperationRequest JSON)

```json
{
  "type": "COMPUTATION",
  "x": 0,
  "y": 64,
  "z": 64,
  "event": "redstone_pulse",
  "payload": "level_change=0→15",
  "ts": 1718874123456
}
```

> **注意**:Java `SymcCooperationRequest.toJson` 用手写拼接,**不是完整 JSON**。真集成时需用 Jackson 或规范 JSON 解析。这点 M7 后续要修。

---

## 3. 结果

```
e2e: subject=symc.cooperation.test pub=0ms recv=0ms rtt_avg=0ms got=100/100 success=true
     min=364µs max=869.9µs avg=698.388µs
```

| 指标 | 值 |
|---|---|
| 发送条数 | 100 |
| 接收条数 | 100 |
| 成功 | **100/100 = 100%** ✅ |
| RTT min | 364 μs |
| RTT max | 870 μs |
| RTT avg | 698 μs |
| 总时间 | 0 ms(NATS 极快) |

**所有 100 条消息都正确收到**。RTT 700μs 跟 exp1-bus-nats 64B 单 sender P99 874μs 一致 —— NATS pub/sub 模式在 e2e 表现稳定。

---

## 4. 结论

**M7 NATS pub/sub 模式端到端走得通** ✅

- ✅ publisher 发消息到 NATS
- ✅ subscriber 收消息
- ✅ 字段匹配(模拟 CooperationRequest JSON 格式)
- ✅ RTT 700μs 稳定

**接下来**:
- 启 paperclip(真 MC server)
- 让 Paper 加载 `SymcCooperationRequest` 钩子
- 触发真红石事件
- 验证 NATS 收到(把模拟 publisher 换成真 Paper)

**遗留**(M8 后续):
- ❌ Java `toJson` 用手写拼接,不是真 JSON — M8 真集成前要换 Jackson 或 GSON
- ❌ 没有 reply subject(单向 fire-and-forget,响应要另开 topic)
- ❌ 没有错误恢复(NATS 断了 symc 不重连)
- ❌ `SymcWriteAuthority` / `SymcAntiCheat` 还没接 NATS

---

## 5. 复现

```bash
# 1. 启 NATS(如果没在跑)
mkdir -p D:/engine/symc/third-party/nats-data
"D:/go/bin/nats-server.exe" -p 4222 -js -sd D:/engine/symc/third-party/nats-data -l D:/engine/symc/third-party/nats.log &

# 2. 跑 e2e
cd D:/engine/symc
go run ./cmd/e2e

# 期望输出:e2e: ... got=100/100 success=true
```

---

*本报告是 M8 端到端 e2e 验证的留档。Paper 真集成(加载 symc 钩子 + 真 MC 事件触发)留到 M8 真阶段。*
