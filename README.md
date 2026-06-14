# Echo Core

> 基于 Gin + GORM + 多 Agent 编排 + RAG 的虚拟陪伴平台。

Echo Core 是一个虚拟陪伴平台。

---

## 一、核心特性

- **多厂商 LLM 兼容**：默认通过 OpenAI 兼容协议接入 `MiniMax\Qwen\Gemin`，可在请求级覆盖。
- **多 Agent 编排（ReAct）**：内置 `默认 Agent` 与 `搜索 Agent`，基于 ReAct 循环自动决定是否调用工具。
- **工具调用（Function Calling）**：支持把任意 Go 函数注册为 Agent 工具，包括 RAG 检索、计算、查询时间等。
- **RAG 知识库**：通过 `echo-ai` Python 服务完成向量化入库与近邻检索，返回带下载链接的命中结果。
- **多层次记忆系统**
  - 短期记忆：`session_message`（按 session + user 隔离）
  - 长期记忆：`user_memory`（用户级偏好/知识）
  - 对话摘要：当消息超过 20 条时自动生成 `conversation_summary`，用于压缩上下文
- **文件存储（七牛云）**：提供"获取上传 Token → 客户端直传 → 后端登记"的标准流程，并触发 RAG 入库。
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
│
├── service/                   # 业务服务层
│   ├── chat_service.go        # 核心：会话/记忆/Agent 调度
│   ├── file_service.go        # 七牛云上传 Token + 文件登记
│   ├── summarizer_service.go  # 自动摘要（消息数 > 20 触发）
│   └── request/               # 服务层入参 DTO
│
├── repository/                # 仓储层（GORM 封装）
│   ├── file_repository.go
│   └── memory_repository.go
│
├── models/                    # 数据库模型 / 领域实体
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

## 四、对话核心链路

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

## 五、本地启动

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

## 六、系统架构与流程

> 本节用图形把"系统怎么分层、请求怎么走完一条链路"画清楚。

### 1. 系统分层

```
                          ┌────────────────────────────────────┐
                          │        Client (Web / App)          │
                          │  HTTP · SSE · WebSocket            │
                          └────────────┬───────────────────────┘
                                       │
              ┌────────────────────────┼────────────────────────┐
              ▼                        ▼                        ▼
     ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────────┐
     │  /api/auth/*     │    │  /api/chat       │    │  /api/file  /dept    │
     │  用户与会话       │    │  POST(SSE)/WS/JSON│   │  /api/chat/memory    │
     └────────┬─────────┘    └────────┬─────────┘    └──────────┬───────────┘
              │                       │                           │
              ▼                       ▼                           ▼
        ┌─────────────────────────────────────────────────────────────────┐
        │             Gin Router (routes/router.go)                        │
        └─────────────────────────────────────────────────────────────────┘
                                       │
              ┌────────────────────────┼────────────────────────┐
              ▼                        ▼                        ▼
     ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────────┐
     │   UserHandler    │    │ ChatStreamHandler│    │ FileHandler / Dept   │
     │                  │    │   + ChatHandler  │    │                      │
     └────────┬─────────┘    └────────┬─────────┘    └──────────┬───────────┘
              │                       │                           │
              ▼                       ▼                           ▼
     ┌──────────────────┐    ┌──────────────────┐    ┌──────────────────────┐
     │  UserService     │    │   ChatService    │    │  FileService         │
     │  SessionStore    │    │  ┌────────────┐  │    │  Tx{ DB + /ingest } │
     │ (内存,可换 Redis) │    │  │MemorySvc   │  │    │                      │
     └────────┬─────────┘    │  │Summarizer  │  │    └──────────┬───────────┘
              │              │  │PromptCache │  │               │
              │              │  │Orchestrator│  │               │
              │              │  └─────┬──────┘  │               │
              │              └────────┼─────────┘               │
              │                       │                         │
              │                       ▼                         │
              │              ┌──────────────────┐               │
              │              │  Agent (ReAct)   │               │
              │              │  default / search│               │
              │              │  ReActEngine     │               │
              │              │  Tools + RAG     │               │
              │              └────────┬─────────┘               │
              │                       │                         │
              │       ┌───────────────┼───────────────┐         │
              │       ▼               ▼               ▼         │
              │  ┌──────────┐    ┌──────────┐    ┌──────────┐  │
              │  │ AI Client│    │ RAGClient│    │  七牛 OSS │  │
              │  │(OpenAI   │    │ echo-ai  │    │ upload + │  │
              │  │ compat)  │    │ (Python) │    │ download │  │
              │  └────┬─────┘    └────┬─────┘    └────┬─────┘  │
              │       │               │               │         │
              └───────┴───────┬───────┴───────┬───────┘         │
                              │               │                 │
                              ▼               ▼                 ▼
                    ┌─────────────────────────────────────────────────┐
                    │       Repository (GORM) → MySQL                 │
                    │  user / session_message / user_memory /        │
                    │  conversation_summary / file / department /    │
                    │  agent_config                                   │
                    └─────────────────────────────────────────────────┘
```

### 2. 对话主链路（SSE 流式）

```
Client ──POST /api/chat──▶ ChatStreamHandler
                                │
                                ▼
                           ChatService.ChatStream
                                │
   ┌────────────────────────────┴────────────────────────────┐
   │ ① MemorySvc.BuildMemoryContext   → 长期记忆           │
   │ ② Summarizer.GetSummaryMetaLight → 1 次轻量查询        │
   │ ③ SaveSessionMessage(user)        → 先存本轮          │
   │ ④ GetSessionMessages(window+1)    → 历史              │
   │ ⑤ ShouldSummarize → GenerateSummary(旧+新) → Upsert  │
   │ ⑥ BuildContext = [System+Memory+Summary] + 滑动窗口   │
   │ ⑦ PromptCache.Get/Set (sha1 前缀)                     │
   └────────────────────────────┬────────────────────────────┘
                                ▼
                MultiAgentOrchestrator.RunStream
                                │
                ┌───────────────┴───────────────┐
                │ RouteAgent → routeWithLLM     │
                │ (function-calling 强制选 Agent) │
                └───────────────┬───────────────┘
                                ▼
                  ReActEngine.ExecuteStream (≤10 步)
                  ┌─────────┴─────────┐
                  │ ChatStreamWithTools
                  │  ├─ onContent  → SSE delta
                  │  └─ tool_calls → onToolEvent
                  │     → 执行 tool → tool 消息回填
                  │  → 继续直到无 tool_calls
                  └─────────┬─────────┘
                                ▼
   ┌────────────────────────────┴────────────────────────────┐
   │ ⑧ SaveSessionMessage(assistant)                          │
   │ ⑨ MemoryService.ExtractAsync (异步抽长期记忆)            │
   └────────────────────────────┬────────────────────────────┘
                                ▼
        SSE 帧: start → delta*N → tool_call?/tool_result? → finish
```

### 3. RAG 检索（search Agent）

```
search_knowledge(query)
   │
   ▼
RAGClient.SearchKnowledge ──POST──▶ echo-ai /chat (Python)
                                          │
                                          ▼
                              向量检索 + 候选 metadata
                                          │
                          ◀──── candidates[].source_url
   │
   ▼
RAGClient.buildFullURL
   ├─ 已是 http(s)        → 原样
   ├─ 含 clouddn.com      → 加 http://
   └─ 其它                → 拼 QINIU_DOMAIN
   │
   ▼
"文件: name, 下载链接: url" → 注入给 LLM → 整理回复
```

### 4. 文件入库（DB 事务）

```
Client ──POST /api/file/token──────▶ FileService.GetUploadToken
                                          │ 生成 key
                                          ▼
                                    { token, uploadURL, key }
                                          │
Client ──直传─────────────────────────────────────▶ 七牛 OSS
                                          │
Client ──POST /api/file/register──▶ FileService.RegisterFile (Tx)
                                          │ ① Insert file row
                                          │ ② POST /ingest_file
                                          │ ③ Commit / Rollback
                                          ▼
                                    文件被 search Agent 检索到
```

### 5. 用户与会话

```
Client ──POST /api/auth/login──▶ UserService.Login
                                      │ ① GetByUsername
                                      │ ② IsEnabled
                                      │ ③ VerifyPassword (bcrypt+salt)
                                      │ ④ SessionStore.Create (24h)
                                      │ ⑤ UpdateLastLogin
                                      ▼
                              { sessionId, expireAt, user }

Client ──POST /api/auth/check──▶ UserService.CheckSession
                                      │ SessionStore.Get + Touch
                                      ▼
                              { valid, username, userId, expireAt }
```

### 6. 上下文工程（Prefix Cache 友好）

```
请求进入
  ↓
[System Prompt]   Agent 自身的 system
[Memory]          MemoryService.BuildMemoryContext(userId)   ← 变化时 key 变
[Summary]         Summarizer.BuildContext 注入摘要             ← 变化时 key 变
[Sliding Window]  最近 20 条消息 (role/content/tool_call_id 完整)
[Current Msg]     本轮 user 消息
  ↓
拼出 messages
  ↓
PromptCacheKey(user, session, sha1(memCtx), summary.UpdatedAt, model)
  ↓
Get(key) → HIT  复用 prefix
       → MISS  重新拼 prefix 并 Set(key, content, 5min TTL)
```

### 7. 当前主要问题与低成本方案

> 完整论证见 [`ARCHITECTURE.md` §四](./ARCHITECTURE.md)。

| 优先级 | 问题 | 业界做法 | 本系统低成本方案 |
| --- | --- | --- | --- |
| **P0** | 路由 LLM 多余往返 (~300ms) | 规则 + 缓存兜底 | 路由结果分钟级缓存, 未命中再走 LLM |
| **P0** | 长期记忆去重粒度粗 (Contains) | 向量召回 + LLM 二次判重 | 复用 `GetTextEmbedding`, Top 3 候选 + LLM 判同义 |
| **P1** | Session / Cache 内存版不支持多实例 | Redis 共享状态 | `RedisPromptCache` 接口已占位, 引入 `go-redis` 即用 |
| **P1** | 摘要触发间隔固定 20 条 | 动态 token 阈值 | 累计 token 超过上份摘要 1.2× 再触发 |
| **P2** | WebSocket 无事件序号, 无法断点续传 | `seq` + `resume` | `WSOutgoingMessage` 加 `seq`, 重连按 lastSeq 重发 |
| **P2** | 工具调用无超时/重试 | 指数退避 + fallback | `withRetry(handler, max=2)`, 超时 5s 终止本轮 ReAct |
| **P3** | 缺可观测埋点 | Prometheus 指标 | 暴露 5~6 个 counter/histogram, `GET /metrics` 即可 |



