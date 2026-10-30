package llamaworker

import (
	"context"
	"io"
	"time"

	"p11.local/compute/shared/gpuid"
)

func encodeIdentity(e *encoder, i gpuid.Identity) {
	for _, s := range []string{i.Device, i.UUID, i.PCI, i.Name} {
		e.str(s)
	}
	e.u64(i.TotalBytes)
}
func decodeIdentity(d *decoder) gpuid.Identity {
	return gpuid.Identity{Device: d.str(128), UUID: d.str(40), PCI: d.str(16), Name: d.str(192), TotalBytes: uint64(d.u32())<<32 | uint64(d.u32())}
}
func decodeInventory(raw []byte) ([]gpuid.Identity, error) {
	d := decoder{raw: raw}
	if d.str(64) != Profile || d.str(64) != UpstreamCommit {
		return nil, ErrUnknown
	}
	n := d.u32()
	if n > 64 {
		return nil, ErrUnknown
	}
	items := make([]gpuid.Identity, 0, n)
	for range n {
		items = append(items, decodeIdentity(&d))
	}
	if !d.end() || gpuid.ValidateInventory(items) != nil {
		return nil, ErrUnknown
	}
	return items, nil
}

// DiscoverPackage verifies every pack pin and runs one owned metadata-only child.
// The private CPU-shaped inspection config is never returned or opened for work.
// No model/context is loaded; no device approval, Host rule or execution slot is
// created. Discovery is not remote attestation or performance qualification.
func DiscoverPackage(ctx context.Context, runtimeRoot string, document FilePin) ([]gpuid.Identity, error) {
	s := OwnerSelection{Capability: "metadata_only", Resource: "metadata_only", ContextTokens: 32, BatchTokens: 1, Threads: 1}
	env, err := PackageEnvironment(document.SHA256, s)
	if err != nil {
		return nil, err
	}
	c, err := LoadPackage(ctx, runtimeRoot, document, s, env)
	if err != nil {
		return nil, err
	}
	return discover(ctx, c)
}

func discover(ctx context.Context, c Config) ([]gpuid.Identity, error) {
	if ctx.Err() != nil {
		return nil, ErrUnknown
	}
	p, err := launch(c)
	if err != nil {
		return nil, ErrUnknown
	}
	stop := func() { p.kill(); _ = p.input.Close(); _ = p.control.Close(); _ = p.output.Close() }
	defer stop()
	type result struct {
		items []gpuid.Identity
		err   error
	}
	done := make(chan result, 1)
	go func() {
		var e encoder
		for _, s := range []string{Profile, UpstreamCommit, c.Library, c.BackendRegistry, c.BackendCore} {
			e.str(s)
		}
		defer clear(e.Bytes())
		if writeFrame(p.input, kProbe, e.Bytes()) != nil {
			done <- result{err: ErrUnknown}
			return
		}
		kind, raw, err := readFrame(p.output)
		defer clear(raw)
		if err != nil || kind != kInventory {
			done <- result{err: ErrUnknown}
			return
		}
		items, err := decodeInventory(raw)
		if err == nil {
			_, extra, end := readFrame(p.output)
			clear(extra)
			if end != io.EOF {
				err = ErrUnknown
			}
		}
		done <- result{items, err}
	}()
	var answer result
	select {
	case answer = <-done:
	case <-ctx.Done():
		answer.err = ErrUnknown
	}
	if answer.err != nil {
		stop()
	}
	select {
	case err = <-p.wait:
		if err != nil {
			answer.err = ErrUnknown
		}
	case <-ctx.Done():
		stop()
		select {
		case <-p.wait:
		case <-time.After(5 * time.Second):
		}
		answer.err = ErrUnknown
	}
	if answer.err != nil {
		return nil, ErrUnknown
	}
	return answer.items, nil
}
