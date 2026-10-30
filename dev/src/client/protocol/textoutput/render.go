// Package textoutput renders an already authorized original text result as
// OpenAI Chat/Responses or Anthropic Messages. It grants no execution, funding,
// trusted usage or final buyer ACK, and creates no listener or credential.
package textoutput

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"

	"p11.local/compute/client/supervisor"
	relay "p11.local/compute/gateway/core"
)

var ErrOutput = errors.New("original text result delivery failed")
var ErrLimit = errors.New("text protocol result boundary exceeded")

// Sink must honor ctx and a bounded write deadline. A generic io.Writer is not
// sufficient: a blocked client must not hold an unbounded delivery goroutine.
type Sink interface {
	Write(context.Context, []byte) error
}
type Limits struct{ ResponseBytes, EventBytes, TextBytes int }

func DefaultLimits() Limits { return Limits{512 << 10, 64 << 10, 512 << 10} }

// Receipt means the local sink accepted bytes only. Counts are native protocol
// candidates, NEVER trusted usage/settlement. No acceptance ACK is synthesized.
type Receipt struct {
	ResultID                  string
	Profile                   relay.Profile
	InputTokens, OutputTokens uint32
	Bytes                     int64
	Terminal                  string
}

type object = map[string]any
type renderer struct {
	ctx        context.Context
	sink       Sink
	out        supervisor.TextOutput
	descriptor supervisor.TextDescriptor
	limits     Limits
	sequence   int
	receipt    Receipt
}

func (r *renderer) encoded(name string, o object) ([]byte, error) {
	if name != "" {
		o["type"] = name
		if r.descriptor.Manifest.Spec.Protocol == relay.ResponsesText {
			o["sequence_number"] = r.sequence
			r.sequence++
		}
	}
	b, e := json.Marshal(o)
	if e != nil {
		return nil, ErrOutput
	}
	max := r.limits.ResponseBytes
	if r.descriptor.Manifest.Spec.Stream {
		max = r.limits.EventBytes
	}
	if len(b) > max {
		clear(b)
		return nil, ErrLimit
	}
	if r.descriptor.Manifest.Spec.Stream {
		prefix := ""
		if name != "" {
			prefix = "event: " + name + "\n"
		}
		frame := make([]byte, 0, len(prefix)+len(b)+8)
		frame = append(frame, prefix...)
		frame = append(frame, "data: "...)
		frame = append(frame, b...)
		frame = append(frame, '\n', '\n')
		clear(b)
		return frame, nil
	}
	return b, nil
}
func (r *renderer) write(b []byte) error {
	defer clear(b)
	if r.ctx.Err() != nil {
		return r.ctx.Err()
	}
	if e := r.sink.Write(r.ctx, b); e != nil {
		return ErrOutput
	}
	r.receipt.Bytes += int64(len(b))
	return nil
}
func (r *renderer) event(name string, o object) error {
	b, e := r.encoded(name, o)
	if e != nil {
		return e
	}
	return r.write(b)
}
func (r *renderer) id() string { return r.descriptor.ResultID }
func (r *renderer) chat(choices []object, usage any) object {
	o := object{"id": "chatcmpl_" + r.id(), "object": "chat.completion.chunk", "created": r.descriptor.CreatedUnix, "model": r.descriptor.Manifest.Spec.Model, "choices": choices}
	if r.descriptor.IncludeUsage {
		o["usage"] = usage
	}
	return o
}
func choice(delta object, finish any) []object {
	return []object{{"index": 0, "delta": delta, "finish_reason": finish, "logprobs": nil}}
}
func (r *renderer) response(status string, items []object, usage any) object {
	o := object{"id": "resp_" + r.id(), "object": "response", "created_at": r.descriptor.CreatedUnix, "model": r.descriptor.Manifest.Spec.Model, "status": status, "store": false, "output": items, "usage": usage, "error": nil, "incomplete_details": nil}
	if status == "incomplete" {
		o["incomplete_details"] = object{"reason": "max_output_tokens"}
	}
	return o
}
func (r *renderer) item(status, text string) object {
	return object{"id": "msg_" + r.id(), "type": "message", "role": "assistant", "status": status, "content": []object{part(text)}}
}
func part(text string) object {
	return object{"type": "output_text", "text": text, "annotations": []any{}, "logprobs": []any{}}
}
func (r *renderer) content(o object) object {
	o["item_id"] = "msg_" + r.id()
	o["output_index"] = 0
	o["content_index"] = 0
	return o
}

func (r *renderer) begin(s supervisor.TextStart) error {
	switch r.descriptor.Manifest.Spec.Protocol {
	case relay.ChatText:
		return r.event("", r.chat(choice(object{"role": "assistant", "content": ""}, nil), nil))
	case relay.MessagesText:
		if e := r.event("message_start", object{"message": object{"id": "msg_" + r.id(), "type": "message", "role": "assistant", "model": r.descriptor.Manifest.Spec.Model, "content": []any{}, "stop_reason": nil, "stop_sequence": nil, "usage": object{"input_tokens": s.InputTokens, "output_tokens": 0}}}); e != nil {
			return e
		}
		return r.event("content_block_start", object{"index": 0, "content_block": object{"type": "text", "text": ""}})
	case relay.ResponsesText:
		if e := r.event("response.created", object{"response": r.response("in_progress", []object{}, nil)}); e != nil {
			return e
		}
		item := r.item("in_progress", "")
		item["content"] = []any{}
		if e := r.event("response.output_item.added", object{"output_index": 0, "item": item}); e != nil {
			return e
		}
		return r.event("response.content_part.added", r.content(object{"part": part("")}))
	}
	return ErrOutput
}

func (r *renderer) delta(s string) error {
	switch r.descriptor.Manifest.Spec.Protocol {
	case relay.ChatText:
		return r.event("", r.chat(choice(object{"content": s}, nil), nil))
	case relay.MessagesText:
		return r.event("content_block_delta", object{"index": 0, "delta": object{"type": "text_delta", "text": s}})
	case relay.ResponsesText:
		return r.event("response.output_text.delta", r.content(object{"delta": s, "logprobs": []any{}}))
	}
	return ErrOutput
}

func (r *renderer) finish(text string, v supervisor.TextResult) error {
	p := r.descriptor.Manifest.Spec.Protocol
	usage := object{"input_tokens": v.InputTokens, "output_tokens": v.OutputTokens}
	reason := "stop"
	anthropic := "end_turn"
	status := "completed"
	if v.Reason == "limit" {
		reason = "length"
		anthropic = "max_tokens"
		status = "incomplete"
	}
	var stop any
	if v.Reason == "stop" {
		b, e := r.out.StopSequence(r.ctx, v.StopIndex)
		if e != nil || len(b) < 1 || len(b) > 256 || !utf8.Valid(b) {
			clear(b)
			return ErrOutput
		}
		if p == relay.MessagesText {
			stop = string(b)
			anthropic = "stop_sequence"
		}
		clear(b)
	}
	if p == relay.ChatText {
		usage = object{"prompt_tokens": v.InputTokens, "completion_tokens": v.OutputTokens, "total_tokens": uint64(v.InputTokens) + uint64(v.OutputTokens)}
	} else if p == relay.ResponsesText {
		usage["total_tokens"] = uint64(v.InputTokens) + uint64(v.OutputTokens)
	}
	var frames [][]byte
	add := func(name string, o object) error {
		b, e := r.encoded(name, o)
		if e == nil {
			frames = append(frames, b)
		}
		return e
	}
	defer func() {
		for _, b := range frames {
			clear(b)
		}
	}()
	var e error
	if !r.descriptor.Manifest.Spec.Stream {
		switch p {
		case relay.ChatText:
			e = add("", object{"id": "chatcmpl_" + r.id(), "object": "chat.completion", "created": r.descriptor.CreatedUnix, "model": r.descriptor.Manifest.Spec.Model, "choices": []object{{"index": 0, "message": object{"role": "assistant", "content": text}, "finish_reason": reason, "logprobs": nil}}, "usage": usage})
		case relay.MessagesText:
			e = add("", object{"id": "msg_" + r.id(), "type": "message", "role": "assistant", "model": r.descriptor.Manifest.Spec.Model, "content": []object{{"type": "text", "text": text}}, "stop_reason": anthropic, "stop_sequence": stop, "usage": usage})
		case relay.ResponsesText:
			e = add("", r.response(status, []object{r.item(status, text)}, usage))
		}
	} else {
		switch p {
		case relay.ChatText:
			e = add("", r.chat(choice(object{}, reason), nil))
			if e == nil && r.descriptor.IncludeUsage {
				e = add("", r.chat([]object{}, usage))
			}
			if e == nil {
				frames = append(frames, []byte("data: [DONE]\n\n"))
			}
		case relay.MessagesText:
			e = add("content_block_stop", object{"index": 0})
			if e == nil {
				e = add("message_delta", object{"delta": object{"stop_reason": anthropic, "stop_sequence": stop}, "usage": object{"output_tokens": v.OutputTokens}})
			}
			if e == nil {
				e = add("message_stop", object{})
			}
		case relay.ResponsesText:
			e = add("response.output_text.done", r.content(object{"text": text, "logprobs": []any{}}))
			if e == nil {
				e = add("response.content_part.done", r.content(object{"part": part(text)}))
			}
			if e == nil {
				e = add("response.output_item.done", object{"output_index": 0, "item": r.item(status, text)})
			}
			if e == nil {
				e = add("response."+status, object{"response": r.response(status, []object{r.item(status, text)}, usage)})
			}
		}
	}
	// Preencode ALL terminal events before sending any. Final response/event
	// overflow must not produce a partial false successful terminal sequence.
	if e != nil {
		return e
	}
	for _, b := range frames {
		if e = r.write(b); e != nil {
			return e
		}
	}
	r.receipt.InputTokens = v.InputTokens
	r.receipt.OutputTokens = v.OutputTokens
	r.receipt.Terminal = "COMPLETED"
	if status == "incomplete" {
		r.receipt.Terminal = "INCOMPLETE"
	}
	return nil
}

// Render closes its reader on every path. Cancellation/invalid UTF-8/native
// error/missing authenticated END/size overflow never creates a success tail.
// The finite complete-text buffer is needed by Responses' repeated final text;
// its serialized final event can hit EventBytes before TextBytes. Admission
// must explicitly budget that profile before enabling public execution.
func Render(ctx context.Context, sink Sink, out supervisor.TextOutput, limits Limits) (Receipt, error) {
	if out == nil || sink == nil {
		return Receipt{}, ErrOutput
	}
	defer out.Close(context.Background())
	d := out.Descriptor()
	p := d.Manifest.Spec.Protocol
	r := renderer{ctx: ctx, sink: sink, out: out, descriptor: d, limits: limits, receipt: Receipt{ResultID: d.ResultID, Profile: p}}
	if limits.ResponseBytes < 4096 || limits.ResponseBytes > 8<<20 || limits.EventBytes < 2048 || limits.EventBytes > 256<<10 || limits.TextBytes < 1 || limits.TextBytes > 8<<20 || d.ResultID == "" || len(d.ResultID) > 96 || d.CreatedUnix <= 0 || (p != relay.ChatText && p != relay.ResponsesText && p != relay.MessagesText) {
		return r.receipt, ErrOutput
	}
	start, e := out.Started(ctx)
	if e != nil || start.InputTokens == 0 || uint64(start.InputTokens) > uint64(d.Manifest.InputTokenLimit) {
		return r.receipt, ErrOutput
	}
	if d.Manifest.Spec.Stream {
		if e = r.begin(start); e != nil {
			return r.receipt, e
		}
	}
	var text, tail []byte
	defer func() { clear(text[:cap(text)]); clear(tail[:cap(tail)]) }()
	for {
		b, readErr := out.Read(ctx)
		if len(b) > 0 {
			if len(text)+len(tail)+len(b) > limits.TextBytes {
				clear(b[:cap(b)])
				return r.receipt, ErrLimit
			}
			joined := append(tail, b...)
			tail = nil
			clear(b[:cap(b)])
			at := 0
			for at < len(joined) && utf8.FullRune(joined[at:]) {
				v, n := utf8.DecodeRune(joined[at:])
				if v == utf8.RuneError && n == 1 {
					clear(joined[:cap(joined)])
					return r.receipt, ErrOutput
				}
				at += n
			}
			if len(joined)-at > 3 {
				clear(joined[:cap(joined)])
				return r.receipt, ErrOutput
			}
			tail = append([]byte(nil), joined[at:]...)
			text = append(text, joined[:at]...)
			if d.Manifest.Spec.Stream {
				// Worst-case JSON escaping is six bytes per raw byte. Slice only on rune
				// boundaries, leaving fixed metadata room inside the event bound.
				max := (limits.EventBytes - 1536) / 6
				for pos := 0; pos < at; {
					end := pos + max
					if end > at {
						end = at
					}
					for end > pos && end < at && !utf8.RuneStart(joined[end]) {
						end--
					}
					if end == pos {
						clear(joined)
						return r.receipt, ErrLimit
					}
					e = r.delta(string(joined[pos:end]))
					if e != nil {
						clear(joined)
						return r.receipt, e
					}
					pos = end
				}
			}
			clear(joined[:cap(joined)])
		}
		if readErr != nil {
			if readErr != io.EOF || len(tail) > 0 {
				return r.receipt, ErrOutput
			}
			break
		}
		if len(b) == 0 {
			return r.receipt, ErrOutput
		}
	}
	v, e := out.Result(ctx)
	if e != nil || ctx.Err() != nil || v.InputTokens != start.InputTokens || uint64(v.OutputTokens) > uint64(d.Manifest.Spec.MaxOutputTokens) || (v.Reason != "eog" && v.Reason != "limit" && v.Reason != "stop") || (v.Reason != "stop" && v.StopIndex != supervisor.NoStopIndex) {
		return r.receipt, ErrOutput
	}
	e = r.finish(string(text), v)
	return r.receipt, e
}
