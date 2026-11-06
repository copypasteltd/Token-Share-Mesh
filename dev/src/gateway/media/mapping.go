// Package media serves explicitly approved OpenAI media dialects inside one
// original rental. It opens no listener and grants no paid execution authority.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"p11.local/compute/client/execution"
	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/client/rentalmedia"
	"p11.local/compute/shared/strictjson"
)

const Images = "P11_OPENAI_IMAGES_RENTAL_1"
const Videos = "P11_OPENAI_VIDEOS_LEGACY_RENTAL_1"
const Speech = "P11_OPENAI_SPEECH_RENTAL_1"
const Assets = "P11_RESPONSES_ASSET_RENTAL_1"
const MaximumJSONBytes = 2 << 20
const MaximumRequestBytes = 66 << 20

var ErrRequest = errors.New("unsupported approved media request")
var ErrAccess = errors.New("original media access unavailable")
var ErrConflict = errors.New("original media request is query-only")
var identifier = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,127}$`)
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_:.+-]{1,128}$`)

// Mapping is independent owner-approved configuration, never buyer JSON. Its
// slots refer to the exact pinned recipe. Qualification/publication still
// requires that recipe's real schema/output to match all advertised choices.
type Mapping struct {
	Profile, ManifestHash, ApprovalHash, RecipeHash, PolicyHash, Model   string
	PromptSlot, WidthSlot, HeightSlot, SecondsSlot, VoiceSlot, ImageSlot string
	FramesSlot, FrameRateSlot                                            string
	FrameRate                                                            int
	Sizes, Seconds, Voices                                               []string
	DefaultSize, DefaultSeconds, DefaultVoice                            string
	Defaults                                                             map[string]json.RawMessage
	MaximumPromptBytes                                                   int
	LegacyVideosExplicitlyApproved                                       bool
}

func (m Mapping) Digest() string {
	raw, _ := json.Marshal(m)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func (m Mapping) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[approved original media mapping]")
}
func cloneMapping(m Mapping) Mapping {
	m.Sizes = slices.Clone(m.Sizes)
	m.Seconds = slices.Clone(m.Seconds)
	m.Voices = slices.Clone(m.Voices)
	defaults := map[string]json.RawMessage{}
	for k, v := range m.Defaults {
		defaults[k] = slices.Clone(v)
	}
	m.Defaults = defaults
	return m
}
func (m Mapping) binds(p rentalaccess.Prepared, policy rentalaccess.MediaPolicy) bool {
	d := p.Details()
	if !policy.Binds(p) || d.Privacy != "STANDARD" || m.Profile != d.Protocol || m.Model != d.PublicSpec.WorkflowID || m.ManifestHash != p.Digest() || m.ApprovalHash != p.ApprovalDigest() || m.RecipeHash != d.RecipeHash || m.PolicyHash != policy.Digest() || m.MaximumPromptBytes < 1 || m.MaximumPromptBytes > 65536 || !identifier.MatchString(m.PromptSlot) || len(m.Defaults) > 24 {
		return false
	}
	seen := map[string]bool{}
	for _, slot := range []string{m.PromptSlot, m.WidthSlot, m.HeightSlot, m.SecondsSlot, m.VoiceSlot, m.ImageSlot, m.FramesSlot, m.FrameRateSlot} {
		if slot == "" {
			continue
		}
		if !identifier.MatchString(slot) || len(slot) > 64 || seen[slot] {
			return false
		}
		seen[slot] = true
	}
	for k, v := range m.Defaults {
		var scalar any
		if !identifier.MatchString(k) || len(k) > 64 || seen[k] || len(v) > 4096 || strictjson.Decode(v, &scalar, 4096) != nil {
			return false
		}
		switch scalar.(type) {
		case string, json.Number:
		default:
			return false
		}
	}
	if len(m.Sizes) > 16 || len(m.Seconds) > 8 || len(m.Voices) > 32 {
		return false
	}
	if m.Profile != Videos && (m.FramesSlot != "" || m.FrameRateSlot != "" || m.FrameRate != 0) {
		return false
	}
	if m.Profile == Videos {
		if (m.FramesSlot == "") == (m.SecondsSlot == "") {
			return false
		}
		if m.FramesSlot != "" && (m.FrameRateSlot == "" || m.FrameRate < 1 || m.FrameRate > 60) {
			return false
		}
		if m.FramesSlot == "" && (m.FrameRateSlot != "" || m.FrameRate != 0) {
			return false
		}
	}
	for _, v := range m.Sizes {
		if _, _, ok := dimensions(v); !ok {
			return false
		}
	}
	for _, v := range m.Seconds {
		if !slices.Contains([]string{"4", "8", "12"}, v) {
			return false
		}
	}
	for _, v := range m.Voices {
		if !identifier.MatchString(v) {
			return false
		}
	}
	switch m.Profile {
	case Images:
		return policy.Kind == "PNG" && m.WidthSlot != "" && m.HeightSlot != "" && slices.Contains(m.Sizes, m.DefaultSize) && len(m.Seconds) == 0 && len(m.Voices) == 0 && m.SecondsSlot == "" && m.VoiceSlot == ""
	case Videos:
		return policy.Kind == "MP4" && m.LegacyVideosExplicitlyApproved && m.WidthSlot != "" && m.HeightSlot != "" && slices.Contains(m.Sizes, m.DefaultSize) && slices.Contains(m.Seconds, m.DefaultSeconds) && len(m.Voices) == 0 && m.VoiceSlot == "" && videoSizes(m.Sizes)
	case Speech:
		return policy.Kind == "WAV" && m.VoiceSlot != "" && slices.Contains(m.Voices, m.DefaultVoice) && m.ImageSlot == "" && m.WidthSlot == "" && m.HeightSlot == "" && m.SecondsSlot == "" && len(m.Sizes) == 0 && len(m.Seconds) == 0
	case Assets:
		return (policy.Kind == "GLB" || policy.Kind == "WAV") && m.WidthSlot == "" && m.HeightSlot == "" && m.SecondsSlot == "" && m.VoiceSlot == "" && m.ImageSlot == "" && len(m.Sizes) == 0 && len(m.Seconds) == 0 && len(m.Voices) == 0
	}
	return false
}
func dimensions(value string) (int, int, bool) {
	parts := strings.Split(value, "x")
	if len(parts) != 2 {
		return 0, 0, false
	}
	w, a := strconv.Atoi(parts[0])
	h, b := strconv.Atoi(parts[1])
	return w, h, a == nil && b == nil && w >= 64 && h >= 64 && w <= 8192 && h <= 8192 && strconv.Itoa(w)+"x"+strconv.Itoa(h) == value
}
func videoSizes(sizes []string) bool {
	for _, s := range sizes {
		if !slices.Contains([]string{"720x1280", "1280x720", "1024x1792", "1792x1024"}, s) {
			return false
		}
	}
	return len(sizes) > 0
}

// Session is bound by the node to one original prepared/authorized ComfyRental.
// Check must authenticate this exact original buyer Bearer for every request.
// Seal takes ownership of all assets even when it refuses the request.
type Session interface {
	Check(context.Context, string, string) error
	Seal(context.Context, string, string, []byte, []rentalaccess.MediaAsset) (*rentalaccess.MediaInput, error)
	Invoke(context.Context, *rentalaccess.MediaInput) (execution.RentalInvocation, error)
	Status(context.Context, string, string) (rentalmedia.MediaState, error)
	ExportStatus(context.Context, string, string) (rentalmedia.MediaState, error)
	Output(context.Context, string, string) (*rentalmedia.Output, error)
	Cancel(context.Context, string, string) error
	Discard(context.Context, string, string) error
}
type Config struct {
	Prepared                     rentalaccess.Prepared
	Policy                       rentalaccess.MediaPolicy
	Mapping                      Mapping
	Session                      Session
	OriginalContext              context.Context
	StopOriginal                 func()
	AllowedHosts, AllowedOrigins []string
	PublicBaseURL                string
	Concurrent                   int
	Total, Write, Retention      time.Duration
}
