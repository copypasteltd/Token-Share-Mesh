package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"time"

	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/shared/strictjson"
)

func inspectKernel(args []string, input io.Reader, output, diagnostic io.Writer) int {
	reject := func() int { fmt.Fprintln(diagnostic, "local kernel inspection rejected"); return 2 }
	flags := flag.NewFlagSet("kernel-inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("runtime-root", "", "explicit private absolute runtime root")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *root == "" {
		return reject()
	}
	raw, err := io.ReadAll(io.LimitReader(input, 4097))
	if err != nil || len(raw) > 4096 {
		clear(raw)
		return reject()
	}
	defer clear(raw)
	var request struct {
		Package       string                     `json:"package"`
		PackageSHA256 string                     `json:"package_sha256"`
		Environment   string                     `json:"environment"`
		Selection     llamaworker.OwnerSelection `json:"selection"`
	}
	if strictjson.Decode(raw, &request, 4096) != nil {
		return reject()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := llamaworker.LoadPackage(ctx, *root, llamaworker.FilePin{Path: request.Package, SHA256: request.PackageSHA256}, request.Selection, request.Environment)
	if err != nil {
		return reject()
	}
	// Inspection loads no library/weights, starts no worker and enrolls no device.
	view := struct {
		Profile          string                     `json:"profile"`
		EngineRelease    string                     `json:"engine_release"`
		PackageSHA256    string                     `json:"package_sha256"`
		Environment      string                     `json:"environment"`
		ModelSHA256      string                     `json:"model_sha256"`
		TemplateSHA256   string                     `json:"template_sha256"`
		Selection        llamaworker.OwnerSelection `json:"selection"`
		ExecutionEnabled bool                       `json:"execution_enabled"`
		Qualification    string                     `json:"qualification"`
	}{llamaworker.Profile, llamaworker.EngineRelease, request.PackageSHA256, c.Environment, c.Model.SHA256, c.Template.SHA256, request.Selection, false, "NOT_GRANTED"}
	if json.NewEncoder(output).Encode(view) != nil {
		return 1
	}
	return 0
}

// discoverGPU returns local metadata only and starts no content listener.
func discoverGPU(args []string, input io.Reader, output, diagnostic io.Writer) int {
	reject := func() int { fmt.Fprintln(diagnostic, "local GPU discovery rejected"); return 2 }
	flags := flag.NewFlagSet("gpu-discover", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	root := flags.String("runtime-root", "", "explicit private absolute runtime root")
	if flags.Parse(args) != nil || flags.NArg() != 0 || *root == "" {
		return reject()
	}
	raw, err := io.ReadAll(io.LimitReader(input, 4097))
	defer clear(raw)
	if err != nil || len(raw) > 4096 {
		return reject()
	}
	var request struct {
		Package       string `json:"package"`
		PackageSHA256 string `json:"package_sha256"`
	}
	if strictjson.Decode(raw, &request, 4096) != nil {
		return reject()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	items, err := llamaworker.DiscoverPackage(ctx, *root, llamaworker.FilePin{Path: request.Package, SHA256: request.PackageSHA256})
	if err != nil {
		return reject()
	}
	view := struct {
		Profile          string `json:"profile"`
		GPUs             any    `json:"gpus"`
		ExecutionEnabled bool   `json:"execution_enabled"`
		Qualification    string `json:"qualification"`
	}{llamaworker.Profile, items, false, "NOT_GRANTED"}
	if json.NewEncoder(output).Encode(view) != nil {
		return 1
	}
	return 0
}
