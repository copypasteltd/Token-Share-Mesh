//go:build !p11_sodium || !cgo || (!windows && !linux)

package sodium

// An ordinary control-only build stays closed to this suite. It must never
// claim that copying bytes, a Go substitute or local-at-rest AEAD is E2E.
type State struct{}

func Available() error                                   { return ErrUnavailable }
func Keypair() ([]byte, []byte, error)                   { return nil, nil, ErrUnavailable }
func Seal([]byte, []byte) ([]byte, error)                { return nil, ErrUnavailable }
func Unseal([]byte, []byte, []byte) ([]byte, error)      { return nil, ErrUnavailable }
func NewPush([]byte) (*State, []byte, error)             { return nil, nil, ErrUnavailable }
func NewPull([]byte, []byte) (*State, error)             { return nil, ErrUnavailable }
func (*State) Push([]byte, []byte, bool) ([]byte, error) { return nil, ErrUnavailable }
func (*State) Pull([]byte, []byte) ([]byte, bool, error) { return nil, false, ErrUnavailable }
func (*State) Close()                                    {}
