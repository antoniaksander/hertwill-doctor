package config

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultTimeoutSeconds = 30
	MaxLimit              = 100
	DefaultLimit          = 10
)

type Config struct {
	WooBaseURL        string
	WooConsumerKey    string
	WooConsumerSecret string

	HertwillBaseURL     string
	HertwillAccessToken string
	HertwillEmail       string
	HertwillPassword    string

	TimeoutSeconds int
	Warnings       []string
	LoadedDotEnv   bool
}

type Source struct {
	values map[string]string
}

func Load() Config {
	source := Source{values: map[string]string{}}
	loaded, _ := loadDotEnv(".env", source.values)

	cfg := Config{
		WooBaseURL:          source.Get("WOO_BASE_URL"),
		WooConsumerKey:      source.Get("WOO_CONSUMER_KEY"),
		WooConsumerSecret:   source.Get("WOO_CONSUMER_SECRET"),
		HertwillBaseURL:     source.Get("HERTWILL_BASE_URL"),
		HertwillAccessToken: source.Get("HERTWILL_ACCESS_TOKEN"),
		HertwillEmail:       source.Get("HERTWILL_EMAIL"),
		HertwillPassword:    source.Get("HERTWILL_PASSWORD"),
		TimeoutSeconds:      DefaultTimeoutSeconds,
		LoadedDotEnv:        loaded,
	}

	if raw := source.Get("HWD_TIMEOUT_SECONDS"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			cfg.Warnings = append(cfg.Warnings, "HWD_TIMEOUT_SECONDS must be a positive integer; using default 30 seconds")
		} else {
			cfg.TimeoutSeconds = n
		}
	}

	return cfg
}

func (s Source) Get(key string) string {
	if value, ok := os.LookupEnv(key); ok {
		return strings.TrimSpace(value)
	}
	return strings.TrimSpace(s.values[key])
}

func (c Config) Timeout() time.Duration {
	if c.TimeoutSeconds <= 0 {
		return time.Duration(DefaultTimeoutSeconds) * time.Second
	}
	return time.Duration(c.TimeoutSeconds) * time.Second
}

func (c Config) ValidateWoo() Validation {
	missing := missingFields(map[string]string{
		"WOO_BASE_URL":        c.WooBaseURL,
		"WOO_CONSUMER_KEY":    c.WooConsumerKey,
		"WOO_CONSUMER_SECRET": c.WooConsumerSecret,
	})
	if len(missing) == 0 {
		return Validation{OK: true, Message: "complete"}
	}
	if len(missing) == 3 {
		return Validation{OK: false, Message: "missing WooCommerce configuration"}
	}
	return Validation{OK: false, Message: "incomplete WooCommerce configuration; missing " + strings.Join(missing, ", ")}
}

func (c Config) ValidateHertwill() Validation {
	if c.HertwillAccessToken != "" {
		return Validation{OK: true, Message: "access token configured"}
	}
	hasEmail := c.HertwillEmail != ""
	hasPassword := c.HertwillPassword != ""
	if hasEmail && hasPassword {
		return Validation{OK: true, Message: "email/password configured; login endpoint must be verified before use"}
	}
	if hasEmail || hasPassword {
		missing := "HERTWILL_PASSWORD"
		if hasPassword {
			missing = "HERTWILL_EMAIL"
		}
		return Validation{OK: false, Message: "incomplete Hertwill email/password configuration; missing " + missing}
	}
	return Validation{OK: false, Message: "missing Hertwill configuration"}
}

type Validation struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

func ValidateLimit(limit int) error {
	if limit <= 0 {
		return fmt.Errorf("limit must be positive")
	}
	if limit > MaxLimit {
		return fmt.Errorf("limit must be <= %d", MaxLimit)
	}
	return nil
}

func missingFields(fields map[string]string) []string {
	var missing []string
	for key, value := range fields {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	return missing
}

func loadDotEnv(path string, values map[string]string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			values[key] = value
		}
	}
	return true, scanner.Err()
}
