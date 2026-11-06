package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"p11.local/compute/client/crypto-gateway/endpoint"
	"p11.local/compute/client/node"
	"p11.local/compute/shared/strictjson"
	"strings"
	"time"
)

type cryptoCreate struct {
	Owner    string `json:"owner_id"`
	Endpoint string `json:"endpoint_id"`
	Role     string `json:"role"`
	Expires  string `json:"expires_at"`
}
type cryptoPin struct {
	Bundle       endpoint.Bundle       `json:"bundle"`
	Verification endpoint.Verification `json:"verification"`
}
type cryptoRevoke struct {
	Bundle string `json:"bundle_hash"`
}
type cryptoRotate struct {
	Bundle  string `json:"bundle_hash"`
	Expires string `json:"expires_at"`
}

func decodeCrypto(input io.Reader, target any) error {
	raw, e := io.ReadAll(io.LimitReader(input, 16385))
	defer clear(raw)
	if e != nil || strictjson.Decode(raw, target, 16384) != nil {
		return endpoint.ErrState
	}
	return nil
}

// Metadata-only key lifecycle. It exposes no private key, seller prompt,
// plaintext decryption, generic signing oracle or content HTTP listener.
func manageCrypto(ctx context.Context, action string, args []string, input io.Reader, output, diagnostic io.Writer) int {
	reject := func() int {
		fmt.Fprintln(diagnostic, "endpoint key operation rejected; original identity retained")
		return 1
	}
	switch action {
	case "crypto-init", "crypto-create", "crypto-public", "crypto-pin", "crypto-rotate", "crypto-pin-rotate", "crypto-revoke", "crypto-peer-revoke":
	default:
		return reject()
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	state := flags.String("state", "", "original private absolute node state")
	if ctx == nil || flags.Parse(args) != nil || flags.NArg() != 0 || *state == "" || input == nil {
		return reject()
	}
	if action == "crypto-init" {
		var empty struct{}
		if decodeCrypto(input, &empty) != nil {
			return reject()
		}
		input = strings.NewReader("{}")
	}
	s, e := node.Open(ctx, *state)
	if e != nil {
		return reject()
	}
	defer s.Close()
	var keys *endpoint.Vault
	if action == "crypto-init" {
		keys, e = s.InitEndpointKeys()
	} else {
		keys, e = s.OpenEndpointKeys()
	}
	if e != nil {
		return reject()
	}
	now := time.Now().UTC()
	var value any
	switch action {
	case "crypto-init", "crypto-public":
		var empty struct{}
		if decodeCrypto(input, &empty) != nil {
			return reject()
		}
		value, e = keys.Public()
	case "crypto-create":
		var in cryptoCreate
		if decodeCrypto(input, &in) != nil {
			return reject()
		}
		expires, te := time.Parse(time.RFC3339Nano, in.Expires)
		if te != nil || expires.UTC().Format(time.RFC3339Nano) != in.Expires {
			return reject()
		}
		value, e = s.CreateLocalEndpoint(in.Owner, in.Endpoint, in.Role, now, expires)
	case "crypto-pin":
		var in cryptoPin
		if decodeCrypto(input, &in) != nil {
			return reject()
		}
		e = keys.PinInitial(in.Bundle, in.Verification, now)
	case "crypto-rotate":
		var in cryptoRotate
		if decodeCrypto(input, &in) != nil {
			return reject()
		}
		expires, te := time.Parse(time.RFC3339Nano, in.Expires)
		if te != nil || expires.UTC().Format(time.RFC3339Nano) != in.Expires {
			return reject()
		}
		value, e = keys.RotateLocal(in.Bundle, now, expires)
	case "crypto-pin-rotate":
		var in endpoint.Transition
		if decodeCrypto(input, &in) != nil {
			return reject()
		}
		e = keys.RotatePeer(in, now)
	case "crypto-revoke", "crypto-peer-revoke":
		var in cryptoRevoke
		if decodeCrypto(input, &in) != nil {
			return reject()
		}
		if action == "crypto-revoke" {
			e = keys.RevokeLocal(in.Bundle)
		} else {
			e = keys.RevokePeer(in.Bundle)
		}
	}
	if e != nil {
		return reject()
	}
	if value == nil {
		value = struct {
			Status string `json:"status"`
		}{"RECORDED"}
	}
	if json.NewEncoder(output).Encode(value) != nil {
		return reject()
	}
	return 0
}
