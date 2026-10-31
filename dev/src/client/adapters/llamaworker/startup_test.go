package llamaworker

import (
	"context"
	"errors"
	"io"
	relay "p11.local/compute/gateway/core"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestNativeStartupReportsOnlyBoundedStagesAndNeverGrantsWork(t *testing.T) {
	for _, tc := range []struct {
		name     string
		profile  string
		stage    uint32
		trailing bool
		valid    bool
	}{
		{"load_dependency", Profile, 1, false, true}, {"resolve_api", Profile, 4, false, true}, {"unknown_stage", Profile, 999, false, false},
		{"zero_stage", Profile, 0, false, false}, {"old_profile", "P11_LLAMA_PIPE_2", 1, false, false}, {"trailing", Profile, 1, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w, m, b := textCase(relay.ChatText, chatRequest)
			ir, iw := io.Pipe()
			or, ow := io.Pipe()
			cr, cw := io.Pipe()
			wait := make(chan error, 1)
			var once sync.Once
			quit := func() {
				once.Do(func() {
					ir.Close()
					iw.Close()
					or.Close()
					ow.Close()
					cr.Close()
					cw.Close()
					wait <- nil
				})
			}
			a := newAdapter(c, child{input: iw, control: cw, output: or, kill: quit, wait: wait})
			defer quit()
			go func() {
				var e encoder
				e.str(tc.profile)
				e.u32(tc.stage)
				e.u32(126)
				if tc.trailing {
					e.u32(99)
				}
				_ = writeFrame(ow, kInitError, e.Bytes())
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var err error
			select {
			case err = <-a.ready:
			case <-ctx.Done():
				t.Fatal("startup result did not terminate")
			}
			var startup StartupError
			if tc.valid {
				if !errors.As(err, &startup) || startup.NativeCode != 126 || !errors.Is(err, ErrUnknown) || strings.Contains(startup.Error(), "sensitive") {
					t.Fatal("bounded startup cause lost", err)
				}
			} else if err != ErrUnknown {
				t.Fatal("invalid startup metadata surfaced", err)
			}
			select {
			case <-a.exited:
			case <-ctx.Done():
				t.Fatal("owned startup fixture did not exit")
			}
			if _, err = a.SubmitInput(nativeInputContext(t, ctx, w, m, b), w, m, b); err == nil {
				t.Fatal("failed startup accepted new work")
			}
			clear(b)
		})
	}
}
