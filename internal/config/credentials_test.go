package config

import (
	"strings"
	"testing"
)

// A credential passed as a flag is readable by every other user on the host
// through `ps aux`. None of the four may be accepted in that form — and the
// review's proposed fix of dropping the name tag would not have achieved this,
// since kong names an untagged field's flag after the field.
func TestLoad_RejectsCredentialFlags(t *testing.T) {
	for _, flag := range []string{"--api-token", "--instagram-password", "--anthropic-api-key", "--llm-api-key"} {
		if _, err := load([]string{flag, "secret"}); err == nil {
			t.Errorf("load(%s secret) error = nil, want an unknown-flag refusal", flag)
		}
	}
}

// ...and all four still arrive through the environment.
func TestLoad_ReadsCredentialsFromTheEnvironment(t *testing.T) {
	t.Setenv("API_TOKEN", "tok")
	t.Setenv("INSTAGRAM_PASSWORD", "pw")
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant")
	t.Setenv("LLM_API_KEY", "sk-llm")

	cfg, err := load(nil)
	if err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if cfg.APIToken != "tok" || cfg.InstagramPassword != "pw" || cfg.AnthropicAPIKey != "sk-ant" || cfg.LLMAPIKey != "sk-llm" {
		t.Errorf("credentials = %q %q %q %q, want tok pw sk-ant sk-llm",
			cfg.APIToken, cfg.InstagramPassword, cfg.AnthropicAPIKey, cfg.LLMAPIKey)
	}
}

// With no flag to list them under, --help is the only place an operator finds
// the credential names; they are in the description.
func TestDescription_NamesEveryCredential(t *testing.T) {
	for _, env := range []string{"API_TOKEN", "INSTAGRAM_PASSWORD", "LLM_API_KEY", "ANTHROPIC_API_KEY"} {
		if !strings.Contains(description, env) {
			t.Errorf("--help description does not mention %s", env)
		}
	}
}
