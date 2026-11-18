package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/continuation"
	"github.com/sfrieds3/spool/internal/history"
)

const exitCanceled = 130

var errPickCanceled = errors.New("canceled")

func terminalStreams(in io.Reader, out io.Writer) bool {
	i, ok := in.(interface{ Fd() uintptr })
	if !ok {
		return false
	}
	o, ok := out.(interface{ Fd() uintptr })
	return ok && terminalFD(i.Fd()) && terminalFD(o.Fd())
}
func (a app) pickFail(err error, code int) int {
	if errors.Is(err, context.Canceled) || errors.Is(err, errPickCanceled) {
		code = exitCanceled
	}
	var he *history.Error
	if errors.As(err, &he) {
		code = exitCode(he.Code)
		if he.Code == history.CodeCanceled {
			code = exitCanceled
		}
	}
	fmt.Fprintln(a.stderr, "spool pick:", continuation.CleanLabel(err.Error()))
	return code
}
func (a app) pick(args []string) int {
	fs := flag.NewFlagSet("spool pick", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	path := fs.String("config", "", "Configuration file")
	harness := fs.String("harness", "", "Filter pi, claude, or codex")
	refOnly := fs.Bool("ref", false, "Print only a revalidated selected reference")
	fs.Usage = func() {
		fmt.Fprintln(a.stderr, "Usage: spool pick [--config FILE] [--harness pi|claude|codex] [--ref]\nEnter selects an action: inspect, native resume, or prepare a reviewed handoff.\nRequires terminal stdin/stdout and optional fzf on inherited PATH. UI uses stderr; --ref emits one ref.\nDrafting needs configured provider/model and Pi, with separate disclosure confirmation.\nLaunch/resume requires separate confirmation and inherits terminal streams.\nExit: 0 completed/save-only, 1 failure/empty/no-match, 2 invalid/config/non-TTY, 3 missing session, 130 cancel.")
		fs.PrintDefaults()
	}
	pos, err := parseArgs(fs, args)
	if err == flag.ErrHelp {
		return 0
	}
	if err != nil {
		return a.pickFail(err, exitInvalid)
	}
	if len(pos) != 0 || *harness != "" && !config.IsHarness(*harness) {
		return a.pickFail(errors.New("unexpected arguments or unsupported harness"), exitInvalid)
	}
	cfg, err := config.Load(*path, a.env)
	if err != nil {
		return a.pickFail(err, exitInvalid)
	}
	terminal := a.terminal
	if terminal == nil {
		terminal = terminalStreams
	}
	if !terminal(a.stdin, a.stdout) {
		return a.pickFail(errors.New("stdin and stdout must independently be terminals (macOS/Linux)"), exitInvalid)
	}
	fzf, err := exec.LookPath("fzf")
	if err != nil {
		return a.pickFail(errors.New("fzf is not installed on inherited PATH"), exitOperational)
	}
	ctx, stop := signal.NotifyContext(a.ctx, os.Interrupt)
	defer stop()
	hist := history.NewService(cfg)
	rows, err := continuation.Catalog(ctx, hist, *harness, func(d history.Diagnostic) {
		b, _ := history.Marshal(d)
		fmt.Fprintln(a.stderr, continuation.CleanLabel(string(b)))
	})
	if err != nil {
		return a.pickFail(err, exitOperational)
	}
	if len(rows) == 0 {
		return a.pickFail(errors.New("no sessions available"), exitOperational)
	}
	previewPath := ""
	if *path != "" {
		previewPath, err = filepath.Abs(*path)
		if err != nil {
			return a.pickFail(err, exitInvalid)
		}
	}
	ref, err := a.chooseSession(ctx, fzf, rows, previewPath)
	if err != nil {
		return a.pickFail(err, exitOperational)
	}
	if _, herr := hist.Describe(ctx, ref); herr != nil {
		return a.pickFail(herr, exitOperational)
	}
	if *refOnly {
		if _, err := fmt.Fprintln(a.stdout, ref); err != nil {
			return a.pickFail(err, exitOperational)
		}
		return 0
	}
	return a.pickActions(ctx, cfg, hist, ref)
}
func sessionRow(s history.Session) string {
	field := func(p *string) string {
		if p == nil {
			return ""
		}
		return continuation.CleanLabel(*p)
	}
	started := ""
	if s.StartedAt != nil {
		started = continuation.CleanLabel(s.StartedAt.Value)
	}
	return strings.Join([]string{s.Ref, continuation.CleanLabel(s.Harness), started, field(s.Cwd), field(s.Title)}, "\t")
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func previewCommand(executable, path string) string {
	cmd := shellQuote(executable) + " __pick-preview"
	if path != "" {
		cmd += " --config " + shellQuote(path)
	}
	return cmd + " -- {1}"
}

// pickPreview keeps the shared read contract intact while escaping controls for
// terminal presentation. This private entry point is used only by fzf previews.
func (a app) pickPreview(args []string) int {
	var output bytes.Buffer
	reader := a
	reader.stdout = &output
	code := reader.read(args)
	if _, err := fmt.Fprintln(a.stdout, safeInspectJSON(strings.TrimSuffix(output.String(), "\n"))); err != nil {
		return exitOperational
	}
	return code
}

func fzfEnvironment() []string {
	var env []string
	for _, e := range os.Environ() {
		key, _, _ := strings.Cut(e, "=")
		switch key {
		case "FZF_DEFAULT_OPTS", "FZF_DEFAULT_OPTS_FILE", "FZF_DEFAULT_COMMAND", "SHELL":
			continue
		}
		env = append(env, e)
	}
	return append(env, "FZF_DEFAULT_OPTS=", "FZF_DEFAULT_OPTS_FILE=", "FZF_DEFAULT_COMMAND=", "SHELL=/bin/sh")
}

// pickCapture bounds an entire returned row, not merely its ref prefix.
type pickCapture struct {
	buf      bytes.Buffer
	overflow bool
	cancel   context.CancelFunc
}

func (b *pickCapture) Write(p []byte) (int, error) {
	const max = 64 << 10
	if len(p) > max-b.buf.Len() {
		b.overflow = true
		if b.cancel != nil {
			b.cancel()
		}
		return 0, errors.New("fzf selection exceeds limit")
	}
	return b.buf.Write(p)
}
func (a app) chooseSession(ctx context.Context, fzf string, sessions []history.Session, path string) (string, error) {
	executable, err := os.Executable()
	if err != nil {
		return "", err
	}
	offered := map[string]string{}
	var input strings.Builder
	for _, s := range sessions {
		row := sessionRow(s)
		offered[row] = s.Ref
		input.WriteString(row)
		input.WriteByte('\n')
	}
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(childCtx, fzf, "--no-multi", "--no-ansi", "--delimiter=\t", "--with-nth=2..", "--nth=2..", "--preview", previewCommand(executable, path), "--header=Untrusted history: Enter chooses an action; Esc cancels")
	cmd.Env = fzfEnvironment()
	cmd.Stdin = strings.NewReader(input.String())
	cmd.Stderr = a.stderr
	cmd.WaitDelay = time.Second
	out := &pickCapture{cancel: cancel}
	cmd.Stdout = out
	err = cmd.Run()
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if out.overflow {
		return "", errors.New("fzf selection exceeds limit")
	}
	if err != nil {
		var e *exec.ExitError
		if errors.As(err, &e) && e.ExitCode() == 130 {
			return "", errPickCanceled
		}
		return "", errors.New("fzf failed or no match")
	}
	selected := strings.TrimSuffix(out.buf.String(), "\n")
	ref, ok := offered[selected]
	if !ok || strings.ContainsAny(selected, "\r\n") {
		return "", errors.New("fzf returned an altered, multiple, or unoffered row")
	}
	if _, err := history.DecodeRef(ref); err != nil {
		return "", err
	}
	return ref, nil
}
