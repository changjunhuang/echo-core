package service

import (
	"echo-core/models"
	"echo-core/remote"
	"echo-core/repository"
	"log"
	"strings"
	"time"
)

// Summarizer 对话摘要器
//
// 业界设计要点（成本/性能）：
//  1. 增量摘要：旧摘要 + 新增消息 → 新摘要，而非每次从零压缩。
//     - 节省 token：单次摘要 LLM 输入量 = O(新增消息)，与 O(全部历史) 的方案相比，
//     随对话变长 token 增长保持线性而非平方级。
//     - 保留历史要点：旧摘要作为"前情提要"传入，新摘要天然继承。
//  2. 触发间隔：上次摘要覆盖 MessageCount 后，再累计 ≥triggerDelta 条新消息才再次触发，
//     避免每轮 LLM 调用浪费钱。
//  3. Upsert 语义：同一 session 始终只保留一份最新摘要（由 UpdatedAt 标识）。
//  4. 滑动窗口 BuildContext：返回 [系统] + [用户长期记忆] + [对话摘要] + [最近 N 条消息]，
//     在不丢上下文的前提下显著降低 token。
//  5. BuildContext 不再访问 DB：调用方传入预取的 meta，避免一请求 3 次 GetLatestSummary。
type Summarizer struct {
	aiClient *remote.AIClient
	memRepo  *repository.MemoryRepository
	// windowSize 滑动窗口保留的最近消息条数
	windowSize int
	// triggerDelta 触发增量摘要所需的新增消息条数
	triggerDelta int
}

// NewSummarizer 构造 Summarizer（默认窗口 20，触发间隔 20）
func NewSummarizer(aiClient *remote.AIClient, memRepo *repository.MemoryRepository) *Summarizer {
	return &Summarizer{
		aiClient:     aiClient,
		memRepo:      memRepo,
		windowSize:   20,
		triggerDelta: 20,
	}
}

// WithWindow 调整窗口大小（链式调用，方便测试与未来动态调参）
func (s *Summarizer) WithWindow(n int) *Summarizer {
	if n > 0 {
		s.windowSize = n
	}
	return s
}

// WithTriggerDelta 调整触发间隔
func (s *Summarizer) WithTriggerDelta(n int) *Summarizer {
	if n > 0 {
		s.triggerDelta = n
	}
	return s
}

// ShouldSummarize 判断是否需要重新生成摘要
// messageCount      - 当前消息总数
// lastSummarizedCnt - 上次摘要覆盖到的消息条数（无摘要时传 0）
func (s *Summarizer) ShouldSummarize(messageCount, lastSummarizedCnt int) bool {
	if lastSummarizedCnt <= 0 {
		return messageCount > s.windowSize
	}
	return messageCount-lastSummarizedCnt >= s.triggerDelta
}

// WindowSize 暴露给外层用于 BuildContext 的滑动窗口大小
func (s *Summarizer) WindowSize() int {
	return s.windowSize
}

// SummaryMeta 摘要元信息（用于 BuildContext 与触发判断）
type SummaryMeta struct {
	ID           uint
	Summary      string
	MessageCount int
	UpdatedAt    time.Time
}

// GetSummaryMeta 获取某 session 的最新摘要与元信息；无摘要时返回 (nil, nil)
func (s *Summarizer) GetSummaryMeta(sessionID string) (*SummaryMeta, error) {
	summary, err := s.memRepo.GetLatestSummary(sessionID)
	if err != nil || summary == nil {
		return nil, err
	}
	return &SummaryMeta{
		ID:           summary.ID,
		Summary:      summary.Summary,
		MessageCount: summary.MessageCount,
		UpdatedAt:    summary.UpdatedAt,
	}, nil
}

// GetSummaryMetaLight 轻量查询（不取 summary 大字段）— 用于 prefix cache key 计算
func (s *Summarizer) GetSummaryMetaLight(sessionID string) (*SummaryMeta, error) {
	summary, err := s.memRepo.GetLatestSummaryVersion(sessionID)
	if err != nil || summary == nil {
		return nil, err
	}
	return &SummaryMeta{
		ID:           summary.ID,
		MessageCount: summary.MessageCount,
		UpdatedAt:    summary.UpdatedAt,
	}, nil
}

// GenerateSummary 增量生成摘要并落库
// 业界实现：仅对 lastSummarizedCnt 之后的「新增消息」做摘要，旧摘要作为前缀传入 LLM。
// 相比「每次把全部历史都发给 LLM」，token 成本随对话增长保持 O(新增) 而非 O(全部)。
func (s *Summarizer) GenerateSummary(sessionID, userID string, messages []remote.AIChatMessage) (string, error) {
	if len(messages) == 0 {
		return "", nil
	}

	// 1) 取出上一份摘要（如果有）
	prevSummary := ""
	startIdx := 0
	if existing, err := s.memRepo.GetLatestSummary(sessionID); err == nil && existing != nil {
		prevSummary = existing.Summary
		// 仅对"上次摘要未覆盖"的消息做增量摘要
		// MessageCount 字段记录"该摘要覆盖到的消息条数"（即已摘要的消息数）
		if existing.MessageCount > 0 && existing.MessageCount < len(messages) {
			startIdx = existing.MessageCount
		} else if existing.MessageCount >= len(messages) {
			// 摘要已覆盖全部消息，无需重做
			log.Printf("[Summarizer] 摘要已是最新 | sessionID: %s | covered: %d | total: %d", sessionID, existing.MessageCount, len(messages))
			return existing.Summary, nil
		}
		log.Printf("[Summarizer] 发现上一份摘要 | sessionID: %s | prev_len: %d | prev_msg_count: %d | new_msgs: %d",
			sessionID, len(prevSummary), existing.MessageCount, len(messages)-startIdx)
	} else {
		log.Printf("[Summarizer] 无上一份摘要，将从零生成 | sessionID: %s | total_msgs: %d", sessionID, len(messages))
	}

	newMessages := messages[startIdx:]
	if len(newMessages) == 0 {
		log.Printf("[Summarizer] 无新增消息，跳过摘要 | sessionID: %s", sessionID)
		return prevSummary, nil
	}

	// 2) 调 LLM 生成摘要（增量）
	summary, err := s.aiClient.GenerateSummary(prevSummary, newMessages)
	if err != nil {
		return "", err
	}

	// 3) Upsert 到 DB
	if err := s.memRepo.UpsertConversationSummary(sessionID, userID, summary, len(messages)); err != nil {
		log.Printf("[Summarizer] 摘要落库失败 | sessionID: %s | error: %v", sessionID, err)
		return summary, err
	}

	log.Printf("[Summarizer] 摘要已更新 | sessionID: %s | summary_len: %d | covered_msgs: %d | new_msgs: %d",
		sessionID, len(summary), len(messages), len(newMessages))
	return summary, nil
}

// GetSummary 获取会话摘要（最新一条）；出错时返回空串+错误（修复旧版吞错的隐患）
func (s *Summarizer) GetSummary(sessionID, userID string) (string, error) {
	summary, err := s.memRepo.GetLatestSummary(sessionID)
	if err != nil {
		return "", err
	}
	if summary == nil {
		return "", nil
	}
	return summary.Summary, nil
}

// InvalidateSummary 删除某 session 的摘要（让 prefix cache 也跟着失效）
// 修复旧版 UpdateSummary(0, "") 无效调用：现在真正删除记录，下次请求会重新生成。
func (s *Summarizer) InvalidateSummary(sessionID string) error {
	log.Printf("[Summarizer] 失效摘要 | sessionID: %s", sessionID)
	return s.memRepo.DeleteSummary(sessionID)
}

// BuildContextInputs BuildContext 的入参聚合
// 聚合后避免参数列表过长；同时也方便在 BuildContext 内部避免重复查询。
type BuildContextInputs struct {
	SessionID     string
	UserID        string
	SystemPrompt  string
	MemoryContext string
	History       []remote.AIChatMessage
	// Meta 摘要元信息；为 nil 时 BuildContext 内部按需拉取（兼容老调用方）。
	// 业界最佳实践：调用方在更外层只查 1 次 DB，把 meta 传进来复用。
	Meta *SummaryMeta
}

// BuildContext 构造带「摘要 + 滑动窗口」的上下文
// 形如：[系统提示][用户长期记忆][对话摘要] + [最近 N 条消息]
//
// 性能要点：
//   - 不再访问 DB：当 inputs.Meta 非 nil 时直接复用，0 额外查询。
//   - 滑动窗口：仅保留最近 windowSize 条，避免长对话把 token 撑爆。
//   - 前缀稳定：system 段顺序固定（system → memory → summary），仅尾部追加最近消息，
//     LLM 上游 Prefix Cache 才能真正命中。
func (s *Summarizer) BuildContext(inputs BuildContextInputs) ([]remote.AIChatMessage, *SummaryMeta) {
	log.Printf("[Summarizer] BuildContext | sessionID: %s | userID: %s | history_size: %d | has_meta: %v",
		inputs.SessionID, inputs.UserID, len(inputs.History), inputs.Meta != nil)

	// 1) 复用 meta（外部预取）或按需拉取（兼容老调用方）
	meta := inputs.Meta
	if meta == nil {
		if m, _ := s.GetSummaryMetaLight(inputs.SessionID); m != nil {
			meta = m
		}
	}
	if meta == nil {
		meta = &SummaryMeta{}
	}

	// 2) 构造前缀（稳定不变 → 利于 Prefix Cache 命中）
	prefix := s.buildPrefix(inputs.SystemPrompt, inputs.MemoryContext, meta)

	// 3) 滑动窗口：仅保留最近 windowSize 条
	window := s.tailWindow(inputs.History, s.windowSize)
	log.Printf("[Summarizer] 滑动窗口 | sessionID: %s | window_size: %d | total_history: %d",
		inputs.SessionID, len(window), len(inputs.History))

	// 4) 拼装
	out := make([]remote.AIChatMessage, 0, len(window)+1)
	if prefix != "" {
		out = append(out, remote.AIChatMessage{Role: "system", Content: prefix})
	}
	out = append(out, window...)
	return out, meta
}

// buildPrefix 拼装「系统提示 + 用户长期记忆 + 对话摘要」三段式前缀
// 顺序保持稳定 → 字节级一致 → 利于上游 LLM Prefix Cache 命中
func (s *Summarizer) buildPrefix(systemPrompt, memoryContext string, meta *SummaryMeta) string {
	var b strings.Builder
	if strings.TrimSpace(systemPrompt) != "" {
		b.WriteString(systemPrompt)
	}
	if strings.TrimSpace(memoryContext) != "" {
		b.WriteString("\n\n")
		b.WriteString(memoryContext)
	}
	if meta != nil && strings.TrimSpace(meta.Summary) != "" {
		b.WriteString("\n\n【历史对话摘要（覆盖到第 ")
		b.WriteString(itoa(meta.MessageCount))
		b.WriteString(" 条消息，跨最近会话累积）】\n")
		b.WriteString(meta.Summary)
		b.WriteString("\n\n")
		b.WriteString("（以上为更早对话的压缩摘要；请把『最近 N 条消息』视为最新交互，摘要为辅。）")
	}
	return b.String()
}

// tailWindow 截取尾部 windowSize 条
func (s *Summarizer) tailWindow(history []remote.AIChatMessage, windowSize int) []remote.AIChatMessage {
	if windowSize <= 0 || len(history) <= windowSize {
		return history
	}
	return history[len(history)-windowSize:]
}

// itoa 避免引入 strconv
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// EnsureModelsImport 防止 models 包被 IDE 误删（用于 _ 引用）—— 编译期保证导入存在
var _ = models.ConversationSummary{}
var _ = time.Now
