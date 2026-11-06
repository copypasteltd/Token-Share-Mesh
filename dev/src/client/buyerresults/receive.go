// Package buyerresults receives an original plaintext result into an encrypted
// local cache. Read-only delegation never becomes model or acceptance authority.
package buyerresults

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/client/results"
	"p11.local/compute/client/supervisor"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

var ErrReceive = errors.New("original result could not be fully received and verified; no local review receipt issued")

type Received struct {
	cache  *results.Imported
	review resultwire.LocalReview
}

func (r *Received) Open(ctx context.Context) (supervisor.TextOutput, error) { return r.cache.Open(ctx) }
func (r *Received) Review() resultwire.LocalReview                          { return r.review }
func (r *Received) Close() error                                            { return r.cache.Discard() }

type remote struct {
	http          *http.Client
	origin, token string
	head          resultwire.ContentHead
	next          int
	pending       [][]byte
}

func origin(value string) (*url.URL, error) {
	u, e := url.Parse(value)
	if e != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return nil, ErrReceive
	}
	ip := net.ParseIP(u.Hostname())
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || ip != nil && ip.IsLoopback())) {
		return nil, ErrReceive
	}
	return u, nil
}
func (r *remote) close() {
	for _, b := range r.pending {
		clear(b)
	}
	r.pending = nil
	r.http.CloseIdleConnections()
	r.token = ""
}
func (r *remote) get(ctx context.Context, path string, out any, maximum int) error {
	req, e := http.NewRequestWithContext(ctx, "GET", r.origin+"/v1/p11"+path, nil)
	if e != nil {
		return ErrReceive
	}
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Accept", "application/json")
	res, e := r.http.Do(req)
	if e != nil {
		return ErrReceive
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return ErrReceive
	}
	media, _, e := mime.ParseMediaType(res.Header.Get("Content-Type"))
	if e != nil || media != "application/json" {
		return ErrReceive
	}
	b, e := io.ReadAll(io.LimitReader(res.Body, int64(maximum+1)))
	defer clear(b)
	if e != nil || len(b) > maximum || strictjson.Decode(b, out, maximum) != nil {
		return ErrReceive
	}
	return nil
}
func (r *remote) Read(ctx context.Context) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if r.next >= r.head.Chunks {
		return nil, io.EOF
	}
	if len(r.pending) == 0 {
		count := r.head.Chunks - r.next
		if count > 4 {
			count = 4
		}
		r.pending = make([][]byte, count)
		errs := make([]error, count)
		owned, cancel := context.WithCancel(ctx)
		defer cancel()
		var wg sync.WaitGroup
		for slot := 0; slot < count; slot++ {
			wg.Add(1)
			go func(slot, index int) {
				defer wg.Done()
				var c resultwire.ContentChunk
				e := r.get(owned, "/result-content/"+r.head.Delivery+"/chunks/"+strconv.Itoa(index), &c, 65536)
				expected := int64(resultwire.ChunkBytes)
				if left := r.head.Declaration.OutputBytes - int64(index)*resultwire.ChunkBytes; left < expected {
					expected = left
				}
				if e == nil && (c.Validate() != nil || c.Scope != r.head.Declaration.Scope || c.Delivery != r.head.Delivery || c.Head != r.head.Digest() || c.Index != index || int64(len(c.Content)) != expected) {
					e = ErrReceive
				}
				if e != nil {
					clear(c.Content)
					errs[slot] = e
					cancel()
					return
				}
				r.pending[slot] = c.Content
			}(slot, r.next+slot)
		}
		wg.Wait()
		for _, e := range errs {
			if e != nil {
				for _, b := range r.pending {
					clear(b)
				}
				r.pending = nil
				return nil, e
			}
		}
	}
	b := r.pending[0]
	r.pending[0] = nil
	r.pending = r.pending[1:]
	r.next++
	return b, nil
}

func Receive(ctx context.Context, packet resultwire.ReadPacket, store *results.Store) (*Received, error) {
	g := packet.Grant
	u, e := origin(packet.Origin)
	expires, te := time.Parse(time.RFC3339Nano, g.Expires)
	secret, se := base64.RawURLEncoding.Strict().DecodeString(packet.Secret)
	defer clear(secret)
	if store == nil || e != nil || te != nil || !time.Now().Before(expires) || g.Profile != resultwire.GrantProfile || !ids.Valid(g.ID) || !ids.Valid(g.Order) || !ids.Valid(g.Job) || !ids.Valid(g.Seller) || !ids.Valid(g.Delivery) || !executionlease.Hash(g.Head) || len(packet.Secret) != 43 || se != nil || len(secret) != 32 || base64.RawURLEncoding.EncodeToString(secret) != packet.Secret {
		return nil, ErrReceive
	}
	ctx, cancel := context.WithDeadline(ctx, expires)
	defer cancel()
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 10 * time.Second, MaxResponseHeaderBytes: 16384, MaxConnsPerHost: 4, MaxIdleConnsPerHost: 4, IdleConnTimeout: 15 * time.Second}
	r := &remote{http: &http.Client{Transport: transport, Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, origin: u.Scheme + "://" + u.Host, token: "p11r." + g.ID + "." + packet.Secret}
	defer r.close()
	if e = r.get(ctx, "/result-content/"+g.Delivery+"/manifest", &r.head, 32768); e != nil {
		return nil, e
	}
	h := r.head
	d := h.Declaration
	headExpiry, he := time.Parse(time.RFC3339Nano, h.Expires)
	if h.Validate() != nil || he != nil || expires.After(headExpiry) || h.State != "READY" || h.End == nil || h.Digest() != g.Head || h.Delivery != g.Delivery || d.Scope.Job != g.Job || d.Scope.Order != g.Order || d.Scope.Seller != g.Seller {
		return nil, ErrReceive
	}
	completed, _ := time.Parse(time.RFC3339Nano, d.Completed)
	spec := results.ImportSpec{Descriptor: supervisor.TextDescriptor{Work: supervisor.Work{RequestID: "result_read_" + g.ID, LocalGeneration: "result_read_" + g.ID, Resource: d.Scope.Resource, Environment: d.Scope.Environment}, Manifest: h.Manifest, ResultID: d.Result, CreatedUnix: completed.Unix(), IncludeUsage: h.IncludeUsage}, Start: supervisor.TextStart{InputTokens: uint32(d.InputTokens)}, Result: supervisor.TextResult{InputTokens: uint32(d.InputTokens), OutputTokens: uint32(d.OutputTokens), Reason: d.Finish, StopIndex: supervisor.NoStopIndex}, OutputHash: d.OutputHash, OutputBytes: d.OutputBytes}
	if d.Finish == "stop" {
		spec.Stops = []string{h.End.Stop}
		spec.Result.StopIndex = 0
	}
	cached, e := store.Import(ctx, spec, r)
	if e != nil {
		return nil, ErrReceive
	}
	return &Received{cache: cached, review: resultwire.LocalReview{Profile: resultwire.ReviewProfile, Job: g.Job, Delivery: g.Delivery, Revision: d.Revision, Manifest: resultwire.Digest(d), Output: d.OutputHash}}, nil
}
