package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDir(t *testing.T) {
	tmp := t.TempDir()
	configDir := tmp
	configFile := filepath.Join(configDir, FileName)
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
			workdir: filepath.Join(t.TempDir(), "nested"),
			wantErr: ErrNotFound,
		},
		{
			name:     "explicit dir missing file",
			explicit: filepath.Join(tmp, "empty"),
			workdir:  tmp,
			wantErr:  ErrInvalid,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.envValue != "" {
				t.Setenv(EnvVar, tt.envValue)
			}
			if tt.explicit != "" {
				if err := os.MkdirAll(tt.explicit, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tt.workdir != "" {
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
