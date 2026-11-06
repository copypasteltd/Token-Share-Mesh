package node

import (
	"context"
	"encoding/base64"
	"io"
	"sync"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

type resultUploadCheckpoint struct {
	Head         string `json:"head_digest"`
	IncludeUsage bool   `json:"include_usage"`
	Ready        bool   `json:"ready"`
}

func (h *Host) contentHead(c hostCommand, include bool) (resultwire.ContentHead, error) {
	var out resultwire.ContentHead
	var ack deliveryAck
	var m executionlease.Manifest
	if c.ResultDelivery == nil || !validDeliveryAck(c.ResultDeliveryAck, *c.ResultDelivery) || strictjson.Decode(c.ResultDeliveryAck, &ack, 4096) != nil || strictjson.Decode(c.Manifest, &m, 16384) != nil {
		return out, ErrHostPending
	}
	key := h.node.key()
	defer clear(key)
	if len(key) != 64 {
		return out, ErrHost
	}
	d := *c.ResultDelivery
	completed, e := time.Parse(time.RFC3339Nano, d.Completed)
	if e != nil {
		return out, ErrHost
	}
	out = resultwire.ContentHead{Profile: resultwire.ContentProfile, Delivery: ack.ID, Declaration: d, Public: base64.RawURLEncoding.EncodeToString(key[32:]), Manifest: m, IncludeUsage: include, ChunkBytes: resultwire.ChunkBytes, Chunks: resultwire.ChunkCount(d.OutputBytes), Expires: completed.Add(30 * time.Minute).UTC().Format(time.RFC3339Nano), State: "UPLOADING"}
	if out.Validate() != nil {
		return resultwire.ContentHead{}, ErrHost
	}
	return out, nil
}
func (h *Host) validateContentCheckpoint(c hostCommand) bool {
	if c.ResultContent == nil {
		return true
	}
	head, e := h.contentHead(c, c.ResultContent.IncludeUsage)
	return e == nil && head.Digest() == c.ResultContent.Head
}
func checkContentAck(raw []byte, head resultwire.ContentHead, index *int, digest string, ready bool) error {
	var a resultwire.ContentAck
	if strictjson.Decode(raw, &a, 4096) != nil || a.Delivery != head.Delivery || a.Head != head.Digest() || a.Digest != digest || (index == nil) != (a.Index == nil) || index != nil && *index != *a.Index || (a.State != "UPLOADING" && a.State != "READY") || ready && a.State != "READY" {
		return ErrHostPending
	}
	return nil
}
func (h *Host) sendContent(ctx context.Context, head resultwire.ContentHead, step string, body any, index *int, digest string, ready bool) error {
	reply, e := h.client.SendResultContent(ctx, head.Declaration.Scope.Attempt, step, devicecontrol.ContentCommandKey(step, body), body)
	defer clear(reply.Body)
	if e != nil {
		return e
	}
	if reply.Status != 200 {
		return classify(reply)
	}
	return checkContentAck(reply.Body, head, index, digest, ready)
}

// PublishResult never calls Start/Submit. The first pass records the exact
// original result at EOF; the second pass transfers that same encrypted result.
// Four bounded transfer workers reduce RTT cost, without holding the GPU lock.
func (p *PreparedExecution) PublishResult(ctx context.Context) (resultwire.ContentAck, error) {
	var zero resultwire.ContentAck
	c, e := p.host.command(ctx, p.id)
	if e != nil {
		return zero, e
	}
	if c.ResultContent != nil && c.ResultContent.Ready {
		head, e := p.host.contentHead(c, c.ResultContent.IncludeUsage)
		if e != nil {
			return zero, e
		}
		return resultwire.ContentAck{Delivery: head.Delivery, Head: head.Digest(), State: "READY"}, nil
	}
	if c.ResultDelivery == nil {
		out, e := p.Output(ctx)
		if e != nil {
			return zero, e
		}
		for {
			b, re := out.Read(ctx)
			empty := len(b) == 0
			clear(b)
			if empty && re == nil {
				_ = out.Close(context.Background())
				return zero, ErrHostPending
			}
			if re != nil {
				_ = out.Close(context.Background())
				if re != io.EOF {
					return zero, re
				}
				break
			}
		}
	}
	if _, e = p.host.ReportDelivery(ctx, p.id); e != nil {
		return zero, e
	}
	c, e = p.host.command(ctx, p.id)
	if e != nil {
		return zero, e
	}
	out, e := p.Output(ctx)
	if e != nil {
		return zero, e
	}
	defer out.Close(context.Background())
	head, e := p.host.contentHead(c, out.Descriptor().IncludeUsage)
	if e != nil {
		return zero, e
	}
	if out.Descriptor().Manifest.Digest() != head.Manifest.Digest() || out.Descriptor().ResultID != head.Declaration.Result {
		return zero, ErrHostPolicy
	}
	expires, _ := time.Parse(time.RFC3339Nano, head.Expires)
	if !time.Now().Before(expires) {
		return zero, ErrHostPending
	}
	e = p.host.update(ctx, p.id, func(c *hostCommand) error {
		if c.ResultContent != nil {
			if c.ResultContent.Head != head.Digest() || c.ResultContent.IncludeUsage != head.IncludeUsage {
				return ErrHostPolicy
			}
			return nil
		}
		c.ResultContent = &resultUploadCheckpoint{Head: head.Digest(), IncludeUsage: head.IncludeUsage}
		return nil
	})
	if e != nil {
		return zero, e
	}
	begin := resultwire.BeginContent{Scope: head.Declaration.Scope, Delivery: head.Delivery, IncludeUsage: head.IncludeUsage}
	if e = p.host.sendContent(ctx, head, "begin", begin, nil, "", false); e != nil {
		return zero, e
	}
	start, e := out.Started(ctx)
	if e != nil || int64(start.InputTokens) != head.Declaration.InputTokens {
		return zero, ErrHostPending
	}
	owned, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan resultwire.ContentChunk)
	var wg sync.WaitGroup
	var first error
	var mu sync.Mutex
	fail := func(e error) {
		mu.Lock()
		if first == nil {
			first = e
			cancel()
		}
		mu.Unlock()
	}
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range jobs {
				if owned.Err() == nil {
					if e := p.host.sendContent(owned, head, "chunks", chunk, &chunk.Index, chunk.Digest, false); e != nil {
						fail(e)
					}
				}
				clear(chunk.Content)
			}
		}()
	}
	buffer := make([]byte, 0, resultwire.ChunkBytes)
	index := 0
	dispatch := func(b []byte) error {
		chunk := resultwire.ContentChunk{Profile: resultwire.ContentProfile, Scope: head.Declaration.Scope, Delivery: head.Delivery, Head: head.Digest(), Index: index, Digest: resultwire.BytesDigest(b), Content: b}
		index++
		select {
		case jobs <- chunk:
			return nil
		case <-owned.Done():
			clear(b)
			return owned.Err()
		}
	}
	reading := true
	for reading {
		b, re := out.Read(owned)
		if len(b) == 0 && re == nil {
			fail(ErrHostPending)
			break
		}
		for at := 0; at < len(b); {
			take := resultwire.ChunkBytes - len(buffer)
			if left := len(b) - at; take > left {
				take = left
			}
			buffer = append(buffer, b[at:at+take]...)
			at += take
			if len(buffer) == resultwire.ChunkBytes {
				if e = dispatch(buffer); e != nil {
					break
				}
				buffer = make([]byte, 0, resultwire.ChunkBytes)
			}
		}
		clear(b)
		if e != nil {
			fail(e)
			break
		}
		if re != nil {
			reading = false
			if re != io.EOF {
				fail(re)
			}
		}
	}
	if owned.Err() == nil && len(buffer) > 0 {
		e = dispatch(buffer)
		buffer = nil
		if e != nil {
			fail(e)
		}
	}
	clear(buffer)
	close(jobs)
	wg.Wait()
	if first != nil {
		return zero, first
	}
	if owned.Err() != nil {
		return zero, owned.Err()
	}
	if index != head.Chunks {
		return zero, ErrHostPending
	}
	result, e := out.Result(ctx)
	if e != nil || int64(result.InputTokens) != head.Declaration.InputTokens || int64(result.OutputTokens) != head.Declaration.OutputTokens || result.Reason != head.Declaration.Finish {
		return zero, ErrHostPending
	}
	terminal := resultwire.End{}
	if result.Reason == "stop" {
		stop, e := out.StopSequence(ctx, result.StopIndex)
		if e != nil {
			return zero, e
		}
		terminal.Stop = string(stop)
		clear(stop)
	}
	seal := resultwire.SealContent{Scope: head.Declaration.Scope, Delivery: head.Delivery, Head: head.Digest(), End: terminal}
	if e = p.host.sendContent(ctx, head, "seal", seal, nil, "", true); e != nil {
		return zero, e
	}
	if e = p.host.update(ctx, p.id, func(c *hostCommand) error {
		if c.ResultContent == nil || c.ResultContent.Head != head.Digest() {
			return ErrHost
		}
		c.ResultContent.Ready = true
		return nil
	}); e != nil {
		return zero, e
	}
	return resultwire.ContentAck{Delivery: head.Delivery, Head: head.Digest(), State: "READY"}, nil
}

// RecoverResultPublication queries an original upload only. It can confirm an
// already sealed remote result after local content keys were lost on restart.
// Incomplete uploads require the original local result; they never rerun it.
func (h *Host) RecoverResultPublication(ctx context.Context, id string) (resultwire.ContentAck, error) {
	var zero resultwire.ContentAck
	owned, finish, e := h.beginWork(ctx, id)
	if e != nil {
		return zero, e
	}
	defer finish()
	c, e := h.command(owned, id)
	if e != nil {
		return zero, e
	}
	if c.ResultContent == nil {
		return zero, ErrHostPending
	}
	head, e := h.contentHead(c, c.ResultContent.IncludeUsage)
	if e != nil {
		return zero, e
	}
	reply, e := h.client.ReadResultContentStatus(owned, head.Declaration.Scope.Attempt, head.Delivery)
	defer clear(reply.Body)
	if e != nil {
		return zero, e
	}
	if reply.Status != 200 {
		return zero, classify(reply)
	}
	if e = checkContentAck(reply.Body, head, nil, "", false); e != nil {
		return zero, e
	}
	var ack resultwire.ContentAck
	_ = strictjson.Decode(reply.Body, &ack, 4096)
	if ack.State == "READY" {
		e = h.update(owned, id, func(c *hostCommand) error {
			if c.ResultContent == nil || c.ResultContent.Head != head.Digest() {
				return ErrHost
			}
			c.ResultContent.Ready = true
			return nil
		})
	}
	return ack, e
}
