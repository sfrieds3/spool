package config

import "path/filepath"

// Built-in source identifiers.
const (
	DefaultPiID           = "pi"
	DefaultClaudeID       = "claude"
	DefaultCodexID        = "codex"
	DefaultCodexArchiveID = "codex-archive"
)

// DefaultSources resolves the native history roots of the executing host:
//
//   - pi: $PI_CODING_AGENT_SESSION_DIR, else $PI_CODING_AGENT_DIR/sessions,
//     else ~/.pi/agent/sessions
//   - claude: $CLAUDE_CONFIG_DIR/projects, else ~/.claude/projects
//   - codex: $CODEX_HOME/sessions, else ~/.codex/sessions
//   - codex-archive: $CODEX_HOME/archived_sessions, else ~/.codex/archived_sessions
//
// Environment-selected paths must be absolute. The home directory is only
// required when a source falls back to it.
func DefaultSources(env Env) ([]Source, error) {
	var homeErr error
	home := ""
	needHome := func() (string, error) {
		if home == "" && homeErr == nil {
			home, homeErr = env.home()
			if homeErr != nil {
				homeErr = invalid("cannot determine home directory: %v", homeErr)
			}
		}
		return home, homeErr
	}

	piRoot, err := env.absEnv("PI_CODING_AGENT_SESSION_DIR")
	if err != nil {
		return nil, err
	}
	if piRoot == "" {
		dir, err := env.absEnv("PI_CODING_AGENT_DIR")
		if err != nil {
			return nil, err
		}
		if dir == "" {
			h, err := needHome()
			if err != nil {
				return nil, err
			}
			dir = filepath.Join(h, ".pi", "agent")
		}
		piRoot = filepath.Join(dir, "sessions")
	}

	claudeDir, err := env.absEnv("CLAUDE_CONFIG_DIR")
	if err != nil {
		return nil, err
	}
	if claudeDir == "" {
		h, err := needHome()
		if err != nil {
			return nil, err
		}
		claudeDir = filepath.Join(h, ".claude")
	}

	codexHome, err := env.absEnv("CODEX_HOME")
	if err != nil {
		return nil, err
	}
	if codexHome == "" {
		h, err := needHome()
		if err != nil {
			return nil, err
		}
		codexHome = filepath.Join(h, ".codex")
	}

	mk := func(id, harness, root string) Source {
		return Source{ID: id, Harness: harness, Root: root, Enabled: true, Default: true}
	}
	return []Source{
		mk(DefaultPiID, HarnessPi, piRoot),
		mk(DefaultClaudeID, HarnessClaude, filepath.Join(claudeDir, "projects")),
		mk(DefaultCodexID, HarnessCodex, filepath.Join(codexHome, "sessions")),
		mk(DefaultCodexArchiveID, HarnessCodex, filepath.Join(codexHome, "archived_sessions")),
	}, nil
}
