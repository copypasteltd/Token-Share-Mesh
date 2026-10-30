package node

import (
	"context"
	"crypto/rand"
	"github.com/godbus/dbus/v5"
	"os"
	"strings"
	"time"
)

const secretService = "org.freedesktop.secrets"
const secretRoot = dbus.ObjectPath("/org/freedesktop/secrets")

type secretWire interface {
	call(context.Context, dbus.ObjectPath, string, ...any) *dbus.Call
}
type secretConnection struct {
	conn  *dbus.Conn
	owner string
}

func (s secretConnection) call(ctx context.Context, path dbus.ObjectPath, method string, args ...any) *dbus.Call {
	return s.conn.Object(s.owner, path).CallWithContext(ctx, method, dbus.FlagNoAutoStart, args...)
}

type secretValue struct {
	Session     dbus.ObjectPath
	Parameters  []byte
	Value       []byte
	ContentType string
}

func desktopMaster(parent context.Context, id string, create bool) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	address := os.Getenv("DBUS_SESSION_BUS_ADDRESS")
	// Never spawn dbus-launch/autolaunch, connect TCP, or open a system bus.
	if !(strings.HasPrefix(address, "unix:path=/") || strings.HasPrefix(address, "unix:abstract=")) || strings.ContainsAny(address, ";\r\n") || len(address) > 4096 {
		return nil, ErrVault
	}
	conn, err := dbus.Connect(address, dbus.WithContext(ctx))
	if err != nil {
		return nil, ErrVault
	}
	defer conn.Close()
	var owner string
	if conn.BusObject().CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", dbus.FlagNoAutoStart, secretService).Store(&owner) != nil || !strings.HasPrefix(owner, ":") {
		return nil, ErrVault
	}
	return secretMaster(ctx, secretConnection{conn, owner}, id, create)
}

func secretMaster(ctx context.Context, wire secretWire, id string, create bool) ([]byte, error) {
	var output dbus.Variant
	var session dbus.ObjectPath
	if wire.call(ctx, secretRoot, "org.freedesktop.Secret.Service.OpenSession", "plain", dbus.MakeVariant("")).Store(&output, &session) != nil || !session.IsValid() || session == "/" {
		return nil, ErrVault
	}
	defer func() {
		closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		wire.call(closeCtx, session, "org.freedesktop.Secret.Session.Close")
	}()
	attrs := map[string]string{"application": "org.tokensharemesh.node", "installation": id, "purpose": "control-state-master-v1"}
	var unlocked, locked []dbus.ObjectPath
	if wire.call(ctx, secretRoot, "org.freedesktop.Secret.Service.SearchItems", attrs).Store(&unlocked, &locked) != nil || len(locked) > 0 || len(unlocked) > 1 {
		return nil, ErrVault
	}
	if len(unlocked) == 1 {
		var secret secretValue
		if wire.call(ctx, unlocked[0], "org.freedesktop.Secret.Item.GetSecret", session).Store(&secret) != nil || secret.Session != session || len(secret.Parameters) != 0 || len(secret.Value) != 32 || secret.ContentType != "application/octet-stream" {
			clear(secret.Value)
			return nil, ErrVault
		}
		return secret.Value, nil
	}
	if !create {
		return nil, ErrVault
	}
	var collection dbus.ObjectPath
	if wire.call(ctx, secretRoot, "org.freedesktop.Secret.Service.ReadAlias", "default").Store(&collection) != nil || !collection.IsValid() || collection == "/" {
		return nil, ErrVault
	}
	var isLocked dbus.Variant
	if wire.call(ctx, collection, "org.freedesktop.DBus.Properties.Get", "org.freedesktop.Secret.Collection", "Locked").Store(&isLocked) != nil {
		return nil, ErrVault
	}
	lockedBool, ok := isLocked.Value().(bool)
	if !ok || lockedBool {
		return nil, ErrVault
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, ErrVault
	}
	props := map[string]dbus.Variant{"org.freedesktop.Secret.Item.Label": dbus.MakeVariant("Token Share Mesh provider control"), "org.freedesktop.Secret.Item.Attributes": dbus.MakeVariant(attrs)}
	var item, prompt dbus.ObjectPath
	if wire.call(ctx, collection, "org.freedesktop.Secret.Collection.CreateItem", props, secretValue{session, []byte{}, key, "application/octet-stream"}, false).Store(&item, &prompt) != nil || !item.IsValid() || item == "/" || prompt != "/" {
		clear(key)
		return nil, ErrVault
	}
	return key, nil
}
