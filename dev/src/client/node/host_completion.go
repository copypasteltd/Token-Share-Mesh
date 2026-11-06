package node

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"io"
	"sync"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

// custodyOutput hashes exactly the authenticated bytes already transferred to
// its consumer. No second output buffer, engine call, automatic acceptance or
// network submission is created. EOF first persists one original declaration.
type custodyOutput struct {
	supervisor.TextOutput
	host    *Host
	command string
	lease   executionlease.Lease
	mu      sync.Mutex
	digest  hash.Hash
	size    int64
	ended   bool
	err     error
}

func completionOutput(out supervisor.TextOutput, h *Host, id string, l executionlease.Lease) supervisor.TextOutput {
	return &custodyOutput{TextOutput: out, host: h, command: id, lease: l, digest: sha256.New()}
}
func (o *custodyOutput) Read(ctx context.Context) ([]byte, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil {
		return nil, o.err
	}
	if o.ended {
		return nil, io.EOF
	}
	b, e := o.TextOutput.Read(ctx)
	if len(b) > 0 {
		if int64(len(b)) > 32<<20-o.size {
			clear(b)
			o.err = ErrHostPending
			return nil, o.err
		}
		o.size += int64(len(b))
		_, _ = o.digest.Write(b)
	}
	if e == io.EOF {
		r, re := o.TextOutput.Result(ctx)
		if re != nil {
			clear(b)
			o.err = ErrHostPending
			return nil, o.err
		}
		d := o.Descriptor()
		if d.Manifest.Digest() != o.lease.ManifestHash || !ids.Valid(d.ResultID) {
			clear(b)
			o.err = ErrHost
			return nil, o.err
		}
		declaration := resultwire.Delivery{Profile: resultwire.DeliveryProfile, Scope: resultwire.Original(o.lease), Result: d.ResultID, Revision: 1, OutputHash: hex.EncodeToString(o.digest.Sum(nil)), OutputBytes: o.size, InputTokens: int64(r.InputTokens), OutputTokens: int64(r.OutputTokens), Finish: r.Reason, Completed: time.Now().UTC().Format(time.RFC3339Nano)}
		if pe := o.host.keepDelivery(ctx, o.command, declaration); pe != nil {
			clear(b)
			o.err = pe
			return nil, pe
		}
		o.ended = true
	} else if e != nil {
		o.err = e
	}
	return b, e
}
func (o *custodyOutput) Result(ctx context.Context) (supervisor.TextResult, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.err != nil || !o.ended {
		return supervisor.TextResult{}, ErrHostPending
	}
	return o.TextOutput.Result(ctx)
}
func (o *custodyOutput) Close(ctx context.Context) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.TextOutput.Close(ctx)
}

func sameResult(a, b resultwire.Delivery) bool {
	// Time/signature are frozen at the FIRST successful metadata commit. Reopen
	// and recovery must retain those bytes, never sign another result revision.
	a.Completed, b.Completed = "", ""
	a.Signature, b.Signature = "", ""
	return a == b
}
func (h *Host) keepDelivery(ctx context.Context, id string, in resultwire.Delivery) error {
	owned, finish, e := h.beginWork(ctx, id)
	if e != nil {
		return e
	}
	defer finish()
	return h.update(owned, id, func(c *hostCommand) error {
		if c.Phase != "SUBMITTED" && c.Phase != "CLEANED" {
			return ErrHostPending
		}
		var l executionlease.Lease
		var m executionlease.Manifest
		if strictjson.Decode(c.Lease, &l, executionlease.MaximumBody) != nil || strictjson.Decode(c.Manifest, &m, 16384) != nil || resultwire.Original(l) != in.Scope || m.Digest() != l.ManifestHash {
			return ErrHost
		}
		if c.ResultDelivery != nil {
			if !sameResult(*c.ResultDelivery, in) {
				return ErrHost
			}
			return nil
		}
		hard, e := time.Parse(time.RFC3339Nano, l.HardDeadline)
		completed, ce := time.Parse(time.RFC3339Nano, in.Completed)
		if e != nil || ce != nil || completed.After(hard) {
			return ErrHostPending
		}
		key := h.node.key()
		defer clear(key)
		report, e := resultwire.SignDelivery(key, in)
		if e != nil {
			return ErrHost
		}
		c.ResultDelivery = &report
		return nil
	})
}

type deliveryAck struct {
	ID           string    `json:"delivery_id"`
	Attempt      string    `json:"attempt_id"`
	Revision     int64     `json:"revision"`
	ManifestHash string    `json:"manifest_hash"`
	OutputHash   string    `json:"output_digest"`
	OutputBytes  int64     `json:"output_bytes"`
	State        string    `json:"state"`
	Deadline     time.Time `json:"decision_deadline"`
	Usage        string    `json:"usage_state"`
}

func validDeliveryAck(raw []byte, d resultwire.Delivery) bool {
	var a deliveryAck
	end, _ := time.Parse(time.RFC3339Nano, d.Completed)
	return strictjson.Decode(raw, &a, 4096) == nil && ids.Valid(a.ID) && a.Attempt == d.Scope.Attempt && a.Revision == d.Revision && a.ManifestHash == resultwire.Digest(d) && a.OutputHash == d.OutputHash && a.OutputBytes == d.OutputBytes && (a.State == "WAITING_BUYER" || a.State == "STALE") && !a.Deadline.Before(end) && !a.Deadline.After(end.Add(120*time.Second)) && a.Usage == "UNVERIFIED"
}
func (h *Host) validateResultReport(c hostCommand) bool {
	if c.ResultDelivery == nil {
		return len(c.ResultDeliveryAck) == 0 && c.UsageStatement == nil && len(c.UsageAck) == 0
	}
	if c.Phase != "SUBMITTED" && c.Phase != "CLEANED" {
		return false
	}
	var l executionlease.Lease
	if strictjson.Decode(c.Lease, &l, executionlease.MaximumBody) != nil || c.ResultDelivery.Scope != resultwire.Original(l) {
		return false
	}
	key := h.node.key()
	defer clear(key)
	if c.ResultDelivery.Verify(key.Public().(ed25519.PublicKey)) != nil {
		return false
	}
	if len(c.ResultDeliveryAck) > 0 && !validDeliveryAck(c.ResultDeliveryAck, *c.ResultDelivery) {
		return false
	}
	if c.UsageStatement == nil {
		return len(c.UsageAck) == 0
	}
	if len(c.ResultDeliveryAck) == 0 || c.UsageStatement.Scope != c.ResultDelivery.Scope || c.UsageStatement.DeliveryHash != resultwire.Digest(*c.ResultDelivery) {
		return false
	}
	if e := c.UsageStatement.Validate(); e != nil {
		return false
	}
	return len(c.UsageAck) == 0 || validUsageAck(c.UsageAck, c.UsageStatement.Scope.Job)
}

// ReportDelivery explicitly drains persisted ORIGINAL metadata. It can be
// retried after cleanup/restart without reopening content or submitting work.
// Unknown replies keep the same report, signature, hash and idempotency key.
func (h *Host) ReportDelivery(ctx context.Context, id string) (json.RawMessage, error) {
	owned, finish, e := h.beginWork(ctx, id)
	if e != nil {
		return nil, e
	}
	defer finish()
	c, e := h.command(owned, id)
	if e != nil {
		return nil, e
	}
	if c.ResultDelivery == nil {
		return nil, ErrHostPending
	}
	if len(c.ResultDeliveryAck) > 0 {
		return append(json.RawMessage(nil), c.ResultDeliveryAck...), nil
	}
	report := *c.ResultDelivery
	prepared, e := h.client.PrepareDelivery(report.Scope.Attempt, "result_"+resultwire.Digest(report), report)
	if e != nil {
		return nil, ErrHost
	}
	reply, e := h.client.SendBounded(owned, prepared, 4096)
	if e != nil {
		return nil, e
	}
	defer clear(reply.Body)
	if reply.Status != 200 {
		return nil, classify(reply)
	}
	if !validDeliveryAck(reply.Body, report) {
		return nil, ErrHostPending
	}
	e = h.update(owned, id, func(current *hostCommand) error {
		if current.ResultDelivery == nil || *current.ResultDelivery != report {
			return ErrHost
		}
		current.ResultDeliveryAck = append(json.RawMessage(nil), reply.Body...)
		return nil
	})
	if e != nil {
		return nil, e
	}
	return append(json.RawMessage(nil), reply.Body...), nil
}

// ReportIndependentUsage forwards exact externally signed metadata only. It
// cannot create a meter statement, mark native counters trusted or change keys.
// The first statement is persisted before submission. Restart recovery retains
// its original signature and recovery key even when the network result is unknown.
func (h *Host) ReportIndependentUsage(ctx context.Context, id string, in resultwire.Statement) (devicecontrol.Reply, error) {
	owned, finish, e := h.beginWork(ctx, id)
	if e != nil {
		return devicecontrol.Reply{}, e
	}
	defer finish()
	if e := in.Validate(); e != nil {
		return devicecontrol.Reply{}, ErrHostPolicy
	}
	encoded, encodeErr := json.Marshal(in)
	if encodeErr != nil || len(encoded) > 16384 || decodeStatementCopy(encoded, &in) != nil {
		return devicecontrol.Reply{}, ErrHostPolicy
	}
	c, e := h.command(owned, id)
	if e != nil {
		return devicecontrol.Reply{}, e
	}
	if c.ResultDelivery == nil || len(c.ResultDeliveryAck) == 0 || in.Scope != c.ResultDelivery.Scope || in.DeliveryHash != resultwire.Digest(*c.ResultDelivery) {
		return devicecontrol.Reply{}, ErrHostPending
	}
	if c.UsageStatement != nil && resultwire.Digest(*c.UsageStatement) != resultwire.Digest(in) {
		return devicecontrol.Reply{}, ErrHostPolicy
	}
	if len(c.UsageAck) > 0 {
		return devicecontrol.Reply{Status: 200, Body: append([]byte(nil), c.UsageAck...), Replayed: true}, nil
	}
	if c.UsageStatement == nil {
		if e = h.update(owned, id, func(current *hostCommand) error {
			if current.UsageStatement != nil {
				return ErrHostPending
			}
			copy := in
			current.UsageStatement = &copy
			return nil
		}); e != nil {
			return devicecontrol.Reply{}, e
		}
	}
	prepared, e := h.client.PrepareUsage(in.Scope.Attempt, "meter_"+resultwire.Digest(in), in)
	if e != nil {
		return devicecontrol.Reply{}, ErrHost
	}
	reply, e := h.client.SendBounded(owned, prepared, 4096)
	if e != nil {
		return reply, e
	}
	if reply.Status != 200 {
		return reply, nil
	}
	if !validUsageAck(reply.Body, in.Scope.Job) {
		clear(reply.Body)
		return devicecontrol.Reply{}, ErrHostPending
	}
	e = h.update(owned, id, func(current *hostCommand) error {
		if current.UsageStatement == nil || resultwire.Digest(*current.UsageStatement) != resultwire.Digest(in) {
			return ErrHost
		}
		current.UsageAck = append(json.RawMessage(nil), reply.Body...)
		return nil
	})
	if e != nil {
		clear(reply.Body)
		return devicecontrol.Reply{}, e
	}
	return reply, nil
}

func validUsageAck(raw []byte, job string) bool {
	var a struct {
		Job      string `json:"job_id"`
		Delivery string `json:"delivery_id,omitempty"`
		ID       string `json:"id"`
		Version  int64  `json:"version"`
		State    string `json:"state"`
		Billing  struct {
			State         string `json:"state"`
			Qualification string `json:"qualification_id,omitempty"`
			Journal       string `json:"journal_id,omitempty"`
			Gross         int64  `json:"gross_fen"`
			Commission    int64  `json:"commission_fen"`
			Seller        int64  `json:"provisional_seller_fen"`
		} `json:"billing"`
	}
	if strictjson.Decode(raw, &a, 4096) != nil || !ids.Valid(a.ID) || a.Version != 1 || a.State != "VERIFIED" || a.Job != job || a.Delivery != "" {
		return false
	}
	b := a.Billing
	return (b.State == "HELD_PENDING_EVIDENCE" || b.State == "HELD_FINANCIAL_REVIEW" || b.State == "QUALIFIED_CHANNEL_COST_UNKNOWN") && b.Gross >= 0 && b.Gross <= 9007199254740991 && b.Commission >= 0 && b.Commission <= b.Gross && b.Seller == b.Gross-b.Commission && (b.Qualification == "" || ids.Valid(b.Qualification)) && (b.Journal == "" || ids.Valid(b.Journal))
}
func (h *Host) RecoverIndependentUsage(ctx context.Context, id string) (devicecontrol.Reply, error) {
	c, e := h.command(ctx, id)
	if e != nil {
		return devicecontrol.Reply{}, e
	}
	if c.UsageStatement == nil {
		return devicecontrol.Reply{}, ErrHostPending
	}
	return h.ReportIndependentUsage(ctx, id, *c.UsageStatement)
}

func decodeStatementCopy(raw []byte, dst *resultwire.Statement) error {
	var frozen resultwire.Statement
	if e := strictjson.Decode(raw, &frozen, 16384); e != nil {
		return e
	}
	*dst = frozen
	return nil
}
