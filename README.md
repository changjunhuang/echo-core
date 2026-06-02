# Echo Core

> 基于 Gin + GORM + 多 Agent 编排 + RAG 的对话与知识库一体化后端服务。

Echo Core 是一个面向"对话式 AI 应用"的基础后端，向上对接任意 **OpenAI 兼容**的大模型服务，向下对接 **MySQL** 做业务/记忆持久化，对外提供 **七牛云** 文件存储接入，并通过内置的 **Python RAG 服务（echo-ai）** 接入向量知识库。系统内置基于 ReAct 模式的多 Agent 编排器，支持工具调用、短期/长期记忆、自动摘要等能力。

---

## 一、核心特性

- **多厂商 LLM 兼容**：默认通过 OpenAI 兼容协议接入 `SiliconFlow / OpenAI / DeepSeek / Gemini / 通义千问 / 自定义厂商`，可在请求级覆盖。
- **多 Agent 编排（ReAct）**：内置 `默认 Agent` 与 `搜索 Agent`，基于 ReAct 循环自动决定是否调用工具。
- **工具调用（Function Calling）**：支持把任意 Go 函数注册为 Agent 工具，包括 RAG 检索、计算、查询时间等。
- **RAG 知识库**：通过 `echo-ai` Python 服务完成向量化入库与近邻检索，返回带下载链接的命中结果。
- **多层次记忆系统**
  - 短期记忆：`session_message`（按 session + user 隔离）
  - 长期记忆：`user_memory`（用户级偏好/知识）
  - 对话摘要：当消息超过 20 条时自动生成 `conversation_summary`，用于压缩上下文
- **文件存储（七牛云）**：提供"获取上传 Token → 客户端直传 → 后端登记"的标准流程，并触发 RAG 入库。
- **业务 CRUD 示例**：内置 `Department` 模块，作为标准 GORM CRUD 模板。
- **结构化日志**：所有核心链路带 `log.Printf` 流水日志，便于排查。

---

## 二、技术栈

| 类别       | 选型                                                  |
| ---------- | ----------------------------------------------------- |
| Web 框架   | Gin `v1.12.0`                                         |
| ORM        | GORM `v1.31.1` + MySQL Driver `v1.6.0`                |
| 对象存储   | 七牛云 Go SDK `v7.26.4`                               |
| 配置       | `github.com/joho/godotenv`                            |
| 唯一 ID    | `github.com/google/uuid`                              |
| AI 协议    | OpenAI 兼容 `chat/completions` + `embeddings`         |
| 知识库     | 外部 Python 服务 `echo-ai`（`/ingest_file`、`/embedding`、`/text-embedding`、`/chat`） |

---

## 三、目录结构

```
echo-core/
├── main.go                    # 入口：加载 .env、初始化 DB、注册路由、启动 Gin
├── go.mod / go.sum
├── .env                       # 本地环境变量（不入库实际凭据）
│
├── config/                    # 全局配置
│   └── database.go            # MySQL 初始化 + 自动迁移
│
├── routes/                    # 路由注册
│   └── router.go              # 聚合 department / file / chat 三个子路由
│
├── handlers/                  # HTTP 处理器
│   ├── chat_handler.go
│   ├── file_handler.go
│   └── department_handle.go
│
├── service/                   # 业务服务层
│   ├── chat_service.go        # 核心：会话/记忆/Agent 调度
│   ├── file_service.go        # 七牛云上传 Token + 文件登记
│   ├── department_service.go
│   ├── summarizer_service.go  # 自动摘要（消息数 > 20 触发）
│   └── request/               # 服务层入参 DTO
│
├── repository/                # 仓储层（GORM 封装）
│   ├── department_repository.go
│   ├── file_repository.go
│   └── memory_repository.go
│
├── models/                    # 数据库模型 / 领域实体
│   ├── department.go
│   ├── file.go
│   └── memory.go              # SessionMessage / UserMemory / ConversationSummary / AgentConfig
│
├── dto/                       # 对外 DTO（请求/响应）
│
├── remote/                    # 外部服务 HTTP 客户端
│   ├── ai_client.go           # OpenAI 兼容 Chat / Stream / Embedding / Summary
│   ├── vector_remote.go       # echo-ai（Python RAG）客户端
│   ├── request/               # 远程请求 DTO
│   └── response/              # 远程响应 DTO
│
├── agent/                     # 多 Agent 与工具链
│   ├── react_engine.go        # ReAct 引擎 + MultiAgentOrchestrator
│   └── tools.go               # 内置工具（默认 / 搜索 RAG）+ RAG 客户端
│
├── utils/
│   └── system_config.go       # GetEnv / GetEnvFirst
│
└── web/                       # 前端资源占位目录
```

---

## 四、数据模型

| 表名                  | 作用             | 关键字段                                                                                |
| --------------------- | ---------------- | --------------------------------------------------------------------------------------- |
| `Department`          | 部门（CRUD 示例） | id, name, create_time, update_time                                                     |
| `file`                | 文件元数据        | id, name, user_id, key, file_type (1 文本/2 图片/3 视频/4 音频), biz_type, status, config (JSON) |
| `session_message`     | 短期记忆          | id, session_id, user_id, role, content, tool_calls, tool_result, created_at           |
| `user_memory`         | 长期记忆          | id, user_id, memory_type (preference/info/summary/knowledge), content, embedding, ...  |
| `conversation_summary`| 对话摘要          | id, session_id, user_id, summary, created_at                                           |
| `agent_config`        | Agent 配置（预留）| id, agent_type, name, description, tools (JSON), prompt, is_default                    |

迁移由 `config/database.go::autoMigrate` 在启动时自动执行。

---

## 五、配置说明（.env）

```env
# 应用
APP_PORT=8080
APP_MODE=debug
JWT_SECRET=your-secret-key

# MySQL
DB_HOST=...
DB_PORT=3306
DB_USER=root
DB_PASSWORD=...
DB_NAME=...

# 七牛云
QINIU_ACCESS_KEY=...
QINIU_SECRET_KEY=...
QINIU_BUCKET_NAME=...
QINIU_DOMAIN=...

# LLM（OpenAI 兼容协议）
LLM_PROVIDER=qwen                          # 当前默认走通义千问
LLM_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
LLM_MODEL=qwen3.7-max
LLM_API_KEY=sk-...

# echo-ai（Python RAG 服务）远程地址
ECHO_AI_REMOTE_BASE_URL=http://localhost:8000
```

### LLM 厂商切换示例

| 厂商       | Provider 配置            | 备注                            |
| ---------- | ------------------------ | ------------------------------- |
| 通义千问   | `LLM_PROVIDER=qwen`      | 默认配置                        |
| SiliconFlow | `LLM_PROVIDER=siliconflow` + `SILICONFLOW_API_KEY` | 兼容 OpenAI 端点              |
| OpenAI     | `LLM_PROVIDER=openai` + `OPENAI_API_KEY`            |                                |
| DeepSeek   | `LLM_PROVIDER=deepseek` + `DEEPSEEK_API_KEY`        |                                |
| Gemini     | `LLM_PROVIDER=gemini` + `GEMINI_API_KEY`            | OpenAI 兼容端点                |
| 自定义     | `LLM_PROVIDER=custom` + `LLM_API_KEY` + `LLM_BASE_URL` | 任意 OpenAI 兼容服务       |

### 环境变量优先级

1. **请求体**中的 `apiKey` / `baseUrl` / `model` / `provider`（单次覆盖）
2. `.env` 中的 `LLM_API_KEY` / `LLM_BASE_URL` / `LLM_MODEL` / `LLM_PROVIDER`
3. 当前 Provider 对应的专用 Key（如 `SILICONFLOW_API_KEY`、`OPENAI_API_KEY` ...）

> 注：当前路由层 `routes/router.go::chatRegisterRoutes` 中只读取 `LLM_BASE_URL / LLM_API_KEY / LLM_MODEL` 来构建默认 AI 客户端，单次请求级的 Provider 切换可在 `service/chat_service.go` 内扩展使用。

---

## 六、API 概览

所有接口统一以 `/api` 为前缀，根路径 `GET /` 返回 `Hello from Gin!`。

### 1. 部门（CRUD 示例）

| Method | Path                       | 说明           |
| ------ | -------------------------- | -------------- |
| GET    | `/api/department`          | 分页查询       |
| POST   | `/api/department`          | 创建部门       |
| GET    | `/api/department/:id`      | 详情           |
| PUT    | `/api/department/:id`      | 更新           |
| DELETE | `/api/department/:id`      | 删除           |

请求/响应 DTO 见 `dto/department_dto.go`。

### 2. 文件（七牛云 + RAG 入库）

| Method | Path                  | 说明                                 |
| ------ | --------------------- | ------------------------------------ |
| POST   | `/api/file/token`     | 获取七牛云上传 Token（key 由后端生成） |
| POST   | `/api/file/register`  | 客户端上传完成后调用，登记文件元数据并触发 RAG 入库 |

**Token 请求**：

```json
{ "fileName": "doc.pdf", "fileSize": 1024, "mimeType": "application/pdf", "bizType": "knowledge" }
```

**登记请求**：

```json
{
  "userId": "u_001",
  "fileName": "doc.pdf",
  "key": "knowledge/20260602/xxx.pdf",
  "fileType": 1,
  "bizType": 0
}
```

> `RegisterFile` 内部使用 MySQL 事务：先写 `file` 记录，再调用 `echo-ai /ingest_file` 推送向量化任务，任意一步失败整体回滚。

### 3. 对话 / 记忆 / Agent

| Method | Path                  | 说明                            |
| ------ | --------------------- | ------------------------------- |
| POST   | `/api/chat`           | 单次/多轮对话                  |
| GET    | `/api/chat/history`   | 查询会话历史                    |
| GET    | `/api/chat/summary`   | 获取当前会话摘要                |
| GET    | `/api/chat/memory`    | 获取用户长期记忆                |
| POST   | `/api/chat/memory`    | 保存/更新用户长期记忆           |
| GET    | `/api/chat/agents`    | 获取已注册 Agent 名称列表       |
| DELETE | `/api/chat/session`   | 清空会话消息（保留摘要）        |

**`POST /api/chat` 请求体**：

```json
{
  "userId":    "u_001",
  "sessionId": "s_001",
  "message":   "介绍一下我们公司的报销流程",
  "provider":  "siliconflow",          // 可选：覆盖默认厂商
  "model":     "deepseek-ai/DeepSeek-V3",  // 可选：覆盖默认模型
  "baseUrl":   "https://api.siliconflow.cn/v1",  // 可选
  "apiKey":    "sk-runtime-xxx"        // 可选
}
```

**响应体**：

```json
{
  "code": 200,
  "data": {
    "reply":     "...",
    "session_id":"s_001"
  }
}
```

**`POST /api/chat/memory` 请求体**：

```json
{ "userId": "u_001", "type": "preference", "content": "用户偏好简洁回答" }
```

---

## 七、对话核心链路

```
客户端
  │  POST /api/chat
  ▼
ChatHandler
  │  解析 userId / sessionId / message
  ▼
ChatService.Chat
  │  1) MemoryRepository.GetSessionMessages → 历史 100 条
  │  2) Summarizer.ShouldSummarize (>20) → 自动调 LLM 生成 summary 并落库
  │  3) 追加当前 user 消息
  ▼
MultiAgentOrchestrator.Orchestrate
  │  按关键词路由到「default」/「search」Agent
  ▼
ReActEngine.Execute (max 10 步)
  │  loop:
  │    - AIClient.Chat(messages, tools)        # OpenAI 兼容 /chat/completions
  │    - 若返回 tool_calls → 解析 → 执行工具 → 把结果作为 tool 消息回填
  │    - 若返回 content    → 终止并返回
  ▼
ChatService.Chat
  │  4) 持久化 user 消息 & assistant 回复
  ▼
返回 reply
```

### 路由规则（`MultiAgentOrchestrator.routeToAgent`）

匹配以下任一关键词（中英文）即路由到 `search` Agent，否则路由到 `default` Agent：

`搜索 / 查找 / 查询 / 信息 / 知道 / 了解 / 介绍 / 什么 / 如何 / 怎么 / 为什么 / 哪里 / 多少 / search / find / query / info`

### 内置 Agent 工具集

| Agent     | 工具                                          | 说明                                                                 |
| --------- | --------------------------------------------- | -------------------------------------------------------------------- |
| `default` | `get_weather` / `calculate` / `get_time`      | 通用工具示例（天气/计算/当前时间）                                   |
| `search`  | `search_knowledge` / `web_search`             | 知识库优先（调用 echo-ai `/chat`），未命中再考虑外部网络（当前为占位）|

工具扩展只需在 `agent/tools.go` 中新增 `Tool{ Name, Description, Parameters, Handler }` 并注册到对应 Agent。

---

## 八、RAG 知识库（echo-ai 对接）

`service/chat_service.go` 启动时构造：

```go
ragBaseURL := "http://localhost:8000"
ragDomain  := utils.GetEnv("QINIU_DOMAIN", "tfpdkiq9g.hn-bkt.clouddn.com")
svc.ragClient = agent.NewRAGClientWithDomain(ragBaseURL, "", ragDomain)
```

依赖的 Python 服务接口：

| Endpoint           | 用途                                                | 调用方                                       |
| ------------------ | --------------------------------------------------- | -------------------------------------------- |
| `/ingest_file`     | 上传文件后触发向量化入库                             | `FileService.RegisterFile`                  |
| `/embedding`       | 图片向量化（multipart 上传）                          | `VectorRemote.GetImageEmbedding`            |
| `/text-embedding`  | 文本向量化                                           | `VectorRemote.GetTextEmbedding`             |
| `/chat`            | 近邻检索（返回带 metadata 的候选）                    | `RAGClient.SearchKnowledge`（search Agent） |

`RAGClient.buildFullURL` 会把 `source_url` 拼成可下载的七牛云链接，搜索结果以 `文件: name, 下载链接: url` 的形式回传给模型。

---

## 九、本地启动

```bash
# 1. 安装依赖
go mod tidy

# 2. 配置 .env（参考第五节）

# 3. 启动 echo-ai（Python RAG 服务，需自行准备，默认 8000 端口）

# 4. 启动服务
go run main.go
# 或直接运行已编译产物
./echo-core.exe
```

默认监听 `:8080`（`APP_PORT`）。GORM 会在首次启动时自动建表。

---

## 十、典型交互示例

```bash
# 1. 获取上传 Token
curl -X POST http://localhost:8080/api/file/token \
  -H 'Content-Type: application/json' \
  -d '{"fileName":"a.pdf","fileSize":1024,"mimeType":"application/pdf","bizType":"knowledge"}'

# 2. 客户端拿到 token + key 后直传七牛云（前端 SDK）

# 3. 通知后端登记并入库 RAG
curl -X POST http://localhost:8080/api/file/register \
  -H 'Content-Type: application/json' \
  -d '{"userId":"u_001","fileName":"a.pdf","key":"knowledge/20260602/<uuid>.pdf","fileType":1,"bizType":0}'

# 4. 多轮对话
curl -X POST http://localhost:8080/api/chat \
  -H 'Content-Type: application/json' \
  -d '{"userId":"u_001","sessionId":"s_001","message":"总结一下我刚才上传的 PDF"}'

# 5. 保存用户偏好
curl -X POST http://localhost:8080/api/chat/memory \
  -H 'Content-Type: application/json' \
  -d '{"userId":"u_001","type":"preference","content":"希望回答简洁、列点输出"}'
```





