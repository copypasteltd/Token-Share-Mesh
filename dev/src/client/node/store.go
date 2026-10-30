// Package node implements the native provider's persistent control agent.
// It does not execute jobs, approve hardware or handle customer content.
package node

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"p11.local/compute/client/protocol/devicecontrol"
	"p11.local/compute/shared/ids"
	"p11.local/compute/shared/strictjson"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

const stateLimit = 131072
const stateProfile = "P11_NODE_STATE_1"

var ErrState = errors.New("native state unavailable, corrupt or incompatible; do not recreate a registered identity")
var ErrLocked = errors.New("native state is owned by another process")
var ErrVault = errors.New("OS key store unavailable or locked; unlock the user key store and explicitly retry")

type Config struct {
	Base    string `json:"control_base"`
	Model   string `json:"model_name"`
	Kind    string `json:"resource_kind"`
	OSBuild string `json:"os_build"`
	Release string `json:"client_release_id"`
}

// journalBytes uses canonical standard base64, matching encoding/json's []byte
// writer while making that representation explicit to strictjson's shape check.
type journalBytes []byte

func (b *journalBytes) UnmarshalJSON(raw []byte) error {
	var encoded string
	if json.Unmarshal(raw, &encoded) != nil || len(encoded) > stateLimit {
		return ErrState
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != encoded {
		return ErrState
	}
	*b = decoded
	return nil
}

type command struct {
	Kind       string       `json:"kind"`
	Key        string       `json:"idempotency_key"`
	Body       journalBytes `json:"body"`
	Credential string       `json:"pairing_credential,omitempty"`
}
type state struct {
	Profile      string       `json:"profile"`
	Installation string       `json:"installation_id"`
	Platform     string       `json:"platform"`
	Config       Config       `json:"config"`
	Seed         journalBytes `json:"control_seed"`
	DeviceID     string       `json:"device_id"`
	SellerID     string       `json:"seller_id"`
	Version      int64        `json:"device_version"`
	RemoteState  string       `json:"remote_state"`
	Sequence     *string      `json:"reserved_sequence"`
	Pending      *command     `json:"pending_command"`
	LastOutcome  string       `json:"last_control_outcome"`
}

// Store holds an OS-level single-writer lock until Close, including during
// network operations. A missing key or corrupt journal never creates new keys.
type Store struct {
	root     *os.Root
	path     string
	lock     *os.File
	master   []byte
	s        state
	poisoned bool
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", ErrState
	}
	return hex.EncodeToString(b), nil
}
func text(value string, maximum int) bool {
	return value != "" && len(value) <= maximum && utf8.ValidString(value) && strings.TrimSpace(value) == value && strings.IndexFunc(value, unicode.IsControl) < 0
}
func validConfig(c Config) bool {
	if devicecontrol.ValidateBase(c.Base) != nil || !text(c.Model, 160) || !text(c.OSBuild, 160) || !ids.Valid(c.Release) || len(c.Release) > 96 || (c.Kind != "GPU_DEVICE" && c.Kind != "AUTHORIZED_RELAY") {
		return false
	}
	lower := strings.ToLower(c.Model)
	return lower != "android设备" && lower != "android device" && lower != "gpu" && lower != "unknown" && lower != "设备"
}

func openRoot(path string, create bool) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrState
	}
	// No symlink/junction/reparse component is an accepted state location.
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		if err := checkPath(p, true); err != nil {
			return nil, ErrState
		}
		if filepath.Dir(p) == p {
			break
		}
	}
	if create {
		if err := makePrivateDirectory(path); err != nil {
			return nil, ErrState
		}
	}
	if err := checkPath(path, true); err != nil {
		return nil, ErrState
	}
	if err := securePath(path, true); err != nil {
		return nil, ErrState
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrState
	}
	s := &Store{root: r, path: path}
	if err = s.checkFile("control.lock", true); err != nil {
		s.Close()
		return nil, err
	}
	if _, e := r.Lstat("control.lock"); errors.Is(e, os.ErrNotExist) {
		s.lock, err = createPrivateFile(s, "control.lock")
	} else {
		s.lock, err = r.OpenFile("control.lock", os.O_RDWR, 0600)
	}
	if err != nil {
		s.Close()
		return nil, ErrState
	}
	if err = securePath(filepath.Join(path, "control.lock"), false); err != nil {
		s.Close()
		return nil, ErrState
	}
	if err = lockFile(s.lock); err != nil {
		s.Close()
		return nil, ErrLocked
	}
	return s, nil
}
func (s *Store) checkFile(name string, missing bool) error {
	info, err := s.root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) && missing {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return ErrState
	}
	if err = checkPath(filepath.Join(s.path, name), false); err != nil {
		return ErrState
	}
	if err = securePath(filepath.Join(s.path, name), false); err != nil {
		return ErrState
	}
	return nil
}
func (s *Store) read(name string, maximum int) ([]byte, error) {
	if s.checkFile(name, false) != nil {
		return nil, ErrState
	}
	f, err := s.root.Open(name)
	if err != nil {
		return nil, ErrState
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(maximum+1)))
	if err != nil || len(b) > maximum {
		return nil, ErrState
	}
	return b, nil
}
func (s *Store) writeNew(name string, b []byte) error {
	if s.checkFile(name, true) != nil {
		return ErrState
	}
	f, err := createPrivateFile(s, name)
	if err != nil {
		return ErrState
	}
	if securePath(filepath.Join(s.path, name), false) != nil {
		f.Close()
		return ErrState
	}
	_, err = f.Write(b)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil || closeErr != nil {
		return ErrState
	}
	return syncDirectory(s.path)
}

func Create(ctx context.Context, path string, config Config) (*Store, error) {
	return createWith(ctx, path, config, loadMaster)
}

type masterLoader func(context.Context, *Store, string, bool) ([]byte, error)

func createWith(ctx context.Context, path string, config Config, load masterLoader) (*Store, error) {
	if !validConfig(config) {
		return nil, ErrState
	}
	s, err := openRoot(path, true)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	installation, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	if err = s.writeNew("installation.id", []byte(installation)); err != nil {
		return nil, err
	}
	s.master, err = load(ctx, s, installation, true)
	if err != nil {
		return nil, ErrVault
	}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, ErrState
	}
	defer clear(key)
	s.s = state{Profile: stateProfile, Installation: installation, Platform: strings.ToUpper(runtime.GOOS), Config: config, Seed: key.Seed(), LastOutcome: "LOCAL_ONLY"}
	if err = s.save(); err != nil {
		return nil, err
	}
	ok = true
	return s, nil
}

func Open(ctx context.Context, path string) (*Store, error) {
	return openWith(ctx, path, loadMaster)
}
func openWith(ctx context.Context, path string, load masterLoader) (*Store, error) {
	s, err := openRoot(path, false)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			s.Close()
		}
	}()
	install, err := s.read("installation.id", 64)
	if err != nil || len(install) != 64 {
		return nil, ErrState
	}
	decoded, err := hex.DecodeString(string(install))
	if err != nil || hex.EncodeToString(decoded) != string(install) {
		return nil, ErrState
	}
	s.master, err = load(ctx, s, string(install), false)
	if err != nil {
		return nil, ErrVault
	}
	sealed, err := s.read("state.enc", stateLimit)
	if err != nil {
		return nil, err
	}
	gcm, err := makeCipher(s.master)
	if err != nil || len(sealed) < gcm.NonceSize()+gcm.Overhead() {
		return nil, ErrState
	}
	plain, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(stateProfile+"\n"+string(install)))
	if err != nil {
		return nil, ErrState
	}
	defer clear(plain)
	if strictjson.Decode(plain, &s.s, stateLimit) != nil || s.validate() != nil || s.s.Installation != string(install) {
		return nil, ErrState
	}
	ok = true
	return s, nil
}
func makeCipher(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, ErrState
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrState
	}
	return cipher.NewGCM(b)
}
func (s *Store) validate() error {
	v := s.s
	if v.Profile != stateProfile || len(v.Seed) != 32 || v.Platform != strings.ToUpper(runtime.GOOS) || !validConfig(v.Config) || len(v.Installation) != 64 || (v.DeviceID != "" && (!ids.Valid(v.DeviceID) || !ids.Valid(v.SellerID) || v.Version < 1)) {
		return ErrState
	}
	if v.Sequence != nil {
		if _, err := counter(*v.Sequence); err != nil {
			return ErrState
		}
	}
	if p := v.Pending; p != nil {
		if !ids.Valid(p.Key) || len(p.Key) < 8 || len(p.Key) > 128 || len(p.Body) > devicecontrol.MaximumBody {
			return ErrState
		}
		switch p.Kind {
		case "ENROLLMENT":
			var e devicecontrol.Enrollment
			id, err := devicecontrol.PairingID(p.Credential)
			if v.DeviceID != "" || err != nil || strictjson.Decode(p.Body, &e, devicecontrol.MaximumBody) != nil || e != s.enrollment(id) {
				return ErrState
			}
		case "HEARTBEAT":
			var h devicecontrol.Heartbeat
			if v.DeviceID == "" || p.Credential != "" || strictjson.Decode(p.Body, &h, devicecontrol.MaximumBody) != nil || h.Attempt != "" || h.Fence != "" || h.State != "UNKNOWN" || h.Version < 1 || h.Recorded.IsZero() || v.Sequence == nil || h.Sequence != *v.Sequence {
				return ErrState
			}
		default:
			return ErrState
		}
	}
	return nil
}
func (s *Store) save() error {
	if s.root == nil || s.poisoned || s.validate() != nil {
		return ErrState
	}
	plain, err := json.Marshal(s.s)
	if err != nil {
		return ErrState
	}
	defer clear(plain)
	gcm, err := makeCipher(s.master)
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return ErrState
	}
	sealed := gcm.Seal(nonce, nonce, plain, []byte(stateProfile+"\n"+s.s.Installation))
	if len(sealed) > stateLimit {
		return ErrState
	}
	name, err := randomHex(16)
	if err != nil {
		return err
	}
	name = "state-next-" + name
	if err = s.writeNew(name, sealed); err != nil {
		s.poisoned = true
		return ErrState
	}
	defer s.root.Remove(name)
	if s.checkFile("state.enc", true) != nil || replaceFile(s.path, s.root, name, "state.enc") != nil || syncDirectory(s.path) != nil {
		s.poisoned = true
		return ErrState
	}
	return nil
}
func (s *Store) key() ed25519.PrivateKey { return ed25519.NewKeyFromSeed(s.s.Seed) }
func (s *Store) fingerprint() string {
	k := s.key()
	defer clear(k)
	h := sha256.Sum256(k.Public().(ed25519.PublicKey))
	return hex.EncodeToString(h[:])
}
func (s *Store) enrollment(id string) devicecontrol.Enrollment {
	k := s.key()
	defer clear(k)
	pub := k.Public().(ed25519.PublicKey)
	fp := sha256.Sum256(append(append([]byte{}, pub...), []byte(s.s.Installation)...))
	return devicecontrol.Enrollment{PairingID: id, PublicKey: base64.RawURLEncoding.EncodeToString(pub), KeyProfile: devicecontrol.KeyProfile, OSBuild: s.s.Config.OSBuild, ClientRelease: s.s.Config.Release, Fingerprint: hex.EncodeToString(fp[:]), Kind: s.s.Config.Kind, Model: s.s.Config.Model}
}
func counter(value string) (uint64, error) {
	v, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(v, 10) != value {
		return 0, ErrState
	}
	return v, nil
}
func (s *Store) Close() {
	clear(s.master)
	clear(s.s.Seed)
	if s.s.Pending != nil {
		clear(s.s.Pending.Body)
		s.s.Pending.Credential = ""
	}
	if s.lock != nil {
		s.lock.Close()
		s.lock = nil
	}
	if s.root != nil {
		s.root.Close()
		s.root = nil
	}
}
