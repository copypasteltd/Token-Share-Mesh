package main

// SOURCE_ONLY CLI boundary definitions; no engine or node is opened here.
import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"p11.local/compute/client/adapters/llamaworker"
	"p11.local/compute/client/node"
)

func TestNativeProviderCLIUsesStrictLocalStdinConfiguration(t *testing.T) {
	root := t.TempDir()
	selection := llamaworker.OwnerSelection{Capability: "cap_test", Resource: "resource_test", GPUDevice: "CUDA0", GPUUUID: "GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21", GPUPCI: "0000:01:00.0", GPULayers: 999, ContextTokens: 2048, BatchTokens: 32, Threads: 1}
	environment, err := llamaworker.PackageEnvironment(strings.Repeat("a", 64), selection)
	if err != nil {
		t.Fatal(err)
	}
	config := node.NativeProviderConfig{Profile: node.NativeProviderProfile, Installation: node.NativeInstallation{RuntimeRoot: root, Package: llamaworker.FilePin{Path: filepath.Join(root, "package.json"), SHA256: strings.Repeat("a", 64)}, Selection: selection, Environment: environment}}
	raw, _ := json.Marshal(config)
	decoded, err := decodeNativeProvider(raw)
	if err != nil || decoded.Profile != node.NativeProviderProfile || decoded.ProgressMilliseconds != 5000 {
		t.Fatal("native foreground entry lost explicit config", err)
	}
	var object map[string]json.RawMessage
	_ = json.Unmarshal(raw, &object)
	for _, field := range []string{"prompt", "api_key", "executable", "seller_id", "privacy_downgrade", "device_qualified"} {
		object[field] = json.RawMessage(`true`)
		bad, _ := json.Marshal(object)
		if _, err := decodeNativeProvider(bad); err == nil {
			t.Fatal("unapproved configuration field accepted", field)
		}
		delete(object, field)
	}
}

func TestNativeProviderCLIRejectsEmptyOversizeAndTrailingConfig(t *testing.T) {
	for _, raw := range []string{"", "null", "[]", "{} {}", strings.Repeat(" ", 16385), `{"profile_id":"P11_NATIVE_PROVIDER_1","profile_id":"other"}`} {
		if _, err := decodeNativeProvider([]byte(raw)); err == nil {
			t.Fatal("ambiguous or unbounded config accepted")
		}
	}
}
