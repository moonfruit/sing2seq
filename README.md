# sing2seq

将 [sing-box](https://github.com/SagerNet/sing-box) 日志转换为 CLEF 格式并投递到 [Seq](https://datalust.co/seq) 的命令行工具。

## 安装

```bash
go build -o sing2seq .
```

## 使用方法

```bash
sing-box run ... 2>&1 | sing2seq -url http://localhost:5341 -api-key XXX
```

### 命令行参数

| 参数 | 说明 | 环境变量 | 默认值 |
|------|------|----------|--------|
| `-url` | Seq 服务器地址 | `SEQ_URL` | 必需 |
| `-api-key` | Seq API Key | `SEQ_API_KEY` | 必需 |
| `-insecure` | 跳过 TLS 证书验证 | — | `false` |

### 完整示例

```bash
# 使用环境变量
export SEQ_URL=http://localhost:5341
export SEQ_API_KEY=your-api-key
sing-box run -c config.json 2>&1 | ./sing2seq

# 跳过 TLS 验证（自签名证书）
sing-box run ... 2>&1 | ./sing2seq -url https://seq.example.com -api-key XXX -insecure
```

### 本地 Seq

```bash
docker compose up -d
```

默认管理员密码：`password`，访问 http://localhost:5341。

## 工作原理

```
sing-box 日志
    │  (通过管道)
    ▼
parser.go
    │  解析日志行，提取 level、message、module、连接信息等
    ▼
CLEF 事件 (JSON per line)
    │  批量缓冲
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
- **阻塞关闭**：`Close()` 会等待所有待处理事件投递完成后才返回

## 依赖

- Go 1.26+
- sing-box 日志输出
- Seq 服务器

## 许可

MIT
