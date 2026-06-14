# Echo Core · 架构设计文档

## 1. 系统概览

### 1.1 定位与边界

**Echo Core** 是一个虚拟陪伴平台。

### 1.2 外部依赖一览

| 依赖                | 角色                              | 协议 / 形态          | 故障容忍策略             |
| ------------------- | --------------------------------- | -------------------- | ------------------------ |
| **MySQL 8.x**       | 业务/记忆/摘要/用户/文件元数据持久化 | GORM + 驱动          | 唯一强依赖；无降级        |
| **七牛云 OSS**      | 文件直传 + 公开/私有下载链接        | 七牛 Go SDK          | 上传失败重试 3 次；下载拼 URL 失败容错（直接传 source_url）|
| **LLM（OpenAI 兼容）** | 文本生成 / 摘要 / Embedding       | HTTPS + SSE          | 上游超时 120s；客户端无限流控 |
| **echo-ai（Python）** | 向量化入库 + RAG 检索              | HTTP JSON            | 检索失败→`search_knowledge` 工具降级文本；入库失败→事务回滚 |
| **Session / Cache** | 会话、前缀缓存                     | 进程内（接口预留 Redis）| 重启即丢失；接受         |

### 1.3 关键非功能约束

| 约束       | 现状                                       | 演进目标                  |
| ---------- | ------------------------------------------ | ------------------------- |
| 并发       | 单实例 ~百 QPS（取决于 LLM 与 echo-ai）     | 引入 Redis 共享状态后多实例 |
| 时延       | 首字 1~2s（含 LLM TTFB + 工具调用）         | 路由缓存命中后省 300ms     |
| 持久化     | 全部落 MySQL；Session 内存                  | Session 切 Redis         |
| 可观测     | `log.Printf` 流水                           | Prometheus 指标 + Trace  |
| 安全       | bcrypt+salt、账号枚举防护、登录失败有日志   | 限流 / 防注入 / Token 鉴权 |
| 部署       | 单二进制 + `.env`                           | Docker + CI               |

---

## 2. 分层架构

### 2.1 分层总览

```
┌────────────────────────────────────────────────────────────────────────────┐
│  L1  接入层   Gin Router · 4 个子路由组 (auth / chat / file / department)   │
├────────────────────────────────────────────────────────────────────────────┤
│  L2  适配层   Handlers (ChatHandler / ChatStreamHandler / UserHandler …)    │
│               · 协议解析 (HTTP/SSE/WS)                                       │
│               · 入参校验                                                     │
│               · 响应序列化                                                    │
├────────────────────────────────────────────────────────────────────────────┤
│  L3  服务层   Services                                                       │
│               · ChatService     主对话编排                                   │
│               · Summarizer      增量摘要 + 滑动窗口                          │
│               · MemoryService   长期记忆抽取/合并                            │
│               · PromptCache     前缀缓存（内存 + Redis 占位）                 │
│               · UserService     账号 / 登录 / 会话                          │
│               · FileService     七牛 token + 入库事务                        │
├────────────────────────────────────────────────────────────────────────────┤
│  L4  编排层   Agent (ReActEngine + MultiAgentOrchestrator + Tools)          │
│               · Agent 路由（routeWithLLM）                                   │
│               · ReAct 循环（执行 / 流式）                                     │
│               · 工具调用治理                                                  │
├────────────────────────────────────────────────────────────────────────────┤
│  L5  IO 适配  remote/ (ai_client / vector_remote) + 七牛 SDK                 │
│               · OpenAI 兼容 chat / stream / stream+tools / embedding        │
│               · echo-ai HTTP 调用                                            │
├────────────────────────────────────────────────────────────────────────────┤
│  L6  持久层   repository/ (GORM) → models/ → MySQL                          │
│               · AutoMigrate                                                   │
│               · 连接池 10/100 / 1h                                           │
└────────────────────────────────────────────────────────────────────────────┘
```

### 2.2 调用方向

- **请求方向**：L1 → L2 → L3 → L4 → L5 → L6
- **响应方向**：L6 → L5 → L4 → L3 → L2 → L1
- **跨层访问约束**（业界通用原则）：
  - L1 不直接调 L3+（避免绕过适配层）
  - L3 可调 L4、L5、L6，但**不可直接调 L2**（适配层无状态外泄）
  - L4 仅依赖 L5（外部 IO），不直接读 DB（需要持久化时通过 L3 走）

### 2.3 各层职责与可替换性

| 层       | 关键类型 / 函数                                | 可替换性                          | 替换示例                                  |
| -------- | --------------------------------------------- | --------------------------------- | ----------------------------------------- |
| L1 路由  | `routes/router.go::SetupRoutes`                | 中（换 Web 框架时整段重写）        | gin → echo / chi                          |
| L2 适配  | `handlers/*`                                  | 中（每个 handler 独立）            | 拆出 `chat_stream_handler.go` 即可独立维护 |
| L3 服务  | `service/ChatService` 等                       | 高（接口稳定）                     | 替换为不同业务编排策略                    |
| L4 编排  | `agent.MultiAgentOrchestrator` / `ReActEngine` | 高（Agent 协议与 Engine 解耦）     | 替换为 LangGraph / 状态机                  |
| L5 IO    | `remote.AIClient`                              | 高（OpenAI 兼容协议）              | 接入 Claude / Gemini 同协议                 |
| L6 持久  | `repository.MemoryRepository` 等               | 高（GORM 抽象）                    | 切 PostgreSQL / 加 Redis 缓存              |

### 2.4 核心抽象

```go
// agent.Tool — 工具的最小契约
type Tool struct {
    Name        string
    Description string
    Parameters  map[string]interface{}                  // OpenAI 兼容 schema
    Handler     func(params map[string]interface{}) (string, error)
}

// service.PromptCache — 前缀缓存抽象
type PromptCache interface {
    Get(key string) (string, bool)
    Set(key, value string, ttl time.Duration)
    Del(key string)
    Stats() CacheStats
}

// utils.SessionStore — 会话存储抽象
type SessionStore interface {
    Create(userID uint, username string, ttl time.Duration) (*Session, error)
    Get(sessionID string) (*Session, error)
    Touch(sessionID string) error
    Delete(sessionID string) error
}
```

这三个接口是系统的"扩展点"：新增工具、引入 Redis 缓存、切换分布式会话都从这些接口切入。

---

## 3. 端到端流程

### 3.1 对话主链路（POST /api/chat · SSE）

**代码位置**：`handlers/chat_stream_handler.go::ChatHandleSSE` → `service/chat_service.go::ChatStream` → `agent/react_engine.go::ExecuteStream`

```
Client ──POST /api/chat { userId, sessionId, message, … }──▶ Gin Router
                                                                  │
                          ┌───────────────────────────────────────┘
                          ▼
              ChatStreamHandler.ChatHandleSSE
                          │ 1) ShouldBindJSON → service.ChatRequest
                          │ 2) 校验 userId/sessionId/message 非空
                          │ 3) 设置 SSE 响应头 (text/event-stream)
                          │ 4) writeSSEEvent("start", {sessionId})
                          │ 5) 拿 http.Flusher
                          ▼
              ChatService.ChatStream (req, onChunk)
                          │
   ┌──────────────────────┴──────────────────────┐
   │ A. 上下文准备                                │
   │   ① MemoryService.BuildMemoryContext        │  ← 跨会话长期记忆
   │   ② Summarizer.GetSummaryMetaLight (1 次)   │  ← 复用 prefetch
   │   ③ SaveSessionMessage (user 消息)          │  ← 先存,BuildContext 不漏
   │   ④ GetSessionMessages (window+1)            │  ← 限速到 windowSize
   │   ⑤ ShouldSummarize → GenerateSummary       │  ← 旧摘要+新增消息
   │   ⑥ Summarizer.BuildContext                 │  ← prefix + sliding window
   │   ⑦ PromptCache.Get/Set (key 涵盖全因子)      │
   └──────────────────────┬──────────────────────┘
                          ▼
              MultiAgentOrchestrator.RunStream
                          │ RouteAgent → routeWithLLM
                          │   (构造 select_agent 工具, tool_choice=required)
                          ▼
              Agent.RunStream → ReActEngine.ExecuteStream (≤10 步)
                  loop:
                    ChatStreamWithTools (OpenAI 兼容 SSE)
                      ├─ onContent  → onChunk(StreamChunk{Delta,Reply})
                      └─ 累积 tool_calls (按 index 合并 arguments 跨帧)
                    if tool_calls:
                      for tc in toolCalls:
                        执行 tc.Handler(params)  → onChunk(StreamChunk{ToolResult})
                        把 tool 结果以 tool 角色回填 context
                      继续 loop
                    else:
                      break
                          │
   ┌──────────────────────┴──────────────────────┐
   │ B. 落库与记忆                                │
   │   ⑧ SaveSessionMessage (assistant)          │
   │   ⑨ MemoryService.ExtractAsync (goroutine)  │  ← 异步抽长期记忆
   └──────────────────────┬──────────────────────┘
                          ▼
              onChunk(StreamChunk{Done:true, Reply})
                          │
                          ▼
              writeSSEEvent("finish", {reply, sessionId})
                          │
                          ▼
                          Client
```

**SSE 事件序列**：

```
event: start        data: {"sessionId":"s_001"}
event: delta        data: {"delta":"今天", "reply":"今天"}
event: delta        data: {"delta":"天气", "reply":"今天天气"}
event: tool_call    data: {"id":"call_xxx", "function":{"name":"search_knowledge", "arguments":"..."}}
event: tool_result  data: {"id":"call_xxx", "name":"search_knowledge", "result":"..."}
event: delta        data: {"delta":"根据", "reply":"根据"}
... (直到 finish)
event: finish       data: {"reply":"完整回复", "sessionId":"s_001"}
```

### 3.2 WebSocket 全双工链路

**代码位置**：`handlers/chat_stream_handler.go::ChatHandleWS` → 复用 `ChatService.ChatStream`

```
Client ──GET /api/chat/ws──▶ WebSocket Upgrade (gorilla/websocket)
                                       │
                                       ▼
                          ReadMessage 循环
                                       │
                          解析 WSIncomingMessage { type, userId, sessionId, message }
                                       │
                          switch type:
                            "ping"  → WriteJSON({type:"pong", timestamp})
                            "chat"  → WriteJSON({type:"start"})
                                     → ChatStream(req, onChunk)
                                          ↓
                                     WriteJSON per StreamChunk
                                     (delta / tool_call / tool_result / finish / error)
                            default  → WriteJSON({type:"error", error:"unknown type"})
```

**与 SSE 区别**：SSE 是单向（服务端推），WebSocket 是全双工（客户端可随时发新消息，服务端并发推流）；两者在 service 层共用 `ChatStream` 回调，回调把每片 `StreamChunk` 序列化为各自协议的帧。

### 3.3 RAG 检索链路（search Agent）

**代码位置**：`agent/tools.go::SearchTools` → `agent/tools.go::RAGClient.SearchKnowledge` → 外部 `echo-ai /chat`

```
search Agent.RunStream (用户问题含 RAG/知识库/我的文件/… 关键词)
   │
   ▼
LLM 决定调用 search_knowledge(query)
   │  query 应当是检索目标（"黄色小狗图片"），不是修饰语（"在我的库中帮我找一下"）
   ▼
RAGClient.SearchKnowledge
   │ 构造 { messages:[{role,content:query}], query }
   │ POST {ECHO_AI_REMOTE_BASE_URL}/chat
   ▼
echo-ai (Python)
   │ ① query → embedding
   │ ② 向量库近邻检索
   │ ③ 候选 metadata (fileId, fileName, source_url, …)
   ▼
ChatResponse{ candidates:[ … ] }
   │
   ▼
for candidate:
   RAGClient.buildFullURL(candidate.Metadata.SourceURL)
      ├─ 已 http(s)        → 原样
      ├─ 含 clouddn.com    → 加 http://
      └─ 其它              → 拼 QINIU_DOMAIN
   拼成 "文件: name, 下载链接: url"
   │
   ▼
search_knowledge 工具结果 = 上述拼接串
   │
   ▼
回填到 ReAct context (tool 角色) → LLM 据此整理最终回复
```

### 3.4 文件入库链路（七牛 + echo-ai + DB 事务）

**代码位置**：`service/file_service.go`

```
Client ──POST /api/file/token {fileName, fileSize, mimeType, bizType}──▶
   FileService.GetUploadToken
      │ ① getQiniuConfig  校验 4 个 env
      │ ② PutPolicy { Scope:bucket, Expires:3600, InsertOnly:1 }
      │ ③ key = bizType/YYYYMMDD/uuid.<ext>
      │ ④ 重试 3 次（防御性）
      ▼
   { token, uploadURL:"https://upload.qiniup.com", key }

Client ──PUT uploadURL + token + key──▶ 七牛 OSS（直传, 不经后端）

Client ──POST /api/file/register {userId, fileName, key, fileType, bizType}──▶
   FileService.RegisterFile (DB 事务)
      │ ① tx := DB.Begin()
      │ ② Insert file row (in tx)
      │ ③ POST {ECHO_AI_REMOTE_BASE_URL}/ingest_file
      │      body: { user_id, file: { file_id, file_name, file_key, url } }
      │      url = QINIU_DOMAIN + "/" + key
      │ ④ 成功 → tx.Commit()
      │ 失败 → tx.Rollback()
      ▼
   { id, userId, key, status:1 }

后续: search Agent 触发时, RAGClient 可检索到该文件
```

### 3.5 用户与会话链路

**代码位置**：`service/user_service.go` · `utils/session.go`

```
┌─── 写 ───┐
│ POST /api/auth/login {username, password}                            │
│    │ ① GetByUsername  (GORM)                                        │
│    │ ② IsEnabled       (status==1)                                  │
│    │ ③ VerifyPassword  (bcrypt+salt)                                 │
│    │ ④ SessionStore.Create (24h) → 生成 32B 随机 sessionId           │
│    │ ⑤ UpdateLastLogin (best-effort, 失败不阻塞)                     │
│    ▼                                                                  │
│ { sessionId, expireAt, user: { id, username, … } }                  │
└────────────┘

┌─── 读 ───┐
│ POST /api/auth/check {sessionId}                                     │
│    │ ① SessionStore.Get → 不存在/过期返 {valid:false}               │
│    │ ② SessionStore.Touch → 续期 (避免活跃用户被登出)                 │
│    ▼                                                                  │
│ { valid, username, userId, expireAt }                                │
└────────────┘

┌─── 销 ───┐
│ POST /api/auth/logout {sessionId}                                    │
│    │ ① SessionStore.Delete (不存在不报错)                             │
│    ▼                                                                  │
│ { code:200, message:"退出成功" }                                    │
└────────────┘
```

**SessionStore 实现**：`utils/session.go::MemorySessionStore` —— `sync.RWMutex` 保护 `map`，后台 goroutine 5min 清理一次；接口 `SessionStore` 预留 Redis 替换位（参见 §5 P1）。

---

## 4. 上下文工程（Prefix Cache 友好）

> 这是整个系统最值得投入理解的一块。LLM 的"前缀缓存"按字节级前缀命中，命中率直接决定 token 单价（命中后通常按 25%~50% 折扣价计费）。

### 4.1 上下文拼装顺序

```
最终 messages 数组 = [
  { role: "system", content: <Prefix> },   ← 不变 → 利于 cache
  { role: "user",   content: <消息1> },     ← 滑动窗口尾部
  { role: "assistant", … },
  …
  { role: "user",   content: <本轮消息> }   ← 唯一变化
]
```

`<Prefix>` 由三段固定顺序拼成（**顺序不能变，否则前缀字节级不一致，cache 失效**）：

```
┌─ System Prompt（Agent 自身）─┐  ①
├─ 长期记忆（BuildMemoryContext）─┐  ②
└─ 对话摘要（BuildContext 注入）─┐  ③
```

`Summarizer.buildPrefix` 即按此顺序构造；每次调用 LLM 都要保证这三段的**字节级一致**才能命中上游 cache。

### 4.2 本地 PromptCache

```go
key := PromptCacheKey("user", userID,
                      "session", sessionID,
                      "mem", sha1(memCtx),      // 长期记忆哈希
                      "sumv", summary.UpdatedAt, // 摘要版本
                      "model", aiClient.ModelName())

v, ok := cache.Get(key)
if !ok {
    prefix := buildPrefix(...)
    cache.Set(key, prefix, 5*time.Minute)
}
```

**为什么 key 必须涵盖这些因子？**

- `user`：不同用户的长期记忆不同
- `session`：不同会话的摘要不同
- `mem`：长期记忆更新后，前缀内容变了 → 旧 key 必须不命中
- `sumv`：摘要更新后，前缀内容变了
- `model`：不同模型的 prefix 边界不同（OpenAI 1024 token / Anthropic 较短 breakpoint）

**业界依据**：OpenAI 文档明确"prompt caching works on the longest prefix match"，本地缓存同构：key 多一份因子，少一次错命中。

### 4.3 失效策略

| 触发事件                  | 当前做法                                | 演进（P1）                              |
| ------------------------- | --------------------------------------- | --------------------------------------- |
| 摘要生成 / 摘要失效       | `cache.Del(currentKey)`                 | 同上                                    |
| 会话清空 (`DELETE /chat/session`) | `cache.Del(currentKey)`        | 同上                                    |
| 长期记忆新增 / 更新 / 删除 | 依赖 5min TTL 自然过期（不主动失效）     | 引入按 user 维度的 tag，遍历 store 批量失效 |
| 模型切换                  | key 中带 `model` 字段，自动失效         | 同上                                    |

### 4.4 摘要与记忆的成本曲线

| 策略                          | 成本（token）                          | 上下文保真度 |
| ----------------------------- | -------------------------------------- | ------------ |
| 每次把全部历史塞给 LLM        | O(N)，随对话变长线性增长                | 100%         |
| 仅末尾 windowSize=20          | O(1)，固定成本                          | 仅尾部 20 条 |
| **本系统：摘要 + 滑动窗口**   | O(新增) 摘要 + O(1) 窗口               | 摘要 + 尾部  |
| 向量召回历史片段              | O(query) + 片段数                      | 局部相关     |

**为什么选"摘要 + 滑动窗口"**：业界 LangChain ConversationSummaryBufferMemory、LlamaIndex Memory 的成熟做法；增量摘要保证旧信息不丢，滑动窗口保证当前对话精度。

---

## 5. 当前主要问题与低成本方案

> **方法论**：P0 是必须尽快解决的体验/成本问题；P1 是规模化前必做；P2 是 SLA 提升项；P3 是工程化基建。
> 每条方案都标注了：**业务影响** · **改动范围** · **风险** · **上线步骤**。

### P0-1 · 路由决策的多余 LLM 往返

**问题**：`MultiAgentOrchestrator.routeWithLLM` 每次都用 LLM 选 Agent，1 次额外往返 ≈ 200~500ms，是端到端首字时延的最大来源。

**业界做法**：分级路由：规则匹配 → 缓存命中 → LLM 兜底。OpenAI Function Calling 文档也建议"对于稳定任务优先用 deterministic routing"。

**本系统低成本方案**：

| 维度     | 描述                                                |
| -------- | --------------------------------------------------- |
| 业务影响 | 首字时延 -30%，白天流量大时节省约 ¥X/天（按 LLM 计费）|
| 改动范围 | 新增 `service/route_cache.go`（~80 行）             |
| 风险     | 缓存命中错误路由 → 用户体验下滑，需监控告警          |
| 上线步骤 | ① 增加路由结果 cache（key=user+session 路由请求 hash, 5min TTL）<br>② 写监控：路由准确率（埋点 trace）<br>③ 灰度 10% → 50% → 100% |
| 验证指标 | `chat_request_total{route="cache_hit"|"llm"}`、`avg_first_token_ms` |

```go
// 伪代码
func (o *Orchestrator) RouteAgent(userInput string) *Agent {
    if cached, ok := o.routeCache.Get(routeKey(userInput)); ok {
        return o.agents[cached]
    }
    name, err := o.routeWithLLM(userInput)
    if err == nil {
        o.routeCache.Set(routeKey(userInput), name, 5*time.Minute)
        return o.agents[name]
    }
    return o.fallbackAgent
}
```

### P0-2 · 长期记忆去重粒度粗

**问题**：`memory_service.go::isDuplicate` 用 `strings.Contains` 做包含关系判定，对"同义不同表述"（"用户喜欢简洁回答" vs "用户偏好简短回复"）无法识别 → 长期记忆脏数据膨胀。

**业界做法**：向量召回候选 + LLM 二次判重。Mem0、Letta 等开源 memory 框架都是这套。

**本系统低成本方案**：

| 维度     | 描述                                                |
| -------- | --------------------------------------------------- |
| 业务影响 | 长期记忆条数预计 -60%，context token 节省 ~200/会话  |
| 改动范围 | `service/memory_service.go` ~50 行；新增 `embedding_repo` 可选 |
| 风险     | embedding 调 LLM 增加 ~50ms；LLM 判重增加 ~150ms     |
| 上线步骤 | ① 复用 `AIClient.GetTextEmbedding`（已有）<br>② 抽取时先 embedding 候选，再 LLM 判同义<br>③ 灰度对比脏数据率 |
| 验证指标 | `user_memory_count_avg`、`memory_duplicate_rate`    |

```go
// 伪代码
func (s *MemoryService) ExtractAndSave(userID, userMsg, asstReply string) error {
    items := s.extractWithLLM(userMsg, asstReply)         // ~150ms
    existing := s.memRepo.ListUserMemories(userID)        // ~10ms
    for _, it := range items {
        // 1) 向量召回 Top 3 候选
        candidates := recallByEmbedding(it.Content, existing)  // ~50ms
        // 2) LLM 二次判重
        if !s.llmIsDuplicate(it, candidates) {                  // ~150ms
            s.memRepo.SaveUserMemory(...)
        }
    }
}
```

### P1-1 · Session / PromptCache 内存版不支持水平扩展

**问题**：`utils/session.go::MemorySessionStore`、`service/prompt_cache.go::MemoryPromptCache` 都是 `sync.Map` 进程内实现。多实例部署会状态分裂：用户在 A 实例登录，下个请求落到 B 实例就 401。

**业界做法**：把进程内状态外置到共享存储（Redis）。

**本系统低成本方案**：

| 维度     | 描述                                                |
| -------- | --------------------------------------------------- |
| 业务影响 | 支持多实例部署，水平扩展                            |
| 改动范围 | `service/prompt_cache.go` 已预留 `RedisPromptCache` 占位 → 引入 `go-redis/v9` 实现即可<br>新增 `utils/redis_session.go`（~100 行）|
| 风险     | Redis 故障 → 全站 401，需降级到"任意实例 session" 兜底 |
| 上线步骤 | ① 实现 Redis 版 cache/session（接口 0 侵入）<br>② 通过环境变量切换实现<br>③ 双写验证 1 周后切读 |
| 验证指标 | `session_redis_hit_rate`、`prompt_cache_redis_hit_rate` |

### P1-2 · 摘要触发间隔固定

**问题**：`triggerDelta=20` 是固定值；短对话"过度摘要"（浪费 token），长对话"晚摘要"（context 撑爆）。

**业界做法**：根据 token 消耗动态调整（LangChain 文档建议"1.2× last summary tokens"为阈值）。

**本系统低成本方案**：

| 维度     | 描述                                                |
| -------- | --------------------------------------------------- |
| 业务影响 | 摘要 LLM 调用次数 -30%~50%                          |
| 改动范围 | `service/summarizer_service.go` ~30 行              |
| 风险     | 阈值调太激进 → 摘要滞后；调太保守 → 收益不明显       |
| 上线步骤 | ① 让 LLM 响应带回 `usage.total_tokens`<br>② 累计 `tokens_since_last > 1.2 × last_summary_tokens` 才触发<br>③ 观察 hit/miss 比例调阈值 |

### P2-1 · WebSocket 缺事件序号

**问题**：`WSOutgoingMessage` 无 `seq`，客户端断线重连时无法定位"上次收到哪条"。

**业界做法**：每条消息带 `seq`，客户端缓存 `lastSeq`；重连时上报，服务端从 `lastSeq+1` 重发（参考 Socket.IO、Server-Sent Events 的"Last-Event-ID"机制）。

**本系统低成本方案**：

| 维度     | 描述                                                |
| -------- | --------------------------------------------------- |
| 业务影响 | 网络抖动重连不再丢消息，UX 显著提升                  |
| 改动范围 | `handlers/chat_stream_handler.go` ~30 行；客户端协议微调 |
| 风险     | 重发时内存/网络压力 → 需要 ring buffer 限制重发窗口（最近 1000 条） |
| 上线步骤 | ① 协议字段加 `seq`、`lastSeq`<br>② 服务端维护 ring buffer<br>③ 客户端实现 resume |

### P2-2 · Agent 工具无超时/重试

**问题**：`search_knowledge` 调 echo-ai 失败时直接 `Error executing`，没重试也没超时；RAG 服务偶发抖动会让整轮 ReAct 崩。

**业界做法**：工具级 timeout + 指数退避 + 失败 fallback（参考 LangChain `max_retries`）。

**本系统低成本方案**：

```go
// 伪代码 - withRetry 装饰器
func withRetry(handler ToolHandler, max int, backoff time.Duration) ToolHandler {
    return func(params map[string]interface{}) (string, error) {
        var lastErr error
        for i := 0; i < max; i++ {
            ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
            defer cancel()
            // 实际执行
            result, err := callWithContext(ctx, handler, params)
            if err == nil { return result, nil }
            lastErr = err
            time.Sleep(backoff * time.Duration(i+1))
        }
        return "工具繁忙，请稍后重试", lastErr
    }
}
```

### P3-1 · 缺乏可观测埋点

**问题**：现有 `log.Printf` 流水可读但难聚合；无法回答"路由准确率 / 工具调用频次 / 摘要 token 节省"。

**业界做法**：Prometheus 指标 + 分布式 trace（OpenTelemetry）。

**本系统低成本方案**：仅引入 Prometheus client（5~6 个核心指标），不动 trace。

| 指标                                       | 类型      | 含义                              |
| ------------------------------------------ | --------- | --------------------------------- |
| `chat_request_total{status}`               | counter   | 请求量，按成功/失败分桶            |
| `route_decision_total{agent, source}`      | counter   | 路由命中分布（cache / llm / fallback）|
| `tool_call_total{name, result}`            | counter   | 工具调用成功率                     |
| `summary_token_saved_total`                | counter   | 摘要节省的 token 数                |
| `prompt_cache_hit_ratio`                   | gauge     | 前缀缓存命中率                     |
| `chat_latency_seconds{stage}`              | histogram | 各阶段耗时（build_ctx / llm_ttfb / total）|

**暴露端点**：`GET /metrics`（无需鉴权，部署时由 ingress 收敛）。