package llamaworker

import (
	"context"
	"io"
	"p11.local/compute/shared/gpuid"
	"sync"
	"testing"
	"time"
)

func inventoryPacket(profile string, items []gpuid.Identity) []byte {
	var e encoder
	e.str(profile)
	e.str(UpstreamCommit)
	e.u32(uint32(len(items)))
	for _, i := range items {
		encodeIdentity(&e, i)
	}
	return e.Bytes()
}
func TestNativeGPUInventoryUsesBoundedUnambiguousFrames(t *testing.T) {
	i := gpuid.Identity{Device: "CUDA0", UUID: "GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21", PCI: "0000:01:00.0", Name: "NVIDIA GeForce RTX 3060", TotalBytes: 12 << 30}
	raw := inventoryPacket(Profile, []gpuid.Identity{i})
	got, e := decodeInventory(raw)
	if e != nil || len(got) != 1 || got[0] != i {
		t.Fatal("identity lost")
	}
	for _, bad := range [][]byte{raw[:len(raw)-1], append(append([]byte{}, raw...), 0), inventoryPacket("P11_LLAMA_PIPE_3", []gpuid.Identity{i}), inventoryPacket(Profile, []gpuid.Identity{i, i}), inventoryPacket(Profile, make([]gpuid.Identity, 65))} {
		if _, e := decodeInventory(bad); e == nil {
			t.Fatal("ambiguous frame accepted")
		}
	}
}
func TestNativeGPUDiscoveryRejectsUnapprovedAndCancelledPackages(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := DiscoverPackage(ctx, t.TempDir(), FilePin{}); e == nil {
		t.Fatal("unapproved probe started")
	}
}
func TestNativeReadyRejectsForgedPhysicalSelection(t *testing.T) {
	for _, mode := range []string{"uuid", "pci", "device", "cpu_identity", "old_ready"} {
		t.Run(mode, func(t *testing.T) {
			c := Config{GPULayers: 1, GPUDevice: "CUDA0", GPUUUID: "GPU-c101fc79-ced0-9579-92ab-a20c8dcd8a21", GPUPCI: "0000:01:00.0"}
			observed := gpuid.Identity{Device: c.GPUDevice, UUID: c.GPUUUID, PCI: c.GPUPCI, Name: "NVIDIA GeForce RTX 3060", TotalBytes: 12 << 30}
			switch mode {
			case "uuid":
				observed.UUID = "GPU-d101fc79-ced0-9579-92ab-a20c8dcd8a21"
			case "pci":
				observed.PCI = "0000:02:00.0"
			case "device":
				observed.Device = "CUDA1"
			case "cpu_identity":
				c.GPULayers = 0
			}
			ir, iw := io.Pipe()
			or, ow := io.Pipe()
			cr, cw := io.Pipe()
			wait := make(chan error, 1)
			var once sync.Once
			quit := func() {
				once.Do(func() {
					ir.Close()
					iw.Close()
					or.Close()
					ow.Close()
					cr.Close()
					cw.Close()
					wait <- nil
				})
			}
			defer quit()
			a := newAdapter(c, child{input: iw, control: cw, output: or, kill: quit, wait: wait})
			go func() {
				var e encoder
				e.str(Profile)
				e.str(UpstreamCommit)
				if mode != "old_ready" {
					encodeIdentity(&e, observed)
				}
				_ = writeFrame(ow, kReady, e.Bytes())
			}()
			select {
			case err := <-a.ready:
				if err != ErrUnknown {
					t.Fatal("forged READY granted work")
				}
			case <-time.After(time.Second):
				t.Fatal("ready blocked")
			}
			select {
			case <-a.exited:
			case <-time.After(time.Second):
				t.Fatal("owned fixture not reaped")
			}
		})
	}
}
