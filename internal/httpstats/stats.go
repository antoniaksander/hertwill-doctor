package httpstats

import (
	"net/http"
	"sync"
)

type Stats struct {
	mu sync.Mutex

	HertwillAttempts int               `json:"hertwill_api_attempts"`
	HertwillFailures int               `json:"hertwill_api_failures"`
	WooAttempts      int               `json:"woocommerce_rest_attempts"`
	WooFailures      int               `json:"woocommerce_rest_failures"`
	RateLimitInfo    map[string]string `json:"rate_limit_info,omitempty"`
}

func (s *Stats) HertwillAttempt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.HertwillAttempts++
}

func (s *Stats) HertwillFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.HertwillFailures++
}

func (s *Stats) WooAttempt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.WooAttempts++
}

func (s *Stats) WooFailure() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.WooFailures++
}

func (s *Stats) RecordHertwillRateLimit(headers http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, key := range []string{"RateLimit", "RateLimit-Policy"} {
		value := headers.Get(key)
		if value == "" {
			continue
		}
		if s.RateLimitInfo == nil {
			s.RateLimitInfo = map[string]string{}
		}
		s.RateLimitInfo[key] = value
	}
}

func (s *Stats) Snapshot() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	rateLimitInfo := map[string]string(nil)
	if len(s.RateLimitInfo) > 0 {
		rateLimitInfo = make(map[string]string, len(s.RateLimitInfo))
		for key, value := range s.RateLimitInfo {
			rateLimitInfo[key] = value
		}
	}
	return Stats{
		HertwillAttempts: s.HertwillAttempts,
		HertwillFailures: s.HertwillFailures,
		WooAttempts:      s.WooAttempts,
		WooFailures:      s.WooFailures,
		RateLimitInfo:    rateLimitInfo,
	}
}
