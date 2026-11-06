package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"time"

	"p11.local/compute/internal/invocation"
	"p11.local/compute/shared/strictjson"
)

func configuredResultContent(completion *invocation.Completion) (*invocation.ResultContent, error) {
	path := os.Getenv("P11_RESULT_CONTENT_KEYS_FILE")
	if path == "" {
		return nil, nil
	}
	if completion == nil {
		return nil, fmt.Errorf("original completion is required for result content")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, fmt.Errorf("result content key file unavailable")
	}
	defer f.Close()
	raw, e := io.ReadAll(io.LimitReader(f, 8193))
	defer clear(raw)
	if e != nil || len(raw) < 1 || len(raw) > 8192 {
		return nil, fmt.Errorf("bounded result content key file required")
	}
	var c struct {
		Profile string            `json:"profile_id"`
		Current string            `json:"current_key_version"`
		Keys    map[string]string `json:"keys"`
	}
	if strictjson.Decode(raw, &c, 8192) != nil || c.Profile != "P11_RESULT_CONTENT_KEYS_1" || len(c.Keys) < 1 || len(c.Keys) > 16 {
		return nil, fmt.Errorf("explicit result content key ring required")
	}
	keys := map[string][]byte{}
	defer func() {
		for _, k := range keys {
			clear(k)
		}
	}()
	for id, encoded := range c.Keys {
		key, e := base64.RawURLEncoding.Strict().DecodeString(encoded)
		if e != nil || len(key) != 32 || base64.RawURLEncoding.EncodeToString(key) != encoded {
			clear(key)
			return nil, fmt.Errorf("canonical result content key required")
		}
		keys[id] = key
	}
	return invocation.NewResultContent(completion, c.Current, keys)
}

func runResultRetirement(ctx context.Context, s *invocation.ResultContent) {
	if s == nil {
		return
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		bounded, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, _ = s.SweepExpired(bounded, 32)
		cancel()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
