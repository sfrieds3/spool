package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		code       int
		wantStdout string
		wantStderr string
	}{
		{name: "default help", wantStdout: "spool search"},
		{name: "explicit help", args: []string{"--help"}, wantStderr: "Usage: spool"},
		{name: "version", args: []string{"--version"}, wantStdout: "spool dev\n"},
		{name: "unknown flag", args: []string{"--unknown"}, code: 2, wantStderr: "flag provided but not defined"},
		{name: "unknown command", args: []string{"unknown-command"}, code: 2, wantStderr: "unexpected argument"},
		{name: "version with extra argument", args: []string{"--version", "extra"}, code: 2, wantStderr: "unexpected argument"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := run(tt.args, &stdout, &stderr); code != tt.code {
				t.Fatalf("exit code = %d, want %d", code, tt.code)
			}
			for _, output := range []struct {
				name string
				got  string
				want string
			}{
				{"stdout", stdout.String(), tt.wantStdout},
				{"stderr", stderr.String(), tt.wantStderr},
			} {
				if output.want == "" && output.got != "" {
					t.Errorf("%s = %q, want empty output", output.name, output.got)
				} else if !strings.Contains(output.got, output.want) {
					t.Errorf("%s = %q, want substring %q", output.name, output.got, output.want)
				}
			}
		})
	}
}
