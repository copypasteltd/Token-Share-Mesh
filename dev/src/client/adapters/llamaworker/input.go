package llamaworker

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}
type textBlock struct {
	Text string `json:"text"`
}

func content(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []textBlock
	_ = json.Unmarshal(raw, &blocks)
	var b strings.Builder
	for _, v := range blocks {
		b.WriteString(v.Text)
	}
	return b.String()
}

// normalize only follows the already strict native-text preflight. Additional
// local-profile limits are explicit rejections, never silently dropped fields.
func normalize(ctx context.Context, w supervisor.Work, m executionlease.Manifest, raw []byte, c Config) ([]byte, time.Time, error) {
	deadline, e := time.Parse(time.RFC3339Nano, m.HardDeadline)
	if e != nil || !deadline.After(time.Now()) || m.Profile != executionlease.ManifestProfile || len(raw) > maxBody || len(raw) != int(m.InputBytes) || !executionlease.Hash(m.InputDigest) || !devicecontrol.CheckPlaintextBinding(ctx, w.RequestID, m, raw) ||
		w.RequestID == "" || len(w.RequestID) > 192 || strings.IndexByte(w.RequestID, 0) >= 0 || w.LocalGeneration == "" || w.Environment != c.Environment || w.Resource != m.Resource || w.Resource != c.Resource ||
		m.EnvironmentDigest != c.Environment || m.Capability != c.Capability || m.EngineKind != "LLAMA_CPP_NATIVE" || m.EngineRelease != EngineRelease ||
		m.ModelDigest != c.Model.SHA256 || m.RecipeDigest != c.Template.SHA256 || m.Mode != "PLAINTEXT" || m.Privacy != "STANDARD" ||
		m.InputTokenLimit < 1 || m.InputTokenLimit > int64(c.ContextTokens) || m.Spec.MaxOutputTokens < 1 || m.Spec.MaxOutputTokens > int(c.ContextTokens) {
		return nil, time.Time{}, ErrInput
	}
	meta, e := relay.Preflight(m.Spec.Protocol, m.Spec.Model, raw)
	if e != nil || meta.MaxOutputTokens != m.Spec.MaxOutputTokens || meta.Stream != m.Spec.Stream {
		return nil, time.Time{}, ErrInput
	}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	var msgs []message
	add := func(role, text string) { b, _ := json.Marshal(text); msgs = append(msgs, message{role, b}) }
	switch m.Spec.Protocol {
	case relay.ChatText:
		_ = json.Unmarshal(obj["messages"], &msgs)
	case relay.ResponsesText:
		if v, ok := obj["instructions"]; ok {
			add("system", content(v))
		}
		var text string
		if json.Unmarshal(obj["input"], &text) == nil {
			add("user", text)
		} else {
			var tail []message
			_ = json.Unmarshal(obj["input"], &tail)
			msgs = append(msgs, tail...)
		}
	case relay.MessagesText:
		if v, ok := obj["system"]; ok {
			add("system", content(v))
		}
		var tail []message
		_ = json.Unmarshal(obj["messages"], &tail)
		msgs = append(msgs, tail...)
	default:
		return nil, time.Time{}, ErrInput
	}
	if len(msgs) < 1 || len(msgs) > 130 {
		return nil, time.Time{}, ErrInput
	}
	temperature, topP := 1.0, 1.0
	if v, ok := obj["temperature"]; ok {
		_ = json.Unmarshal(v, &temperature)
	}
	if v, ok := obj["top_p"]; ok {
		_ = json.Unmarshal(v, &topP)
	}
	// top_p=0 has no supported llama sampler semantics in this local profile.
	if math.IsNaN(temperature) || math.IsInf(temperature, 0) || topP <= 0 {
		return nil, time.Time{}, ErrInput
	}
	var stops []string
	for _, name := range []string{"stop", "stop_sequences"} {
		if v, ok := obj[name]; ok {
			var s string
			if json.Unmarshal(v, &s) == nil {
				stops = []string{s}
			} else {
				_ = json.Unmarshal(v, &stops)
			}
		}
	}
	for _, s := range stops {
		if len(s) < 1 || len(s) > 256 {
			return nil, time.Time{}, ErrInput
		}
	}
	var out encoder
	out.u32(uint32(m.InputTokenLimit))
	out.u32(uint32(m.Spec.MaxOutputTokens))
	out.real(temperature)
	out.real(topP)
	out.u32(uint32(len(stops)))
	for _, s := range stops {
		out.str(s)
	}
	out.u32(uint32(len(msgs)))
	for _, v := range msgs {
		s := content(v.Content)
		if strings.IndexByte(s, 0) >= 0 || !utf8.ValidString(s) {
			clear(out.Bytes())
			return nil, time.Time{}, ErrInput
		}
		out.str(v.Role)
		out.str(s)
	}
	if out.Len() > maxBody {
		clear(out.Bytes())
		return nil, time.Time{}, ErrInput
	}
	return out.Bytes(), deadline, nil
}
