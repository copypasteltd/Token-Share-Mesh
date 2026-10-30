package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"p11.local/compute/client/execution"
	"p11.local/compute/client/node"
	"p11.local/compute/shared/strictjson"
	"strings"
	"syscall"
)

func main() { os.Exit(run()) }
func run() int {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: p11-node init|enroll|recover|status|heartbeat|run|execution-init|execution-status|host-init|host-status --state <absolute-private-directory>")
		return 2
	}
	action := os.Args[1]
	if action != "init" && action != "enroll" && action != "recover" && action != "status" && action != "heartbeat" && action != "run" && action != "execution-init" && action != "execution-status" && action != "host-init" && action != "host-status" {
		fmt.Fprintln(os.Stderr, "unknown native control command")
		return 2
	}
	flags := flag.NewFlagSet("p11-node", flag.ContinueOnError)
	// Flag diagnostics would echo arbitrary caller arguments, including a
	// mistakenly supplied credential. Emit only fixed safe diagnostics.
	flags.SetOutput(io.Discard)
	path := flags.String("state", "", "explicit private absolute state directory")
	refresh := flags.Bool("refresh", false, "refresh registered status from the control API")
	if flags.Parse(os.Args[2:]) != nil || flags.NArg() != 0 || *path == "" || *refresh && action != "status" {
		fmt.Fprintln(os.Stderr, "invalid native control options")
		return 2
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	var s *node.Store
	var err error
	if action == "init" {
		var raw []byte
		raw, err = readInput(4096)
		if err == nil {
			var config node.Config
			err = strictjson.Decode(raw, &config, 4096)
			if err == nil {
				s, err = node.Create(ctx, *path, config)
			}
		}
		clear(raw)
	} else {
		s, err = node.Open(ctx, *path)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, safe(err))
		return 1
	}
	defer s.Close()
	var output any
	switch action {
	case "enroll":
		var token []byte
		token, err = readInput(512)
		if err == nil {
			err = s.Enroll(ctx, strings.TrimSpace(string(token)))
		}
		clear(token)
	case "recover":
		err = s.Recover(ctx)
	case "status":
		if *refresh {
			err = s.Refresh(ctx)
		}
	case "heartbeat":
		err = s.Step(ctx)
	case "run":
		err = s.Run(ctx, func(status node.Status) { _ = json.NewEncoder(os.Stdout).Encode(status) })
	case "execution-init", "execution-status":
		var ledger *execution.Store
		if action == "execution-init" {
			ledger, err = s.InitExecution(ctx)
		} else {
			ledger, err = s.OpenExecution(ctx)
		}
		if err == nil {
			output, err = ledger.Snapshot(ctx)
		}
	case "host-init", "host-status":
		var host *node.Host
		if action == "host-init" {
			var raw []byte
			raw, err = readInput(131072)
			if err == nil {
				var c node.HostConfig
				err = strictjson.Decode(raw, &c, 131072)
				if err == nil {
					host, err = s.InitHost(ctx, c)
				}
			}
			clear(raw)
		} else {
			host, err = s.OpenHost(ctx)
		}
		if err == nil {
			output, err = host.Snapshot(ctx)
		}
	}
	if action != "run" && (action != "execution-init" && action != "execution-status" || err == nil) {
		if output == nil {
			output = s.Status()
		}
		_ = json.NewEncoder(os.Stdout).Encode(output)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, safe(err))
		return 1
	}
	return 0
}
func readInput(maximum int) ([]byte, error) {
	info, err := os.Stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice != 0 {
		return nil, errors.New("bounded piped standard input required; credentials are not accepted in command arguments")
	}
	b, err := io.ReadAll(io.LimitReader(os.Stdin, int64(maximum+1)))
	if err != nil || len(b) == 0 || len(b) > maximum {
		clear(b)
		return nil, errors.New("bounded piped standard input required")
	}
	return b, nil
}
func safe(err error) string {
	for _, e := range []error{node.ErrState, node.ErrLocked, node.ErrVault, node.ErrPending, node.ErrAuthority, node.ErrRejected, node.ErrEnrollment, node.ErrCounter, node.ErrHost, node.ErrHostPolicy, node.ErrHostPending, execution.ErrStorage, execution.ErrRecovery, execution.ErrFence, execution.ErrBusy, execution.ErrEvidence, execution.ErrCapacity} {
		if errors.Is(err, e) {
			return e.Error()
		}
	}
	return "native control operation unavailable; retain the original state and recover pending commands"
}
