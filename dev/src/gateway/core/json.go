package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf16"

	"p11.local/compute/shared/strictjson"
)

type object map[string]json.RawMessage

// encoding/json otherwise replaces unpaired escaped surrogates with U+FFFD.
// Reject that lossy interpretation before the shared duplicate/depth scan.
func unicodeExact(raw []byte) bool {
	for i := 0; i < len(raw); i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		v, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if v >= 0xDC00 && v <= 0xDFFF {
			return false
		}
		if v >= 0xD800 && v <= 0xDBFF {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			w, e := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if e != nil || !utf16.IsSurrogate(rune(w)) || w < 0xDC00 {
				return false
			}
			i += 6
		}
	}
	return true
}

func parseObject(raw []byte, max int) (object, error) {
	var out object
	if !unicodeExact(raw) || strictjson.Decode(raw, &out, max) != nil || out == nil {
		return nil, errProtocol
	}
	return out, nil
}
func fields(o object, allowed string) bool {
	for k, v := range o {
		if !strings.Contains("|"+allowed+"|", "|"+k+"|") || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return false
		}
	}
	return true
}
func text(v json.RawMessage) (string, bool) {
	var s string
	e := json.Unmarshal(v, &s)
	return s, e == nil && len(v) > 0 && !bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}
func isText(v json.RawMessage) bool { _, ok := text(v); return ok }
func str(o object, k string) string { v, _ := text(o[k]); return v }
func number(v json.RawMessage, max uint64) (uint64, bool) {
	if len(v) == 0 || len(v) > 20 {
		return 0, false
	}
	for _, c := range v {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, e := strconv.ParseUint(string(v), 10, 64)
	return n, e == nil && n <= max
}
func boolean(v json.RawMessage) (bool, bool) {
	if string(v) == "true" {
		return true, true
	}
	if string(v) == "false" {
		return false, true
	}
	return false, false
}
func array(v json.RawMessage) ([]json.RawMessage, bool) {
	var a []json.RawMessage
	e := json.Unmarshal(v, &a)
	return a, e == nil && a != nil
}
func encode(v any) json.RawMessage   { b, _ := json.Marshal(v); return b }
func child(v json.RawMessage) object { o, _ := parseObject(v, 512<<10); return o }
func nullish(v json.RawMessage) bool {
	return len(v) == 0 || bytes.Equal(bytes.TrimSpace(v), []byte("null"))
}

type request struct {
	raw                  []byte
	stream, includeUsage bool
	maxOutput            int
	hash                 string
}

func validateRequest(raw []byte, b Binding, l Limits) (request, error) {
	o, e := parseObject(raw, l.RequestBytes)
	bad := func() (request, error) { return request{}, errProtocol }
	if e != nil || str(o, "model") != b.PublicModel {
		return bad()
	}
	var s bool
	if v, ok := o["stream"]; ok {
		var valid bool
		s, valid = boolean(v)
		if !valid {
			return bad()
		}
	}
	var n uint64
	var ok bool
	usage := false
	switch b.Profile {
	case ChatText:
		if !fields(o, "model|messages|max_tokens|max_completion_tokens|stream|stream_options|n|temperature|top_p|stop|store") {
			return bad()
		}
		if !messages(o["messages"], b.Profile) {
			return bad()
		}
		v, present := o["max_completion_tokens"]
		if present {
			if _, both := o["max_tokens"]; both {
				return bad()
			}
		} else {
			v = o["max_tokens"]
		}
		n, ok = number(v, uint64(l.MaxOutputTokens))
		if v, exists := o["n"]; exists && string(v) != "1" {
			return bad()
		}
		if v, exists := o["store"]; exists && string(v) != "false" {
			return bad()
		}
		if v, exists := o["stream_options"]; exists {
			x := child(v)
			if !s || x == nil || !fields(x, "include_usage") {
				return bad()
			}
			if v, exists = x["include_usage"]; exists {
				var good bool
				usage, good = boolean(v)
				if !good {
					return bad()
				}
			}
		}
	case ResponsesText:
		if !fields(o, "model|input|instructions|max_output_tokens|stream|store|background") || string(o["store"]) != "false" {
			return bad()
		}
		if v, present := o["background"]; present && string(v) != "false" {
			return bad()
		}
		if v, present := o["instructions"]; present && !isText(v) {
			return bad()
		}
		if !isText(o["input"]) && !messages(o["input"], b.Profile) {
			return bad()
		}
		n, ok = number(o["max_output_tokens"], uint64(l.MaxOutputTokens))
	case MessagesText:
		if !fields(o, "model|messages|system|max_tokens|stream|stop_sequences") || !messages(o["messages"], b.Profile) {
			return bad()
		}
		if v, present := o["system"]; present && !textContent(v, "text") {
			return bad()
		}
		n, ok = number(o["max_tokens"], uint64(l.MaxOutputTokens))
	}
	if !ok || n == 0 {
		return bad()
	}
	for _, k := range []string{"temperature", "top_p"} {
		if v, exists := o[k]; exists {
			var f float64
			max := 2.0
			if k == "top_p" {
				max = 1
			}
			if json.Unmarshal(v, &f) != nil || f < 0 || f > max {
				return bad()
			}
		}
	}
	for _, k := range []string{"stop", "stop_sequences"} {
		if v, exists := o[k]; exists {
			if k == "stop" && isText(v) {
				continue
			}
			a, valid := array(v)
			if !valid || len(a) > 4 {
				return bad()
			}
			for _, v := range a {
				if !isText(v) {
					return bad()
				}
			}
		}
	}
	d := sha256.Sum256(raw)
	o["model"] = encode(b.BackendModel)
	return request{encode(o), s, usage, int(n), hex.EncodeToString(d[:])}, nil
}

func messages(v json.RawMessage, p Profile) bool {
	a, ok := array(v)
	if !ok || len(a) < 1 || len(a) > 128 {
		return false
	}
	for _, v := range a {
		o := child(v)
		if o == nil || !fields(o, "role|content") {
			return false
		}
		r := str(o, "role")
		if r != "user" && r != "assistant" && !(p != MessagesText && (r == "system" || r == "developer")) {
			return false
		}
		kind := "text"
		if p == ResponsesText && r != "assistant" {
			kind = "input_text"
		}
		if p == ResponsesText && r == "assistant" {
			kind = "output_text"
		}
		if !textContent(o["content"], kind) {
			return false
		}
	}
	return true
}
func textContent(v json.RawMessage, kind string) bool {
	if isText(v) {
		return true
	}
	a, ok := array(v)
	if !ok || len(a) < 1 || len(a) > 128 {
		return false
	}
	for _, v := range a {
		o := child(v)
		if o == nil || !fields(o, "type|text") || str(o, "type") != kind || !isText(o["text"]) {
			return false
		}
	}
	return true
}

func protocolUsage(v json.RawMessage, p Profile) (*Usage, error) {
	if nullish(v) {
		return nil, nil
	}
	o := child(v)
	if o == nil {
		return nil, errProtocol
	}
	i, oKey := "input_tokens", "output_tokens"
	if p == ChatText {
		i, oKey = "prompt_tokens", "completion_tokens"
	}
	in, a := number(o[i], 1<<53-1)
	out, b := number(o[oKey], 1<<53-1)
	if !a || !b {
		return nil, errProtocol
	}
	if p != MessagesText {
		total, ok := number(o["total_tokens"], 1<<53-1)
		if !ok || total != in+out {
			return nil, errProtocol
		}
	}
	if p == MessagesText {
		for _, k := range []string{"cache_creation_input_tokens", "cache_read_input_tokens"} {
			if v, yes := o[k]; yes {
				if _, ok := number(v, 1<<53-1); !ok {
					return nil, errProtocol
				}
			}
		}
	}
	for _, pair := range []struct {
		key, field string
		max        uint64
	}{{"prompt_tokens_details", "cached_tokens", in}, {"input_tokens_details", "cached_tokens", in}, {"completion_tokens_details", "reasoning_tokens", out}, {"output_tokens_details", "reasoning_tokens", out}} {
		if v, yes := o[pair.key]; yes && !nullish(v) {
			d := child(v)
			if d == nil {
				return nil, errProtocol
			}
			if v, yes = d[pair.field]; yes {
				if _, ok := number(v, pair.max); !ok {
					return nil, errProtocol
				}
			}
		}
	}
	return &Usage{in, out}, nil
}
