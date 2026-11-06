package rentalaccess

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	"p11.local/compute/client/protocol/executionlease"
	"p11.local/compute/shared/contractspec"
	"p11.local/compute/shared/strictjson"
)

const ManifestProfile = "P11_RENTAL_EXECUTION_MANIFEST_1"
const ApprovalProfile = "P11_NATIVE_RENTAL_APPROVAL_1"
const MaximumManifestBytes = 16384

type Manifest struct {
	Profile         string                 `json:"profile_id"`
	Reference       string                 `json:"manifest_ref"`
	Session         string                 `json:"session_id"`
	Order           string                 `json:"order_id"`
	Buyer           string                 `json:"buyer_id"`
	Seller          string                 `json:"seller_id"`
	Device          string                 `json:"device_id"`
	Resource        string                 `json:"resource_id"`
	Generation      string                 `json:"resource_lock_generation"`
	Capability      string                 `json:"capability_id"`
	Environment     string                 `json:"environment_revision"`
	EnvironmentHash string                 `json:"kernel_environment_digest"`
	ContractHash    string                 `json:"contract_hash"`
	Mode            string                 `json:"service_mode"`
	Privacy         string                 `json:"privacy_profile"`
	Engine          string                 `json:"engine_kind"`
	Release         string                 `json:"engine_release"`
	ModelHash       string                 `json:"model_digest"`
	RecipeHash      string                 `json:"recipe_digest"`
	Protocol        string                 `json:"protocol_profile_id"`
	PublicSpec      contractspec.Selection `json:"public_spec"`
}

// LocalApproval is explicit owner-approved configuration, not a network DTO.
// The original local UI/configuration must bind this exact authenticated C2C
// preparation to independently installed environment/model/physical GPU facts.
// Copying a task response into LocalApproval is not independent approval.
type LocalApproval struct {
	Profile      string    `json:"profile_id"`
	Manifest     Manifest  `json:"original_manifest"`
	ManifestHash string    `json:"manifest_hash"`
	PhysicalHash string    `json:"physical_digest"`
	Connection   string    `json:"connection_ref"`
	Issuer       string    `json:"issuer_id"`
	PrepareUntil time.Time `json:"prepare_until"`
}

type Prepared struct {
	approval LocalApproval
	raw      []byte
	digest   string
	valid    bool
}

func (m Manifest) shape() bool {
	for _, v := range []string{m.Reference, m.Session, m.Order, m.Buyer, m.Seller, m.Device, m.Resource, m.Capability, m.Environment, m.Protocol} {
		if !identifier(v) {
			return false
		}
	}
	for _, v := range []string{m.EnvironmentHash, m.ContractHash, m.ModelHash, m.RecipeHash} {
		if !executionlease.Hash(v) {
			return false
		}
	}
	if len(m.Release) < 1 || len(m.Release) > 128 {
		return false
	}
	for _, r := range m.Release {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' || r == '+') {
			return false
		}
	}
	g, err := strconv.ParseInt(m.Generation, 10, 64)
	if err != nil || g < 1 || strconv.FormatInt(g, 10) != m.Generation || m.Profile != ManifestProfile || m.Mode != "GPU_RENTAL" || m.Privacy != "STANDARD" || m.Buyer == m.Seller {
		return false
	}
	if m.Engine != "LLAMA_CPP_NATIVE" && m.Engine != "LM_STUDIO" && m.Engine != "COMFYUI" {
		return false
	}
	s := m.PublicSpec
	if s.DurationMS < 1 || s.DurationMS > 86400000 || s.InputTokenLimit < 0 || s.OutputTokenLimit < 0 || s.InputTokenLimit > 10000000 || s.OutputTokenLimit > 10000000 {
		return false
	}
	model := identifier(s.ModelID) && s.WorkflowID == "" && s.WorkflowRevision == 0
	workflow := s.ModelID == "" && identifier(s.WorkflowID) && s.WorkflowRevision > 0
	return model || workflow
}

// VerifyPreparation validates public metadata only. It creates a reservation
// capability, never INVOKE authority or proof of hardware readiness/payment.
func VerifyPreparation(raw []byte, approved LocalApproval, now time.Time) (Prepared, error) {
	p, err := DecodePreparationRecord(raw, approved)
	if err != nil || !now.Before(p.approval.PrepareUntil) || p.approval.PrepareUntil.Sub(now) > 2*time.Minute {
		return Prepared{}, ErrAccess
	}
	return p, nil
}

// DecodePreparationRecord reads already approved, encrypted-journal metadata.
// It deliberately restores no live execution/control permission. ValidAt still
// enforces the preparation TTL and reopened physical owners remain quarantined.
func DecodePreparationRecord(raw []byte, approved LocalApproval) (Prepared, error) {
	var m Manifest
	approved.PrepareUntil = approved.PrepareUntil.UTC()
	if strictjson.Decode(raw, &m, MaximumManifestBytes) != nil || !m.shape() || approved.Profile != ApprovalProfile || approved.Manifest != m || !executionlease.Hash(approved.ManifestHash) || !executionlease.Hash(approved.PhysicalHash) || !identifier(approved.Connection) || !identifier(approved.Issuer) || approved.PrepareUntil.Year() < 2000 || approved.PrepareUntil.Year() > 2100 {
		return Prepared{}, ErrAccess
	}
	sum := sha256.Sum256(raw)
	hash := hex.EncodeToString(sum[:])
	if hash != approved.ManifestHash {
		return Prepared{}, ErrAccess
	}
	return Prepared{approved, append([]byte(nil), raw...), hash, true}, nil
}

func (p Prepared) ValidAt(now time.Time) bool {
	return p.valid && now.Before(p.approval.PrepareUntil) && p.approval.PrepareUntil.Sub(now) <= 2*time.Minute && p.approval.Manifest.shape()
}
func (p Prepared) Details() Manifest {
	if !p.valid {
		return Manifest{}
	}
	return p.approval.Manifest
}
func (p Prepared) Approval() LocalApproval {
	if !p.valid {
		return LocalApproval{}
	}
	return p.approval
}
func (p Prepared) Raw() []byte {
	if !p.valid {
		return nil
	}
	return append([]byte(nil), p.raw...)
}
func (p Prepared) Digest() string {
	if !p.valid {
		return ""
	}
	return p.digest
}
func (p Prepared) ApprovalDigest() string {
	if !p.valid {
		return ""
	}
	raw, err := json.Marshal(p.approval)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// BindsGrant uses original preparation metadata; a current grant/term record
// must separately supply digest/terms/hard-deadline before Verifier.Verify.
func (p Prepared) BindsGrant(e Expected) bool {
	if !p.valid {
		return false
	}
	m := p.approval.Manifest
	return e.shape() == nil && e.Issuer == p.approval.Issuer && e.Subject == m.Buyer && e.Audience == m.Device && e.Order == m.Order && e.Session == m.Session && e.Seller == m.Seller && e.Device == m.Device && e.Resource == m.Resource && e.Generation == m.Generation && e.Environment == m.Environment && e.EnvironmentHash == m.EnvironmentHash && e.Manifest == m.Reference && e.ManifestHash == p.digest
}
