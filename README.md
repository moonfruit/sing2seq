# sing2seq

将 [sing-box](https://github.com/SagerNet/sing-box) 日志解析成 CLEF 格式并投递到 [Seq](https://datalust.co/seq) 的 Go 库和命令行工具。

## 安装 CLI

```bash
go install github.com/moonfruit/sing2seq/cmd/sing2seq@latest
```

> **v1.3.0 变更**：模块路径从 `sing2seq` 迁移到 `github.com/moonfruit/sing2seq`，安装路径需加 `/cmd/sing2seq` 后缀。旧路径不再可用。

## CLI 使用方法

### `pipe`（默认）— 从 stdin 读取日志

```bash
sing-box run ... 2>&1 | sing2seq -u http://localhost:5341 -k XXX
```

不带子命令时等同于 `pipe`，继承其全部 flag。

`-u ""` 调试模式（不指定 URL）现在**同时**把事件写到 stdout（CLEF JSON）并渲染到 stderr（彩色）。v1.2 仅写 stdout，v1.3.0 两路都走。

### `run` — 直接拉起 sing-box 子进程

```bash
sing2seq run -u http://localhost:5341 -k XXX -c config.json
# 或透传任意 sing-box run 参数：
sing2seq run -u http://localhost:5341 -k XXX -- -c config.json --foo bar
```

`run` 执行 `sing-box run`（`-p/--sing-box` 可指定其它路径），把子进程 stderr 同时复制到本进程 stderr 与解析器；进程退出码与子进程对齐；期间收到的 SIGINT/SIGTERM/SIGHUP 会转发给子进程。启用 `--timestamp` 时会自动注入临时配置 `{"log":{"timestamp":true}}` 确保解析器能拿到时间戳。

### 命令行参数

`pipe` 与 `run` 共享的 flag：

| 参数                  | 说明                                         | 默认值  |
|-----------------------|----------------------------------------------|---------|
| `-u`, `--url`         | Seq 服务器地址；为空时改为把 CLEF JSON 写到 stdout | 空      |
| `-k`, `--api-key`     | Seq API Key                                  | 空      |
| `--insecure`          | 跳过 TLS 证书验证                             | `false` |
| `--timestamp`         | pretty stderr 带时间戳；`run` 下还会让 sing-box 输出时间戳 | `false` |
| `--disable-color`     | pretty stderr 禁用 ANSI 颜色                 | `false` |

`run` 专属 flag：

| 参数                         | 说明                        | 默认值     |
|------------------------------|-----------------------------|------------|
| `-p`, `--sing-box`           | 要执行的 sing-box 命令       | `sing-box` |
| `-c`, `--config`             | sing-box 配置文件路径，可重复 | 无         |
| `-C`, `--config-directory`   | sing-box 配置目录，可重复    | 无         |
| `-D`, `--directory`          | sing-box 工作目录            | 无         |

`-v`/`--version` 打印版本号；`-h`/`--help` 显示帮助。

### 完整示例

```bash
# 跳过 TLS 验证（自签名证书）
sing-box run ... 2>&1 | sing2seq -u https://seq.example.com -k XXX --insecure

# 让 sing2seq 自己拉起 sing-box，并启用时间戳
sing2seq run -u http://localhost:5341 -k XXX --timestamp -c /etc/sing-box/config.json

# 不指定 -u，事件同时写到 stdout（CLEF JSON）和 stderr（彩色）
sing-box run ... 2>&1 | sing2seq | jq .
```

### 本地 Seq

```bash
docker compose up -d
```

默认管理员密码：`password`，访问 http://localhost:5341。

## 库使用

### `clef` — CLEF 事件与内部总线

```go
import "github.com/moonfruit/sing2seq/clef"

// 解析 sing-box 日志行
ev := clef.ParseSingBoxLine(line) // returns *clef.Event; nil if not matched

// 内部发布/订阅
bus := clef.NewBus(256)
em  := clef.NewEmitter(clef.EmitterConfig{Source: "myapp", Bus: bus})
bus.Subscribe(mySubscriber) // mySubscriber implements clef.Subscriber
em.PublishExternal(ev)      // 发布解析到的外部事件
em.Warn("module", "event_id", "message template {Field}", map[string]any{"Field": 42})
bus.Close() // 排空所有订阅方 channel 后再返回
```

### `seq` — 异步批量投递到 Seq

```go
import "github.com/moonfruit/sing2seq/seq"

sk := seq.NewSink(seq.Config{
    URL:     "http://localhost:5341",
    APIKey:  "xxx",
    Emitter: em, // 可选，用于发出 sink 自身诊断事件
})
sk.Start()
sk.Submit(ev)
err := sk.Close() // 阻塞直到所有 pending 事件投递完成
```

## 诊断事件

sing2seq 自身产生的诊断事件与 sing-box 解析事件混合出现在 Seq 中，可通过 `Source="sing2seq"` 区分：

| EventID                  | 级别    | 字段                              | 触发条件                                         |
|--------------------------|---------|-----------------------------------|--------------------------------------------------|
| `buffer_overflow`        | Warning | `Dropped`, `TotalDropped`         | 待处理缓冲区超过 `MaxPending`（默认 50,000 条）   |
| `post_failed`            | Warning | `Pending`, `Error`, `RetryIn`     | HTTP POST 失败，进入指数退避重试                  |
| `shutdown_post_failed`   | Error   | `Pending`, `Error`                | 关闭排空阶段 POST 仍失败，丢弃剩余事件            |

所有诊断事件的 `Module="seq.sink"`，`Source="sing2seq"`。

## 工作原理

```
sing-box 日志
    │  (stdin 或 run 拉起子进程的 stderr)
    ▼
clef.ParseSingBoxLine
    │  解析日志行 → *clef.Event  (Source="sing-box")
    ▼
clef.Bus (in-process pub/sub)
    ├─→ pretty renderer (stderr)
    └─→ seq.Sink 或 stdoutSink
           │  异步批量 POST / 写 stdout JSON
           ▼
        Seq /ingest/clef 端点
```

sing2seq 自身通过 `clef.Emitter` 发出诊断事件（`Source="sing2seq"`），同样经 Bus 流向 pretty renderer 和 Seq。

## 依赖

- Go 1.26+
- sing-box 日志输出
- Seq 服务器

## 许可

MIT
