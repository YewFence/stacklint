package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDir(t *testing.T) {
	tmp := t.TempDir()
	configDir := filepath.Join(tmp, DirName)
	configFile := filepath.Join(configDir, FileName)
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configFile, []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name     string
		explicit string
		envValue string
		workdir  string
		want     string
		wantErr  error
	}{
		{
			name:     "explicit flag",
			explicit: configDir,
			workdir:  tmp,
			want:     configDir,
		},
		{
			name:     "env var",
			envValue: configDir,
			workdir:  tmp,
			want:     configDir,
		},
		{
			name:    "upward search from subdir",
			workdir: filepath.Join(tmp, "sub", "deep"),
			want:    configDir,
		},
		{
			name:     "flag takes precedence over env",
			explicit: configDir,
			envValue: "/nonexistent",
			workdir:  tmp,
			want:     configDir,
		},
		{
			name:    "not found",
			workdir: t.TempDir(),
			wantErr: ErrNotFound,
		},
		{
			name:     "explicit dir missing file",
			explicit: tmp,
			workdir:  tmp,
			wantErr:  ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(EnvVar, tt.envValue)
			}
			if tt.workdir != "" && tt.workdir != tmp {
				if err := os.MkdirAll(tt.workdir, 0o755); err != nil {
					t.Fatal(err)
				}
			}

			got, err := ResolveDir(tt.explicit, tt.workdir)
			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("expected error %v, got nil", tt.wantErr)
				}
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("expected error containing %v, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if !filepath.IsAbs(got) {
				t.Errorf("result is not absolute: %q", got)
			}
		})
	}
}
