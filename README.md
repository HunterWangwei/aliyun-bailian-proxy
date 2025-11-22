# 阿里云百炼智能体API转发服务

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

这是一个用Go语言编写的中转服务，将OpenAI API请求格式转换为阿里云百炼智能体API请求格式。

## 核心特性

**完全透明的格式转换**：
- 客户端只需要使用标准的OpenAI API格式
- 转发站自动将OpenAI格式转换为阿里云百炼原生格式
- 转发站自动将阿里云响应转换回OpenAI格式
- 客户端完全不需要了解阿里云的原生格式

## 功能特性

- ✅ 完全兼容OpenAI Chat Completions API格式
- ✅ 支持流式响应（Stream）
- ✅ 自动转发请求到阿里云百炼智能体API
- ✅ 支持环境变量配置
- ✅ 健康检查端点
- ✅ 完整的错误处理

## 快速开始

### 1. 环境要求

- Go 1.21 或更高版本
- 阿里云百炼智能体应用ID
- 阿里云百炼API Key

### 2. 安装依赖

```bash
go mod tidy
```

### 3. 配置环境变量

创建 `.env` 文件或直接设置环境变量：

```bash
# 必需配置
export ALIYUN_APP_ID="your-app-id"
export ALIYUN_API_KEY="your-api-key"

# 可选配置
export PORT="8081"  # 服务端口，默认8080（示例使用8081）
export ALIYUN_BASE_URL="https://dashscope.aliyuncs.com"  # 默认值
```

### 4. 运行服务

```bash
go run main.go
```

或者编译后运行：

```bash
go build -o aliyun-bailian-proxy
./aliyun-bailian-proxy
```

### 5. 测试服务

#### 健康检查

```bash
curl http://localhost:8081/health
```

#### 发送聊天请求

```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer your-openai-key" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [
      {"role": "system", "content": "你是一个有帮助的助手。"},
      {"role": "user", "content": "你好！"}
    ],
    "temperature": 0.7
  }'
```

#### 流式请求

```bash
curl -X POST http://localhost:8081/v1/chat/completions \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gpt-3.5-turbo",
    "messages": [
      {"role": "user", "content": "写一首诗"}
    ],
    "stream": true
  }'
```

## API端点

### POST /v1/chat/completions

接收OpenAI格式的聊天完成请求，转发到阿里云百炼智能体API。

**请求格式**（与OpenAI API完全兼容）：

```json
{
  "model": "gpt-3.5-turbo",
  "messages": [
    {"role": "system", "content": "你是一个有帮助的助手。"},
    {"role": "user", "content": "你好！"}
  ],
  "temperature": 0.7,
  "max_tokens": 1000,
  "stream": false
}
```

**支持的参数**：
- `model`: 模型名称（会被转发，但阿里云会使用配置的智能体）
- `messages`: 消息列表（必需）
- `temperature`: 温度参数
- `top_p`: Top-p采样
- `max_tokens`: 最大token数
- `stream`: 是否流式返回
- `presence_penalty`: 存在惩罚
- `frequency_penalty`: 频率惩罚
- `stop`: 停止序列
- 其他OpenAI兼容参数

### GET /health

健康检查端点，返回服务状态。

## 环境变量说明

| 变量名 | 说明 | 必需 | 默认值 |
|--------|------|------|--------|
| `ALIYUN_APP_ID` | 阿里云百炼智能体应用ID | 是 | - |
| `ALIYUN_API_KEY` | 阿里云百炼API Key | 是 | - |
| `PORT` | 服务监听端口 | 否 | 8080（示例使用8081） |
| `ALIYUN_BASE_URL` | 阿里云API基础URL | 否 | https://dashscope.aliyuncs.com |
| `USE_NATIVE_API` | 是否使用原生API格式（true/false） | 否 | true |
| `PROXY_URL` | 代理URL（预留） | 否 | - |
| `REQUEST_TIMEOUT` | 非流式请求超时时间（秒） | 否 | 120 |
| `STREAM_TIMEOUT` | 流式请求超时时间（秒） | 否 | 300 |
| `MAX_IDLE_CONNS` | 最大空闲连接数 | 否 | 100 |
| `MAX_IDLE_CONNS_PER_HOST` | 每个主机最大空闲连接数 | 否 | 50 |
| `MAX_CONNS_PER_HOST` | 每个主机最大连接数 | 否 | 100 |
| `IDLE_CONN_TIMEOUT` | 空闲连接超时时间（秒） | 否 | 90 |
| `REQUEST_TIMEOUT` | 非流式请求超时时间（秒） | 否 | 120 |
| `STREAM_TIMEOUT` | 流式请求超时时间（秒） | 否 | 300 |
| `MAX_IDLE_CONNS` | 最大空闲连接数 | 否 | 100 |
| `MAX_IDLE_CONNS_PER_HOST` | 每个主机最大空闲连接数 | 否 | 50 |
| `MAX_CONNS_PER_HOST` | 每个主机最大连接数 | 否 | 100 |
| `IDLE_CONN_TIMEOUT` | 空闲连接超时时间（秒） | 否 | 90 |

## 部署

### Docker部署

创建 `Dockerfile`：

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o aliyun-bailian-proxy

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /app/aliyun-bailian-proxy .
EXPOSE 8080
CMD ["./aliyun-bailian-proxy"]
```

构建和运行：

```bash
docker build -t aliyun-bailian-proxy .
docker run -d -p 8081:8080 \
  -e ALIYUN_APP_ID=xxxxxxxxxxxxx \
  -e ALIYUN_API_KEY=sk-5dfxxxxxxxxxxxxxxxxxxx \
  aliyun-bailian-proxy
```

### 直接部署

```bash
# 编译
go build -o aliyun-bailian-proxy

# 运行
ALIYUN_APP_ID=your-app-id \
ALIYUN_API_KEY=your-api-key \
PORT=8081 \
./aliyun-bailian-proxy
```

## 使用场景

1. **迁移现有OpenAI应用**：无需修改客户端代码，只需更改API端点
2. **统一API接口**：多个应用可以统一使用OpenAI格式调用阿里云服务
3. **开发测试**：在开发环境中使用阿里云服务，生产环境切换到OpenAI

## 故障排除

### 错误：Agent request must contain input messages

如果遇到此错误，可以尝试以下解决方案：

1. **使用原生API格式**：设置环境变量 `USE_NATIVE_API=true`，将使用阿里云百炼原生API格式而不是兼容模式。

```bash
export USE_NATIVE_API=true
```

2. **检查端点配置**：确保 `ALIYUN_BASE_URL` 正确设置为 `https://dashscope.aliyuncs.com`

3. **验证应用ID和API Key**：确保应用ID和API Key正确，并且API Key有访问该应用的权限

## 性能优化（支持1000+ RPM）

本服务已针对高并发场景进行优化，支持1000+ RPM（每分钟请求数）的稳定运行：

### 优化特性

1. **连接池复用**：
   - 使用全局HTTP客户端，复用TCP连接
   - 减少连接建立和TLS握手开销
   - 默认配置：最大100个空闲连接，每主机50个空闲连接

2. **合理的超时设置**：
   - 非流式请求：120秒超时
   - 流式请求：300秒超时
   - 避免资源泄漏和连接堆积

3. **连接管理**：
   - 自动管理连接生命周期
   - 空闲连接90秒后自动关闭
   - Keep-Alive保持连接活跃

### 高并发配置建议

对于1000+ RPM的场景，建议调整以下环境变量：

```bash
# 连接池配置（根据实际负载调整）
export MAX_IDLE_CONNS=200              # 增加最大空闲连接数
export MAX_IDLE_CONNS_PER_HOST=100     # 增加每主机空闲连接数
export MAX_CONNS_PER_HOST=200          # 增加每主机最大连接数

# 超时配置
export REQUEST_TIMEOUT=120             # 非流式请求超时
export STREAM_TIMEOUT=300              # 流式请求超时
export IDLE_CONN_TIMEOUT=90            # 空闲连接超时
```

### 性能监控

建议在生产环境中：
- 监控连接池使用情况
- 监控请求响应时间
- 监控错误率和超时率
- 根据实际负载调整连接池参数

### 系统资源建议

- **CPU**: 2核心以上
- **内存**: 512MB以上（根据并发量调整）
- **网络**: 稳定的网络连接，低延迟
- **Go版本**: 1.21或更高版本（已优化GC性能）

## 注意事项

1. 确保阿里云百炼智能体应用已正确配置
2. API Key需要有访问对应应用的权限
3. 流式响应需要客户端支持Server-Sent Events (SSE)
4. 建议在生产环境中使用HTTPS和适当的认证机制
5. 如果兼容模式不工作，可以尝试使用原生API格式（设置 `USE_NATIVE_API=true`）
6. 高并发场景下，建议监控系统资源使用情况，必要时调整连接池参数

## 上传到 GitHub

### 快速开始

1. **安装 Git**（如果还没有）
   - Windows: 下载 https://git-scm.com/download/win
   - 或使用 GitHub Desktop: https://desktop.github.com/

2. **初始化仓库**
   ```bash
   git init
   git add .
   git commit -m "Initial commit: 阿里云百炼智能体API转发服务"
   ```

3. **在 GitHub 上创建仓库并推送**
   ```bash
   git remote add origin https://github.com/YOUR_USERNAME/YOUR_REPO_NAME.git
   git branch -M main
   git push -u origin main
   ```

详细步骤请参考项目中的 `GITHUB_SETUP.md` 文件。

## 许可证

MIT License - 详见 [LICENSE](LICENSE) 文件

## 贡献

欢迎提交 Issue 和 Pull Request！

## 相关链接

- [阿里云百炼智能体API文档](https://bailian.console.aliyun.com/?spm=5176.12818093_47.resourceCenter.1.5dcd2cc9LBm5ZF&tab=doc#/doc/?type=app&url=2881515)
- [OpenAI API文档](https://platform.openai.com/docs/api-reference/chat)

## 参考文档

- [阿里云百炼智能体API文档](https://bailian.console.aliyun.com/?spm=5176.12818093_47.resourceCenter.1.5dcd2cc9LBm5ZF&tab=doc#/doc/?type=app&url=2881515)
- [OpenAI API文档](https://platform.openai.com/docs/api-reference/chat)

