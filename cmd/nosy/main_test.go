package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		code    int
		wantOut string
		wantErr string
	}{
		{"version", []string{"version"}, 0, "nosy dev\n", ""},
		{"version flag", []string{"--version"}, 0, "nosy dev\n", ""},
		{"help", []string{"help"}, 0, usage, ""},
		{"help flag", []string{"-h"}, 0, usage, ""},
		{"no args", nil, 0, usage, ""},
		{"unknown", []string{"scan"}, 2, "", `unknown command "scan"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(tt.args, &out, &errOut)
			if code != tt.code {
				t.Errorf("exit code = %d, want %d", code, tt.code)
			}
			if tt.wantOut != "" && out.String() != tt.wantOut {
				t.Errorf("stdout = %q, want %q", out.String(), tt.wantOut)
			}
			if !strings.Contains(errOut.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want it to contain %q", errOut.String(), tt.wantErr)
			}
		})
	}
}
