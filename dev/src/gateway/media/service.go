package media

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"p11.local/compute/client/rentalmedia"
)

type entry struct {
	mu                                      sync.Mutex
	id, public, file, digest, size, seconds string
	created, completed, expires             time.Time
	submitted                               chan struct{}
	failed                                  bool
	cancelled                               atomic.Bool
	cache                                   *stagedAsset
	bytes                                   int64
	reservation                             int64
}

// Handler owns entered HTTP calls, encrypted caches and its expiry loop. The
// original node must Join this owner before releasing its master/ledger/root.
// It cannot be created from a network route or an unconfirmed buyer request.
type Handler struct {
	mu                  sync.Mutex
	config              Config
	key                 []byte
	life                context.Context
	cancel              context.CancelFunc
	stopOriginal        func()
	stopOriginalContext func() bool
	entries             map[string]*entry
	slots               chan struct{}
	ingress             chan struct{}
	operations          sync.WaitGroup
	closed              bool
	reserved            int64
	closeOnce           sync.Once
	done, expiryDone    chan struct{}
}

const cacheCapacity int64 = 64 << 20

func New(c Config) (*Handler, error) {
	if c.Session == nil || c.OriginalContext == nil || c.OriginalContext.Err() != nil || c.StopOriginal == nil || !c.Mapping.binds(c.Prepared, c.Policy) || len(c.AllowedHosts) < 1 || len(c.AllowedHosts) > 8 || len(c.AllowedOrigins) > 8 {
		return nil, ErrAccess
	}
	if c.Concurrent == 0 {
		c.Concurrent = 2
	}
	if c.Total == 0 {
		c.Total = 90 * time.Second
	}
	if c.Write == 0 {
		c.Write = 5 * time.Second
	}
	if c.Retention == 0 {
		c.Retention = 5 * time.Minute
	}
	if c.Concurrent < 1 || c.Concurrent > 4 || c.Total < time.Second || c.Total > 10*time.Minute || c.Write < 20*time.Millisecond || c.Write > 10*time.Second || c.Retention < time.Second || c.Retention > 30*time.Minute {
		return nil, ErrAccess
	}
	for _, host := range c.AllowedHosts {
		if host == "" || len(host) > 253 || host != strings.ToLower(host) || strings.ContainsAny(host, "/@?#\\ \t\r\n") {
			return nil, ErrAccess
		}
	}
	for _, origin := range c.AllowedOrigins {
		u, e := url.Parse(origin)
		if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.String() != origin {
			return nil, ErrAccess
		}
	}
	if c.PublicBaseURL != "" {
		u, e := url.Parse(c.PublicBaseURL)
		if e != nil || u.Scheme != "http" && u.Scheme != "https" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.String() != c.PublicBaseURL {
			return nil, ErrAccess
		}
		found := false
		for _, host := range c.AllowedHosts {
			found = found || u.Host == host
		}
		if !found {
			return nil, ErrAccess
		}
	}
	c.Mapping = cloneMapping(c.Mapping)
	c.AllowedHosts = append([]string{}, c.AllowedHosts...)
	c.AllowedOrigins = append([]string{}, c.AllowedOrigins...)
	key := make([]byte, 32)
	if _, e := rand.Read(key); e != nil {
		return nil, ErrAccess
	}
	life, cancel := context.WithCancel(context.Background())
	h := &Handler{config: c, key: key, life: life, cancel: cancel, stopOriginal: c.StopOriginal, entries: map[string]*entry{}, slots: make(chan struct{}, c.Concurrent), ingress: make(chan struct{}, 1), done: make(chan struct{}), expiryDone: make(chan struct{})}
	// This callback stops admission, never synthesizes original stop evidence.
	stopContext := context.AfterFunc(c.OriginalContext, h.RequestStop)
	h.mu.Lock()
	h.stopOriginalContext = stopContext
	closed := h.closed
	h.mu.Unlock()
	if closed {
		stopContext()
	}
	go h.expire()
	return h, nil
}
func (h *Handler) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original media gateway redacted]")
}
func (h *Handler) MarshalJSON() ([]byte, error) { return []byte(`{"redacted":true}`), nil }
func (h *Handler) RequestStop() {
	if h == nil {
		return
	}
	first := false
	h.closeOnce.Do(func() { h.mu.Lock(); h.closed = true; h.mu.Unlock(); h.cancel(); first = true })
	if !first {
		return
	}
	// Hooks outside Once allow the original node to call this owner back.
	h.stopOriginal()
	go func() {
		h.operations.Wait()
		<-h.expiryDone
		h.mu.Lock()
		for _, e := range h.entries {
			e.mu.Lock()
			if e.cache != nil {
				e.cache.Close()
				e.cache = nil
			}
			e.mu.Unlock()
		}
		clear(h.key)
		h.reserved = 0
		stopContext := h.stopOriginalContext
		h.mu.Unlock()
		if stopContext != nil {
			stopContext()
		}
		close(h.done)
	}()
}
func (h *Handler) Join(ctx context.Context) error {
	if h == nil || ctx == nil {
		return ErrAccess
	}
	select {
	case <-ctx.Done():
		return ErrAccess
	case <-h.done:
		return nil
	}
}
func (h *Handler) Close(ctx context.Context) error { h.RequestStop(); return h.Join(ctx) }
func (h *Handler) enter() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.operations.Add(1)
	return true
}
func (h *Handler) expire() {
	defer close(h.expiryDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.life.Done():
			return
		case now := <-ticker.C:
			h.mu.Lock()
			list := make([]*entry, 0, len(h.entries))
			for _, e := range h.entries {
				list = append(list, e)
			}
			h.mu.Unlock()
			for _, e := range list {
				e.mu.Lock()
				var freed int64
				if e.cache != nil && !now.Before(e.expires) {
					e.cache.Close()
					e.cache = nil
					e.failed = true
					freed = e.reservation
					e.reservation = 0
					e.bytes = 0
				}
				e.mu.Unlock()
				if freed != 0 {
					h.mu.Lock()
					h.reserved -= freed
					h.mu.Unlock()
				}
			}
		}
	}
}
func (h *Handler) reserve(ctx context.Context, r *request, key string) (*entry, bool, error) {
	if ctx == nil || ctx.Err() != nil || h.life.Err() != nil {
		return nil, false, ErrAccess
	}
	params, assets, e := r.parameters(h.config.Mapping)
	if e != nil {
		return nil, false, e
	}
	defer clear(params)
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, false, ErrAccess
	}
	keyCopy := append([]byte{}, h.key...)
	h.mu.Unlock()
	defer clear(keyCopy)
	hash := hmac.New(sha256.New, keyCopy)
	// Random local staging nonces must not make the same multipart bytes look
	// different. Recompute asset HMAC over authenticated staged plaintext using
	// nondestructive bounded blocks; content digests never leave this owner.
	framing, _ := json.Marshal([]string{h.config.Mapping.Profile, r.model, r.size, r.seconds, r.voice, r.format, string(params)})
	defer clear(framing)
	_, _ = hash.Write(framing)
	if len(assets) > 0 {
		for _, a := range r.assets {
			if e = a.hashInto(ctx, h.life, hash); e != nil {
				return nil, false, e
			}
		}
	}
	digest := hex.EncodeToString(hash.Sum(nil))
	var token string
	if key != "" {
		idHash := hmac.New(sha256.New, keyCopy)
		_, _ = idHash.Write([]byte("original-media-key\x00" + key))
		token = hex.EncodeToString(idHash.Sum(nil)[:16])
	} else {
		raw := make([]byte, 16)
		if _, e = rand.Read(raw); e != nil {
			return nil, false, ErrAccess
		}
		token = hex.EncodeToString(raw)
		clear(raw)
	}
	id := "invoke_" + token
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || ctx.Err() != nil {
		return nil, false, ErrAccess
	}
	if original := h.entries[id]; original != nil {
		if original.digest != digest {
			return nil, false, ErrConflict
		}
		return original, true, nil
	}
	if len(h.entries) >= 4096 {
		return nil, false, ErrAccess
	}
	if h.reserved+h.config.Policy.MaximumOutputBytes > cacheCapacity {
		return nil, false, ErrAccess
	}
	prefix := "image_"
	if h.config.Mapping.Profile == Videos {
		prefix = "video_"
	}
	if h.config.Mapping.Profile == Assets {
		prefix = "resp_"
	}
	if h.config.Mapping.Profile == Speech {
		prefix = "speech_"
	}
	out := &entry{id: id, public: prefix + token, file: "file_" + token, digest: digest, size: r.size, seconds: r.seconds, created: time.Now().UTC(), submitted: make(chan struct{}), reservation: h.config.Policy.MaximumOutputBytes}
	h.reserved += out.reservation
	h.entries[id] = out
	return out, false, nil
}
func (h *Handler) lookup(public, prefix string) (*entry, error) {
	if !strings.HasPrefix(public, prefix) {
		return nil, ErrAccess
	}
	token := strings.TrimPrefix(public, prefix)
	if len(token) != 32 {
		return nil, ErrAccess
	}
	if _, e := hex.DecodeString(token); e != nil {
		return nil, ErrAccess
	}
	h.mu.Lock()
	e := h.entries["invoke_"+token]
	closed := h.closed
	h.mu.Unlock()
	if closed || e == nil {
		return nil, ErrAccess
	}
	return e, nil
}
func (h *Handler) submit(ctx context.Context, credential string, r *request, e *entry) error {
	defer close(e.submitted)
	params, assets, err := r.parameters(h.config.Mapping)
	if err != nil {
		e.mu.Lock()
		e.failed = true
		h.releaseReservation(e)
		e.mu.Unlock()
		return ErrRequest
	}
	defer clear(params)
	input, err := h.config.Session.Seal(ctx, credential, e.id, params, assets)
	if err == nil {
		defer input.Close()
		_, err = h.config.Session.Invoke(ctx, input)
	}
	if err != nil {
		e.mu.Lock()
		e.failed = true
		h.releaseReservation(e)
		e.mu.Unlock()
		return ErrConflict
	}
	return nil
}
func (h *Handler) state(ctx context.Context, credential string, e *entry) (rentalmedia.MediaState, error) {
	return h.originalState(ctx, credential, e, false)
}
func (h *Handler) originalState(ctx context.Context, credential string, e *entry, exporting bool) (rentalmedia.MediaState, error) {
	select {
	case <-ctx.Done():
		return rentalmedia.MediaState{}, ErrAccess
	case <-e.submitted:
	default:
		return rentalmedia.MediaState{NativeState: "SUBMITTING"}, nil
	}
	e.mu.Lock()
	failed := e.failed
	e.mu.Unlock()
	if failed {
		return rentalmedia.MediaState{}, ErrConflict
	}
	if exporting {
		return h.config.Session.ExportStatus(ctx, credential, e.id)
	}
	return h.config.Session.Status(ctx, credential, e.id)
}

type outputReader struct {
	ctx    context.Context
	output *rentalmedia.Output
}

func (r outputReader) Read(b []byte) (int, error) { return r.output.Read(r.ctx, b) }
func (h *Handler) cache(ctx context.Context, credential string, e *entry) error {
	// Caller owns e.mu. No gateway lock is held across original native reads.
	if e.failed || e.cancelled.Load() {
		return ErrAccess
	}
	if e.cache != nil {
		if !time.Now().Before(e.expires) {
			return ErrAccess
		}
		return nil
	}
	if !e.completed.IsZero() {
		return ErrAccess
	}
	if e.reservation != h.config.Policy.MaximumOutputBytes {
		return ErrAccess
	}
	success := false
	defer func() {
		if !success {
			e.failed = true
			h.releaseReservation(e)
		}
	}()
	out, err := h.config.Session.Output(ctx, credential, e.id)
	if err != nil {
		return ErrAccess
	}
	defer out.Close()
	length, err := out.Seek(ctx, 0, io.SeekEnd)
	if err != nil || length < 1 || length > h.config.Policy.MaximumOutputBytes {
		e.failed = true
		return ErrAccess
	}
	if _, err = out.Seek(ctx, 0, io.SeekStart); err != nil {
		e.failed = true
		return ErrAccess
	}
	staged, err := stageAsset(ctx, outputReader{ctx, out}, length)
	if err != nil {
		e.failed = true
		return ErrAccess
	}
	if staged.size != length || !validResult(staged, h.config.Policy.Kind) {
		staged.Close()
		e.failed = true
		return ErrAccess
	}
	if out.Close() != nil || e.cancelled.Load() || ctx.Err() != nil {
		staged.Close()
		e.failed = true
		return ErrAccess
	}
	e.cache = staged
	e.bytes = staged.size
	e.completed = time.Now().UTC()
	e.expires = e.completed.Add(h.config.Retention)
	h.mu.Lock()
	h.reserved -= e.reservation - e.bytes
	h.mu.Unlock()
	e.reservation = e.bytes
	success = true
	return nil
}

// Caller holds the original entry mutex. This releases only local RAM cache
// accounting, never native ownership, acceptance, budget, funds or billable time.
func (h *Handler) releaseReservation(e *entry) {
	n := e.reservation
	e.reservation = 0
	if n != 0 {
		h.mu.Lock()
		h.reserved -= n
		h.mu.Unlock()
	}
}
func (h *Handler) dropCache(e *entry) {
	if e.cache != nil {
		e.cache.Close()
		e.cache = nil
	}
	e.bytes = 0
	h.releaseReservation(e)
}
func validResult(s *stagedAsset, kind string) bool {
	switch kind {
	case "PNG":
		return s.png()
	case "MP4":
		return len(s.head) >= 12 && string(s.head[4:8]) == "ftyp"
	case "WAV":
		return len(s.head) >= 12 && string(s.head[:4]) == "RIFF" && string(s.head[8:12]) == "WAVE"
	case "GLB":
		return len(s.head) >= 12 && string(s.head[:4]) == "glTF"
	}
	return false
}
func mimeType(kind string) string {
	switch kind {
	case "PNG":
		return "image/png"
	case "MP4":
		return "video/mp4"
	case "WAV":
		return "audio/wav"
	case "GLB":
		return "model/gltf-binary"
	}
	return "application/octet-stream"
}
func (h *Handler) await(ctx context.Context, credential string, e *entry) error {
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil || h.config.Session.Check(ctx, credential, "EXPORT_SELF") != nil {
			return ErrAccess
		}
		e.mu.Lock()
		cached := e.cache != nil && time.Now().Before(e.expires) && !e.failed
		cancelled := e.cancelled.Load()
		failed := e.failed || !e.expires.IsZero() && !time.Now().Before(e.expires)
		e.mu.Unlock()
		if failed || cancelled {
			return ErrAccess
		}
		if cached {
			return nil
		}
		state, err := h.originalState(ctx, credential, e, true)
		if err != nil {
			return err
		}
		if state.Stopped {
			if cancelled || !state.Succeeded || !state.ResultAvailable {
				return ErrAccess
			}
			e.mu.Lock()
			err = h.cache(ctx, credential, e)
			e.mu.Unlock()
			return err
		}
		select {
		case <-ctx.Done():
			return ErrAccess
		case <-ticker.C:
		}
	}
}
func (h *Handler) json(w *wire, value any) error {
	raw, e := json.Marshal(value)
	if e != nil {
		return ErrAccess
	}
	defer clear(raw)
	if len(raw) > MaximumJSONBytes {
		return ErrRequest
	}
	return w.write(raw)
}
