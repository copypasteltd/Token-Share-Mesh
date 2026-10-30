// Package llamaworker connects one approved native llama.cpp worker to an
// execution supervisor. It opens no content listener and grants no authority.
package llamaworker

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strconv"
	"unicode/utf8"
)

const (
	Profile             = "P11_LLAMA_PIPE_3"
	UpstreamCommit      = "8642541b255bfb5dc39cd351a178be09013a55d6"
	EngineRelease       = "llama.cpp-v0.8.0-8642541b"
	maxFrame            = 65536 // includes the type byte, excludes the four-byte prefix
	maxBody             = 512 << 10
	maxJobs             = 1024
	kInit          byte = 1
	kBegin         byte = 2
	kData          byte = 3
	kEnd           byte = 4
	kCancel        byte = 5
	kReady         byte = 129
	kStarted       byte = 130
	kOutput        byte = 131
	kDone          byte = 132
	kError         byte = 133
	kInitError     byte = 134
)

// StartupError reports only a fixed stage and numeric native error. It contains
// no library path, prompt or upstream diagnostic. It is not cleanup proof.
type StartupError struct {
	Stage      string
	NativeCode uint32
}

func (e StartupError) Error() string {
	return "native kernel startup rejected at " + e.Stage + " (native=" + strconv.FormatUint(uint64(e.NativeCode), 10) + ")"
}
func (e StartupError) Unwrap() error { return ErrUnknown }

var startupStages = [...]string{"", "LOAD_MAIN", "LOAD_REGISTRY", "LOAD_CORE", "RESOLVE_API", "LOAD_BACKENDS", "SELECT_DEVICE", "LOAD_MODEL"}

var (
	ErrConfig   = errors.New("native kernel configuration rejected")
	ErrInput    = errors.New("native kernel input rejected")
	ErrUnknown  = errors.New("native kernel outcome unknown")
	ErrWork     = errors.New("native kernel original work mismatch")
	ErrCapacity = errors.New("native kernel bounded capacity exhausted")
	ErrOutput   = errors.New("native kernel output unavailable or discarded")
)

type encoder struct{ bytes.Buffer }

func (e *encoder) u32(v uint32)   { _ = binary.Write(&e.Buffer, binary.BigEndian, v) }
func (e *encoder) u64(v uint64)   { _ = binary.Write(&e.Buffer, binary.BigEndian, v) }
func (e *encoder) real(v float64) { e.u64(math.Float64bits(v)) }
func (e *encoder) str(v string)   { e.u32(uint32(len(v))); _, _ = e.WriteString(v) }

type decoder struct {
	raw []byte
	at  int
	bad bool
}

func (d *decoder) take(n int) []byte {
	if d.bad || n < 0 || n > len(d.raw)-d.at {
		d.bad = true
		return nil
	}
	b := d.raw[d.at : d.at+n]
	d.at += n
	return b
}
func (d *decoder) u32() uint32 {
	b := d.take(4)
	if d.bad {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}
func (d *decoder) str(limit int) string {
	n := d.u32()
	if uint64(n) > uint64(limit) {
		d.bad = true
		return ""
	}
	b := d.take(int(n))
	if !utf8.Valid(b) {
		d.bad = true
	}
	return string(b)
}
func (d *decoder) end() bool { return !d.bad && d.at == len(d.raw) }

func writeFrame(w io.Writer, kind byte, b []byte) error {
	if len(b)+1 > maxFrame {
		return ErrInput
	}
	var h [5]byte
	binary.BigEndian.PutUint32(h[:4], uint32(len(b)+1))
	h[4] = kind
	for _, part := range [][]byte{h[:], b} {
		for len(part) > 0 {
			n, e := w.Write(part)
			if e != nil {
				return e
			}
			if n == 0 {
				return io.ErrShortWrite
			}
			part = part[n:]
		}
	}
	return nil
}
func readFrame(r io.Reader) (byte, []byte, error) {
	var h [4]byte
	if _, e := io.ReadFull(r, h[:]); e != nil {
		return 0, nil, e
	}
	n := binary.BigEndian.Uint32(h[:])
	if n < 1 || n > maxFrame {
		return 0, nil, ErrUnknown
	}
	b := make([]byte, n)
	if _, e := io.ReadFull(r, b); e != nil {
		clear(b)
		return 0, nil, e
	}
	return b[0], b[1:], nil
}
