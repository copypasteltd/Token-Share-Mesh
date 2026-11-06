package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"p11.local/compute/client/crypto-gateway/ciphercustody"
	"p11.local/compute/client/node"
)

// Metadata and deadline cleanup only. There is no arbitrary file/content copy,
// plaintext export or caller-provided acceptance/ACK command.
func manageCipherCustody(ctx context.Context, action string, args []string, input io.Reader, output, diagnostic io.Writer) int {
	if output == nil || diagnostic == nil {
		return 2
	}
	reject := func() int {
		fmt.Fprintln(diagnostic, "original cipher custody operation rejected; original state retained")
		return 1
	}
	if ctx == nil || input == nil {
		return reject()
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	path := flags.String("state", "", "original private absolute state")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *path == "" {
		return reject()
	}
	var policy ciphercustody.Policy
	switch action {
	case "cipher-init":
		if decodeCrypto(input, &policy) != nil || policy.Validate() != nil {
			return reject()
		}
	case "cipher-status", "cipher-expire":
		var empty struct{}
		if decodeCrypto(input, &empty) != nil {
			return reject()
		}
	default:
		return reject()
	}
	s, e := node.Open(ctx, *path)
	if e != nil {
		return reject()
	}
	defer s.Close()
	var store *ciphercustody.Store
	if action == "cipher-init" {
		store, e = s.InitCipherCustody(policy)
	} else {
		store, e = s.OpenCipherCustody()
	}
	if e != nil {
		return reject()
	}
	if action == "cipher-expire" {
		if _, e = store.Expire(ctx, time.Now().UTC()); e != nil {
			return reject()
		}
	}
	snapshot, e := store.Snapshot()
	if e != nil || json.NewEncoder(output).Encode(snapshot) != nil {
		return reject()
	}
	return 0
}
