package config

import "testing"

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
