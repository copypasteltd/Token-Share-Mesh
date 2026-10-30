package core

// RequestMetadata is bounded native-protocol preflight, not usage or execution
// authority. The exact body digest is transient inside the plaintext domain.
type RequestMetadata struct {
	Digest          string
	MaxOutputTokens int
	Stream          bool
}

// Preflight uses the SAME strict text parser as forwarding. No network,
// credentials, persistence, model rewriting or paid side effects occur here.
func Preflight(profile Profile, publicModel string, raw []byte) (RequestMetadata, error) {
	r, err := validateRequest(raw, Binding{Profile: profile, PublicModel: publicModel, BackendModel: publicModel}, DefaultLimits())
	if err != nil {
		return RequestMetadata{}, err
	}
	defer clear(r.raw)
	return RequestMetadata{Digest: r.hash, MaxOutputTokens: r.maxOutput, Stream: r.stream}, nil
}
