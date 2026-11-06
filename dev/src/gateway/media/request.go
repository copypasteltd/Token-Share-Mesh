package media

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"p11.local/compute/client/protocol/rentalaccess"
	"p11.local/compute/shared/strictjson"
)

type request struct {
	model, prompt, size, seconds, voice, format string
	background                                  bool
	assets                                      []*stagedAsset
}

func (r *request) Format(w fmt.State, _ rune) {
	_, _ = io.WriteString(w, "[original media request redacted]")
}
func (r *request) clear() {
	if r == nil {
		return
	}
	r.prompt = ""
	for _, a := range r.assets {
		a.Close()
	}
	r.assets = nil
}

type imageRequest struct {
	Model        string `json:"model"`
	Prompt       string `json:"prompt"`
	Size         string `json:"size,omitempty"`
	N            *int   `json:"n,omitempty"`
	Format       string `json:"response_format,omitempty"`
	OutputFormat string `json:"output_format,omitempty"`
	Stream       bool   `json:"stream,omitempty"`
}
type videoRequest struct {
	Model     string `json:"model"`
	Prompt    string `json:"prompt"`
	Size      string `json:"size,omitempty"`
	Seconds   string `json:"seconds,omitempty"`
	Reference *struct {
		ImageURL string `json:"image_url,omitempty"`
		FileID   string `json:"file_id,omitempty"`
	} `json:"input_reference,omitempty"`
}
type speechRequest struct {
	Model        string       `json:"model"`
	Input        string       `json:"input"`
	Voice        string       `json:"voice"`
	Format       string       `json:"response_format"`
	Speed        *json.Number `json:"speed,omitempty"`
	StreamFormat string       `json:"stream_format,omitempty"`
}
type assetRequest struct {
	Model      string `json:"model"`
	Input      string `json:"input"`
	Background bool   `json:"background"`
	Store      *bool  `json:"store,omitempty"`
	Stream     bool   `json:"stream,omitempty"`
	Text       struct {
		Format struct {
			Type   string          `json:"type"`
			Name   string          `json:"name"`
			Strict bool            `json:"strict"`
			Schema json.RawMessage `json:"schema"`
		} `json:"format"`
	} `json:"text"`
}

const AssetSchema = `{"type":"object","additionalProperties":false,"properties":{"file_id":{"type":"string"},"mime_type":{"type":"string"},"bytes":{"type":"integer"}},"required":["file_id","mime_type","bytes"]}`

func sameSchema(raw []byte) bool {
	var got, want map[string]any
	if strictjson.Decode(raw, &got, 4096) != nil || strictjson.Decode([]byte(AssetSchema), &want, 4096) != nil {
		return false
	}
	a, _ := json.Marshal(got)
	b, _ := json.Marshal(want)
	return string(a) == string(b)
}
func parseRequest(ctx context.Context, r *http.Request, m Mapping) (out *request, err error) {
	out = &request{}
	defer func() {
		if err != nil {
			out.clear()
		}
	}()
	typ, options, e := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if e != nil {
		return out, ErrRequest
	}
	if typ == "multipart/form-data" {
		if m.Profile != Videos && !(m.Profile == Images && r.URL.Path == "/v1/images/edits") || len(options) != 1 || len(options["boundary"]) < 1 || len(options["boundary"]) > 70 {
			return out, ErrRequest
		}
		return parseMultipart(ctx, r, m, out, options["boundary"])
	}
	if typ != "application/json" || len(options) > 1 || len(options) == 1 && strings.ToLower(options["charset"]) != "utf-8" || r.URL.Path == "/v1/images/edits" {
		return out, ErrRequest
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, MaximumJSONBytes+1))
	defer clear(raw)
	if e != nil || len(raw) > MaximumJSONBytes || ctx.Err() != nil {
		return out, ErrRequest
	}
	switch m.Profile {
	case Images:
		var v imageRequest
		if strictjson.Decode(raw, &v, MaximumJSONBytes) != nil || v.N != nil && *v.N != 1 || v.Stream || v.OutputFormat != "" && v.OutputFormat != "png" {
			return out, ErrRequest
		}
		out.model, out.prompt, out.size, out.format = v.Model, v.Prompt, v.Size, v.Format
	case Videos:
		var v videoRequest
		if strictjson.Decode(raw, &v, MaximumJSONBytes) != nil {
			return out, ErrRequest
		}
		out.model, out.prompt, out.size, out.seconds = v.Model, v.Prompt, v.Size, v.Seconds
		if v.Reference != nil {
			if v.Reference.FileID != "" || m.ImageSlot == "" || !strings.HasPrefix(v.Reference.ImageURL, "data:image/png;base64,") {
				return out, ErrRequest
			}
			asset, e := stageAsset(ctx, base64.NewDecoder(base64.StdEncoding, strings.NewReader(strings.TrimPrefix(v.Reference.ImageURL, "data:image/png;base64,"))), rentalaccess.MaximumMediaAssetsBytes)
			if e != nil {
				return out, e
			}
			out.assets = append(out.assets, asset)
			if !asset.png() {
				return out, ErrRequest
			}
		}
	case Speech:
		var v speechRequest
		if strictjson.Decode(raw, &v, MaximumJSONBytes) != nil || v.Speed != nil && v.Speed.String() != "1" || v.StreamFormat != "" && v.StreamFormat != "audio" {
			return out, ErrRequest
		}
		out.model, out.prompt, out.voice, out.format = v.Model, v.Input, v.Voice, v.Format
	case Assets:
		var v assetRequest
		if strictjson.Decode(raw, &v, MaximumJSONBytes) != nil || !v.Background || v.Store != nil && !*v.Store || v.Stream || v.Text.Format.Type != "json_schema" || v.Text.Format.Name != "mesh_asset_v1" || !v.Text.Format.Strict || !sameSchema(v.Text.Format.Schema) {
			return out, ErrRequest
		}
		out.model, out.prompt, out.background = v.Model, v.Input, true
	default:
		return out, ErrRequest
	}
	if !out.validate(m) {
		return out, ErrRequest
	}
	return out, nil
}
func parseMultipart(ctx context.Context, r *http.Request, m Mapping, out *request, boundary string) (_ *request, err error) {
	reader := multipart.NewReader(io.LimitReader(r.Body, MaximumRequestBytes+1), boundary)
	seen := map[string]bool{}
	defer func() {
		if err != nil {
			out.clear()
		}
	}()
	for count := 0; ; count++ {
		part, e := reader.NextRawPart()
		if e == io.EOF {
			break
		}
		if e != nil || count >= 12 {
			return out, ErrRequest
		}
		if len(part.Header) > 2 || part.Header.Get("Content-Transfer-Encoding") != "" {
			return out, ErrRequest
		}
		for name, values := range part.Header {
			if (name != "Content-Disposition" && name != "Content-Type") || len(values) != 1 {
				return out, ErrRequest
			}
		}
		name := part.FormName()
		if name == "" || seen[name] {
			return out, ErrRequest
		}
		seen[name] = true
		asset := name == "image" && m.Profile == Images || name == "input_reference" && m.Profile == Videos
		if asset {
			if m.ImageSlot == "" || len(out.assets) != 0 || part.Header.Get("Content-Type") != "image/png" {
				return out, ErrRequest
			}
			staged, e := stageAsset(ctx, part, rentalaccess.MaximumMediaAssetsBytes)
			if e != nil {
				return out, e
			}
			out.assets = append(out.assets, staged)
			if !staged.png() {
				return out, ErrRequest
			}
			continue
		}
		if part.FileName() != "" {
			return out, ErrRequest
		}
		raw, e := io.ReadAll(io.LimitReader(part, 65537))
		if e != nil || len(raw) > 65536 || !utf8.Valid(raw) {
			clear(raw)
			return out, ErrRequest
		}
		value := string(raw)
		clear(raw)
		switch name {
		case "model":
			out.model = value
		case "prompt":
			out.prompt = value
		case "size":
			out.size = value
		case "seconds":
			if m.Profile != Videos {
				return out, ErrRequest
			}
			out.seconds = value
		case "n":
			if m.Profile != Images || value != "1" {
				return out, ErrRequest
			}
		case "response_format":
			if m.Profile != Images {
				return out, ErrRequest
			}
			out.format = value
		case "output_format":
			if m.Profile != Images || value != "png" {
				return out, ErrRequest
			}
		default:
			return out, ErrRequest
		}
	}
	if m.Profile == Images && len(out.assets) != 1 || !out.validate(m) {
		return out, ErrRequest
	}
	return out, nil
}
func (r *request) validate(m Mapping) bool {
	if r.model != m.Model || len(r.prompt) < 1 || len(r.prompt) > m.MaximumPromptBytes || !utf8.ValidString(r.prompt) || strings.ContainsRune(r.prompt, 0) {
		return false
	}
	if r.size == "" {
		r.size = m.DefaultSize
	}
	if r.seconds == "" {
		r.seconds = m.DefaultSeconds
	}
	if r.voice == "" {
		r.voice = m.DefaultVoice
	}
	switch m.Profile {
	case Images:
		if r.format == "" {
			r.format = "b64_json"
		}
		return slices.Contains(m.Sizes, r.size) && (r.format == "b64_json" || r.format == "url")
	case Videos:
		return slices.Contains(m.Sizes, r.size) && slices.Contains(m.Seconds, r.seconds)
	case Speech:
		return slices.Contains(m.Voices, r.voice) && r.format == "wav" && utf8.RuneCountInString(r.prompt) <= 4096
	case Assets:
		return r.background
	}
	return false
}
func (r *request) parameters(m Mapping) ([]byte, []rentalaccess.MediaAsset, error) {
	values := map[string]json.RawMessage{}
	for k, v := range m.Defaults {
		values[k] = v
	}
	set := func(name string, value any) {
		if name != "" {
			values[name], _ = json.Marshal(value)
		}
	}
	set(m.PromptSlot, r.prompt)
	if m.WidthSlot != "" {
		w, h, _ := dimensions(r.size)
		set(m.WidthSlot, w)
		set(m.HeightSlot, h)
	}
	if m.SecondsSlot != "" {
		seconds, _ := strconv.Atoi(r.seconds)
		set(m.SecondsSlot, seconds)
	}
	if m.FramesSlot != "" {
		seconds, _ := strconv.Atoi(r.seconds)
		set(m.FramesSlot, seconds*m.FrameRate)
		set(m.FrameRateSlot, m.FrameRate)
	}
	set(m.VoiceSlot, r.voice)
	assets := []rentalaccess.MediaAsset{}
	for _, a := range r.assets {
		set(m.ImageSlot, "input_image")
		assets = append(assets, rentalaccess.MediaAsset{ID: "input_image", Bytes: a.size, Source: a})
	}
	raw, e := json.Marshal(values)
	return raw, assets, e
}
