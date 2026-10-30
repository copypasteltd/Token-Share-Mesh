package gpuid

import (
	"strings"
	"testing"
)

const id = "GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21"

func TestPhysicalGPUCanonicalDriverForms(t *testing.T) {
	u, p, e := Normalize("GPU-C101FC79-CED0-9579-92AB-A20C8DCD8A21", "00000000:01:00.0")
	if e != nil || u != id || p != "0000:01:00.0" {
		t.Fatal("driver normalization")
	}
	for _, v := range [][2]string{{"MIG-" + id, "0000:01:00.0"}, {"GPU-00000000-0000-0000-0000-000000000000", "0000:01:00.0"}, {id, "0001:01:00.0"}, {id, "0000:01:20.0"}, {id, "0000:01:00.8"}, {id, "0000:01:00.0\x00"}} {
		if _, _, e := Normalize(v[0], v[1]); e == nil {
			t.Fatal("ambiguous identity accepted")
		}
	}
}
func TestPhysicalGPUDigestSurvivesOrdinalAndSlotChange(t *testing.T) {
	a := Identity{"CUDA0", id, "0000:01:00.0", "NVIDIA GeForce RTX 3060", 12 << 30}
	b := a
	b.Device = "CUDA1"
	b.PCI = "0000:02:00.0"
	if a.Validate() != nil || b.Validate() != nil {
		t.Fatal("identity")
	}
	x, _ := Digest(a.UUID)
	y, _ := Digest(b.UUID)
	z, _ := Digest("GPU-d101fc79-ced0-9579-92ab-a20c8dcd8a21")
	if x != y || x == z || len(x) != 64 {
		t.Fatal("physical alias split")
	}
}
func TestPhysicalGPUInventoryRejectsAliasesAndUnboundedMetadata(t *testing.T) {
	a := Identity{"CUDA0", id, "0000:01:00.0", "NVIDIA GeForce RTX 3060", 12 << 30}
	if ValidateInventory([]Identity{a}) != nil || ValidateInventory(nil) != nil {
		t.Fatal("valid inventory")
	}
	for _, change := range []func(*Identity){func(b *Identity) { b.Device = "CUDA1" }, func(b *Identity) { b.UUID = "GPU-d101fc79-ced0-9579-92ab-a20c8dcd8a21" }, func(b *Identity) { b.PCI = "0000:02:00.0" }} {
		b := a
		change(&b)
		if ValidateInventory([]Identity{a, b}) == nil {
			t.Fatal("alias accepted")
		}
	}
	for _, change := range []func(*Identity){func(b *Identity) { b.Name = strings.Repeat("a", 193) }, func(b *Identity) { b.Name = "prompt\n" }, func(b *Identity) { b.TotalBytes = 0 }, func(b *Identity) { b.TotalBytes = 1 << 41 }, func(b *Identity) { b.Device = "CUDA64" }} {
		b := a
		change(&b)
		if b.Validate() == nil {
			t.Fatal("invalid metadata")
		}
	}
	if ValidateInventory(make([]Identity, 65)) == nil {
		t.Fatal("unbounded inventory")
	}
}
