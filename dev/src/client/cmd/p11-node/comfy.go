package main

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"p11.local/compute/client/comfyenv"
	"p11.local/compute/client/node"
	"p11.local/compute/shared/strictjson"
)

type comfyTrust struct {
	Issuer  string            `json:"issuer_id"`
	Pins    map[string]string `json:"ed25519_pins"`
	Revoked []string          `json:"revoked_catalog_digests"`
}
type comfyRequest struct {
	Catalog  json.RawMessage   `json:"signed_catalog"`
	Archives map[string]string `json:"local_archives"`
}

// Pins and consent are explicitly selected LOCAL files, separate from the
// signed network/catalog request. This CLI performs no downloads, execution,
// activation, model acquisition, qualification, heartbeat or content listener.
func manageComfy(ctx context.Context, action string, args []string, input io.Reader, output, diagnostic io.Writer) int {
	reject := func() int {
		fmt.Fprintln(diagnostic, "local ComfyUI environment operation rejected; original state retained")
		return 1
	}
	switch action {
	case "comfy-init", "comfy-status", "comfy-install", "comfy-verify", "comfy-delete":
	default:
		return reject()
	}
	flags := flag.NewFlagSet(action, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	state := flags.String("state", "", "original private absolute node state")
	trustPath := flags.String("trust", "", "independent local issuer/pins/revocation file")
	consentPath := flags.String("consent", "", "independent exact local owner consent file")
	digest := flags.String("digest", "", "exact installed environment digest")
	if ctx == nil || flags.Parse(args) != nil || flags.NArg() != 0 || *state == "" {
		return reject()
	}
	needsPlan := action == "comfy-install" || action == "comfy-verify"
	if needsPlan != (*trustPath != "" && *consentPath != "") || !needsPlan && (*trustPath != "" || *consentPath != "") || ((action == "comfy-delete" || action == "comfy-verify") != (*digest != "")) {
		return reject()
	}
	s, err := node.Open(ctx, *state)
	if err != nil {
		return reject()
	}
	defer s.Close()
	var registry *comfyenv.Registry
	if action == "comfy-init" {
		registry, err = s.InitKernelRegistry()
	} else {
		registry, err = s.OpenKernelRegistry()
	}
	if err != nil {
		return reject()
	}
	var value any
	switch action {
	case "comfy-install", "comfy-verify":
		var trust comfyTrust
		var consent comfyenv.Consent
		if readComfyOwnerFile(*trustPath, &trust) != nil || readComfyOwnerFile(*consentPath, &consent) != nil {
			return reject()
		}
		pins := map[string]ed25519.PublicKey{}
		for id, encoded := range trust.Pins {
			key, e := base64.RawURLEncoding.Strict().DecodeString(encoded)
			if e != nil || len(key) != ed25519.PublicKeySize || base64.RawURLEncoding.EncodeToString(key) != encoded {
				return reject()
			}
			pins[id] = ed25519.PublicKey(key)
		}
		v, e := comfyenv.NewVerifier(trust.Issuer, pins, trust.Revoked)
		if e != nil {
			return reject()
		}
		defer v.Close()
		var plan comfyenv.Plan
		if action == "comfy-install" {
			raw, e := io.ReadAll(io.LimitReader(input, comfyenv.MaximumCatalogBytes+1))
			defer clear(raw)
			var request comfyRequest
			if e != nil || strictjson.Decode(raw, &request, comfyenv.MaximumCatalogBytes) != nil {
				return reject()
			}
			plan, e = v.Verify(request.Catalog, consent, time.Now().UTC())
			if e != nil {
				return reject()
			}
			value, e = registry.Install(ctx, plan, request.Archives)
			if e != nil {
				return reject()
			}
		} else {
			plan, e = registry.ReadPlan(ctx, *digest, v, consent)
			if e != nil || registry.VerifyInstalled(ctx, plan) != nil {
				return reject()
			}
			value = struct {
				Digest        string `json:"environment_digest"`
				Phase         string `json:"phase"`
				Qualification string `json:"qualification"`
			}{plan.Digest(), "VERIFIED", "NOT_GRANTED"}
		}
	case "comfy-delete":
		if registry.Delete(ctx, *digest) != nil {
			return reject()
		}
		value = struct {
			Phase string `json:"phase"`
		}{"DELETED_OR_ALREADY_DELETED"}
	default:
		candidates, selections, e := registry.Snapshot()
		if e != nil {
			return reject()
		}
		value = struct {
			Candidates    []comfyenv.Candidate `json:"candidates"`
			Selections    []comfyenv.Selection `json:"local_selections"`
			Execution     bool                 `json:"execution_enabled"`
			Qualification string               `json:"qualification"`
		}{candidates, selections, false, "NOT_GRANTED"}
	}
	view := struct {
		Profile       string `json:"profile_id"`
		Action        string `json:"operation"`
		Result        any    `json:"result"`
		Execution     bool   `json:"execution_enabled"`
		Qualification string `json:"qualification"`
	}{"P11_LOCAL_COMFY_OPERATION_1", action, value, false, "NOT_GRANTED"}
	if json.NewEncoder(output).Encode(view) != nil {
		return reject()
	}
	return 0
}

func readComfyOwnerFile(name string, target any) error {
	if !filepath.IsAbs(name) || filepath.Clean(name) != name {
		return comfyenv.ErrCatalog
	}
	// Reject every symlink/reparse component, not merely the final filename.
	for part := name; ; part = filepath.Dir(part) {
		info, e := os.Lstat(part)
		if e != nil || info.Mode()&(os.ModeSymlink|os.ModeIrregular) != 0 {
			return comfyenv.ErrCatalog
		}
		if parent := filepath.Dir(part); parent == part {
			break
		}
	}
	f, e := os.Open(name)
	if e != nil {
		return comfyenv.ErrCatalog
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 65536 {
		return comfyenv.ErrCatalog
	}
	raw, e := io.ReadAll(io.LimitReader(f, 65537))
	defer clear(raw)
	if e != nil || strictjson.Decode(raw, target, 65536) != nil {
		return comfyenv.ErrCatalog
	}
	return nil
}
