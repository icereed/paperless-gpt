# Chatanywhere Provider 配置指南

Chatanywhere 是一个 OpenAI API 转发服务，提供国内可用的 API 访问。

## 配置步骤

### 1. 环境变量配置

在 `docker-compose.yml` 中修改以下配置：

```yaml
environment:
  # 将 LLM Provider 改为 chatanywhere
  LLM_PROVIDER: "chatanywhere"
  
  # 设置模型（支持所有 OpenAI 模型）
  LLM_MODEL: "gpt-4o"  # 或 gpt-4-turbo, gpt-3.5-turbo 等
  
  # Chatanywhere API Key（必填）
  CHATANYWHERE_API_KEY: "your-chatanywhere-api-key-here"
  
  # Base URL（可选，默认国内地址）
  CHATANYWHERE_BASE_URL: "https://api.chatanywhere.tech/v1"  # 国内首选
  # 或使用国外地址：
  # CHATANYWHERE_BASE_URL: "https://api.chatanywhere.org/v1"
```

### 2. 使用 .env 文件（推荐）

创建 `.env` 文件：

```bash
CHATANYWHERE_API_KEY=your-chatanywhere-api-key-here
```

然后在 `docker-compose.yml` 中引用：

```yaml
CHATANYWHERE_API_KEY: "${CHATANYWHERE_API_KEY}"
```

### 3. 重启服务

```bash
docker-compose up -d --no-deps --force-recreate paperless-gpt
```

## 可用端点

- **国内首选**: `https://api.chatanywhere.tech/v1` （默认）
- **国外使用**: `https://api.chatanywhere.org/v1`

## 支持的模型

所有 OpenAI 模型，包括但不限于：
- `gpt-4o`
- `gpt-4-turbo`
- `gpt-4`
- `gpt-3.5-turbo`
- `gpt-3.5-turbo-16k`

## 注意事项

1. Chatanywhere 使用 OpenAI 兼容接口，所以配置方式与 OpenAI 相同
2. 确保你的 API Key 有效且有足够的额度
3. 如果遇到网络问题，可以尝试切换 Base URL（国内/国外）
4. 该服务由第三方提供，使用前请了解其服务条款

## 完整配置示例

```yaml
paperless-gpt:
  image: paperless-gpt:local
  environment:
    # Paperless 集成
    PAPERLESS_BASE_URL: "http://paperless-ngx:8000"
    PAPERLESS_API_TOKEN: "your-paperless-token"
    
    # LLM 配置
    LLM_PROVIDER: "chatanywhere"
    LLM_MODEL: "gpt-4o"
    CHATANYWHERE_API_KEY: "${CHATANYWHERE_API_KEY}"
    CHATANYWHERE_BASE_URL: "https://api.chatanywhere.tech/v1"
    
    # 其他配置...
```
