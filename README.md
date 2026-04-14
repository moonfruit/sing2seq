# sing2seq

将 [sing-box](https://github.com/SagerNet/sing-box) 日志转换为 CLEF 格式并投递到 [Seq](https://datalust.co/seq) 的命令行工具。

## 安装

```bash
go build -o sing2seq .
```

## 使用方法

`sing2seq` 提供两种工作模式：

### `pipe`（默认）— 从 stdin 读取日志

```bash
sing-box run ... 2>&1 | sing2seq -u http://localhost:5341 -k XXX
```

不带子命令时等同于 `pipe`，并继承其全部 flag。

### `run` — 直接拉起 sing-box 子进程

```bash
sing2seq run -u http://localhost:5341 -k XXX -c config.json
# 或透传任意 sing-box run 参数：
sing2seq run -u http://localhost:5341 -k XXX -- -c config.json --foo bar
```

`run` 会执行 `sing-box run`（通过 `-p/--sing-box` 可指定其它路径），把子进程 stderr 同时复制到本进程 stderr 与解析器；进程退出码与子进程对齐，期间收到的 SIGINT/SIGTERM/SIGHUP 会转发给子进程。`-c/-C/-D` 等同于 sing-box 的同名选项；启用 `--timestamp` 时会自动注入一份临时配置 `{"log":{"timestamp":true}}`，确保解析器能拿到时间戳。

### 命令行参数

`pipe` 与 `run` 共享的 flag：

| 参数                       | 说明                                                              | 默认值     |
|--------------------------|-----------------------------------------------------------------|---------|
| `-u`, `--url`            | Seq 服务器地址；为空时改为把 CLEF JSON 写到 stdout（便于调试）                      | 空       |
| `-k`, `--api-key`        | Seq API Key                                                     | 空       |
| `--insecure`             | 跳过 TLS 证书验证                                                     | `false` |
| `--timestamp`            | 自身日志带时间戳；`run` 模式下还会让 sing-box 输出时间戳                            | `false` |
| `--disable-color`        | 自身日志禁用 ANSI 颜色；`run` 模式下也会传给 sing-box                           | `false` |

`run` 专属 flag：

| 参数                          | 说明                            | 默认值        |
|-----------------------------|-------------------------------|------------|
| `-p`, `--sing-box`          | 要执行的 sing-box 命令              | `sing-box` |
| `-c`, `--config`            | sing-box 配置文件路径，可重复           | 无          |
| `-C`, `--config-directory`  | sing-box 配置目录，可重复             | 无          |
| `-D`, `--directory`         | sing-box 工作目录                 | 无          |

`-v`/`--version` 打印版本号；`-h`/`--help` 显示帮助。

### 完整示例

```bash
# 跳过 TLS 验证（自签名证书）
sing-box run ... 2>&1 | ./sing2seq -u https://seq.example.com -k XXX --insecure

# 让 sing2seq 自己拉起 sing-box，并启用时间戳
./sing2seq run -u http://localhost:5341 -k XXX --timestamp -c /etc/sing-box/config.json

# 不指定 -u，事件直接打到 stdout（用于调试解析）
sing-box run ... 2>&1 | ./sing2seq | jq .
```

### 本地 Seq

```bash
docker compose up -d
```

默认管理员密码：`password`，访问 http://localhost:5341。

## 工作原理

```
sing-box 日志
    │  (stdin 或 run 拉起子进程的 stderr)
    ▼
parser.go
    │  解析日志行，提取 level、message、module、连接信息等
    ▼
CLEF 事件 (JSON per line)
    │  -u 为空 → 直接写 stdout
    │  -u 有值 → 进入 batcher
    ▼
batcher.go
    │  异步批量 POST
    ▼
Seq /ingest/clef 端点
```

### 架构要点

- **三协程模型**：`parser` 生产事件 → `batcher` 管理缓冲与背压 → `dispatch` 执行 HTTP POST
- **背压控制**：缓冲区上限 50,000 条，超过时裁剪至 25,000 条并丢弃最旧的
- **指数退避**：失败后重试间隔 1s → 2s → 4s ... 最大 60s，成功后重置
- **阻塞关闭**：`Close()` 会等待所有待处理事件投递完成后才返回；若 stdin 已关闭后 Seq 仍不可达，会打印错误并丢弃剩余事件退出，不会无限重试

## 依赖

- Go 1.26+
- sing-box 日志输出
- Seq 服务器

## 许可

MIT
