package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"p11.local/compute/client/node"
	"p11.local/compute/shared/ids"
)

// Explicit public-metadata operations. No prompt, private key or content socket
// is exposed; uncertain writes retain the original node AEAD pending command.
func manageEndpointDirectory(ctx context.Context, action string, args []string, input io.Reader, output, diagnostic io.Writer) int {
	reject := func() int {
		if diagnostic != nil {
			fmt.Fprintln(diagnostic, "public endpoint operation unresolved; retain original keys and recover the original pending command")
		}
		return 1
	}
	switch action {
	case "directory-publish", "directory-revoke", "directory-get", "directory-list", "directory-recover":
	default:
		return reject()
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	state := flags.String("state", "", "original private absolute node state")
	if ctx == nil || input == nil || output == nil || diagnostic == nil || flags.Parse(args) != nil || flags.NArg() != 0 || *state == "" {
		return reject()
	}
	s, e := node.Open(ctx, *state)
	if e != nil {
		return reject()
	}
	defer s.Close()
	var value any
	switch action {
	case "directory-publish", "directory-revoke":
		var in struct {
			Hash string `json:"bundle_hash"`
		}
		if decodeCrypto(input, &in) != nil || len(in.Hash) != 64 {
			return reject()
		}
		if action == "directory-publish" {
			value, e = s.PublishEndpointDirectory(ctx, in.Hash)
		} else {
			value, e = s.RevokeEndpointDirectory(ctx, in.Hash)
		}
	case "directory-get":
		var in struct {
			Recipient string `json:"recipient_id"`
		}
		if decodeCrypto(input, &in) != nil || !ids.Valid(in.Recipient) {
			return reject()
		}
		value, e = s.GetEndpointDirectory(ctx, in.Recipient)
	case "directory-list":
		var in struct {
			Cursor string `json:"cursor,omitempty"`
		}
		if decodeCrypto(input, &in) != nil || len(in.Cursor) > 512 {
			return reject()
		}
		value, e = s.ListEndpointDirectory(ctx, in.Cursor)
	case "directory-recover":
		var empty struct{}
		if decodeCrypto(input, &empty) != nil {
			return reject()
		}
		value, e = s.RecoverEndpointDirectory(ctx)
	}
	if e != nil {
		return reject()
	}
	if json.NewEncoder(output).Encode(value) != nil {
		return reject()
	}
	return 0
}
