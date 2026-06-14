package service

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// PromptCache 系统提示词前缀缓存
// 目标：把「同一前缀的拼装结果」缓存下来，避免每轮重新拼装与重复编码开销。
//
// 业界实现说明（成本/性能角度）：
//   - 上游 LLM 厂商（OpenAI / Anthropic / DeepSeek 等）支持"前缀缓存"：当请求中相同的前缀
//     长度超过 1024 token（OpenAI）/ 较短的 breakpoint（Anthropic）时，命中缓存可省去
//     重新编码的耗时与 token 费用（一般按 cached input 25%~50% 折扣价计费）。
//   - 客户端做"前缀缓存"的额外价值：避免重复拼装、避免重复序列化、便于观测命中率。
//   - 缓存键设计必须涵盖「影响前缀内容的全部因子」：user、session、记忆版本、摘要版本、
//     以及模型名（不同模型前缀边界可能不同）。遗漏任一因子都会导致命中错误前缀。
//   - 本接口把缓存抽象为 Key-Value 形态：Key = 影响因子哈希；Value = 渲染好的前缀字符串。
//   - MVP 用本地内存实现；Redis 实现预留接口（同接口零侵入切换）。
type PromptCache interface {
	// Get 取一个 key；命中且未过期返回 (value, true)，否则 ("", false)
	Get(key string) (string, bool)
	// Set 写入一个 key，过期时间 ttl
	Set(key, value string, ttl time.Duration)
	// Del 删除一个 key（摘要/记忆更新时主动失效）
	Del(key string)
	// Stats 返回缓存命中统计（hit / miss）
	Stats() CacheStats
}

// CacheStats 缓存命中统计
type CacheStats struct {
	Hit  int64 `json:"hit"`
	Miss int64 `json:"miss"`
}

// Total 命中+未命中
func (s CacheStats) Total() int64 { return s.Hit + s.Miss }

// HitRate 命中率（0~1）
func (s CacheStats) HitRate() float64 {
	t := s.Total()
	if t == 0 {
		return 0
	}
	return float64(s.Hit) / float64(t)
}

// PromptCacheKey 计算前缀缓存 Key
// 组成：所有影响前缀内容的因子拼起来做 SHA1
// 业界依据：OpenAI 自动按前缀命中，要让 LLM 命中率高，前缀必须保持字节级一致；
//
//	本地缓存同理——任何因子变化都必须改变 key。
func PromptCacheKey(parts ...string) string {
	h := sha1.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0}) // 因子间加分隔符，避免拼接歧义
	}
	return "pc:" + hex.EncodeToString(h.Sum(nil))
}

// MemoryPromptCache 进程内前缀缓存（sync.Map + 单 key 过期时间）
// 适用：单机部署、缓存量小、TTL 短（分钟级）。
// 业界对比：单实例够用；多实例部署请切到 Redis（接口完全兼容）。
type MemoryPromptCache struct {
	store sync.Map // key -> *cacheEntry
	hit   int64
	miss  int64
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

// NewMemoryPromptCache 创建内存版前缀缓存
func NewMemoryPromptCache() *MemoryPromptCache {
	c := &MemoryPromptCache{}
	go c.sweepLoop(1 * time.Minute)
	return c
}

// Get 取值
func (c *MemoryPromptCache) Get(key string) (string, bool) {
	v, ok := c.store.Load(key)
	if !ok {
		atomic.AddInt64(&c.miss, 1)
		return "", false
	}
	entry := v.(*cacheEntry)
	if time.Now().After(entry.expiresAt) {
		c.store.Delete(key)
		atomic.AddInt64(&c.miss, 1)
		return "", false
	}
	atomic.AddInt64(&c.hit, 1)
	return entry.value, true
}

// Set 写入
func (c *MemoryPromptCache) Set(key, value string, ttl time.Duration) {
	c.store.Store(key, &cacheEntry{
		value:     value,
		expiresAt: time.Now().Add(ttl),
	})
}

// Del 删除
func (c *MemoryPromptCache) Del(key string) {
	c.store.Delete(key)
}

// Stats 返回统计
func (c *MemoryPromptCache) Stats() CacheStats {
	return CacheStats{
		Hit:  atomic.LoadInt64(&c.hit),
		Miss: atomic.LoadInt64(&c.miss),
	}
}

// sweepLoop 周期清理过期项，防止内存膨胀
func (c *MemoryPromptCache) sweepLoop(period time.Duration) {
	t := time.NewTicker(period)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		c.store.Range(func(k, v interface{}) bool {
			if entry, ok := v.(*cacheEntry); ok && now.After(entry.expiresAt) {
				c.store.Delete(k)
			}
			return true
		})
	}
}

// RedisPromptCache Redis 形态的前缀缓存（占位实现）
// 切换方法：构造 ChatService 时把 memoryPromptCache 换成 redisPromptCache 即可，业务层无感。
//
// 预留接口说明：
//   - key 形如 "pc:<sha1>"，与内存版完全兼容
//   - value 为渲染好的前缀字符串
//   - 通过 SET key value EX <ttl_seconds> 实现带 TTL 写入
//   - 删除用 DEL key
//   - 命中/未命中可用 HINCRBY 累计到 hash 字段
type RedisPromptCache struct {
	addr     string
	password string
	db       int
	// client 这里占位不引入 redis 依赖，避免 MVP 阶段引入额外包；
	// 真实实现时引入 github.com/redis/go-redis/v9 即可。
	client interface{}
}

// NewRedisPromptCache 构造 Redis 形态缓存（当前为占位）
func NewRedisPromptCache(addr, password string, db int) *RedisPromptCache {
	log.Printf("[PromptCache] NewRedisPromptCache (占位实现) | addr: %s | db: %d", addr, db)
	return &RedisPromptCache{
		addr:     addr,
		password: password,
		db:       db,
	}
}

// Get 取值（占位）
func (c *RedisPromptCache) Get(key string) (string, bool) {
	// TODO: 接入 go-redis 后实现
	// val, err := c.client.Get(ctx, key).Result()
	// if err != nil { return "", false }
	// return val, true
	return "", false
}

// Set 写入（占位）
func (c *RedisPromptCache) Set(key, value string, ttl time.Duration) {
	// TODO: c.client.Set(ctx, key, value, ttl)
}

// Del 删除（占位）
func (c *RedisPromptCache) Del(key string) {
	// TODO: c.client.Del(ctx, key)
}

// Stats 统计（占位 - 真实实现可通过 Redis HINCRBY 累计）
func (c *RedisPromptCache) Stats() CacheStats {
	return CacheStats{}
}

// ErrPromptCacheMiss 占位实现统一返回的错误（保留供 Redis 实现时使用）
var ErrPromptCacheMiss = errors.New("prompt cache miss")
