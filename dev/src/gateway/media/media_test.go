package media

// SOURCE_ONLY. Synthetic metadata, bounded recorder and fake native ports.
// These definitions are not SDK, GPU, privacy, money or stopped-work evidence.
import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalmedia"
	"p11.local/compute/internal/testsupport/rentaltest"
)

type sessionFixture struct {
	fixture                         rentaltest.Fixture
	policy                          rentalaccess.MediaPolicy
	checks, seals, submits, cancels atomic.Int32
	exportCalls                     atomic.Int32
	denyScope                       string
	queryError                      bool
	entered, release                chan struct{}
}

func (s *sessionFixture) Check(ctx context.Context, credential, scope string) error {
	s.checks.Add(1)
	if ctx.Err() != nil || s.denyScope == scope {
		return ErrAccess
	}
	grant, e := s.fixture.Verifier.Verify(credential, s.fixture.Expected, time.Now())
	if e != nil || !grant.Allows(scope, time.Now()) {
		return ErrAccess
	}
	return nil
}
func (s *sessionFixture) Seal(ctx context.Context, credential, id string, p []byte, a []rentalaccess.MediaAsset) (*rentalaccess.MediaInput, error) {
	s.seals.Add(1)
	return rentalaccess.SealMediaInput(ctx, s.fixture.Prepared, s.fixture.Verifier, credential, s.fixture.Expected, id, "1", s.policy, p, a, time.Now())
}
func (s *sessionFixture) Invoke(ctx context.Context, input *rentalaccess.MediaInput) (execution.RentalInvocation, error) {
	s.submits.Add(1)
	if s.release != nil {
		select {
		case s.entered <- struct{}{}:
		default:
		}
		<-s.release
	}
	d := input.Descriptor()
	plain, e := input.Take(ctx, s.fixture.Prepared, s.fixture.Expected, time.Now())
	if e != nil {
		return execution.RentalInvocation{}, e
	}
	plain.Close()
	return execution.RentalInvocation{ID: d.ID, Session: d.Session, Resource: d.Resource, LocalGeneration: d.LocalGeneration, Grant: d.Grant, GrantDigest: d.GrantDigest, Phase: "EXECUTING", BackendID: "synthetic_only"}, nil
}
func (s *sessionFixture) Status(ctx context.Context, credential, id string) (rentalmedia.MediaState, error) {
	if s.queryError || s.Check(ctx, credential, "QUEUE_SELF") != nil {
		return rentalmedia.MediaState{}, ErrAccess
	}
	return rentalmedia.MediaState{NativeState: "EXECUTING"}, nil
}
func (s *sessionFixture) ExportStatus(ctx context.Context, credential, id string) (rentalmedia.MediaState, error) {
	s.exportCalls.Add(1)
	if s.Check(ctx, credential, "EXPORT_SELF") != nil {
		return rentalmedia.MediaState{}, ErrAccess
	}
	return rentalmedia.MediaState{NativeState: "EXECUTING"}, nil
}
func (s *sessionFixture) Output(context.Context, string, string) (*rentalmedia.Output, error) {
	return nil, ErrAccess
}
func (s *sessionFixture) Cancel(context.Context, string, string) error  { s.cancels.Add(1); return nil }
func (s *sessionFixture) Discard(context.Context, string, string) error { return nil }

type gatewayFixture struct {
	handler    *Handler
	session    *sessionFixture
	credential string
	mapping    Mapping
	stops      *atomic.Int32
}

func newGatewayFixture(t *testing.T, profile string) gatewayFixture {
	t.Helper()
	f := rentaltest.MediaProfile(t, profile)
	d := f.Prepared.Details()
	kind := "PNG"
	if profile == Videos {
		kind = "MP4"
	}
	if profile == Speech {
		kind = "WAV"
	}
	if profile == Assets {
		kind = "GLB"
	}
	policy := rentalaccess.MediaPolicy{Profile: rentalaccess.MediaPolicyProfile, Workflow: d.PublicSpec.WorkflowID, Revision: d.PublicSpec.WorkflowRevision, Protocol: d.Protocol, Kind: kind, MaximumOutputBytes: 1024}
	mapping := Mapping{Profile: profile, ManifestHash: f.Prepared.Digest(), ApprovalHash: f.Prepared.ApprovalDigest(), RecipeHash: d.RecipeHash, PolicyHash: policy.Digest(), Model: d.PublicSpec.WorkflowID, PromptSlot: "prompt", MaximumPromptBytes: 65536, Defaults: map[string]json.RawMessage{}}
	if profile == Images || profile == Videos {
		mapping.WidthSlot = "width"
		mapping.HeightSlot = "height"
		mapping.Sizes = []string{"1024x1024"}
		mapping.DefaultSize = "1024x1024"
	}
	if profile == Videos {
		mapping.LegacyVideosExplicitlyApproved = true
		mapping.Sizes = []string{"1280x720"}
		mapping.DefaultSize = "1280x720"
		mapping.SecondsSlot = "seconds"
		mapping.Seconds = []string{"4", "8"}
		mapping.DefaultSeconds = "4"
	}
	if profile == Speech {
		mapping.VoiceSlot = "voice"
		mapping.Voices = []string{"local_voice"}
		mapping.DefaultVoice = "local_voice"
	}
	session := &sessionFixture{fixture: f, policy: policy}
	stops := &atomic.Int32{}
	h, e := New(Config{Prepared: f.Prepared, Policy: policy, Mapping: mapping, Session: session, OriginalContext: context.Background(), StopOriginal: func() { stops.Add(1) }, AllowedHosts: []string{"127.0.0.1:9381"}, PublicBaseURL: "http://127.0.0.1:9381", Total: time.Second, Retention: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if h.Close(ctx) != nil {
			t.Error("synthetic gateway did not join")
		}
	})
	return gatewayFixture{h, session, f.Credential, mapping, stops}
}

type boundedRecorder struct {
	*httptest.ResponseRecorder
	broken bool
}

func (w *boundedRecorder) SetReadDeadline(time.Time) error  { return nil }
func (w *boundedRecorder) SetWriteDeadline(time.Time) error { return nil }
func (w *boundedRecorder) Write(b []byte) (int, error) {
	if w.broken {
		return 0, io.ErrClosedPipe
	}
	return w.ResponseRecorder.Write(b)
}
func recorder() *boundedRecorder { return &boundedRecorder{ResponseRecorder: httptest.NewRecorder()} }
func (f gatewayFixture) request(path, body string) *http.Request {
	r := httptest.NewRequest("POST", path, strings.NewReader(body))
	r.Host = "127.0.0.1:9381"
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Authorization", "Bearer "+f.credential)
	return r
}
func videoBody(prompt string) string {
	raw, _ := json.Marshal(map[string]any{"model": "workflow_media", "prompt": prompt, "seconds": "4", "size": "1280x720"})
	return string(raw)
}
func imageBody(prompt, format string) string {
	raw, _ := json.Marshal(map[string]any{"model": "workflow_media", "prompt": prompt, "size": "1024x1024", "response_format": format})
	return string(raw)
}
func syntheticPNG() []byte {
	b := make([]byte, 32)
	copy(b, []byte{137, 80, 78, 71, 13, 10, 26, 10})
	copy(b[12:], "IHDR")
	binary.BigEndian.PutUint32(b[16:], 64)
	binary.BigEndian.PutUint32(b[20:], 64)
	return b
}

func TestMediaMappingCannotBorrowAnotherPreparationOrRecipe(t *testing.T) {
	f := newGatewayFixture(t, Images)
	m := f.mapping
	m.RecipeHash = strings.Repeat("1", 64)
	if m.binds(f.handler.config.Prepared, f.handler.config.Policy) {
		t.Fatal("foreign recipe mapping admitted")
	}
	m = f.mapping
	m.ManifestHash = strings.Repeat("2", 64)
	if m.binds(f.handler.config.Prepared, f.handler.config.Policy) {
		t.Fatal("foreign preparation mapping admitted")
	}
}
func TestMediaVideosRequireExplicitLegacyApprovalAndPublishedSizes(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	m := f.mapping
	m.LegacyVideosExplicitlyApproved = false
	if m.binds(f.handler.config.Prepared, f.handler.config.Policy) {
		t.Fatal("retired API claimed current approval")
	}
	m = f.mapping
	m.Sizes = []string{"512x512"}
	m.DefaultSize = "512x512"
	if m.binds(f.handler.config.Prepared, f.handler.config.Policy) {
		t.Fatal("non-schema video size advertised as standard")
	}
}
func TestMediaMappingCopiesOwnerChoicesAndRejectsDuplicateSlots(t *testing.T) {
	f := newGatewayFixture(t, Images)
	clone := cloneMapping(f.mapping)
	clone.Sizes[0] = "64x64"
	if f.mapping.Sizes[0] != "1024x1024" {
		t.Fatal("caller mutated active mapping")
	}
	clone = f.mapping
	clone.HeightSlot = clone.PromptSlot
	if clone.binds(f.handler.config.Prepared, f.handler.config.Policy) {
		t.Fatal("two public fields mapped to same private slot")
	}
}
func TestMediaRequestRejectsAmbiguityAndUnapprovedFieldsBeforeSeal(t *testing.T) {
	f := newGatewayFixture(t, Images)
	for _, raw := range []string{`{"model":"workflow_media","prompt":"a","prompt":"b"}`, `{"model":"workflow_media","prompt":"a","workflow":{"nodes":[]}}`, `{"model":"workflow_media","prompt":"a","n":2}`, `{"model":"workflow_media","prompt":"a","stream":true}`} {
		w := recorder()
		f.handler.Forward(w, f.request("/v1/images/generations", raw))
		if w.Code != 400 {
			t.Fatal("unsupported request admitted", w.Code)
		}
	}
	if f.session.seals.Load() != 0 || f.session.submits.Load() != 0 {
		t.Fatal("rejected fields reached original input")
	}
}
func TestMediaMissingBearerCannotReadInputOrRevealModel(t *testing.T) {
	f := newGatewayFixture(t, Images)
	r := f.request("/v1/images/generations", imageBody("private", "url"))
	r.Header.Del("Authorization")
	w := recorder()
	f.handler.Forward(w, r)
	if w.Code != 401 || f.session.seals.Load() != 0 {
		t.Fatal("missing credential admitted")
	}
}
func TestMediaHostOriginAndCookieAreCheckedBeforeOriginalAdmission(t *testing.T) {
	f := newGatewayFixture(t, Images)
	for _, change := range []func(*http.Request){func(r *http.Request) { r.Host = "foreign.example" }, func(r *http.Request) { r.Header.Set("Origin", "https://foreign.example") }, func(r *http.Request) { r.Header.Set("Cookie", "session=foreign") }, func(r *http.Request) { r.Header["authorization"] = []string{"Bearer duplicate"} }} {
		r := f.request("/v1/images/generations", imageBody("private", "url"))
		change(r)
		w := recorder()
		before := f.session.checks.Load()
		f.handler.Forward(w, r)
		if w.Code != 400 || f.session.checks.Load() != before {
			t.Fatal("foreign browser request reached original scope")
		}
	}
}
func TestMediaAnthropicMessagesCannotPretendToGenerateMedia(t *testing.T) {
	f := newGatewayFixture(t, Images)
	w := recorder()
	f.handler.Forward(w, f.request("/v1/messages", imageBody("private", "url")))
	if w.Code != 404 || f.session.submits.Load() != 0 {
		t.Fatal("messages endpoint fabricated image generation")
	}
}
func TestMediaIdempotentVideoReplaysQueryWithoutAnotherSubmission(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	var id string
	for i := 0; i < 2; i++ {
		r := f.request("/v1/videos", videoBody("private"))
		r.Header.Set("Idempotency-Key", "same-original-key")
		w := recorder()
		out := f.handler.Forward(w, r)
		if !out.Complete || w.Code != 200 {
			t.Fatal("synthetic video response", w.Code)
		}
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		if i == 0 {
			id = v["id"].(string)
		} else if v["id"] != id {
			t.Fatal("replay changed original public resource")
		}
		if v["status"] != "in_progress" || v["progress"] != float64(0) {
			t.Fatal("fake completion/progress")
		}
	}
	if f.session.submits.Load() != 1 {
		t.Fatal("original invocation resubmitted")
	}
}
func TestMediaCacheCapacityIsReservedBeforeAnotherNativeSeal(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	f.handler.config.Policy.MaximumOutputBytes = cacheCapacity
	first := recorder()
	f.handler.Forward(first, f.request("/v1/videos", videoBody("first")))
	if first.Code != 200 {
		t.Fatal("synthetic original request")
	}
	second := recorder()
	f.handler.Forward(second, f.request("/v1/videos", videoBody("second")))
	if second.Code != 429 || f.session.seals.Load() != 1 || f.session.submits.Load() != 1 {
		t.Fatal("cache exhaustion was discovered after native admission")
	}
}
func TestMediaSameKeyChangedPromptConflictsWithoutAnotherSubmission(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	for i, prompt := range []string{"first private prompt", "changed private prompt"} {
		r := f.request("/v1/videos", videoBody(prompt))
		r.Header.Set("Idempotency-Key", "original")
		w := recorder()
		f.handler.Forward(w, r)
		if i == 1 && w.Code != 409 {
			t.Fatal("changed body accepted under original key")
		}
	}
	if f.session.submits.Load() != 1 {
		t.Fatal("conflicting prompt reached backend")
	}
}
func TestMediaVideoResourceDoesNotRetainOriginalPromptOrBearer(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	w := recorder()
	f.handler.Forward(w, f.request("/v1/videos", videoBody("secret prompt not metadata")))
	if strings.Contains(w.Body.String(), "secret prompt") || strings.Contains(w.Body.String(), f.credential) {
		t.Fatal("content or credential reflected")
	}
	if !strings.Contains(w.Body.String(), `"prompt":null`) {
		t.Fatal("video prompt disclosure contract")
	}
}
func TestMediaSpeechRequiresActualWAVAndApprovedVoiceBeforeInput(t *testing.T) {
	f := newGatewayFixture(t, Speech)
	for _, raw := range []string{`{"model":"workflow_media","input":"hello","voice":"local_voice","response_format":"mp3"}`, `{"model":"workflow_media","input":"hello","voice":"foreign_voice","response_format":"wav"}`, `{"model":"workflow_media","input":"hello","voice":"local_voice","response_format":"wav","speed":2}`} {
		w := recorder()
		f.handler.Forward(w, f.request("/v1/audio/speech", raw))
		if w.Code != 400 {
			t.Fatal("unsupported speech format/voice admitted")
		}
	}
	if f.session.submits.Load() != 0 {
		t.Fatal("invalid speech consumed execution")
	}
}
func TestMediaResponsesAssetRequiresDeclaredStrictSchemaAndBackground(t *testing.T) {
	f := newGatewayFixture(t, Assets)
	raw := `{"model":"workflow_media","input":"shape","background":true,"text":{"format":{"type":"json_schema","name":"mesh_asset_v1","strict":true,"schema":` + AssetSchema + `}}}`
	parsed, e := parseRequest(context.Background(), f.request("/v1/responses", raw), f.mapping)
	if e != nil {
		t.Fatal(e)
	}
	parsed.clear()
	for _, bad := range []string{strings.Replace(raw, `"background":true`, `"background":false`, 1), strings.Replace(raw, `"strict":true`, `"strict":false`, 1), strings.Replace(raw, `"file_id"`, `"arbitrary_path"`, 1)} {
		if request, e := parseRequest(context.Background(), f.request("/v1/responses", bad), f.mapping); e == nil {
			request.clear()
			t.Fatal("undeclared asset response admitted")
		}
	}
}
func TestMediaVideoReferenceRejectsExternalURLBeforeNetworkOrSeal(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	f.mapping.ImageSlot = "image"
	r := f.request("/v1/videos", `{"model":"workflow_media","prompt":"private","input_reference":{"image_url":"http://169.254.169.254/secret"}}`)
	if parsed, e := parseRequest(context.Background(), r, f.mapping); e == nil {
		parsed.clear()
		t.Fatal("external download reference accepted")
	}
}
func TestMediaMultipartDuplicateFieldsDoNotReachOriginalInput(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	m.WriteField("model", "workflow_media")
	m.WriteField("prompt", "first")
	m.WriteField("prompt", "second")
	m.Close()
	r := f.request("/v1/videos", body.String())
	r.Header.Set("Content-Type", m.FormDataContentType())
	w := recorder()
	f.handler.Forward(w, r)
	if w.Code != 400 || f.session.submits.Load() != 0 {
		t.Fatal("duplicate multipart field admitted")
	}
}
func TestMediaEncryptedStagingSupportsAuthenticatedRepeatedRanges(t *testing.T) {
	raw := bytes.Repeat([]byte("private-media-block"), 8000)
	staged, e := stageAsset(context.Background(), bytes.NewReader(raw), int64(len(raw)))
	if e != nil {
		t.Fatal(e)
	}
	defer staged.Close()
	for i := 0; i < 2; i++ {
		buf := make([]byte, 100)
		n, e := staged.readAt(context.Background(), buf, 65530)
		if e != nil || n != 6 || !bytes.Equal(buf[:n], raw[65530:65530+n]) {
			t.Fatal("cache range crossed unauthenticated block", e)
		}
	}
	for _, sealed := range staged.blocks {
		if bytes.Contains(sealed, []byte("private-media-block")) {
			t.Fatal("staging retained plaintext body")
		}
	}
}
func TestMediaStagingOwnershipConsumptionErasesPriorCipherBlocks(t *testing.T) {
	raw := bytes.Repeat([]byte("s"), 100000)
	s, e := stageAsset(context.Background(), bytes.NewReader(raw), 100000)
	if e != nil {
		t.Fatal(e)
	}
	out, e := io.ReadAll(s)
	if e != nil || !bytes.Equal(out, raw) {
		t.Fatal("owned staged source bytes", e)
	}
	clear(out)
	for _, b := range s.blocks {
		if b != nil {
			t.Fatal("consumed staging retained duplicate ciphertext")
		}
	}
	s.Close()
	if _, e = s.Read(make([]byte, 1)); e == nil {
		t.Fatal("closed staging key remained readable")
	}
}
func TestMediaStagingTamperingCannotReturnPlaintext(t *testing.T) {
	s, e := stageAsset(context.Background(), strings.NewReader("private"), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.blocks[0][0] ^= 1
	b := make([]byte, 7)
	if n, e := s.readAt(context.Background(), b, 0); e == nil || n != 0 {
		t.Fatal("tampered ciphertext became output")
	}
}
func TestMediaStagingLimitAndPNGDimensionsAreBounded(t *testing.T) {
	if s, e := stageAsset(context.Background(), strings.NewReader("too large"), 2); e == nil {
		s.Close()
		t.Fatal("asset limit ignored")
	}
	raw := syntheticPNG()
	binary.BigEndian.PutUint32(raw[16:], 100000)
	s, e := stageAsset(context.Background(), bytes.NewReader(raw), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if s.png() {
		t.Fatal("oversized decoded image admitted")
	}
}
func TestMediaBodyHMACUsesAssetPlainBytesAndResponseFormat(t *testing.T) {
	f := newGatewayFixture(t, Images)
	f.mapping.ImageSlot = "image"
	f.handler.config.Mapping.ImageSlot = "image"
	var first string
	for i := 0; i < 2; i++ {
		asset, e := stageAsset(context.Background(), bytes.NewReader(syntheticPNG()), 100)
		if e != nil {
			t.Fatal(e)
		}
		r := &request{model: f.mapping.Model, prompt: "private", size: "1024x1024", format: "url", assets: []*stagedAsset{asset}}
		item, replay, e := f.handler.reserve(context.Background(), r, "same-key")
		r.clear()
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = item.id
		} else if !replay || item.id != first {
			t.Fatal("fresh staging nonce caused false body conflict")
		}
	}
	asset, e := stageAsset(context.Background(), bytes.NewReader(syntheticPNG()), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer asset.Close()
	if _, _, e = f.handler.reserve(context.Background(), &request{model: f.mapping.Model, prompt: "private", size: "1024x1024", format: "b64_json", assets: []*stagedAsset{asset}}, "same-key"); e == nil {
		t.Fatal("changed response format accepted under original key")
	}
}
func TestMediaVideoFramesAreDerivedOnlyFromApprovedRate(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	m := f.mapping
	m.SecondsSlot = ""
	m.FramesSlot = "frames"
	m.FrameRateSlot = "fps"
	m.FrameRate = 24
	missingFPS := m
	missingFPS.FrameRateSlot = ""
	if missingFPS.binds(f.handler.config.Prepared, f.handler.config.Policy) {
		t.Fatal("frame count advertised without the encoder FPS mapping")
	}
	if !m.binds(f.handler.config.Prepared, f.handler.config.Policy) {
		t.Fatal("approved frame mapping refused")
	}
	r := &request{model: m.Model, prompt: "private", size: m.DefaultSize, seconds: "4"}
	raw, _, e := r.parameters(m)
	if e != nil {
		t.Fatal(e)
	}
	defer clear(raw)
	var values map[string]any
	json.Unmarshal(raw, &values)
	if values["frames"] != float64(96) || values["fps"] != float64(24) || values["seconds"] != nil {
		t.Fatal("seconds silently treated as frame count")
	}
}
func TestMediaExpiredCacheCannotReanchorRetentionOrReturnBytes(t *testing.T) {
	f := newGatewayFixture(t, Images)
	cache, e := stageAsset(context.Background(), bytes.NewReader(syntheticPNG()), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer cache.Close()
	item := &entry{cache: cache, bytes: cache.size, completed: time.Now().Add(-time.Minute), expires: time.Now().Add(-time.Second)}
	rec := recorder()
	w := &wire{writer: rec, controller: http.NewResponseController(rec), ctx: context.Background(), timeout: time.Second, status: 200, contentType: "application/json"}
	if f.handler.content(w, f.credential, item, "") == nil || rec.Body.Len() != 0 {
		t.Fatal("expired original cache served or renewed")
	}
}
func TestMediaBase64ImageUsesCompletedCacheWithoutFabricatedUsage(t *testing.T) {
	f := newGatewayFixture(t, Images)
	cache, e := stageAsset(context.Background(), bytes.NewReader(syntheticPNG()), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer cache.Close()
	item := &entry{cache: cache, bytes: cache.size, created: time.Now(), expires: time.Now().Add(time.Minute)}
	rec := recorder()
	wire := &wire{writer: rec, controller: http.NewResponseController(rec), ctx: context.Background(), timeout: time.Second, status: 200, contentType: "application/json"}
	if f.handler.image(wire, f.credential, item, "b64_json") != nil {
		t.Fatal("synthetic image render")
	}
	var data map[string]any
	if json.Unmarshal(rec.Body.Bytes(), &data) != nil || data["usage"] != nil || data["job_id"] != nil {
		t.Fatal("invalid standard image result")
	}
}
func TestMediaSingleRangeResponseReturnsExactAuthenticatedBytes(t *testing.T) {
	f := newGatewayFixture(t, Images)
	raw := syntheticPNG()
	cache, e := stageAsset(context.Background(), bytes.NewReader(raw), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer cache.Close()
	item := &entry{cache: cache, bytes: cache.size, expires: time.Now().Add(time.Minute)}
	rec := recorder()
	w := &wire{writer: rec, controller: http.NewResponseController(rec), ctx: context.Background(), timeout: time.Second, status: 200, contentType: "application/json"}
	if f.handler.content(w, f.credential, item, "bytes=2-7") != nil || rec.Code != 206 || !bytes.Equal(rec.Body.Bytes(), raw[2:8]) {
		t.Fatal("range body/metadata mismatch")
	}
}
func TestMediaInvalidRangeReturns416WithoutNativeResubmission(t *testing.T) {
	for _, v := range []string{"bytes=100-200", "bytes=0-1,4-5", "bytes=4-2", "bytes=-0", "items=0-1", "bytes=+1-2", "bytes=999999999999999999999999-"} {
		if _, _, _, e := byteRange(v, 10); e == nil {
			t.Fatal("invalid range accepted", v)
		}
	}
}
func TestMediaInterruptedWriterCannotProduceSuccessfulJSONTail(t *testing.T) {
	f := newGatewayFixture(t, Images)
	cache, e := stageAsset(context.Background(), bytes.NewReader(syntheticPNG()), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer cache.Close()
	item := &entry{cache: cache, bytes: cache.size, created: time.Now(), expires: time.Now().Add(time.Minute)}
	rec := recorder()
	rec.broken = true
	w := &wire{writer: rec, controller: http.NewResponseController(rec), ctx: context.Background(), timeout: time.Second, status: 200, contentType: "application/json"}
	if f.handler.image(w, f.credential, item, "b64_json") == nil {
		t.Fatal("broken writer produced success")
	}
	if strings.HasSuffix(rec.Body.String(), `"}]}`) {
		t.Fatal("success tail after failed write")
	}
}
func TestMediaGatewayRedactsKeysAndLocalAssetContent(t *testing.T) {
	f := newGatewayFixture(t, Images)
	s, e := stageAsset(context.Background(), strings.NewReader("private-content"), 100)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	view := fmt.Sprintf("%+v %#v", f.handler, s)
	raw, _ := json.Marshal(s)
	if strings.Contains(view, "private-content") || strings.Contains(string(raw), "private-content") || !strings.Contains(view, "redacted") {
		t.Fatal("sensitive diagnostic output")
	}
}

type blockedBody struct {
	entered, closed     chan struct{}
	readOnce, closeOnce sync.Once
}

func (b *blockedBody) Read([]byte) (int, error) {
	b.readOnce.Do(func() { close(b.entered) })
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockedBody) Close() error { b.closeOnce.Do(func() { close(b.closed) }); return nil }
func TestMediaCloseInterruptsOwnedBodyBeforeErasingGatewayKeys(t *testing.T) {
	f := newGatewayFixture(t, Images)
	body := &blockedBody{entered: make(chan struct{}), closed: make(chan struct{})}
	r := f.request("/v1/images/generations", "")
	r.Body = body
	r.ContentLength = -1
	returned := make(chan struct{})
	go func() { defer close(returned); f.handler.Forward(recorder(), r) }()
	<-body.entered
	f.handler.RequestStop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if f.handler.Join(ctx) != nil {
		t.Fatal("entered HTTP body not joined")
	}
	<-returned
	for _, b := range f.handler.key {
		if b != 0 {
			t.Fatal("joined gateway key not erased")
		}
	}
	if f.stops.Load() != 1 {
		t.Fatal("original stop hook not once-only")
	}
}
func TestMediaCloseTimeoutRetainsOwnerUntilEnteredSubmitActuallyReturns(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	f.session.entered = make(chan struct{}, 1)
	f.session.release = make(chan struct{})
	release := sync.OnceFunc(func() { close(f.session.release) })
	defer release()
	r := f.request("/v1/videos", videoBody("private"))
	returned := make(chan struct{})
	go func() { defer close(returned); f.handler.Forward(recorder(), r) }()
	<-f.session.entered
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if f.handler.Close(ctx) == nil {
		t.Fatal("entered fake native operation reported joined")
	}
	select {
	case <-f.handler.done:
		t.Fatal("gateway Done before actual native return")
	default:
	}
	if bytes.Equal(f.handler.key, make([]byte, 32)) {
		t.Fatal("original request key erased before join")
	}
	release()
	<-returned
	joined, end := context.WithTimeout(context.Background(), 2*time.Second)
	defer end()
	if f.handler.Join(joined) != nil {
		t.Fatal("actual submit did not join")
	}
}

func TestMediaResponseScopesArePreflightedBeforeNativeInput(t *testing.T) {
	for _, profile := range []string{Images, Videos} {
		t.Run(profile, func(t *testing.T) {
			f := newGatewayFixture(t, profile)
			path, body := "/v1/images/generations", imageBody("private", "url")
			f.session.denyScope = "EXPORT_SELF"
			if profile == Videos {
				path, body = "/v1/videos", videoBody("private")
				f.session.denyScope = "QUEUE_SELF"
			}
			w := recorder()
			f.handler.Forward(w, f.request(path, body))
			if w.Code != 403 || f.session.seals.Load() != 0 || f.session.submits.Load() != 0 {
				t.Fatal("response privilege discovered after execution admission")
			}
		})
	}
}

func TestMediaExportOnlyCredentialCanReadCompletedOriginalCache(t *testing.T) {
	f := newGatewayFixture(t, Images)
	f.session.denyScope = "QUEUE_SELF"
	raw := syntheticPNG()
	cache, err := stageAsset(context.Background(), bytes.NewReader(raw), 100)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("1", 32)
	submitted := make(chan struct{})
	close(submitted)
	item := &entry{id: "invoke_" + token, file: "file_" + token, submitted: submitted,
		cache: cache, bytes: cache.size, completed: time.Now(), expires: time.Now().Add(time.Minute)}
	f.handler.entries[item.id] = item
	r := f.request("/v1/files/"+item.file+"/content", "")
	r.Method = http.MethodGet
	w := recorder()
	out := f.handler.Forward(w, r)
	if !out.Complete || w.Code != 200 || !bytes.Equal(w.Body.Bytes(), raw) || f.session.submits.Load() != 0 {
		t.Fatal("export incorrectly requires queue scope or resubmits work")
	}
}

func TestMediaUncachedExportObservesUnderExportScopeOnly(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	f.session.denyScope = "QUEUE_SELF"
	submitted := make(chan struct{})
	close(submitted)
	item := &entry{id: "invoke_" + strings.Repeat("2", 32), submitted: submitted}
	ctx, end := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer end()
	if f.handler.await(ctx, f.credential, item) == nil || f.session.exportCalls.Load() == 0 || f.session.submits.Load() != 0 {
		t.Fatal("uncached export acquired queue authority or invented a result")
	}
}

func TestMediaUncertainQueryCannotDestroyOriginalEncryptedResult(t *testing.T) {
	f := newGatewayFixture(t, Videos)
	f.session.queryError = true
	cache, err := stageAsset(context.Background(), strings.NewReader("synthetic-cache"), 100)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	submitted := make(chan struct{})
	close(submitted)
	item := &entry{id: "invoke_" + strings.Repeat("3", 32), submitted: submitted,
		cache: cache, bytes: cache.size, reservation: cache.size, expires: time.Now().Add(time.Minute)}
	f.handler.reserved = item.reservation
	w := recorder()
	wire := &wire{writer: w, controller: http.NewResponseController(w), ctx: context.Background(), timeout: time.Second, status: 200, contentType: "application/json"}
	if f.handler.resource(wire, f.credential, item) == nil || item.failed || item.cache != cache || cache.closed || item.reservation != cache.size {
		t.Fatal("uncertain original query erased result or released its cache accounting")
	}
}

func TestMediaCancelledAdmissionCannotReserveCacheOrOriginalID(t *testing.T) {
	f := newGatewayFixture(t, Images)
	ctx, end := context.WithCancel(context.Background())
	end()
	_, _, err := f.handler.reserve(ctx, &request{model: f.mapping.Model, prompt: "private", size: "1024x1024", format: "url"}, "cancelled")
	if err == nil || len(f.handler.entries) != 0 || f.handler.reserved != 0 {
		t.Fatal("cancelled body entered original admission")
	}
}

func TestMediaCancelIntentStopsFollowingChunksWithoutWaitingForReaderLock(t *testing.T) {
	f := newGatewayFixture(t, Assets)
	cache, err := stageAsset(context.Background(), strings.NewReader(strings.Repeat("x", 65537)), 65537)
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	item := &entry{cache: cache, bytes: cache.size, expires: time.Now().Add(time.Minute)}
	w := recorder()
	wire := &wire{writer: w, controller: http.NewResponseController(w), ctx: context.Background(), timeout: time.Second, status: 200, contentType: "application/json"}
	writes := 0
	item.mu.Lock() // Production streaming owns this mutex for cache lifetime.
	err = f.handler.copyCache(wire, f.credential, item, 0, item.bytes, func(b []byte) error {
		writes++
		// A trusted, independently authorized cancel can publish this flag
		// without waiting for the reader mutex or a native stop response.
		item.cancelled.Store(true)
		return nil
	})
	item.mu.Unlock()
	if err == nil || writes != 1 || f.session.cancels.Load() != 0 {
		t.Fatal("local cancel intent failed to stop later chunks or invented native cancellation")
	}
}
