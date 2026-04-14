# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

`sing2seq` 是一个小型 Go 命令行工具，把 [sing-box](https://github.com/SagerNet/sing-box) 的日志解析成 CLEF (Compact Log Event Format) 事件，批量投递到 [Seq](https://datalust.co/seq) 的 `/ingest/clef` 端点。

提供两个子命令（基于 `spf13/cobra`）：

- `pipe`（默认）—— 从 stdin 读日志，等同于不带子命令直接调用。
- `run` —— 自己 `exec` 一个 `sing-box run`，把它的 stderr 同时复制到本进程 stderr 与解析器；进程退出码与子进程对齐。

典型用法：`sing-box run ... 2>&1 | sing2seq -u http://localhost:5341 -k XXX`，或 `sing2seq run -u ... -k ... -c config.json`。

## 常用命令

- 构建：`go build -o sing2seq .`
- 运行：`./sing2seq -u <seq-url> [-k <key>] [--insecure]`，URL 留空时事件会直接写到 stdout（调试用）。
- 本地 Seq：`docker compose up -d`（compose.yaml 启动 `datalust/seq`，管理员密码 `password`）。
- 测试：`go test ./...`（当前覆盖 `parser_test.go`）。
- Go 版本：`go.mod` 声明 `go 1.26`，构建时需要对应或更高版本的 toolchain。

## 架构

代码全部在 `main` 包下：

### `main.go` — CLI 入口与 sink 选择
- 用 cobra 注册 `pipe` 与 `run` 两个子命令，根命令复用 `pipe` 的 flag 集合，所以无子命令调用等价于 `pipe`。
- `Pipe.Run` 用 `bufio.Scanner` 读输入，`scanner.Buffer` 显式设为 4 MiB（`4*1024*1024`），因为 sing-box 某些日志行会超出默认 64 KiB。修改缓冲时注意这点。
- `newSink` 是关键分支：`URL` 为空 → `stdoutSink` 把事件 JSON 行打到 stdout；非空 → `batcherSink` 启动 `Batcher`。`nil` 事件作为关闭信号，触发 flush 或 `Close`。
- `RunCmd.Run` 用 `io.Pipe` 把 `cmd.Stderr` 同时送到 `os.Stderr` 与 `Pipe.Run`；`signal.Notify` 把 SIGINT/SIGTERM/SIGHUP 转发给子进程（前台终端通常已经把信号送给 PG，这里主要是为 systemd / pipeline 场景兜底）。
- `RunCmd.buildRunArgs`：当 `--timestamp` 启用时写一份临时 `{"log":{"timestamp":true}}` 配置并 `-c` 注入，让 sing-box 输出能被 `lineRe` 正确匹配；`--disable-color` 也会透传给 sing-box。

### `log.go` — 自身日志格式化
- `newLogf(timestamp, disableColor)` 工厂返回 `logfFunc`；颜色映射只覆盖 `WARN/ERROR/FATAL`，其它级别原样输出。`--timestamp` 决定是否带 `-0700 YYYY-MM-DD HH:MM:SS.mmm` 前缀，与 sing-box 的 `log.timestamp` 对齐。
- `Pipe.Logf` 懒初始化 `logf`，`Batcher` 通过传入的 `logfFunc` 复用同一套格式化逻辑。

### `parser.go` — 日志行 → CLEF 事件
- `parseLine` 先用 `ansiRe` 去色码，然后 `lineRe` 匹配 sing-box 格式 `<tz> <date> <time> <LEVEL> [<conn> <dur>] <rest>`；`rest` 再用 `moduleRe` 拆出 `module[/type][[tag]]: detail`。
- `levelMap` 将 sing-box 级别映射到 Serilog 级别（如 `WARN→Warning`，`PANIC→Fatal`）。
- 关键点：事件用自定义 `orderedEvent`（`keys` 切片 + `values` map）表示，并实现 `MarshalJSON` 以**保持字段插入顺序**——CLEF 在 Seq UI 里靠此顺序决定展示。修改字段顺序时必须改 `set` 的调用顺序，而不是 map 遍历。
- 无法匹配 `lineRe` 的行会降级为 `Parsed=false` 的原始事件，而不是丢弃。
- `@mt` 模板是根据实际存在的捕获组（conn/type/tag）动态拼接的，保证模板占位符和字段一一对应，否则 Seq 会把消息渲染错。

### `batcher.go` — 异步批处理 + 背压
采用三协程模型（注释里叫 G1/G2/G3）：
- **G1**（`main` 的 scanner 循环）调用 `Submit`，只往 `ch` 里塞事件。
- **G2**（`run`）是唯一的 manager goroutine，`select` 里**只做 O(1) 工作**，永不阻塞 I/O，确保 `ch` 总能及时清空、`Submit` 实际上从不阻塞。它维护 `pending` 缓冲、在途标记 `inflight` 和重试定时器 `retryC`。
- **G3** 是 `dispatch` 启动的一次性 HTTP 发送 goroutine，完成后把结果写入 `resultC` 然后退出。同一时刻最多一个 G3 在飞。

重要不变量和策略：
- `pending` 超过 `maxPending` (50000) 时裁剪到 `dropTarget` (25000)，丢最旧的并通过 `logfFunc` 打印累计丢弃数。改这些常量意味着改变内存上界。
- 失败走指数退避：`initialBackoff=1s` → 翻倍 → `maxBackoff=60s`；成功后重置。
- `Close()` 关闭 `ch` 后 `run` 进入 "closed" 状态，继续排空 `pending` 直到为空且无在途/无待重试才退出——即 `Close` 是阻塞的 flush。
- POST 目标固定为 `<URL>/ingest/clef`，请求头 `Content-Type: application/vnd.serilog.clef`，API key 走 `X-Seq-ApiKey`。批内事件用换行分隔（CLEF 的 ndjson 格式）。
