# go-start

## LLM 接入说明

当前项目的 `service/AgentService.go` 已支持 **OpenAI 兼容接口** 的多厂商接入，默认优先兼容：

- SiliconFlow
- OpenAI
- DeepSeek
- Gemini（OpenAI 兼容端点）

## 当前默认配置

项目的 `.env` 已默认切到 SiliconFlow：

```env
LLM_PROVIDER=siliconflow
LLM_BASE_URL=https://api.siliconflow.cn/v1
LLM_MODEL=deepseek-ai/DeepSeek-V3
SILICONFLOW_API_KEY=your_siliconflow_key
```

### 推荐环境变量

优先使用通用配置：

- `LLM_PROVIDER`
- `LLM_API_KEY`
- `LLM_BASE_URL`
- `LLM_MODEL`

### 常见厂商示例

#### 1. SiliconFlow

```env
LLM_PROVIDER=siliconflow
SILICONFLOW_API_KEY=your_siliconflow_key
LLM_BASE_URL=https://api.siliconflow.cn/v1
LLM_MODEL=deepseek-ai/DeepSeek-V3
```

#### 2. OpenAI

```env
LLM_PROVIDER=openai
OPENAI_API_KEY=your_openai_key
LLM_MODEL=gpt-4o-mini
```

#### 3. DeepSeek

```env
LLM_PROVIDER=deepseek
DEEPSEEK_API_KEY=your_deepseek_key
LLM_MODEL=deepseek-chat
```

#### 4. 自定义兼容厂商

```env
LLM_PROVIDER=custom
LLM_API_KEY=your_key
LLM_BASE_URL=https://your-provider.example.com/v1
LLM_MODEL=your-model-name
```

## /api/chat 请求格式

### 1. 继续使用默认配置

```json
{
  "message": "这张图片里是什么内容？"
}
```

### 2. 单次请求覆盖模型供应商或模型名

```json
{
  "message": "根据图片信息帮我总结一下",
  "provider": "siliconflow",
  "model": "deepseek-ai/DeepSeek-V3"
}
```

### 3. 单次请求切换到其他 OpenAI 兼容厂商

```json
{
  "message": "识别这张图片并回答我",
  "provider": "custom",
  "base_url": "https://your-provider.example.com/v1",
  "model": "your-model-name",
  "api_key": "your_runtime_key"
}
```

请求字段说明：

- `message`：用户问题，必填
- `provider`：可选，当前请求使用的模型供应商
- `model`：可选，当前请求使用的模型名
- `base_url`：可选，当前请求使用的 OpenAI 兼容接口地址
- `api_key`：可选，当前请求使用的 API Key

如果这些字段不传，系统会回退到 `.env` 中的默认配置。

## 环境变量优先级

1. 请求体中的 `api_key` / `base_url` / `model` / `provider`
2. `.env` 中的 `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` / `LLM_PROVIDER`
3. 当前 provider 对应的专用 key，例如：
   - `SILICONFLOW_API_KEY`
   - `OPENAI_API_KEY`
   - `DEEPSEEK_API_KEY`
   - `GEMINI_API_KEY`

## 当前问答链路

1. 用户请求进入 `/api/chat`
2. 服务先到 Weaviate 做 `NearText` 检索
3. 检索结果作为上下文传给大模型
4. 大模型组织自然语言答案返回
5. 如果大模型暂时不可用，会自动降级为候选图片列表

