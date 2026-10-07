package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	FileName = "stacklint.toml"
	EnvVar   = "STACKLINT_CONFIG_DIR"
)

var (
	ErrNotFound = errors.New("stacklint configuration directory not found")
	ErrInvalid  = errors.New("specified directory does not contain stacklint.toml")
)

// ResolveDir locates the configuration directory, returning an absolute path.
// Precedence: explicit dir (if non-empty) > STACKLINT_CONFIG_DIR env > upward search from workdir.
// Explicit directories must contain stacklint.toml; upward search stops at the first directory
// that directly contains stacklint.toml (typically the repository root).
func ResolveDir(explicit string, workdir string) (string, error) {
	if explicit != "" {
		abs, err := filepath.Abs(explicit)
		if err != nil {
			return "", fmt.Errorf("resolve explicit config dir: %w", err)
		}
		if err := validate(abs); err != nil {
			return "", fmt.Errorf("%w: %s", ErrInvalid, abs)
		}
		return abs, nil
	}

	if env := os.Getenv(EnvVar); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", fmt.Errorf("resolve %s: %w", EnvVar, err)
		}
		if err := validate(abs); err != nil {
			return "", fmt.Errorf("%w from %s: %s", ErrInvalid, EnvVar, abs)
		}
		return abs, nil
	}

	abs, err := filepath.Abs(workdir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	return search(abs)
}

func validate(dir string) error {
	path := filepath.Join(dir, FileName)
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory", path)
	}
	return nil
}

func search(start string) (string, error) {
	current := start
	for {
		if err := validate(current); err == nil {
			return current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", fmt.Errorf("%w (searched from %s)", ErrNotFound, start)
		}
		current = parent
	}
}
