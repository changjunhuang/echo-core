package service

import (
	"fmt"
	"strings"

	"go-start/utils"
)

// LLM模型提供厂商
const (
	providerOpenAI      = "openai"
	providerSiliconFlow = "siliconflow"
	providerDeepSeek    = "deepseek"
	providerGemini      = "gemini"
)

// LLMConfig 描述一个 OpenAI 兼容大模型客户端所需配置。
type LLMConfig struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
}

// LLMRequestOptions 描述单次请求对默认 LLM 配置的可选覆盖。
type LLMRequestOptions struct {
	Provider string
	APIKey   string
	BaseURL  string
	Model    string
}

// LoadLLMConfig 从环境变量加载通用 LLM 配置，并兼容常见厂商的专用 key。
func LoadLLMConfig() (LLMConfig, error) {
	provider := normalizeProvider(utils.GetEnv("LLM_PROVIDER", ""))
	if provider == "" {
		provider = inferProviderFromEnv()
	}

	cfg := LLMConfig{
		Provider: provider,
		APIKey:   resolveAPIKey(provider),
		BaseURL:  normalizeBaseURL(utils.GetEnv("LLM_BASE_URL", defaultBaseURL(provider))),
		Model:    strings.TrimSpace(utils.GetEnv("LLM_MODEL", defaultModel(provider))),
	}

	return validateLLMConfig(cfg)
}

// ResolveLLMConfig 将环境默认配置与单次请求覆盖配置合并，生成最终生效配置。
func ResolveLLMConfig(options LLMRequestOptions) (LLMConfig, error) {
	baseCfg, err := LoadLLMConfig()
	if err != nil {
		baseCfg = LLMConfig{}
	}

	provider := normalizeProvider(firstNonEmpty(strings.TrimSpace(options.Provider), baseCfg.Provider))
	if provider == "" {
		provider = inferProviderFromEnv()
	}

	effective := LLMConfig{
		Provider: provider,
		APIKey:   strings.TrimSpace(options.APIKey),
		BaseURL:  normalizeBaseURL(options.BaseURL),
		Model:    strings.TrimSpace(options.Model),
	}

	if effective.APIKey == "" {
		if provider == baseCfg.Provider && baseCfg.APIKey != "" {
			effective.APIKey = baseCfg.APIKey
		} else {
			effective.APIKey = resolveAPIKey(provider)
		}
	}
	if effective.BaseURL == "" {
		if provider == baseCfg.Provider && baseCfg.BaseURL != "" {
			effective.BaseURL = baseCfg.BaseURL
		} else {
			effective.BaseURL = normalizeBaseURL(defaultBaseURL(provider))
		}
	}
	if effective.Model == "" {
		if provider == baseCfg.Provider && baseCfg.Model != "" {
			effective.Model = baseCfg.Model
		} else {
			effective.Model = strings.TrimSpace(defaultModel(provider))
		}
	}

	return validateLLMConfig(effective)
}

func validateLLMConfig(cfg LLMConfig) (LLMConfig, error) {
	cfg.Provider = normalizeProvider(cfg.Provider)
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.BaseURL = normalizeBaseURL(cfg.BaseURL)
	cfg.Model = strings.TrimSpace(cfg.Model)

	if cfg.Provider == "" {
		cfg.Provider = inferProviderFromEnv()
	}
	if cfg.APIKey == "" {
		return cfg, fmt.Errorf("llm api key not set: configure LLM_API_KEY or provider-specific key such as SILICONFLOW_API_KEY/OPENAI_API_KEY/DEEPSEEK_API_KEY/GEMINI_API_KEY")
	}
	if cfg.BaseURL == "" {
		return cfg, fmt.Errorf("llm base url not set: configure LLM_BASE_URL or use a supported provider")
	}
	if cfg.Model == "" {
		return cfg, fmt.Errorf("llm model not set: configure LLM_MODEL")
	}
	return cfg, nil
}

// inferProviderFromEnv 根据环境变量自动推断使用的 LLM 提供商，优先级为：SILICONFLOW > DEEPSEEK > GEMINI > OPENAI。
func inferProviderFromEnv() string {
	switch {
	case utils.GetEnvFirst("SILICONFLOW_API_KEY") != "":
		return providerSiliconFlow
	case utils.GetEnvFirst("DEEPSEEK_API_KEY") != "":
		return providerDeepSeek
	case utils.GetEnvFirst("GEMINI_API_KEY") != "":
		return providerGemini
	case utils.GetEnvFirst("OPENAI_API_KEY") != "":
		return providerOpenAI
	default:
		return providerOpenAI
	}
}

func normalizeProvider(provider string) string {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", providerOpenAI:
		return providerOpenAI
	case "siliconflow", "silicon-flow", "sf":
		return providerSiliconFlow
	case providerDeepSeek:
		return providerDeepSeek
	case providerGemini, "google", "googleai":
		return providerGemini
	default:
		return strings.ToLower(strings.TrimSpace(provider))
	}
}

func resolveAPIKey(provider string) string {
	if key := utils.GetEnvFirst("LLM_API_KEY"); key != "" {
		return key
	}

	switch provider {
	case providerSiliconFlow:
		return utils.GetEnvFirst("SILICONFLOW_API_KEY", "OPENAI_API_KEY")
	case providerDeepSeek:
		return utils.GetEnvFirst("DEEPSEEK_API_KEY", "OPENAI_API_KEY")
	case providerGemini:
		return utils.GetEnvFirst("GEMINI_API_KEY", "OPENAI_API_KEY")
	case providerOpenAI:
		return utils.GetEnvFirst("OPENAI_API_KEY")
	default:
		return utils.GetEnvFirst("OPENAI_API_KEY", "SILICONFLOW_API_KEY", "DEEPSEEK_API_KEY", "GEMINI_API_KEY")
	}
}

func defaultBaseURL(provider string) string {
	switch provider {
	case providerSiliconFlow:
		return "https://api.siliconflow.cn/v1"
	case providerDeepSeek:
		return "https://api.deepseek.com/v1"
	case providerGemini:
		return "https://generativelanguage.googleapis.com/v1beta/openai"
	case providerOpenAI:
		return "https://api.openai.com/v1"
	default:
		return utils.GetEnv("LLM_BASE_URL", "")
	}
}

func defaultModel(provider string) string {
	switch provider {
	case providerSiliconFlow:
		return "deepseek-ai/DeepSeek-V3"
	case providerDeepSeek:
		return "deepseek-chat"
	case providerGemini:
		return "gemini-2.0-flash"
	case providerOpenAI:
		return "gpt-4o-mini"
	default:
		return utils.GetEnv("LLM_MODEL", "")
	}
}

func normalizeBaseURL(baseURL string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
