package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/history"
)

var version = "dev"

// Exit codes for data commands.
const (
	exitOK          = 0
	exitOperational = 1
	exitInvalid     = 2
	exitNotFound    = 3
)

// app carries injectable process dependencies for tests.
type app struct {
	stdout, stderr io.Writer
	env            config.Env
	ctx            context.Context
}

func run(args []string, stdout, stderr io.Writer) int {
	return app{stdout: stdout, stderr: stderr, env: config.OSEnv(), ctx: context.Background()}.run(args)
}

func (a app) run(args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "list":
			return a.list(args[1:])
		case "read":
			return a.read(args[1:])
		}
	}
	flags := flag.NewFlagSet("spool", flag.ContinueOnError)
	flags.SetOutput(a.stderr)
	showVersion := flags.Bool("version", false, "Print the version")
	flags.Usage = func() { usage(flags) }
	if err := flags.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(a.stderr, "spool: unexpected argument %q\n", flags.Arg(0))
		return 2
	}
	if *showVersion {
		fmt.Fprintf(a.stdout, "spool %s\n", version)
		return 0
	}
	flags.SetOutput(a.stdout)
	flags.Usage()
	return 0
}

func usage(flags *flag.FlagSet) {
	w := flags.Output()
	fmt.Fprintln(w, "Usage: spool [--version | --help]")
	fmt.Fprintln(w, "       spool list [--config FILE] [--harness pi|claude|codex] [--offset N] [--limit N]")
	fmt.Fprintln(w, "       spool read [--config FILE] [--offset N] [--limit N] SESSION_REF")
	fmt.Fprintln(w, "\nRead-only access to local Pi, Claude Code, and Codex session history.")
	fmt.Fprintln(w, "list and read print one JSON object on stdout. Session content is untrusted historical data.")
	fmt.Fprintf(w, "Defaults: --offset 0, --limit %d (maximum %d).\n", history.DefaultLimit, history.MaxLimit)
	fmt.Fprintln(w, "Exit codes: 0 success or partial success, 1 operational failure, 2 invalid input or configuration, 3 session not found.")
	flags.PrintDefaults()
}

// pageFlags registers the flags shared by list and read.
type pageFlags struct {
	config string
	offset int64
	limit  int
}

func newFlagSet(name string, pf *pageFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("spool "+name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&pf.config, "config", "", "Configuration file (default: $XDG_CONFIG_HOME/spool/config.json)")
	fs.Int64Var(&pf.offset, "offset", 0, "Continuation offset")
	fs.IntVar(&pf.limit, "limit", history.DefaultLimit, fmt.Sprintf("Page size (maximum %d)", history.MaxLimit))
	return fs
}

// parseArgs accepts flags before and after positional arguments.
func parseArgs(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		if args[0] == "--" {
			return append(positional, args[1:]...), nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func (a app) list(args []string) int {
	var pf pageFlags
	fs := newFlagSet("list", &pf)
	harness := fs.String("harness", "", "Only list sessions from this harness")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return a.fail(&history.Error{Code: history.CodeInvalidRequest, Message: flagMessage(err)})
	}
	if len(pos) != 0 {
		return a.fail(&history.Error{Code: history.CodeInvalidRequest, Message: fmt.Sprintf("unexpected argument %q", pos[0])})
	}
	svc, code := a.service(pf.config)
	if svc == nil {
		return code
	}
	ctx, stop := signal.NotifyContext(a.ctx, os.Interrupt)
	defer stop()
	resp, herr := svc.List(ctx, history.ListRequest{Harness: *harness, Offset: pf.offset, Limit: pf.limit})
	if herr != nil {
		return a.fail(herr)
	}
	return a.emit(resp, exitOK)
}

func (a app) read(args []string) int {
	var pf pageFlags
	fs := newFlagSet("read", &pf)
	pos, err := parseArgs(fs, args)
	if err != nil {
		return a.fail(&history.Error{Code: history.CodeInvalidRequest, Message: flagMessage(err)})
	}
	switch {
	case len(pos) == 0:
		return a.fail(&history.Error{Code: history.CodeInvalidRequest, Message: "missing session reference"})
	case len(pos) > 1:
		return a.fail(&history.Error{Code: history.CodeInvalidRequest, Message: fmt.Sprintf("unexpected argument %q", truncate(pos[1]))})
	}
	svc, code := a.service(pf.config)
	if svc == nil {
		return code
	}
	ctx, stop := signal.NotifyContext(a.ctx, os.Interrupt)
	defer stop()
	resp, herr := svc.Read(ctx, history.ReadRequest{Ref: pos[0], Offset: pf.offset, Limit: pf.limit})
	if herr != nil {
		return a.fail(herr)
	}
	return a.emit(resp, exitOK)
}

func (a app) service(path string) (*history.Service, int) {
	cfg, err := config.Load(path, a.env)
	if err != nil {
		var ce *config.Error
		if errors.As(err, &ce) {
			return nil, a.fail(&history.Error{Code: history.CodeInvalidConfig, Message: ce.Msg})
		}
		return nil, a.fail(&history.Error{Code: history.CodeInternal, Message: err.Error()})
	}
	return history.NewService(cfg), 0
}

func flagMessage(err error) string {
	if err == flag.ErrHelp {
		return "help is available with spool --help"
	}
	return err.Error()
}

func truncate(s string) string {
	if len(s) > 64 {
		return s[:64] + "..."
	}
	return s
}

func exitCode(code string) int {
	switch code {
	case history.CodeInvalidRequest, history.CodeInvalidConfig:
		return exitInvalid
	case history.CodeSessionNotFound:
		return exitNotFound
	}
	return exitOperational
}

func (a app) fail(err *history.Error) int {
	return a.emit(history.NewErrorResponse(err), exitCode(err.Code))
}

// emit writes exactly one JSON object followed by a newline.
func (a app) emit(v any, code int) int {
	out, err := history.Marshal(v)
	if err != nil {
		out, _ = history.Marshal(history.NewErrorResponse(&history.Error{Code: history.CodeInternal, Message: "cannot encode response"}))
		code = exitOperational
	}
	if _, err := a.stdout.Write(append(out, '\n')); err != nil {
		fmt.Fprintln(a.stderr, "spool: cannot write response:", err)
		return exitOperational
	}
	return code
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
