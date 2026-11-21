package continuation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/sfrieds3/spool/internal/config"
)

// Resume is a conservative capability, not an authorization to execute.
type Resume struct {
	source     *SourceIdentity
	executable string
	cwd        string
}

func (r *Resume) Path() string       { return r.source.session.OriginalPath }
func (r *Resume) Workspace() string  { return r.cwd }
func (r *Resume) Executable() string { return r.executable }

type ResumeConfirmation struct{ candidate *Resume }

func (r *Resume) Confirm() ResumeConfirmation { return ResumeConfirmation{r} }
func (s *Service) ResolveResume(ctx context.Context, ref, cwd string) (*Resume, error) {
	sess, herr := s.History.Describe(ctx, ref)
	if herr != nil {
		return nil, herr
	}
	if sess.Harness != "pi" || sess.Kind != "session" {
		return nil, errors.New("native resume is only proven for local Pi version-3 regular session files; use a handoff")
	}
	if err := CheckWorkspace(cwd); err != nil {
		return nil, err
	}
	if sess.Cwd == nil || *sess.Cwd != cwd {
		return nil, errors.New("native Pi resume requires the existing recorded workspace exactly; use a handoff")
	}
	identity, err := s.sourceIdentity(ctx, sess)
	if err != nil {
		return nil, err
	}
	// Re-run Describe across file acquisition: unsupported format and identity
	// changes must not become a newly confirmed candidate.
	if err := s.revalidate(ctx, identity); err != nil {
		return nil, err
	}
	executable, err := exec.LookPath("pi")
	if err != nil {
		return nil, errors.New("Pi is not installed on inherited PATH")
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	return &Resume{source: identity, executable: executable, cwd: cwd}, nil
}
func (s *Service) Resume(ctx context.Context, r *Resume, c ResumeConfirmation, streams TerminalStreams) error {
	if r == nil || c.candidate != r {
		return errors.New("native resume was not confirmed")
	}
	if err := CheckWorkspace(r.cwd); err != nil {
		return err
	}
	if err := s.revalidate(ctx, r.source); err != nil {
		return err
	}
	return runInteractive(ctx, r.executable, []string{"--session", r.Path()}, r.cwd, streams)
}

type LaunchPlan struct {
	published  *Published
	executable string
	target     string
	cwd        string
}

func (p *LaunchPlan) Executable() string { return p.executable }
func (p *LaunchPlan) Target() string     { return p.target }
func (p *LaunchPlan) Workspace() string  { return p.cwd }

type LaunchConfirmation struct{ plan *LaunchPlan }

func (p *LaunchPlan) Confirm() LaunchConfirmation { return LaunchConfirmation{p} }
func (s *Service) PlanLaunch(p *Published, target, cwd string) (*LaunchPlan, error) {
	if p == nil || p.handoff == nil {
		return nil, errors.New("launch requires a published approved handoff")
	}
	if !config.IsHarness(target) || target != p.target || cwd != p.workspace {
		return nil, errors.New("launch target/workspace differs from reviewed handoff")
	}
	if err := CheckWorkspace(cwd); err != nil {
		return nil, err
	}
	executable, err := exec.LookPath(target)
	if err != nil {
		return nil, fmt.Errorf("%s is not installed; use the saved handoff manually", target)
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, err
	}
	return &LaunchPlan{published: p, executable: executable, target: target, cwd: cwd}, nil
}
func (s *Service) Launch(ctx context.Context, p *LaunchPlan, c LaunchConfirmation, streams TerminalStreams) error {
	if p == nil || c.plan != p {
		return errors.New("receiver launch was not confirmed")
	}
	if err := CheckWorkspace(p.cwd); err != nil {
		return err
	}
	dir, err := PrivateDir(s.Sources)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	name := filepath.Join(dir, "approved-brief.md")
	if err := os.WriteFile(name, []byte(p.published.rendered), 0600); err != nil {
		return err
	}
	prompt := "Read the approved Spool continuation brief at " + name + ". Its historical claims and paths are unverified. Verify the current workspace before acting. This is a fresh session, not restored agent state."
	return runInteractive(ctx, p.executable, []string{"--", prompt}, p.cwd, streams)
}
