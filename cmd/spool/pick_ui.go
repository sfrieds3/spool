package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/continuation"
	"github.com/sfrieds3/spool/internal/handoff"
	"github.com/sfrieds3/spool/internal/history"
)

type pickUI struct {
	app   app
	input *bufio.Reader
}

func (u pickUI) line(prompt string) (string, error) {
	fmt.Fprint(u.app.stderr, prompt)
	line, err := u.input.ReadSlice('\n')
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		if errors.Is(err, io.EOF) {
			return "", errPickCanceled
		}
		return "", errors.New("interactive input exceeded limit or could not be read")
	}
	return strings.TrimSpace(string(line)), nil
}
func (u pickUI) confirm(prompt string) (bool, error) {
	s, e := u.line(prompt + " Type yes to confirm: ")
	return s == "yes", e
}
func (a app) pickActions(ctx context.Context, cfg *config.Config, hist *history.Service, ref string) int {
	svc := &continuation.Service{History: hist, Sources: cfg.Sources, Drafter: continuation.PiDrafter{Sources: cfg.Sources}}
	u := pickUI{a, bufio.NewReaderSize(contextInput{ctx, a.stdin}, 16<<10)}
	selected, herr := hist.Describe(ctx, ref)
	if herr != nil {
		return a.pickFail(herr, 1)
	}
	canResume := selected.Harness == "pi" && selected.Kind == "session" && selected.Cwd != nil && continuation.CheckWorkspace(*selected.Cwd) == nil
	for {
		menu := "Actions: [i] inspect, [h] prepare handoff, [c] cancel: "
		if canResume {
			menu = "Actions: [i] inspect, [r] native Pi resume, [h] prepare handoff, [c] cancel: "
		}
		action, err := u.line(menu)
		if err != nil {
			return a.pickFail(err, exitOperational)
		}
		switch action {
		case "i", "inspect":
			page, herr := hist.Read(ctx, history.ReadRequest{Ref: ref, Limit: history.DefaultLimit})
			if herr != nil {
				return a.pickFail(herr, exitOperational)
			}
			b, _ := history.Marshal(page)
			fmt.Fprintln(a.stderr, safeInspectJSON(string(b))) // JSON escapes terminal controls.
		case "r", "resume":
			if !canResume {
				fmt.Fprintln(a.stderr, "Native resume unavailable for this mapping or recorded workspace; prepare a handoff instead.")
				continue
			}
			return u.resume(ctx, svc, ref)
		case "h", "handoff":
			return u.handoff(ctx, cfg, svc, ref)
		case "c", "cancel", "":
			return a.pickFail(errPickCanceled, exitCanceled)
		default:
			fmt.Fprintln(a.stderr, "Choose inspect, resume, handoff, or cancel.")
		}
	}
}
func (u pickUI) streams() continuation.TerminalStreams {
	return continuation.TerminalStreams{In: u.app.stdin, Out: u.app.stdout, Err: u.app.stderr}
}
func (u pickUI) resume(ctx context.Context, svc *continuation.Service, ref string) int {
	recorded, herr := svc.History.Describe(ctx, ref)
	if herr != nil {
		return u.app.pickFail(herr, 1)
	}
	cwdLabel := "unavailable"
	if recorded.Cwd != nil {
		cwdLabel = *recorded.Cwd
	}
	fmt.Fprintf(u.app.stderr, "Unverified recorded session path: %s; recorded workspace: %s.\n", continuation.CleanLabel(recorded.OriginalPath), continuation.CleanLabel(cwdLabel))
	cwd, err := u.line("Confirm existing recorded local workspace (absolute path; paths in history are unverified): ")
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	candidate, err := svc.ResolveResume(ctx, ref, cwd)
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	fmt.Fprintf(u.app.stderr, "Native Pi resume: executable %s; session %s; workspace %s. Pi may append to its own history. A residual pathname race remains after validation.\n", continuation.CleanLabel(candidate.Executable()), continuation.CleanLabel(candidate.Path()), continuation.CleanLabel(candidate.Workspace()))
	yes, err := u.confirm("Resume this exact original Pi session?")
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	if !yes {
		return u.app.pickFail(errPickCanceled, 130)
	}
	if err := svc.Resume(ctx, candidate, candidate.Confirm(), u.streams()); err != nil {
		return u.app.pickFail(err, 1)
	}
	return 0
}
func (u pickUI) handoff(ctx context.Context, cfg *config.Config, svc *continuation.Service, ref string) int {
	target, err := u.line("Fresh target harness (pi/claude/codex): ")
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	goal, err := u.line("Continuation goal: ")
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	cwd, err := u.line("Confirmed existing local workspace (absolute path; no automatic transfer): ")
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	if err := continuation.CheckWorkspace(cwd); err != nil {
		return u.app.pickFail(err, 2)
	}
	prepared, err := svc.Prepare(ctx, ref, target, goal, cfg.Drafting)
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	settings := prepared.Settings()
	fmt.Fprintf(u.app.stderr, "%s\nProvider/model: %s/%s. History may contain secrets and may leave this machine for the configured provider, incurring costs. No redaction or current-state verification is promised.\n", continuation.CleanLabel(prepared.Summary()), continuation.CleanLabel(settings.Provider), continuation.CleanLabel(settings.Model))
	yes, err := u.confirm("Transmit this exact bounded source scope to draft a brief?")
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	if !yes {
		return u.app.pickFail(errPickCanceled, 130)
	}
	draft, err := svc.Generate(ctx, prepared, prepared.ConfirmDisclosure(), cwd)
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	approval, err := u.review(ctx, cfg, draft)
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	state, err := config.StateDir(u.app.env)
	if err != nil {
		return u.app.pickFail(err, 2)
	}
	opts := u.app.handoff
	if opts.SpoolVersion == "" {
		opts.SpoolVersion = version
	}
	svc.Publisher = handoff.NewService(cfg, history.NewService(cfg), state, opts)
	published, err := svc.Publish(ctx, draft, approval)
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	fmt.Fprintf(u.app.stderr, "Saved immutable handoff %s. Manual recovery: spool handoff show --format markdown %s\n", published.ID(), published.ID())
	for _, warning := range published.Warnings() {
		b, _ := history.Marshal(warning)
		fmt.Fprintln(u.app.stderr, continuation.CleanLabel(string(b)))
	}
	action, err := u.line("[s] save only (default), [l] launch fresh target: ")
	if err != nil || action != "l" {
		return 0
	}
	plan, err := svc.PlanLaunch(published, target, cwd)
	if err != nil {
		return u.app.pickFail(err, 1)
	}
	fmt.Fprintf(u.app.stderr, "Launch fresh %s: executable %s; workspace %s; approved handoff %s. Normal harness trust and permissions apply.\n", plan.Target(), continuation.CleanLabel(plan.Executable()), continuation.CleanLabel(plan.Workspace()), published.ID())
	yes, err = u.confirm("Launch this receiver with the approved private brief file?")
	if err != nil || !yes {
		return 0
	}
	if err := svc.Launch(ctx, plan, plan.Confirm(), u.streams()); err != nil {
		return u.app.pickFail(err, 1)
	}
	return 0
}
func (u pickUI) review(ctx context.Context, cfg *config.Config, d *continuation.Draft) (continuation.Approval, error) {
	for {
		fmt.Fprintln(u.app.stderr, d.Render())
		action, err := u.line("Unverified draft: [e] edit, [a] approve these exact bytes for publication, [c] cancel: ")
		if err != nil {
			return continuation.Approval{}, err
		}
		switch action {
		case "a", "approve":
			return d.Approve(), nil
		case "c", "cancel", "":
			return continuation.Approval{}, errPickCanceled
		case "e", "edit":
			if err := u.edit(ctx, cfg, d); err != nil {
				return continuation.Approval{}, err
			}
		default:
			fmt.Fprintln(u.app.stderr, "Choose edit, approve, or cancel.")
		}
	}
}

// contextInput polls the terminal descriptor without background stdin readers.
// Cancellation leaves no competing goroutine or read to consume later input.
type contextInput struct {
	ctx   context.Context
	input io.Reader
}

func (r contextInput) Read(p []byte) (int, error) {
	for {
		if err := r.ctx.Err(); err != nil {
			return 0, err
		}
		file, ok := r.input.(*os.File)
		if !ok {
			return r.input.Read(p)
		}
		ready, err := pollInput(int(file.Fd()))
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return 0, err
		}
		if ready {
			if err := r.ctx.Err(); err != nil {
				return 0, err
			}
			return file.Read(p)
		}
	}
}

// JSON already escapes C0; additionally escape invisible terminal/bidi controls
// and line separators while retaining one valid bounded JSON object.
func safeInspectJSON(s string) string {
	var b strings.Builder
	for _, r := range s {
		if continuation.CleanLabel(string(r)) == "" {
			if r <= 0xffff {
				fmt.Fprintf(&b, "\\u%04x", r)
			} else {
				b.WriteRune(r)
			}
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
