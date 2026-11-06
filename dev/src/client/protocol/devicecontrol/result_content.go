package devicecontrol

import (
	"context"
	"encoding/json"
	"errors"

	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/resultwire"
)

// Content commands are an explicitly content-bearing transport. Unlike the
// device control DTOs, their body may contain output and must never be logged.
func (c *Client) SendResultContent(ctx context.Context, attempt, step, key string, in any) (Reply, error) {
	if !ids.Valid(attempt) || (step != "begin" && step != "chunks" && step != "seal") {
		return Reply{}, errors.New("original content command required")
	}
	raw, e := json.Marshal(in)
	defer clear(raw)
	if e != nil || len(raw) > MaximumBody {
		return Reply{}, errors.New("bounded original content body required")
	}
	p, e := c.Prepare("POST", "/v1/p11/runner/attempts/"+attempt+"/content/"+step, raw, key, "")
	if e != nil {
		return Reply{}, e
	}
	defer clear(p.body)
	return c.SendBounded(ctx, p, 4096)
}
func (c *Client) ReadResultContentStatus(ctx context.Context, attempt, delivery string) (Reply, error) {
	if !ids.Valid(attempt) || !ids.Valid(delivery) {
		return Reply{}, errors.New("original result identity required")
	}
	p, e := c.Prepare("GET", "/v1/p11/runner/attempts/"+attempt+"/content/"+delivery+"/status", nil, "", "")
	if e != nil {
		return Reply{}, e
	}
	return c.SendBounded(ctx, p, 4096)
}
func ContentCommandKey(step string, in any) string {
	return "result_content_" + step + "_" + resultwire.Digest(in)
}
