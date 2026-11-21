package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/sfrieds3/spool/internal/config"
	"github.com/sfrieds3/spool/internal/continuation"
	"github.com/sfrieds3/spool/internal/handoff"
	"github.com/sfrieds3/spool/internal/safefile"
)

// editorArgv understands quoting/escaping only. No shell, substitution, glob,
// redirection, environment interpolation, or command template is evaluated.
func editorArgv(text string) ([]string, error) {
	var args []string
	var token strings.Builder
	var quote rune
	escaped, started := false, false
	for _, r := range text {
		if escaped {
			token.WriteRune(r)
			escaped = false
			started = true
			continue
		}
		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}
		if quote != 0 {
			if r == quote {
				quote = 0
			} else {
				token.WriteRune(r)
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			started = true
			continue
		}
		if unicode.IsSpace(r) {
			if started {
				args = append(args, token.String())
				token.Reset()
				started = false
			}
			continue
		}
		token.WriteRune(r)
		started = true
	}
	if escaped || quote != 0 {
		return nil, errors.New("VISUAL/EDITOR has unterminated quoting")
	}
	if started {
		args = append(args, token.String())
	}
	if len(args) == 0 || args[0] == "" {
		return nil, errors.New("set a trusted VISUAL or EDITOR to edit; approve/cancel remain available")
	}
	return args, nil
}
func (u pickUI) edit(ctx context.Context, cfg *config.Config, d *continuation.Draft) error {
	choice := u.app.env.Getenv("VISUAL")
	if choice == "" {
		choice = u.app.env.Getenv("EDITOR")
	}
	args, err := editorArgv(choice)
	if err != nil {
		return err
	}
	executable, err := exec.LookPath(args[0])
	if err != nil {
		return errors.New("configured editor is not available on inherited PATH")
	}
	dir, err := continuation.PrivateDir(cfg.Sources)
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	root, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer root.Close()
	f, err := root.OpenFile("draft.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(d.Bytes())
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	cmd := exec.CommandContext(ctx, executable, append(args[1:], filepath.Join(dir, "draft.json"))...)
	cmd.Stdin = u.app.stdin
	cmd.Stdout = u.app.stderr
	cmd.Stderr = u.app.stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("editor failed; no handoff published")
	}
	f, info, err := safefile.OpenRegular(root, "draft.json")
	if err != nil {
		return errors.New("edited draft must remain a regular non-symlink file in its private directory")
	}
	defer f.Close()
	if info.Mode().Perm()&0077 != 0 {
		return errors.New("edited draft became accessible to other users")
	}
	data, err := io.ReadAll(io.LimitReader(f, handoff.MaxInputBytes+1))
	if err != nil {
		return err
	}
	return d.Replace(data)
}
