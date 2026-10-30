// Package strictjson decodes bounded control documents without ambiguous keys.
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
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
	shape := json.NewDecoder(bytes.NewReader(raw))
	shape.UseNumber()
	var document any
	targetType := reflect.TypeOf(target)
	if targetType == nil || targetType.Kind() != reflect.Pointer || reflect.ValueOf(target).IsNil() ||
		shape.Decode(&document) != nil || !exactFields(document, targetType.Elem()) {
		return ErrDocument
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(target) != nil || d.Decode(new(any)) != io.EOF {
		return ErrDocument
	}
	return nil
}

// encoding/json accepts case-insensitive aliases. Control DTOs instead use
// exactly their declared JSON names, so aliases cannot override signed fields.
func exactFields(value any, t reflect.Type) bool {
	if t == nil {
		return true
	}
	unmarshaler := reflect.TypeFor[json.Unmarshaler]()
	if t.Implements(unmarshaler) || t.Kind() != reflect.Pointer && reflect.PointerTo(t).Implements(unmarshaler) {
		return true
	}
	for t.Kind() == reflect.Pointer {
		if value == nil {
			return true
		}
		t = t.Elem()
	}
	// encoding/json silently turns null into a scalar's zero value. Control
	// schemas do not permit that alias for an omitted string/number/boolean.
	if value == nil && t.Kind() != reflect.Interface && t.Kind() != reflect.Map && t.Kind() != reflect.Slice {
		return false
	}
	switch t.Kind() {
	case reflect.Struct:
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		fields := map[string]reflect.Type{}
		declaredFields(t, fields)
		for key, item := range object {
			child, ok := fields[key]
			if !ok || !exactFields(item, child) {
				return false
			}
		}
	case reflect.Slice, reflect.Array:
		if value == nil {
			return true
		}
		items, ok := value.([]any)
		if !ok {
			return false
		}
		for _, item := range items {
			if !exactFields(item, t.Elem()) {
				return false
			}
		}
	case reflect.Map:
		if value == nil {
			return true
		}
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		for _, item := range object {
			if !exactFields(item, t.Elem()) {
				return false
			}
		}
	}
	return true
}
func declaredFields(t reflect.Type, out map[string]reflect.Type) {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		tag := strings.Split(field.Tag.Get("json"), ",")[0]
		if tag == "-" {
			continue
		}
		child := field.Type
		for child.Kind() == reflect.Pointer {
			child = child.Elem()
		}
		if field.Anonymous && tag == "" && child.Kind() == reflect.Struct {
			declaredFields(child, out)
			continue
		}
		if tag == "" {
			tag = field.Name
		}
		out[tag] = field.Type
	}
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
