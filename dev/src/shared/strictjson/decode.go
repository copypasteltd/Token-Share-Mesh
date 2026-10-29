// Package strictjson decodes bounded control documents without ambiguous keys.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode/utf8"
)

var ErrDocument = errors.New("invalid or ambiguous JSON document")

// Decode rejects duplicate object keys at every depth, trailing values,
// unknown struct fields, invalid UTF-8 and excessive size/depth. No diagnostic
// includes input contents, which may contain secrets in other callers.
func Decode(raw []byte, target any, maxBytes int) error {
	if len(raw) == 0 || len(raw) > maxBytes || !utf8.Valid(raw) {
		return ErrDocument
	}
	scan := json.NewDecoder(bytes.NewReader(raw))
	scan.UseNumber()
	if value(scan, 0) != nil {
		return ErrDocument
	}
	if _, err := scan.Token(); err != io.EOF {
		return ErrDocument
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return ErrDocument
	}
	return nil
}

func value(d *json.Decoder, depth int) error {
	if depth > 32 {
		return ErrDocument
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	delim, composite := t.(json.Delim)
	if !composite {
		return nil
	}
	switch delim {
	case '{':
		keys := map[string]bool{}
		for d.More() {
			t, err := d.Token()
			if err != nil {
				return err
			}
			key, ok := t.(string)
			if !ok || keys[key] {
				return ErrDocument
			}
			keys[key] = true
			if err = value(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err = value(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return ErrDocument
	}
	end, err := d.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return ErrDocument
	}
	return nil
}
