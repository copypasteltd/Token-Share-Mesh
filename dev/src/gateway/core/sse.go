package core

import (
	"bufio"
	"bytes"
	"io"
	"strings"
	"unicode/utf8"
)

// There is one bounded event buffer, no token channel or whole-stream buffer.
// A blank line dispatches an event; EOF never manufactures a missing terminal.
type eventReader struct {
	r      *bufio.Reader
	max    int
	skipLF bool
}

func newEventReader(r io.Reader, max int) *eventReader {
	return &eventReader{r: bufio.NewReaderSize(r, 4096), max: max}
}
func (r *eventReader) line() ([]byte, error) {
	line := make([]byte, 0, 128)
	for {
		v, e := r.r.ReadByte()
		if e != nil {
			return line, e
		}
		if r.skipLF {
			r.skipLF = false
			if v == '\n' {
				continue
			}
		}
		if v == '\r' {
			r.skipLF = true
			return line, nil
		}
		if v == '\n' {
			return line, nil
		}
		if len(line) >= r.max {
			return nil, errProtocol
		}
		line = append(line, v)
	}
}
func (r *eventReader) next() (string, []byte, error) {
	var name string
	var data []byte
	size := 0
	eventSeen := false
	for {
		line, e := r.line()
		if e != nil {
			return "", nil, e
		}
		size += len(line) + 1
		if size > r.max || !utf8.Valid(line) {
			return "", nil, errProtocol
		}
		if len(line) == 0 {
			if len(data) == 0 {
				size = 0
				name = ""
				eventSeen = false
				continue
			}
			return name, data[:len(data)-1], nil
		}
		if line[0] == ':' {
			continue
		}
		key, value, found := bytes.Cut(line, []byte(":"))
		if !found {
			value = nil
		}
		value = bytes.TrimPrefix(value, []byte(" "))
		switch string(key) {
		case "event":
			if eventSeen || len(value) > 80 || strings.ContainsAny(string(value), "\x00\r\n") {
				return "", nil, errProtocol
			}
			name = string(value)
			eventSeen = true
		case "data":
			data = append(data, value...)
			data = append(data, '\n')
		case "id", "retry": // No automatic SSE reconnection or new execution.
		default:
			return "", nil, errProtocol
		}
	}
}
