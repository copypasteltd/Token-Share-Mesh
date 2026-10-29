package controlhttp

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"p11.local/compute/shared/ids"
	"strings"
)

//go:embed routes.json
var routeDocument []byte

type registeredRoute struct {
	OperationID string `json:"operation_id"`
	Method      string `json:"method"`
	Path        string `json:"path"`
	Handler     string `json:"handler"`
	Anonymous   bool   `json:"anonymous"`
}

func (s *Server) loadRoutes() bool {
	var manifest struct {
		Version    string            `json:"version"`
		BasePath   string            `json:"base_path"`
		Scope      string            `json:"scope"`
		Operations []registeredRoute `json:"operations"`
	}
	if json.Unmarshal(routeDocument, &manifest) != nil || manifest.Version != "P11_CONTROL_HTTP_R004" || manifest.BasePath != prefix || len(manifest.Operations) == 0 {
		return false
	}
	pairs, operations := map[string]bool{}, map[string]bool{}
	for _, r := range manifest.Operations {
		key := r.Method + " " + r.Path
		if !ids.Valid(r.OperationID) || pairs[key] || operations[r.OperationID] || s.handlerNamed(r.Handler) == nil || !strings.HasPrefix(r.Path, "/") ||
			(r.Method != "GET" && r.Method != "POST" && r.Method != "DELETE") {
			return false
		}
		pairs[key], operations[r.OperationID] = true, true
	}
	s.routes = manifest.Operations
	return true
}
func (s *Server) handlerNamed(name string) handler {
	switch name {
	case "challenge":
		return s.challenge
	case "login":
		return s.login
	case "currentSession":
		return s.currentSession
	case "me":
		return s.me
	case "keys":
		return s.keys
	case "revokeKey":
		return s.revokeKey
	case "services":
		return s.services
	case "quote":
		return s.quote
	case "getQuote":
		return s.getQuote
	case "order":
		return s.order
	case "getOrder":
		return s.getOrder
	}
	return nil
}
func pathMatches(pattern, path string) bool {
	if !strings.Contains(pattern, "{") {
		return pattern == path
	}
	before, _, _ := strings.Cut(pattern, "{")
	return strings.HasPrefix(path, before) && ids.Valid(strings.TrimPrefix(path, before))
}
func (s *Server) route(r *http.Request) (handler, string, bool) {
	var selected *registeredRoute
	methods := []string{}
	for i := range s.routes {
		entry := &s.routes[i]
		if !pathMatches(prefix+entry.Path, r.URL.Path) {
			continue
		}
		methods = append(methods, entry.Method)
		if selected == nil || entry.Method == r.Method {
			selected = entry
		}
	}
	if selected == nil {
		return nil, "", false
	}
	return s.handlerNamed(selected.Handler), strings.Join(methods, ","), selected.Anonymous
}
