// Package gpuid gives local physical NVIDIA adapters a stable resource identity.
// Local driver observations are not remote attestation or device qualification.
package gpuid

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

var ErrIdentity = errors.New("physical GPU identity rejected")
var uuidPattern = regexp.MustCompile(`^GPU-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var pciPattern = regexp.MustCompile(`^0000:[0-9a-f]{2}:[01][0-9a-f]\.[0-7]$`)
var devicePattern = regexp.MustCompile(`^CUDA([0-9]|[1-5][0-9]|6[0-3])$`)

type Identity struct {
	Device     string `json:"backend_device"`
	UUID       string `json:"gpu_uuid"`
	PCI        string `json:"gpu_pci"`
	Name       string `json:"name"`
	TotalBytes uint64 `json:"total_bytes"`
}

// Normalize accepts the driver/SMI forms, never MIG or partition aliases.
// This Windows profile supports PCI domain zero only; unknown domains fail closed.
func Normalize(uuid, pci string) (string, string, error) {
	if strings.HasPrefix(uuid, "GPU-") {
		uuid = "GPU-" + strings.ToLower(uuid[4:])
	}
	pci = strings.ToLower(pci)
	if strings.HasPrefix(pci, "00000000:") {
		pci = pci[4:]
	}
	if !uuidPattern.MatchString(uuid) || uuid == "GPU-00000000-0000-0000-0000-000000000000" || !pciPattern.MatchString(pci) {
		return "", "", ErrIdentity
	}
	return uuid, pci, nil
}

// Digest excludes the mutable PCI placement and logical CUDA ordinal, so aliases
// of one board still contend for the same physical gate. Selection pins PCI too.
func Digest(uuid string) (string, error) {
	u, _, err := Normalize(uuid, "0000:00:00.0")
	if err != nil || u != uuid {
		return "", ErrIdentity
	}
	h := sha256.Sum256([]byte("P11_PHYSICAL_GPU_NVIDIA_1\x00" + uuid))
	return hex.EncodeToString(h[:]), nil
}

func (i Identity) Validate() error {
	u, p, err := Normalize(i.UUID, i.PCI)
	if err != nil || u != i.UUID || p != i.PCI || !devicePattern.MatchString(i.Device) || len(i.Name) < 1 || len(i.Name) > 192 || i.TotalBytes < 1 || i.TotalBytes > 1<<40 {
		return ErrIdentity
	}
	for _, c := range i.Name {
		if c < 32 || c > 126 {
			return ErrIdentity
		}
	}
	return nil
}

func ValidateInventory(items []Identity) error {
	if len(items) > 64 {
		return ErrIdentity
	}
	uuid, pci, device := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, i := range items {
		if i.Validate() != nil || uuid[i.UUID] || pci[i.PCI] || device[i.Device] {
			return ErrIdentity
		}
		uuid[i.UUID], pci[i.PCI], device[i.Device] = true, true, true
	}
	return nil
}
