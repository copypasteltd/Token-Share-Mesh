package node

import (
	"bytes"
	"context"
	"errors"
	"github.com/godbus/dbus/v5"
	"strings"
	"testing"
)

type secretTestWire struct {
	mode    string
	closed  bool
	created bool
}

func (s *secretTestWire) call(_ context.Context, path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	result := &dbus.Call{}
	session := dbus.ObjectPath("/org/freedesktop/secrets/session/test")
	switch method {
	case "org.freedesktop.Secret.Service.OpenSession":
		result.Body = []any{dbus.MakeVariant(""), session}
	case "org.freedesktop.Secret.Service.SearchItems":
		unlocked := []dbus.ObjectPath{}
		locked := []dbus.ObjectPath{}
		if s.mode == "existing" || s.mode == "wrong_secret" {
			unlocked = append(unlocked, "/item/one")
		}
		if s.mode == "locked" {
			locked = append(locked, "/item/one")
		}
		if s.mode == "duplicate" {
			unlocked = append(unlocked, "/item/one", "/item/two")
		}
		attrs := args[0].(map[string]string)
		if attrs["application"] != "org.tokensharemesh.node" || attrs["purpose"] != "control-state-master-v1" || len(attrs["installation"]) != 64 {
			result.Err = ErrVault
		}
		result.Body = []any{unlocked, locked}
	case "org.freedesktop.Secret.Item.GetSecret":
		secret := secretValue{session, []byte{}, bytes.Repeat([]byte{8}, 32), "application/octet-stream"}
		if s.mode == "wrong_secret" {
			secret.Session = "/other/session"
		}
		result.Body = []any{secret}
	case "org.freedesktop.Secret.Service.ReadAlias":
		result.Body = []any{dbus.ObjectPath("/collection/default")}
	case "org.freedesktop.DBus.Properties.Get":
		result.Body = []any{dbus.MakeVariant(s.mode == "locked_collection")}
	case "org.freedesktop.Secret.Collection.CreateItem":
		secret := args[1].(secretValue)
		if secret.Session != session || len(secret.Value) != 32 || len(secret.Parameters) != 0 || args[2].(bool) {
			result.Err = ErrVault
		}
		s.created = true
		prompt := dbus.ObjectPath("/")
		if s.mode == "prompt" {
			prompt = "/prompt/user"
		}
		result.Body = []any{dbus.ObjectPath("/item/new"), prompt}
	case "org.freedesktop.Secret.Session.Close":
		s.closed = true
	default:
		result.Err = errors.New("unexpected secret service operation")
	}
	return result
}
func TestSecretServiceLookupCreationAndClosedFailure(t *testing.T) {
	for _, scenario := range []struct {
		mode            string
		create, success bool
	}{{"existing", false, true}, {"empty", true, true}, {"empty", false, false}, {"locked", false, false}, {"duplicate", false, false}, {"wrong_secret", false, false}, {"locked_collection", true, false}, {"prompt", true, false}} {
		t.Run(scenario.mode+"_"+map[bool]string{true: "create", false: "load"}[scenario.create], func(t *testing.T) {
			wire := &secretTestWire{mode: scenario.mode}
			key, err := secretMaster(context.Background(), wire, strings.Repeat("a", 64), scenario.create)
			defer clear(key)
			if scenario.success != (err == nil) || scenario.success && len(key) != 32 || !wire.closed {
				t.Fatal("secret-service lookup/create/session-close boundary failed", err)
			}
			if wire.created && !scenario.create {
				t.Fatal("read silently created an identity key")
			}
		})
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "tcp:host=127.0.0.1,port=1")
	if _, e := desktopMaster(context.Background(), strings.Repeat("a", 64), true); !errors.Is(e, ErrVault) {
		t.Fatal("nonlocal bus accepted")
	}
}
