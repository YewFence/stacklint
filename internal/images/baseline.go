package images

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BurntSushi/toml"

	"github.com/YewFence/stacklint/internal/config"
)

// imagesPolicySnapshot includes only policy relevant to images digests.
// Other sections are decoded leniently so baseline schema changes do not
// prevent comparison.
type imagesPolicySnapshot struct {
	Images    config.ImagesConfig `toml:"images"`
	Approvals struct {
		Images []config.ImageApproval `toml:"image"`
	} `toml:"approvals"`
}

// PolicyChangedAtRef compares image policy and image approvals in the current
// configuration with the materialized baseline. Unreadable baseline policy
// counts as changed; unrelated sections and comments do not.
func PolicyChangedAtRef(configDir, baselineRoot string) (bool, error) {
	current, err := decodeImagesPolicy(filepath.Join(configDir, config.FileName))
	if err != nil {
		return false, err
	}
	if _, err := os.Lstat(filepath.Join(baselineRoot, "approvals.toml")); !os.IsNotExist(err) {
		return true, nil
	}
	baseline, err := decodeImagesPolicy(filepath.Join(baselineRoot, config.FileName))
	if err != nil {
		return true, nil //nolint:nilerr // an unreadable baseline policy is uncomparable, hence changed
	}
	return !reflect.DeepEqual(current, baseline), nil
}

func decodeImagesPolicy(path string) (imagesPolicySnapshot, error) {
	var snapshot imagesPolicySnapshot
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), &snapshot); err != nil {
		return snapshot, fmt.Errorf("parse %s: %w", path, err)
	}
	return snapshot, nil
}
