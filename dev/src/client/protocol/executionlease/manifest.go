package executionlease

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	relay "p11.local/compute/gateway/core"
	"time"
)

type Spec struct {
	Protocol        relay.Profile `json:"protocol_profile_id"`
	Model           string        `json:"public_model"`
	MaxOutputTokens int           `json:"max_output_tokens"`
	Stream          bool          `json:"stream"`
}

const ManifestProfile = "P11_NATIVE_TEXT_MANIFEST_1"

// Manifest projects frozen consent and currently qualified kernel facts. It is
// metadata only, not a downloadable command or a KernelRegistry approval. The
// receiver still needs independently provisioned pins, plan and local approval.
type Manifest struct {
	Profile           string `json:"profile_id"`
	ID                string `json:"execution_manifest_ref"`
	Job               string `json:"job_id"`
	Attempt           string `json:"attempt_id"`
	Order             string `json:"order_id"`
	Route             string `json:"route_id"`
	Seller            string `json:"seller_id"`
	Device            string `json:"device_id"`
	Resource          string `json:"resource_id"`
	Capability        string `json:"capability_id"`
	Generation        string `json:"resource_lock_generation"`
	Fence             string `json:"fencing_token"`
	ContractHash      string `json:"contract_hash"`
	EnvironmentRef    string `json:"kernel_environment_ref"`
	EnvironmentDigest string `json:"kernel_environment_digest"`
	EngineKind        string `json:"engine_kind"`
	EngineRelease     string `json:"engine_release"`
	ModelDigest       string `json:"model_digest"`
	RecipeDigest      string `json:"recipe_digest"`
	Spec              Spec   `json:"request_spec"`
	InputTokenLimit   int64  `json:"input_token_limit"`
	InputAsset        string `json:"input_asset_ref"`
	InputDigest       string `json:"input_ciphertext_digest"`
	InputBytes        int64  `json:"input_bytes"`
	Mode              string `json:"mode"`
	Privacy           string `json:"privacy"`
	HardDeadline      string `json:"hard_deadline"`
}

func (m Manifest) Digest() string {
	b, _ := json.Marshal(m)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ApprovedPlan must be used ONLY with this manifest obtained through an
// independently authenticated order/manifest channel and approved locally.
// Never invoke it on an untrusted companion manifest bundled with a lease.
func (m Manifest) ApprovedPlan() Plan {
	h, _ := time.Parse(time.RFC3339Nano, m.HardDeadline)
	return Plan{Job: m.Job, Attempt: m.Attempt, Order: m.Order, Seller: m.Seller, Device: m.Device,
		Resource: m.Resource, Generation: m.Generation, Capability: m.Capability, Manifest: m.ID, ManifestHash: m.Digest(),
		Environment: m.EnvironmentDigest, Fence: m.Fence, ContractHash: m.ContractHash, HardDeadline: h}
}
