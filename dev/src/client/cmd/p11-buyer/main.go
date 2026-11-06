// p11-buyer receives only an existing result. It cannot invoke a model, accept
// a job, submit payment or load an account session/private provider key.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"p11.local/compute/client/buyerresults"
	"p11.local/compute/client/protocol/textoutput"
	"p11.local/compute/client/results"
	"p11.local/compute/shared/resultwire"
	"p11.local/compute/shared/strictjson"
)

func main() {
	if e := run(os.Args[1:]); e != nil {
		fmt.Fprintln(os.Stderr, "Original result reception failed; no successful local receipt was issued.")
		os.Exit(1)
	}
}
func projectRuntimePath(value string) (string, error) {
	cwd, e := os.Getwd()
	if e != nil {
		return "", e
	}
	for {
		if stat, e := os.Stat(filepath.Join(cwd, "dev", "src", "go.mod")); e == nil && !stat.IsDir() {
			break
		}
		parent := filepath.Dir(cwd)
		if parent == cwd {
			return "", fmt.Errorf("project root unavailable")
		}
		cwd = parent
	}
	base := filepath.Join(cwd, "runtime")
	if !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
	}
	value, e = filepath.Abs(value)
	if e != nil {
		return "", e
	}
	relative, e := filepath.Rel(base, value)
	if e != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("explicit project runtime path required")
	}
	if e = realExistingPrefix(value); e != nil {
		return "", e
	}
	return value, nil
}

// Inspect existing ancestors before creating a directory, so a symlink in an
// otherwise valid lexical runtime path cannot create files outside the project.
func realExistingPrefix(path string) error {
	for {
		_, e := os.Lstat(path)
		if e == nil {
			real, e := filepath.EvalSymlinks(path)
			if e != nil || filepath.Clean(real) != filepath.Clean(path) {
				return fmt.Errorf("real runtime path required")
			}
			return nil
		}
		if !os.IsNotExist(e) {
			return e
		}
		parent := filepath.Dir(path)
		if parent == path {
			return fmt.Errorf("runtime ancestor unavailable")
		}
		path = parent
	}
}

type boundedFileSink struct{ file *os.File }

func (s boundedFileSink) Write(ctx context.Context, b []byte) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	n, e := s.file.Write(b)
	if e != nil || n != len(b) {
		return io.ErrShortWrite
	}
	return ctx.Err()
}
func createOwned(path string) (*os.File, error) {
	parent := filepath.Dir(path)
	if e := realExistingPrefix(path); e != nil {
		return nil, e
	}
	if e := os.MkdirAll(parent, 0700); e != nil {
		return nil, e
	}
	real, e := filepath.EvalSymlinks(parent)
	if e != nil || filepath.Clean(real) != filepath.Clean(parent) {
		return nil, fmt.Errorf("real runtime directory required")
	}
	return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
}
func run(args []string) error {
	flags := flag.NewFlagSet("p11-buyer receive", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	packetName := flags.String("packet", "", "read packet under project runtime")
	cacheName := flags.String("cache", "", "new empty encrypted cache directory under project runtime")
	outputName := flags.String("output", "", "new explicit received output path under project runtime")
	receiptName := flags.String("receipt", "", "new metadata-only review receipt path under project runtime")
	format := flags.String("format", "protocol", "protocol (original OpenAI/Anthropic JSON/SSE) or raw text")
	if len(args) < 1 || args[0] != "receive" || flags.Parse(args[1:]) != nil || flags.NArg() != 0 || *packetName == "" || *cacheName == "" || *outputName == "" || *receiptName == "" || (*format != "protocol" && *format != "raw") {
		return fmt.Errorf("explicit receive arguments required")
	}
	paths := make([]string, 4)
	for i, v := range []string{*packetName, *cacheName, *outputName, *receiptName} {
		p, e := projectRuntimePath(v)
		if e != nil {
			return e
		}
		paths[i] = p
	}
	if paths[0] == paths[2] || paths[0] == paths[3] || paths[2] == paths[3] {
		return fmt.Errorf("separate original input/output paths required")
	}
	f, e := os.Open(paths[0])
	if e != nil {
		return e
	}
	raw, e := io.ReadAll(io.LimitReader(f, 8193))
	_ = f.Close()
	defer clear(raw)
	var packet resultwire.ReadPacket
	if e != nil || len(raw) > 8192 || strictjson.Decode(raw, &packet, 8192) != nil {
		return fmt.Errorf("bounded original read packet required")
	}
	config := results.DefaultConfig(paths[1])
	config.MaxResults = 1
	config.ResultBytes = 33 << 20
	config.TotalBytes = 33 << 20
	config.Retention = 5 * time.Minute
	store, e := results.Open(config)
	if e != nil {
		return e
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = store.Close(ctx)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	received, e := buyerresults.Receive(ctx, packet, store)
	packet.Secret = ""
	if e != nil {
		return e
	}
	defer received.Close()
	out, e := received.Open(ctx)
	if e != nil {
		return e
	}
	defer out.Close(context.Background())
	file, e := createOwned(paths[2])
	if e != nil {
		return e
	}
	identity, e := file.Stat()
	if e != nil {
		_ = file.Close()
		return e
	}
	done := false
	defer func() {
		_ = file.Close()
		if !done {
			info, e := os.Lstat(paths[2])
			if e == nil && os.SameFile(info, identity) {
				_ = os.Remove(paths[2])
			}
		}
	}()
	if *format == "protocol" {
		_, e = textoutput.Render(ctx, boundedFileSink{file}, out, textoutput.DefaultLimits())
	} else {
		for {
			b, re := out.Read(ctx)
			if len(b) > 0 {
				e = boundedFileSink{file}.Write(ctx, b)
			}
			clear(b)
			if e != nil {
				break
			}
			if re != nil {
				if re != io.EOF {
					e = re
				}
				break
			}
		}
		if e == nil {
			_, e = out.Result(ctx)
		}
	}
	if e != nil {
		return e
	}
	if e = file.Sync(); e != nil {
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	receipt, e := createOwned(paths[3])
	if e != nil {
		return e
	}
	receiptIdentity, e := receipt.Stat()
	if e != nil {
		_ = receipt.Close()
		return e
	}
	receiptDone := false
	defer func() {
		_ = receipt.Close()
		if !receiptDone {
			info, e := os.Lstat(paths[3])
			if e == nil && os.SameFile(info, receiptIdentity) {
				_ = os.Remove(paths[3])
			}
		}
	}()
	body, _ := json.Marshal(received.Review())
	body = append(body, '\n')
	n, e := receipt.Write(body)
	if e == nil && n != len(body) {
		e = io.ErrShortWrite
	}
	if e == nil {
		e = receipt.Sync()
	}
	closeError := receipt.Close()
	if e != nil {
		return e
	}
	if closeError != nil {
		return closeError
	}
	receiptDone = true
	done = true
	return nil
}
