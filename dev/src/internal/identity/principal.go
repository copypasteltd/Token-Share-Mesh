package identity

import (
	"p11.local/compute/shared/fault"
	"p11.local/compute/shared/ids"
)

type Kind string

const (
	UserSession Kind = "USER_SESSION"
	APIKey      Kind = "API_KEY"
	Device      Kind = "DEVICE"
)

// Principal must be constructed by a verified credential adapter, never by
// parsing roles/scopes from a request body or an unverified device report.
type Principal struct {
	UserID    string
	Kind      Kind
	KeyID     string
	SessionID string
	Scopes    []string
	RouteIDs  []string
	Roles     []string
	Version   int64
}

func (p Principal) Require(scope, owner string) error {
	if !ids.Valid(p.UserID) {
		return fault.New(fault.Unauthenticated, "Authentication required")
	}
	if owner != "" && p.UserID != owner {
		return fault.New(fault.NotFound, "Resource not found")
	}
	for _, s := range p.Scopes {
		if s == scope {
			return nil
		}
	}
	return fault.New(fault.Forbidden, "Permission denied")
}
func (p Principal) RequireUser(scope string) error {
	if p.Kind != UserSession {
		return fault.New(fault.Forbidden, "Account session required")
	}
	return p.Require(scope, "")
}
