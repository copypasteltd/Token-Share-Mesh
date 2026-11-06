package paymenthost

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"p11.local/compute/internal/payments"
	"p11.local/compute/internal/persistence"
	"p11.local/compute/shared/strictjson"
)

type Host struct {
	mu             sync.Mutex
	db             *persistence.DB
	service        *payments.Machine
	refunds        *payments.RefundService
	refundChannels []*payments.AlipayRefund
	channels       []*payments.Alipay
	proof, receipt *payments.Vault
	capabilities   []string
	life           context.Context
	cancel         context.CancelFunc
	workerStarted  bool
	workerDone     chan struct{}
	stopping       bool
	closed         bool
}

func (h *Host) Format(w fmt.State, _ rune)   { _, _ = io.WriteString(w, "[Machine Pay host redacted]") }
func (h *Host) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }

// Open reads an explicit bounded registry beneath one trusted runtime root.
// It creates no listener, worker, database migration or external channel call.
func Open(db *persistence.DB, rootPath, configName, environment string) (host *Host, err error) {
	if db == nil || db.Pool == nil || !filepath.IsAbs(rootPath) || (environment != "SANDBOX" && environment != "PRODUCTION") {
		return nil, ErrConfiguration
	}
	root, e := os.OpenRoot(rootPath)
	if e != nil {
		return nil, ErrConfiguration
	}
	defer root.Close()
	raw, e := readFile(root, configName, 262144)
	if e != nil {
		return nil, e
	}
	defer clear(raw)
	var spec hostFile
	if strictjson.Decode(raw, &spec, 262144) != nil || spec.Profile != "P11_MACHINE_PAY_HOST_1" || spec.Environment != environment || len(spec.Registrations) < 1 || len(spec.Registrations) > 128 || len(spec.Refunds) > 128 || spec.ProofKeys == spec.ReceiptKeys {
		return nil, ErrConfiguration
	}
	life, cancel := context.WithCancel(context.Background())
	h := &Host{db: db, life: life, cancel: cancel, workerDone: make(chan struct{})}
	// Any partial initialization is owned and closed before returning an error.
	defer func() {
		if err != nil {
			_ = h.Close(context.Background())
		}
	}()
	h.proof, e = readVault(root, spec.ProofKeys, "P11_MACHINE_PAY_PROOF_KEYS_1")
	if e != nil {
		return nil, e
	}
	h.receipt, e = readVault(root, spec.ReceiptKeys, "P11_MACHINE_PAY_RECEIPT_KEYS_1")
	if e != nil {
		return nil, e
	}
	registrations := make([]payments.Registration, 0, len(spec.Registrations))
	for _, item := range spec.Registrations {
		if item.Binding.Environment != environment {
			return nil, ErrConfiguration
		}
		channel, e := readChannel(root, item)
		if e != nil {
			return nil, e
		}
		h.channels = append(h.channels, channel)
		registrations = append(registrations, payments.Registration{CapabilityRef: item.Capability, Channel: channel, Policy: item.Policy})
		h.capabilities = append(h.capabilities, item.Capability)
	}
	h.service, e = payments.NewMachine(db, h.proof, h.receipt, registrations)
	if e != nil {
		return nil, ErrConfiguration
	}
	if len(spec.Refunds) > 0 {
		byCapability := map[string]registrationFile{}
		for _, item := range spec.Registrations {
			byCapability[item.Capability] = item
		}
		refundRegistrations := make([]payments.RefundRegistration, 0, len(spec.Refunds))
		for _, item := range spec.Refunds {
			original, ok := byCapability[item.OriginalCapability]
			if !ok {
				return nil, ErrConfiguration
			}
			channel, e := readRefundChannel(root, original, item.Route)
			if e != nil {
				return nil, ErrConfiguration
			}
			h.refundChannels = append(h.refundChannels, channel)
			refundRegistrations = append(refundRegistrations, payments.RefundRegistration{Ref: item.Ref, OriginalCapability: item.OriginalCapability, Channel: channel})
		}
		h.refunds, e = payments.NewRefundService(h.service, refundRegistrations)
		if e != nil {
			return nil, ErrConfiguration
		}
	}
	return h, nil
}
func (h *Host) Refunds() *payments.RefundService {
	if h == nil {
		return nil
	}
	return h.refunds
}
func (h *Host) Machine() *payments.Machine {
	if h == nil {
		return nil
	}
	return h.service
}

// Close is retryable on a caller deadline. Keys remain owned if actual native
// operations have not joined; a deadline never becomes a false cleanup ACK.
// The enclosing process closes its DB only after this method succeeds.
func (h *Host) Close(ctx context.Context) error {
	if h == nil {
		return nil
	}
	if ctx == nil {
		return payments.ErrUnknown
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil
	}
	h.stopping = true
	if h.cancel != nil {
		h.cancel()
	}
	if h.workerStarted {
		select {
		case <-ctx.Done():
			return payments.ErrUnknown
		case <-h.workerDone:
		}
	}
	if h.refunds != nil {
		if e := h.refunds.Close(ctx); e != nil {
			return e
		}
	}
	if h.service != nil {
		if e := h.service.Close(ctx); e != nil {
			return e
		}
	}
	for _, channel := range h.refundChannels {
		if e := channel.Close(ctx); e != nil {
			return e
		}
	}
	for _, channel := range h.channels {
		if e := channel.Close(ctx); e != nil {
			return e
		}
	}
	h.proof.Close()
	h.receipt.Close()
	h.closed = true
	return nil
}

// RunRecovery is a synchronous, dedicated worker. The caller owns its lifetime;
// cancellation joins the current actual call and its bounded financial save.
// No generic queue consumer can claim VERIFY_MACHINE_PAYMENT.
func (h *Host) RunRecovery(ctx context.Context) error {
	if h == nil || ctx == nil || h.service == nil {
		return ErrConfiguration
	}
	h.mu.Lock()
	if h.stopping || h.closed || h.workerStarted {
		h.mu.Unlock()
		return ErrConfiguration
	}
	h.workerStarted = true
	h.mu.Unlock()
	defer close(h.workerDone)
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := context.AfterFunc(h.life, cancel)
	defer stop()
	ctx = child
	for {
		if ctx.Err() != nil {
			return nil
		}
		worked, e := h.service.RecoverOne(ctx)
		if ctx.Err() != nil {
			return nil
		}
		failed := e != nil
		if h.refunds != nil {
			refunded, refundErr := h.refunds.RecoverOne(ctx)
			worked = worked || refunded
			failed = failed || refundErr != nil
			if ctx.Err() != nil {
				return nil
			}
		}
		funded, fundingErr := h.service.RecoverFundingOne(ctx)
		worked = worked || funded
		failed = failed || fundingErr != nil
		// Retire at most one original expired live-row credential per iteration.
		// This does not erase WAL/backups or destroy long-lived receipt keys.
		failed = h.retireOne(ctx) != nil || failed
		delay := time.Second
		if worked {
			delay = 100 * time.Millisecond
		}
		if failed {
			delay = 2 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
