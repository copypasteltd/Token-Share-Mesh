package node

import (
	"context"
	"net/http"
	"sync"
	"time"

	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/client/protocol/rentalaccess"
)

// RentalCurrent must read independently approved original local metadata and
// physical owner state. It must never derive trust from the incoming Bearer or
// from a control response. A failure prevents permission and closes the window.
type RentalCurrent func(context.Context) (rentalaccess.Expected, error)

// RentalControl joins actual device-signed HTTP transport to an encrypted,
// flushed high-water journal. It grants control freshness, not a physical GPU
// ticket, native invocation admission, trusted availability or billing proof.
// It contains no engine launcher, automatic retries or silent grant replacement.
type RentalControl struct {
	journal   *RentalJournal
	window    *rentalaccess.Window
	grant     rentalaccess.Verified
	verifier  *rentalaccess.Verifier
	client    *devicecontrol.Client
	version   int64
	refresh   sync.Mutex
	mu        sync.RWMutex
	closed    bool
	closeOnce sync.Once
	closeErr  error
	lifetime  context.Context
	cancel    context.CancelFunc
	original  rentalaccess.Expected
}

// ResolveControl supplies the actual independent digest-read path before
// OpenControl. approved and connection come from the original locally approved
// preparation manifest/context; neither can be inferred from the caller token.
func (j *RentalJournal) ResolveControl(ctx context.Context, verifier *rentalaccess.Verifier, credential string, approved rentalaccess.Expected, connection string) (*RentalControl, error) {
	if j == nil || verifier == nil || !j.available() {
		return nil, ErrRentalJournal
	}
	key := j.node.key()
	client, err := devicecontrol.NewClient(j.node.s.Config.Base, j.node.s.DeviceID, key)
	clear(key)
	if err != nil {
		return nil, ErrRentalJournal
	}
	defer client.Close()
	metadata, err := client.RentalGrantExpectation(ctx, approved.Session, approved.Grant)
	if err != nil || metadata.Connection != connection {
		return nil, ErrRentalJournal
	}
	now := time.Now()
	expected, err := metadata.Bind(approved, now)
	if err != nil {
		return nil, ErrRentalJournal
	}
	verified, err := verifier.Verify(credential, expected, now)
	if err != nil || verified.KeyID() != metadata.Metadata.KeyID {
		return nil, ErrRentalJournal
	}
	return j.OpenControl(verifier, credential, expected, metadata.SessionVersion, now)
}

// OpenControl requires the original committed grant expectation/session version
// from a trusted local approval source and separately provisioned public pins.
// The credential is verified in RAM and is not saved to the encrypted journal.
func (j *RentalJournal) OpenControl(verifier *rentalaccess.Verifier, credential string, expected rentalaccess.Expected, version int64, now time.Time) (*RentalControl, error) {
	if j == nil {
		return nil, ErrRentalJournal
	}
	// The parent retains its OS lock/key until all child stores close.
	key := j.node.key()
	client, err := devicecontrol.NewClient(j.node.s.Config.Base, j.node.s.DeviceID, key)
	clear(key)
	if err != nil {
		return nil, ErrRentalJournal
	}
	window, grant, err := j.openWindow(verifier, credential, expected, version, now)
	if err != nil {
		client.Close()
		return nil, err
	}
	lifetime, cancel := context.WithCancel(context.Background())
	control := &RentalControl{journal: j, window: window, grant: grant, verifier: verifier, client: client, version: version, lifetime: lifetime, cancel: cancel, original: expected}
	j.mu.Lock()
	if j.closed || j.closing || j.poisoned {
		j.mu.Unlock()
		cancel()
		client.Close()
		window.Stop()
		return nil, ErrRentalJournal
	}
	j.controls[control] = true
	j.mu.Unlock()
	return control, nil
}

func (j *RentalJournal) available() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return !j.closed && !j.closing && !j.poisoned
}
func (c *RentalControl) available() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return !c.closed && c.journal.available()
}

// Refresh performs exactly one original-sequence exchange. On network failure
// only the already accepted original deadline can remain live. Callers retry
// explicitly and enforce a separate native stop watchdog; no transport result
// extends a missing/expired control window or hard deadline.
func (c *RentalControl) Refresh(ctx context.Context, load RentalCurrent) error {
	if !c.available() || load == nil {
		return ErrRentalJournal
	}
	c.refresh.Lock()
	defer c.refresh.Unlock()
	if !c.available() {
		return ErrRentalJournal
	}
	current, err := load(ctx)
	if err != nil {
		c.window.Stop()
		return ErrRentalJournal
	}
	sequence, err := c.window.Request(current, time.Now())
	if err != nil {
		return err
	}
	claims := c.grant.Details()
	in := rentalaccess.ControlRequest{ExpectedVersion: c.version, Grant: claims.ID, Digest: c.grant.Digest(), Generation: claims.Generation, Terms: claims.Terms, TermsHash: claims.TermsHash, Sequence: sequence}
	prepared, err := c.client.PrepareRentalControl(claims.Session, in)
	if err != nil {
		c.window.Stop()
		return ErrRentalJournal
	}
	requestCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.lifetime, cancel)
	defer stop()
	defer cancel()
	reply, err := c.client.SendBounded(requestCtx, prepared, rentalaccess.MaximumControlBytes)
	if err != nil {
		return err
	}
	defer clear(reply.Body)
	if reply.Status != http.StatusOK {
		// Only temporary transport/service rejection retains the old bounded
		// window. An unsigned error may close permission, never create it.
		if reply.Status != http.StatusTooManyRequests && reply.Status != http.StatusServiceUnavailable {
			c.window.Stop()
		}
		return ErrRentalJournal
	}
	current, err = load(ctx)
	if err != nil || !c.available() {
		c.window.Stop()
		return ErrRentalJournal
	}
	return c.window.Accept(c.verifier, reply.Body, current, time.Now())
}

// CheckScope must precede every native entry, alongside the actual original
// shared resource gate and serial invocation journal. Returning nil here alone
// cannot authorize execution, read/export content or issue a cleanup ACK.
func (c *RentalControl) CheckScope(ctx context.Context, scope string, load RentalCurrent) error {
	if !c.available() || load == nil {
		return ErrRentalJournal
	}
	current, err := load(ctx)
	if err != nil {
		c.window.Stop()
		return ErrRentalJournal
	}
	now := time.Now()
	if !c.grant.Allows(scope, now) {
		return rentalaccess.ErrAccess
	}
	return c.window.Check(current, now)
}

// Close persistently stops this grant's window. It never removes high-water
// tombstones or releases the physical owner. A disk failure keeps it poisoned.
func (c *RentalControl) Close() error {
	if c == nil {
		return ErrRentalJournal
	}
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		c.cancel()
		c.closeErr = c.window.Stop()
		c.client.Close()
		c.journal.mu.Lock()
		delete(c.journal.controls, c)
		c.journal.mu.Unlock()
	})
	return c.closeErr
}

// OriginalExpected returns already independently bound metadata only; callers
// must still refresh actual local approval/physical ownership in RentalCurrent.
func (c *RentalControl) OriginalExpected() rentalaccess.Expected {
	if c == nil {
		return rentalaccess.Expected{}
	}
	return c.original
}
