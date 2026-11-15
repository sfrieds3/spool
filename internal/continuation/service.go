package continuation

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"reflect"
	"unicode/utf8"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/handoff"
	"github.com/sfrieds3/spool/internal/history"
	"github.com/sfrieds3/spool/internal/strictjson"
)

type Publisher interface {
	Create(context.Context, []byte) (*handoff.CreateResponse, *history.Error)
}
type Drafter interface {
	Draft(context.Context, *Prepared) ([]byte, error)
}
type Service struct {
	History   History
	Publisher Publisher
	Drafter   Drafter
	Sources   []config.Source
}

// Disclosure can only be constructed explicitly for an unchanged prepared scope.
type Disclosure struct {
	prepared *Prepared
	digest   [32]byte
}

func (p *Prepared) ConfirmDisclosure() Disclosure { return Disclosure{p, p.digest} }

type content struct {
	Title       *string   `json:"title"`
	Goal        *string   `json:"goal"`
	Constraints *[]string `json:"constraints"`
	Decisions   *[]string `json:"decisions"`
	Progress    *[]string `json:"progress"`
	Outstanding *[]string `json:"outstanding"`
	Notes       *[]string `json:"notes"`
}

type Draft struct {
	prepared     *Prepared
	data         []byte
	trusted      *handoff.Brief
	published    bool
	trustedNotes []string
}

func (d *Draft) Bytes() []byte { return bytes.Clone(d.data) }
func (d *Draft) Render() string {
	b, err := handoff.ParseBrief(d.data)
	if err != nil || b.WorkspacePath == nil {
		return "Invalid unpublished draft; approval unavailable."
	}
	return handoff.Render(&handoff.Handoff{ID: "unpublished-draft", Title: b.Title, Goal: b.Goal, Constraints: b.Constraints, Decisions: b.Decisions, Progress: b.Progress, Outstanding: b.Outstanding, Notes: b.Notes, Workspace: handoff.Workspace{Path: *b.WorkspacePath}, Sessions: []handoff.SessionSnapshot{{Ref: d.prepared.Ref(), Harness: d.prepared.data.Session.Harness, OriginalPath: d.prepared.data.Session.OriginalPath, Cwd: d.prepared.data.Session.Cwd}}})
}

// Generate requires disclosure; it does not publish or launch.
func (s *Service) Generate(ctx context.Context, p *Prepared, c Disclosure, workspace string) (*Draft, error) {
	if p == nil || c.prepared != p || c.digest != sha256.Sum256(p.input) {
		return nil, errors.New("missing or stale disclosure confirmation")
	}
	if err := CheckWorkspace(workspace); err != nil {
		return nil, err
	}
	if s.Drafter == nil {
		return nil, errors.New("Pi drafting adapter unavailable")
	}
	// Fail provenance/schema feasibility before any paid inference.
	if _, err := assemble(p, []byte(`{"title":null,"goal":"Draft pending","constraints":[],"decisions":[],"progress":[],"outstanding":[],"notes":[]}`), workspace); err != nil {
		return nil, err
	}
	request := *p
	request.workspace = workspace
	output, err := s.Drafter.Draft(ctx, &request)
	if err != nil {
		return nil, err
	}
	return assemble(p, output, workspace)
}
func assemble(p *Prepared, output []byte, workspace string) (*Draft, error) {
	if len(output) > handoff.MaxInputBytes || !utf8.Valid(output) {
		return nil, errors.New("draft output exceeds bounds or is not UTF-8")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(output, &fields); err != nil {
		return nil, errors.New("draft must be one JSON object")
	}
	if _, ok := fields["title"]; !ok {
		return nil, errors.New("draft is missing title field")
	}
	var c content
	if err := strictjson.CheckDuplicateKeys(output); err != nil {
		return nil, errors.New("duplicate draft JSON fields")
	}
	if err := strictjson.Decode(output, &c); err != nil {
		return nil, errors.New("invalid draft content JSON (unknown or malformed fields)")
	}
	if c.Goal == nil || c.Constraints == nil || c.Decisions == nil || c.Progress == nil || c.Outstanding == nil || c.Notes == nil {
		return nil, errors.New("draft is missing required content fields")
	}
	notes := append([]string(nil), (*c.Notes)...)
	trustedNotes := provenanceNotes(p)
	notes = append(notes, trustedNotes...)
	v := map[string]any{"version": 1, "title": c.Title, "goal": c.Goal, "constraints": c.Constraints, "decisions": c.Decisions, "progress": c.Progress, "outstanding": c.Outstanding, "notes": notes, "workspace": map[string]any{"path": workspace}, "sessions": []map[string]string{{"ref": p.Ref()}}}
	data, _ := json.Marshal(v)
	b, herr := handoff.ParseBrief(data)
	if herr != nil {
		return nil, herr
	}
	return &Draft{prepared: p, data: data, trusted: b, trustedNotes: trustedNotes}, nil
}

// Replace validates editor bytes and pins application-owned source/workspace/scope.
func (d *Draft) Replace(data []byte) error {
	if d == nil || d.prepared == nil || d.published {
		return errors.New("draft is unavailable or already published")
	}
	b, err := handoff.ParseBrief(data)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(b.Sessions, d.trusted.Sessions) || !reflect.DeepEqual(b.WorkspacePath, d.trusted.WorkspacePath) || !reflect.DeepEqual(b.Repository, d.trusted.Repository) {
		return errors.New("edited application-owned source/workspace metadata")
	}
	n := d.trustedNotes
	if len(b.Notes) < len(n) || !reflect.DeepEqual(b.Notes[len(b.Notes)-len(n):], n) {
		return errors.New("edited application-owned drafting/scope notes")
	}
	d.data = bytes.Clone(data)
	return nil
}

type Approval struct {
	draft  *Draft
	digest [32]byte
	scope  [32]byte
}

func (d *Draft) Approve() Approval {
	if d == nil || d.prepared == nil || d.published {
		return Approval{}
	}
	return Approval{d, sha256.Sum256(d.data), d.prepared.digest}
}

type Published struct {
	handoff   *handoff.Handoff
	warnings  []history.Diagnostic
	target    string
	rendered  string
	workspace string
}

func (p *Published) ID() string { return p.handoff.ID }
func (p *Published) Warnings() []history.Diagnostic {
	return append([]history.Diagnostic(nil), p.warnings...)
}
func (p *Published) Target() string    { return p.target }
func (p *Published) Workspace() string { return p.workspace }
func (p *Published) Rendered() string  { return p.rendered }

func (s *Service) Publish(ctx context.Context, d *Draft, a Approval) (*Published, error) {
	if d == nil || d.prepared == nil || d.published || a.draft != d || a.digest != sha256.Sum256(d.data) || a.scope != d.prepared.digest {
		return nil, errors.New("missing or stale exact-byte approval")
	}
	if err := s.revalidate(ctx, d.prepared.source); err != nil {
		return nil, err
	}
	if s.Publisher == nil {
		return nil, errors.New("handoff publisher unavailable")
	}
	resp, err := s.Publisher.Create(ctx, bytes.Clone(d.data))
	if err != nil {
		return nil, err
	}
	d.published = true
	return &Published{handoff: resp.Handoff, warnings: resp.Warnings, target: d.prepared.Target(), rendered: handoff.Render(resp.Handoff), workspace: resp.Handoff.Workspace.Path}, nil
}

// provenanceNotes preserves exact coverage without exceeding version-1 item
// limits. The entire suffix remains application-owned across editor changes.
func provenanceNotes(p *Prepared) []string {
	var notes []string
	for _, text := range []string{"Model-drafted with " + p.settings.Provider + "/" + p.settings.Model + "; published only after explicit user approval. All generated assertions and recorded paths are unverified.", p.coverage()} {
		for len(text) > handoff.MaxItemBytes {
			cut := handoff.MaxItemBytes
			for !utf8.RuneStart(text[cut]) {
				cut--
			}
			notes = append(notes, text[:cut])
			text = text[cut:]
		}
		if text != "" {
			notes = append(notes, text)
		}
	}
	return notes
}
