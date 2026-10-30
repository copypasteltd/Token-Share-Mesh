package core

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"hash"
	"strings"
)

func alias(o object, b Binding) bool {
	if str(o, "model") != b.BackendModel {
		return false
	}
	o["model"] = encode(b.PublicModel)
	return true
}
func reason(v string, p Profile) bool {
	switch p {
	case ChatText:
		return v == "stop" || v == "length" || v == "content_filter"
	case MessagesText:
		return v == "end_turn" || v == "max_tokens" || v == "stop_sequence" || v == "refusal" || v == "model_context_window_exceeded"
	case ResponsesText:
		return v == "completed" || v == "incomplete" || v == "failed"
	}
	return false
}
func completion(v string, p Profile) string {
	if v == "length" || v == "max_tokens" || v == "model_context_window_exceeded" || v == "incomplete" {
		return "INCOMPLETE"
	}
	if v == "content_filter" || v == "refusal" {
		return "REFUSED"
	}
	if v == "failed" {
		return "FAILED"
	}
	return "COMPLETED"
}

func responseJSON(raw []byte, b Binding, max int) ([]byte, string, *Usage, error) {
	o, e := parseObject(raw, max)
	if e != nil || !identifier.MatchString(str(o, "id")) || !alias(o, b) {
		return nil, "", nil, errProtocol
	}
	u, e := protocolUsage(o["usage"], b.Profile)
	if e != nil {
		return nil, "", nil, e
	}
	var r string
	switch b.Profile {
	case ChatText:
		if !fieldsNullable(o, "id|object|created|model|choices|usage|system_fingerprint|service_tier") || str(o, "object") != "chat.completion" {
			return nil, "", nil, errProtocol
		}
		a, ok := array(o["choices"])
		if !ok || len(a) != 1 {
			return nil, "", nil, errProtocol
		}
		c := child(a[0])
		if c == nil || !fieldsNullable(c, "index|message|finish_reason|logprobs") || string(c["index"]) != "0" || !nullish(c["logprobs"]) {
			return nil, "", nil, errProtocol
		}
		m := child(c["message"])
		if m == nil || !fieldsNullable(m, "role|content|refusal|annotations") || str(m, "role") != "assistant" {
			return nil, "", nil, errProtocol
		}
		if !isText(m["content"]) && !(nullish(m["content"]) && isText(m["refusal"])) {
			return nil, "", nil, errProtocol
		}
		if !emptyOrNullArray(m["annotations"]) {
			return nil, "", nil, errProtocol
		}
		r = str(c, "finish_reason")
	case ResponsesText:
		if !fieldsNullable(o, "id|object|created_at|status|error|incomplete_details|instructions|max_output_tokens|model|output|parallel_tool_calls|previous_response_id|reasoning|store|temperature|text|tool_choice|tools|top_p|truncation|usage|user|metadata|background|service_tier|safety_identifier|prompt_cache_key|max_tool_calls|top_logprobs|completed_at") || str(o, "object") != "response" || !responseOutput(o["output"]) {
			return nil, "", nil, errProtocol
		}
		if string(o["store"]) != "false" || (!nullish(o["background"]) && string(o["background"]) != "false") || !nullish(o["previous_response_id"]) || !emptyOrNullArray(o["tools"]) {
			return nil, "", nil, errProtocol
		}
		r = str(o, "status")
		if r == "completed" && (!nullish(o["error"]) || !nullish(o["incomplete_details"])) {
			return nil, "", nil, errProtocol
		}
		if r == "incomplete" {
			d := child(o["incomplete_details"])
			if d == nil || !(str(d, "reason") == "max_output_tokens" || str(d, "reason") == "content_filter") {
				return nil, "", nil, errProtocol
			}
		}
		// Vendor errors may contain content/URLs. Use the local error writer.
		if r == "failed" {
			return nil, "", nil, errProtocol
		}
	case MessagesText:
		if !fieldsNullable(o, "id|type|role|model|content|stop_reason|stop_sequence|usage") || str(o, "type") != "message" || str(o, "role") != "assistant" || !textContent(o["content"], "text") {
			return nil, "", nil, errProtocol
		}
		r = str(o, "stop_reason")
	}
	if !reason(r, b.Profile) {
		return nil, "", nil, errProtocol
	}
	return encode(o), completion(r, b.Profile), u, nil
}

// Response metadata has documented nullable members. Request fields are stricter.
func fieldsNullable(o object, allowed string) bool {
	for k := range o {
		if !strings.Contains("|"+allowed+"|", "|"+k+"|") {
			return false
		}
	}
	return true
}
func emptyOrNullArray(v json.RawMessage) bool {
	if nullish(v) {
		return true
	}
	a, ok := array(v)
	return ok && len(a) == 0
}
func responseOutput(v json.RawMessage) bool {
	a, ok := array(v)
	if !ok || len(a) > 128 {
		return false
	}
	for _, v := range a {
		o := child(v)
		if o == nil || !fieldsNullable(o, "id|type|role|status|content") || str(o, "type") != "message" || str(o, "role") != "assistant" || !identifier.MatchString(str(o, "id")) {
			return false
		}
		if str(o, "status") != "completed" && str(o, "status") != "incomplete" {
			return false
		}
		parts, ok := array(o["content"])
		if !ok || len(parts) > 128 {
			return false
		}
		for _, v := range parts {
			p := child(v)
			if p == nil || !fieldsNullable(p, "type|text|annotations|logprobs") || str(p, "type") != "output_text" || !isText(p["text"]) || !emptyOrNullArray(p["annotations"]) || !emptyOrNullArray(p["logprobs"]) {
				return false
			}
		}
	}
	return true
}

type streamState struct {
	b                                                 Binding
	includeUsage                                      bool
	started, finished, terminal                       bool
	id, terminalKind                                  string
	usage                                             *Usage
	seq                                               uint64
	seqSeen                                           bool
	block                                             int
	blockOpen, partOpen, itemOpen, textDone, partDone bool
	itemID                                            string
	textHash                                          hash.Hash
}

// Each complete SSE event is validated before it can reach the buyer. Unknown
// semantic events fail this frozen text profile rather than being dropped.
func (s *streamState) event(name string, data []byte) ([]byte, error) {
	if s.terminal {
		return nil, errProtocol
	}
	if s.b.Profile == ChatText && string(data) == "[DONE]" {
		if name != "" || !s.finished || (s.includeUsage && s.usage == nil) {
			return nil, errProtocol
		}
		s.terminal = true
		return []byte("data: [DONE]\n\n"), nil
	}
	o, e := parseObject(data, 64<<10)
	if e != nil {
		return nil, e
	}
	switch s.b.Profile {
	case ChatText:
		if name != "" || !s.chat(o) {
			return nil, errProtocol
		}
	case MessagesText:
		if name != str(o, "type") || !s.messages(name, o) {
			return nil, errProtocol
		}
	case ResponsesText:
		if name != str(o, "type") || !s.responses(name, o) {
			return nil, errProtocol
		}
	}
	line := ""
	if name != "" {
		line = "event: " + name + "\n"
	}
	return append([]byte(line+"data: "), append(encode(o), []byte("\n\n")...)...), nil
}

func (s *streamState) chat(o object) bool {
	if !fieldsNullable(o, "id|object|created|model|choices|usage|system_fingerprint|service_tier|obfuscation") || str(o, "object") != "chat.completion.chunk" || !alias(o, s.b) {
		return false
	}
	id := str(o, "id")
	if !identifier.MatchString(id) || (s.started && s.id != id) {
		return false
	}
	s.started = true
	s.id = id
	a, ok := array(o["choices"])
	if !ok {
		return false
	}
	if len(a) == 0 {
		if !s.finished || s.usage != nil {
			return false
		}
		u, e := protocolUsage(o["usage"], ChatText)
		if e != nil || u == nil {
			return false
		}
		s.usage = u
		return true
	}
	if len(a) != 1 || s.finished || !nullish(o["usage"]) {
		return false
	}
	c := child(a[0])
	if c == nil || !fieldsNullable(c, "index|delta|finish_reason|logprobs") || string(c["index"]) != "0" || !nullish(c["logprobs"]) {
		return false
	}
	d := child(c["delta"])
	if d == nil || !fieldsNullable(d, "role|content|refusal") {
		return false
	}
	if v, yes := d["role"]; yes && string(v) != `"assistant"` {
		return false
	}
	for _, k := range []string{"content", "refusal"} {
		if v, yes := d[k]; yes && !nullish(v) && !isText(v) {
			return false
		}
	}
	if !nullish(c["finish_reason"]) {
		r := str(c, "finish_reason")
		if !reason(r, ChatText) {
			return false
		}
		s.finished = true
		s.terminalKind = completion(r, ChatText)
	}
	return true
}

func (s *streamState) messages(name string, o object) bool {
	if name == "ping" {
		return fieldsNullable(o, "type")
	}
	switch name {
	case "message_start":
		if s.started || !fieldsNullable(o, "type|message") {
			return false
		}
		m := child(o["message"])
		if m == nil || !fieldsNullable(m, "id|type|role|content|model|stop_reason|stop_sequence|usage") || str(m, "type") != "message" || str(m, "role") != "assistant" || !emptyOrNullArray(m["content"]) || !nullish(m["stop_reason"]) || !alias(m, s.b) {
			return false
		}
		id := str(m, "id")
		u, e := protocolUsage(m["usage"], MessagesText)
		if !identifier.MatchString(id) || e != nil || u == nil {
			return false
		}
		s.id = id
		s.usage = u
		s.started = true
		o["message"] = encode(m)
		return true
	case "content_block_start":
		if !s.started || s.finished || s.blockOpen || s.block >= 128 || !fieldsNullable(o, "type|index|content_block") {
			return false
		}
		i, ok := number(o["index"], 127)
		b := child(o["content_block"])
		if !ok || int(i) != s.block || b == nil || !fields(b, "type|text") || str(b, "type") != "text" || !isText(b["text"]) {
			return false
		}
		s.blockOpen = true
		return true
	case "content_block_delta":
		if !s.blockOpen || !fieldsNullable(o, "type|index|delta") {
			return false
		}
		i, ok := number(o["index"], 127)
		d := child(o["delta"])
		return ok && int(i) == s.block && d != nil && fields(d, "type|text") && str(d, "type") == "text_delta" && isText(d["text"])
	case "content_block_stop":
		if !s.blockOpen || !fieldsNullable(o, "type|index") {
			return false
		}
		i, ok := number(o["index"], 127)
		if !ok || int(i) != s.block {
			return false
		}
		s.blockOpen = false
		s.block++
		return true
	case "message_delta":
		if !s.started || s.blockOpen || s.finished || !fieldsNullable(o, "type|delta|usage") {
			return false
		}
		d := child(o["delta"])
		u := child(o["usage"])
		if d == nil || !fieldsNullable(d, "stop_reason|stop_sequence") || !reason(str(d, "stop_reason"), MessagesText) || u == nil || !fields(u, "output_tokens") {
			return false
		}
		n, ok := number(u["output_tokens"], 1<<53-1)
		if !ok || n < s.usage.OutputTokens {
			return false
		}
		s.usage = &Usage{s.usage.InputTokens, n}
		s.finished = true
		s.terminalKind = completion(str(d, "stop_reason"), MessagesText)
		return true
	case "message_stop":
		if !s.finished || s.blockOpen || !fieldsNullable(o, "type") {
			return false
		}
		s.terminal = true
		return true
	}
	return false
}

func (s *streamState) responses(name string, o object) bool {
	n, ok := number(o["sequence_number"], 1<<53-1)
	if !ok || (s.seqSeen && n <= s.seq) {
		return false
	}
	s.seq = n
	s.seqSeen = true
	if name == "response.created" {
		if s.started || !fieldsNullable(o, "type|sequence_number|response") {
			return false
		}
		r := child(o["response"])
		if r == nil || str(r, "object") != "response" || str(r, "status") != "in_progress" || string(r["store"]) != "false" || !emptyOrNullArray(r["output"]) || !alias(r, s.b) || !identifier.MatchString(str(r, "id")) {
			return false
		}
		s.id = str(r, "id")
		s.started = true
		o["response"] = encode(r)
		return true
	}
	if !s.started {
		return false
	}
	if name == "response.in_progress" {
		r := child(o["response"])
		if s.itemOpen || r == nil || str(r, "id") != s.id || str(r, "status") != "in_progress" || string(r["store"]) != "false" || !emptyOrNullArray(r["output"]) || !alias(r, s.b) {
			return false
		}
		o["response"] = encode(r)
		return true
	}
	if name == "response.completed" || name == "response.incomplete" {
		if s.itemOpen || s.partOpen || !s.finished {
			return false
		}
		r := child(o["response"])
		if r == nil || str(r, "id") != s.id || name != "response."+str(r, "status") {
			return false
		}
		items, ok := array(r["output"])
		if !ok || len(items) != 1 || str(child(items[0]), "id") != s.itemID || !s.sameOutput(items[0]) {
			return false
		}
		v, kind, u, e := responseJSON(encode(r), s.b, 64<<10)
		if e != nil {
			return false
		}
		s.terminal = true
		s.terminalKind = kind
		s.usage = u
		o["response"] = v
		return true
	}
	i, indexOK := number(o["output_index"], 0)
	_ = i
	if !indexOK {
		return false
	}
	switch name {
	case "response.output_item.added":
		if s.itemOpen || s.finished {
			return false
		}
		item := child(o["item"])
		if item == nil || !fieldsNullable(item, "id|type|role|status|content") || str(item, "type") != "message" || str(item, "role") != "assistant" || str(item, "status") != "in_progress" || !emptyOrNullArray(item["content"]) || !identifier.MatchString(str(item, "id")) {
			return false
		}
		s.itemID = str(item, "id")
		s.itemOpen = true
		return true
	case "response.output_item.done":
		if !s.itemOpen || s.partOpen || !s.partDone {
			return false
		}
		item := child(o["item"])
		if item == nil || str(item, "id") != s.itemID || !responseOutput(encode([]object{item})) || !s.sameOutput(o["item"]) {
			return false
		}
		s.itemOpen = false
		s.finished = true
		return true
	}
	if str(o, "item_id") != s.itemID || !s.itemOpen {
		return false
	}
	if _, ok := number(o["content_index"], 0); !ok {
		return false
	}
	switch name {
	case "response.content_part.added":
		if s.partOpen || s.partDone {
			return false
		}
		p := child(o["part"])
		if p == nil || !fieldsNullable(p, "type|text|annotations|logprobs") || str(p, "type") != "output_text" || str(p, "text") != "" || !emptyOrNullArray(p["annotations"]) || !emptyOrNullArray(p["logprobs"]) {
			return false
		}
		s.partOpen = true
		s.textHash = sha256.New()
		return true
	case "response.output_text.delta":
		if !s.partOpen || s.textDone || !isText(o["delta"]) {
			return false
		}
		_, _ = s.textHash.Write([]byte(str(o, "delta")))
		return true
	case "response.output_text.done":
		if !s.partOpen || s.textDone || !s.sameText(o["text"]) {
			return false
		}
		s.textDone = true
		return true
	case "response.content_part.done":
		if !s.partOpen || !s.textDone {
			return false
		}
		p := child(o["part"])
		if p == nil || str(p, "type") != "output_text" || !s.sameText(p["text"]) || !emptyOrNullArray(p["annotations"]) || !emptyOrNullArray(p["logprobs"]) {
			return false
		}
		s.partOpen = false
		s.partDone = true
		return true
	}
	return false
}

func (s *streamState) sameText(v json.RawMessage) bool {
	if s.textHash == nil || !isText(v) {
		return false
	}
	h := sha256.Sum256([]byte(stringValue(v)))
	return bytes.Equal(h[:], s.textHash.Sum(nil))
}
func stringValue(v json.RawMessage) string { s, _ := text(v); return s }
func (s *streamState) sameOutput(v json.RawMessage) bool {
	a, ok := array(child(v)["content"])
	return ok && len(a) == 1 && s.sameText(child(a[0])["text"])
}
