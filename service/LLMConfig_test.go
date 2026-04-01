package service

import (
	"testing"
)

func TestLoadLLMConfig_SiliconFlowDefaults(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "siliconflow")
	t.Setenv("LLM_BASE_URL", "")
	t.Setenv("LLM_MODEL", "")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "sf-key")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "")
	t.Setenv("GEMINI_API_KEY", "")

	cfg, err := LoadLLMConfig()
	if err != nil {
		t.Fatalf("LoadLLMConfig returned error: %v", err)
	}
	if cfg.Provider != providerSiliconFlow {
		t.Fatalf("expected provider %q, got %q", providerSiliconFlow, cfg.Provider)
	}
	if cfg.BaseURL != "https://api.siliconflow.cn/v1" {
		t.Fatalf("unexpected base url: %s", cfg.BaseURL)
	}
	if cfg.Model != "deepseek-ai/DeepSeek-V3" {
		t.Fatalf("unexpected model: %s", cfg.Model)
	}
	if cfg.APIKey != "sf-key" {
		t.Fatalf("unexpected api key: %s", cfg.APIKey)
	}
}

func TestResolveLLMConfig_RequestOverridesModelOnly(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "siliconflow")
	t.Setenv("LLM_BASE_URL", "https://api.siliconflow.cn/v1")
	t.Setenv("LLM_MODEL", "deepseek-ai/DeepSeek-V3")
	t.Setenv("LLM_API_KEY", "")
	t.Setenv("SILICONFLOW_API_KEY", "sf-key")

	cfg, err := ResolveLLMConfig(LLMRequestOptions{Model: "Qwen/Qwen2.5-72B-Instruct"})
	if err != nil {
		t.Fatalf("ResolveLLMConfig returned error: %v", err)
	}
	if cfg.Provider != providerSiliconFlow {
		t.Fatalf("expected provider %q, got %q", providerSiliconFlow, cfg.Provider)
	}
	if cfg.Model != "Qwen/Qwen2.5-72B-Instruct" {
		t.Fatalf("expected overridden model, got %q", cfg.Model)
	}
	if cfg.BaseURL != "https://api.siliconflow.cn/v1" {
		t.Fatalf("expected SiliconFlow base url, got %q", cfg.BaseURL)
	}
	if cfg.APIKey != "sf-key" {
		t.Fatalf("expected SiliconFlow api key, got %q", cfg.APIKey)
	}
}

func TestResolveLLMConfig_CustomProviderWithRuntimeOverrides(t *testing.T) {
	t.Setenv("LLM_PROVIDER", "siliconflow")
	t.Setenv("LLM_BASE_URL", "https://api.siliconflow.cn/v1")
	t.Setenv("LLM_MODEL", "deepseek-ai/DeepSeek-V3")
	t.Setenv("SILICONFLOW_API_KEY", "sf-key")
	t.Setenv("LLM_API_KEY", "")

	cfg, err := ResolveLLMConfig(LLMRequestOptions{
		Provider: "custom",
		BaseURL:  "https://example.com/v1/",
		Model:    "custom-model",
		APIKey:   "runtime-key",
	})
	if err != nil {
		t.Fatalf("ResolveLLMConfig returned error: %v", err)
	}
	if cfg.Provider != "custom" {
		t.Fatalf("expected custom provider, got %q", cfg.Provider)
	}
	if cfg.BaseURL != "https://example.com/v1" {
		t.Fatalf("expected normalized base url, got %q", cfg.BaseURL)
	}
	if cfg.Model != "custom-model" {
		t.Fatalf("expected custom model, got %q", cfg.Model)
	}
	if cfg.APIKey != "runtime-key" {
		t.Fatalf("expected runtime api key, got %q", cfg.APIKey)
	}
}
