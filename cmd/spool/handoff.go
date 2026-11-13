package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/handoff"
	"github.com/sfrieds3/spool/internal/history"
)

func invalidRequest(format string, args ...any) *history.Error {
	return &history.Error{Code: history.CodeInvalidRequest, Message: fmt.Sprintf(format, args...)}
}

func (a app) handoffCmd(args []string) int {
	if len(args) == 0 {
		return a.fail(invalidRequest("missing handoff subcommand: create, list, or show"))
	}
	switch args[0] {
	case "create":
		return a.handoffCreate(args[1:])
	case "list":
		return a.handoffList(args[1:])
	case "show":
		return a.handoffShow(args[1:])
	}
	return a.fail(invalidRequest("unknown handoff subcommand %q", truncate(args[0])))
}

// handoffService loads configuration and the state directory.
func (a app) handoffService(path string) (*handoff.Service, int) {
	cfg, code := a.config(path)
	if cfg == nil {
		return nil, code
	}
	state, err := config.StateDir(a.env)
	if err != nil {
		var ce *config.Error
		if errors.As(err, &ce) {
			return nil, a.fail(&history.Error{Code: history.CodeInvalidConfig, Message: ce.Msg})
		}
		return nil, a.fail(&history.Error{Code: history.CodeInternal, Message: err.Error()})
	}
	opts := a.handoff
	if opts.SpoolVersion == "" {
		opts.SpoolVersion = version
	}
	return handoff.NewService(cfg, history.NewService(cfg), state, opts), 0
}

func (a app) handoffCreate(args []string) int {
	var pf pageFlags
	fs := newFlagSet("handoff create", &pf)
	input := fs.String("input", "", "Brief file, or - for stdin")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return a.fail(invalidRequest("%s", flagMessage(err)))
	}
	if len(pos) != 0 {
		return a.fail(invalidRequest("unexpected argument %q", truncate(pos[0])))
	}
	if *input == "" {
		return a.fail(invalidRequest("missing --input FILE (use --input - for stdin)"))
	}
	data, herr := a.readInput(*input)
	if herr != nil {
		return a.fail(herr)
	}
	svc, code := a.handoffService(pf.config)
	if svc == nil {
		return code
	}
	ctx, stop := signal.NotifyContext(a.ctx, os.Interrupt)
	defer stop()
	resp, herr := svc.Create(ctx, data)
	if herr != nil {
		return a.fail(herr)
	}
	return a.emit(resp, exitOK)
}

// readInput reads at most one byte beyond the brief limit so that oversized
// input is rejected without reading it all.
func (a app) readInput(name string) ([]byte, *history.Error) {
	var r io.Reader
	if name == "-" {
		if a.stdin == nil {
			return nil, invalidRequest("stdin is not available")
		}
		r = a.stdin
	} else {
		info, err := os.Stat(name)
		if err != nil {
			return nil, invalidRequest("cannot read --input: %s", errText(err))
		}
		if !info.Mode().IsRegular() {
			return nil, invalidRequest("--input must be a regular file or -")
		}
		f, err := os.Open(name)
		if err != nil {
			return nil, invalidRequest("cannot read --input: %s", errText(err))
		}
		defer f.Close()
		info, err = f.Stat()
		if err != nil {
			return nil, invalidRequest("cannot read --input: %s", errText(err))
		}
		if !info.Mode().IsRegular() {
			return nil, invalidRequest("--input must be a regular file or -")
		}
		r = f
	}
	data, err := io.ReadAll(io.LimitReader(r, handoff.MaxInputBytes+1))
	if err != nil {
		return nil, invalidRequest("cannot read --input: %s", errText(err))
	}
	if len(data) > handoff.MaxInputBytes {
		return nil, invalidRequest("invalid brief: input exceeds %d bytes", handoff.MaxInputBytes)
	}
	return data, nil
}

func (a app) handoffList(args []string) int {
	var pf pageFlags
	fs := newFlagSet("handoff list", &pf)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return a.fail(invalidRequest("%s", flagMessage(err)))
	}
	if len(pos) != 0 {
		return a.fail(invalidRequest("unexpected argument %q", truncate(pos[0])))
	}
	svc, code := a.handoffService(pf.config)
	if svc == nil {
		return code
	}
	ctx, stop := signal.NotifyContext(a.ctx, os.Interrupt)
	defer stop()
	resp, herr := svc.List(ctx, handoff.ListRequest{Offset: pf.offset, Limit: pf.limit})
	if herr != nil {
		return a.fail(herr)
	}
	return a.emit(resp, exitOK)
}

func (a app) handoffShow(args []string) int {
	var pf pageFlags
	fs := newFlagSet("handoff show", &pf)
	format := fs.String("format", "json", "Output format: json or markdown")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return a.fail(invalidRequest("%s", flagMessage(err)))
	}
	if pf.offset != 0 || pf.limit != history.DefaultLimit {
		return a.fail(invalidRequest("handoff show does not accept --offset or --limit"))
	}
	switch {
	case len(pos) == 0:
		return a.fail(invalidRequest("missing handoff id"))
	case len(pos) > 1:
		return a.fail(invalidRequest("unexpected argument %q", truncate(pos[1])))
	}
	if *format != "json" && *format != "markdown" {
		return a.fail(invalidRequest("unknown format %q; use json or markdown", truncate(*format)))
	}
	svc, code := a.handoffService(pf.config)
	if svc == nil {
		return code
	}
	resp, herr := svc.Show(a.ctx, pos[0])
	if herr != nil {
		return a.fail(herr)
	}
	if *format == "markdown" {
		if _, err := io.WriteString(a.stdout, handoff.Render(resp.Handoff)); err != nil {
			fmt.Fprintln(a.stderr, "spool: cannot write response:", err)
			return exitOperational
		}
		return exitOK
	}
	return a.emit(resp, exitOK)
}

func errText(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
