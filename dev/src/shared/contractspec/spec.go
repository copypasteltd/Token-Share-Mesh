// Package contractspec defines content-free public selection fields shared by
// immutable platform contracts and independently approved native manifests.
package contractspec

type Selection struct {
	ModelID          string `json:"model_id,omitempty"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	WorkflowRevision int64  `json:"workflow_revision,omitempty"`
	DurationMS       int64  `json:"duration_ms,omitempty"`
	InputTokenLimit  int64  `json:"input_token_limit,omitempty"`
	OutputTokenLimit int64  `json:"output_token_limit,omitempty"`
}
