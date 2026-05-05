# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

`sing2seq` 是一个 Go 库 + CLI 工具，把 [sing-box](https://github.com/SagerNet/sing-box) 的日志解析成 CLEF (Compact Log Event Format) 事件，批量投递到 [Seq](https://datalust.co/seq) 的 `/ingest/clef` 端点。

模块路径：`github.com/moonfruit/sing2seq`

## 常用命令

- 构建：`go build ./...`
- 测试：`go test ./...` / `go test -race ./...`
- 静态检查：`go vet ./...`
- 构建 CLI：`go build ./cmd/sing2seq`
- 本地 Seq：`docker compose up -d`（compose.yaml，管理员密码 `password`，端口 5341）
- Go 版本：`go.mod` 声明 `go 1.26`

## 目录结构

```
clef/       — 公共库：CLEF 原语与内部总线
seq/        — 公共库：异步批量 HTTP sink
cmd/sing2seq/ — CLI 二进制（cobra）
compose.yaml  — 本地 Seq docker compose
```

## 架构

### `clef/` — CLEF 原语与总线

- **`event.go`** — `Event`：顺序保留的 CLEF 事件（`keys []string` + `values map`），`MarshalJSON` 按 `Set` 插入顺序序列化。Seq UI 按顺序展示字段；改字段顺序时改 `Set` 调用顺序，不是 map 遍历。
- **`level.go`** — `Level`：Trace/Debug/Info/Warn/Error/Fatal，提供 CLEF 名（`"Warning"` 等）和短名映射。
- **`parser.go`** — `ParseSingBoxLine(line string) *Event`：去 ANSI 色码 → 正则匹配 sing-box 日志格式 → 构造 CLEF 事件（`Source="sing-box"`）。无法匹配的行返回 `nil`。`@mt` 模板根据捕获组动态拼接，占位符与字段一一对应。
- **`bus.go`** — `Bus`：lossy 内存 pub/sub。`Publish` 永不阻塞（channel 满则丢弃）。`Close` **先关闭所有订阅方的输入 channel，再等待其 drain goroutine 退出**——保证 Close 前入队的事件都被处理，再返回。订阅方实现 `Subscriber`（`Match` + `Deliver`）接口；也可用 `SubscriberFunc`。
- **`emitter.go`** — `Emitter`：结构化事件发布入口。`PublishExternal(ev)` 把外部解析事件（如 `ParseSingBoxLine` 结果）发布到 Bus；`Info/Warn/Error/...` 构造并发布内部诊断事件。字段：`@t/@l/@mt/Source/Module/EventID` + 自定义字段。

### `seq/` — 异步批量 Seq sink

- **`sink.go`** — `Sink`：三协程模型（G1 Submit → G2 manager → G3 dispatch HTTP）。
  - G2 的 `select` 只做 O(1) 工作，永不阻塞 I/O，保证 `Submit` 实际上从不阻塞。
  - `MaxPending`（默认 50,000）溢出时裁剪到 `DropTarget`（默认 25,000），丢最旧的，并通过 `Emitter` 发出 `buffer_overflow` 诊断事件。
  - 失败走指数退避：`InitialBackoff=1s` → 翻倍 → `MaxBackoff=60s`；成功后重置；通过 `Emitter` 发出 `post_failed` 诊断事件。
  - `Close()` 阻塞直到 pending 全部排空；关闭阶段 POST 仍失败则发出 `shutdown_post_failed` 并丢弃剩余事件。
  - `Config.Emitter` 为 nil 时 fallback 到 stderr 文本打印。
  - POST 目标：`<URL>/ingest/clef`，`Content-Type: application/vnd.serilog.clef`，API key 走 `X-Seq-ApiKey`。

### `cmd/sing2seq/` — CLI（cobra）

- **`main.go`** — cobra 根命令 + `pipe`/`run` 子命令注册。根命令复用 pipe 的 flag 集合，无子命令等价于 `pipe`。
- **`pipe.go`** — `Pipe.Run(r io.Reader)`：Bus 驱动的核心管道。
  - 创建 Bus（perSubBuffer=256）和 Emitter（`Source="sing2seq"`）。
  - 始终订阅 pretty renderer（stderr）。
  - URL 为空 → 订阅 `stdoutSink`（stdout JSON），关闭顺序：`sk.Close` → `bus.Close` → `stdout.Flush`（bus 必须先驱动 Deliver 才能 Flush bufio.Writer）。
  - URL 非空 → 订阅 `seq.Sink`，关闭顺序：`sk.Close`（排空 HTTP 队列）→ `bus.Close`（延迟诊断仍能流到 pretty renderer）。
  - `bufio.Scanner` 缓冲设为 4 MiB（sing-box 某些日志行超出默认 64 KiB）。
- **`run.go`** — `RunCmd`：`exec` sing-box，`io.MultiWriter` 把子进程 stderr 同时送到 `os.Stderr` 与 `Pipe.Run`；转发 SIGINT/SIGTERM/SIGHUP；退出码对齐子进程。`--timestamp` 注入临时 `{"log":{"timestamp":true}}` 配置；`--disable-color` 透传给 sing-box。
- **`pretty.go`** — `prettyRenderer`：`Source="sing-box"` 用 sing-box 样式格式化；`Source="sing2seq"` 用 sing2seq 样式格式化（不同颜色/前缀）。
- **`stdout.go`** — `stdoutSink`：URL="" 模式，把事件序列化为 CLEF JSON 写到 stdout（带 bufio 缓冲，需在 bus.Close 后 Flush）。

## 事件 Source 规范

| Source       | 产生方            | 说明                       |
|--------------|-------------------|----------------------------|
| `sing-box`   | `ParseSingBoxLine` | sing-box 解析的日志事件    |
| `sing2seq`   | `Emitter`         | CLI 自身诊断事件           |

CLI 诊断事件固定字段：`Module="seq.sink"`，`EventID` 见下表。

## 诊断事件 schema

| EventID                | 级别    | 字段                             |
|------------------------|---------|----------------------------------|
| `buffer_overflow`      | Warning | `Dropped`, `TotalDropped`        |
| `post_failed`          | Warning | `Pending`, `Error`, `RetryIn`    |
| `shutdown_post_failed` | Error   | `Pending`, `Error`               |

## 关键设计要点

- **Bus.Close 语义**：关闭前先排空各订阅方的输入 channel，不是原始 lossy-on-close 设计。在 sing2seq 中 Bus 是唯一事件通路，不能丢末尾事件。
- **关闭顺序（seq 模式）**：`sk.Close` → `bus.Close`。先关 Sink 让 HTTP 排空；关闭期间产生的诊断事件经 bus 仍能到达 pretty renderer。
- **关闭顺序（stdout 模式）**：`bus.Close` → `stdout.Flush`。Bus 必须先驱动 Deliver（写入 bufio.Writer），再 Flush，否则末尾事件会丢失。
- **字段顺序**：`Event.MarshalJSON` 按 `Set` 插入顺序输出；Seq UI 展示顺序依赖于此。
