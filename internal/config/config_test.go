package config

import (
	"os"
	"testing"
)

func TestValidateWooIncomplete(t *testing.T) {
	cfg := Config{WooConsumerKey: "ck"}
	result := cfg.ValidateWoo()
	if result.OK {
		t.Fatal("expected invalid config")
	}
	if result.Message == "" {
		t.Fatal("expected warning message")
	}
}

func TestValidateHertwillIncompletePair(t *testing.T) {
	cfg := Config{HertwillEmail: "a@example.com"}
	result := cfg.ValidateHertwill()
	if result.OK {
		t.Fatal("expected invalid config")
	}
	if result.Message != "incomplete Hertwill email/password configuration; missing HERTWILL_PASSWORD" {
		t.Fatalf("unexpected message: %s", result.Message)
	}
}

func TestValidateLimit(t *testing.T) {
	if err := ValidateLimit(10); err != nil {
		t.Fatal(err)
	}
	if err := ValidateLimit(0); err == nil {
		t.Fatal("expected positive limit error")
	}
	if err := ValidateLimit(101); err == nil {
		t.Fatal("expected max limit error")
	}
}

func TestLoadInvalidTimeoutFallsBackWithWarning(t *testing.T) {
	t.Setenv("HWD_TIMEOUT_SECONDS", "nope")
	cfg := Load()
	if cfg.TimeoutSeconds != DefaultTimeoutSeconds {
		t.Fatalf("timeout = %d", cfg.TimeoutSeconds)
	}
	if len(cfg.Warnings) != 1 {
		t.Fatalf("warnings = %+v", cfg.Warnings)
	}
}

func TestLoadUsesHWDEnvFileAndStoreName(t *testing.T) {
	path := t.TempDir() + "/client.env"
	content := "HWD_STORE_NAME=Client A\nWOO_BASE_URL=https://client-a.example\nHERTWILL_API_KEY=hw_test_alias\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"WOO_BASE_URL", "HWD_STORE_NAME", "HERTWILL_ACCESS_TOKEN", "HERTWILL_API_KEY"} {
		os.Unsetenv(key)
	}
	t.Setenv("HWD_ENV_FILE", path)
	cfg := Load()
	if !cfg.LoadedDotEnv || cfg.EnvFile != path {
		t.Fatalf("env file not loaded: %+v", cfg)
	}
	if cfg.StoreName != "Client A" || cfg.WooBaseURL != "https://client-a.example" {
		t.Fatalf("unexpected store config: %+v", cfg)
	}
	if cfg.HertwillAccessToken != "hw_test_alias" {
		t.Fatalf("HERTWILL_API_KEY alias not used: %q", cfg.HertwillAccessToken)
	}
}

func TestLoadWarnsWhenHWDEnvFileMissing(t *testing.T) {
	t.Setenv("HWD_ENV_FILE", t.TempDir()+"/missing.env")
	cfg := Load()
	if cfg.LoadedDotEnv || len(cfg.Warnings) == 0 {
		t.Fatalf("expected a warning for a missing HWD_ENV_FILE: %+v", cfg)
	}
}
