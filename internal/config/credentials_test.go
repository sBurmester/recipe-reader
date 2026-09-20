package config

import (
	"testing"
)

// A credential passed as a flag is readable by every other user on the host
// through `ps aux`. None of the four may be accepted in that form — and the
// review's proposed fix of dropping the name tag would not have achieved this,
// since kong names an untagged field's flag after the field.
func TestLoad_RejectsCredentialFlags(t *testing.T) {
	for _, flag := range []string{"--api-token", "--instagram-password", "--anthropic-api-key", "--llm-api-key"} {
		if _, err := loadArgs([]string{flag, "secret"}); err == nil {
			t.Errorf("loadArgs(%s secret) error = nil, want an unknown-flag refusal", flag)
		}
	}
}

// ...and all four still arrive through the environment.
func TestLoad_ReadsCredentialsFromTheEnvironment(t *testing.T) {
	t.Setenv("API_TOKEN", "tok")
	t.Setenv("INSTAGRAM_PASSWORD", "pw")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
	t.Setenv("LLM_API_KEY", "sk-llm")

	cfg, err := loadArgs(nil)
	if err != nil {
		t.Fatalf("loadArgs() error = %v", err)
	}
	if cfg.HTTP.APIToken != "tok" || cfg.Instagram.Password != "pw" || cfg.LLM.AnthropicAPIKey != "sk-ant" || cfg.LLM.APIKey != "sk-llm" {
		t.Errorf("credentials = %q %q %q %q, want tok pw sk-ant sk-llm",
			cfg.HTTP.APIToken, cfg.Instagram.Password, cfg.LLM.AnthropicAPIKey, cfg.LLM.APIKey)
	}
}
