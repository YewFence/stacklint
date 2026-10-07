package images

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"

	"github.com/BurntSushi/toml"

	"github.com/YewFence/stacklint/internal/config"
)

// imagesPolicySnapshot is the part of stacklint.toml that steers images
// digests; decoded leniently so unknown or renamed keys elsewhere in a
// baseline config do not break the comparison.
type imagesPolicySnapshot struct {
	Images config.ImagesConfig `toml:"images"`
}

// imageApprovalsSnapshot is the part of approvals.toml that steers images
// digests.
type imageApprovalsSnapshot struct {
	Image []config.ImageApproval `toml:"image"`
}

// PolicyChangedAtRef reports whether the images-relevant policy — the
// [images] section of stacklint.toml and the [[image]] approvals — differs
// between the current configuration directory and the materialized baseline
// tree. The comparison is semantic (parsed TOML), so comments and reordering
// do not trigger a full check. A baseline stacklint.toml that cannot be read
// or parsed counts as changed.
func PolicyChangedAtRef(configDir, baselineRoot string) (bool, error) {
	currentImages, err := decodeTOMLFile[imagesPolicySnapshot](filepath.Join(configDir, config.FileName), false)
	if err != nil {
		return false, err
	}
	currentApprovals, err := decodeTOMLFile[imageApprovalsSnapshot](filepath.Join(configDir, config.ApprovalsFileName), true)
	if err != nil {
		return false, err
	}
	baselineImages, err := decodeTOMLFile[imagesPolicySnapshot](filepath.Join(baselineRoot, config.FileName), false)
	if err != nil {
		return true, nil //nolint:nilerr // an unreadable baseline policy is uncomparable, hence changed
	}
	baselineApprovals, err := decodeTOMLFile[imageApprovalsSnapshot](filepath.Join(baselineRoot, config.ApprovalsFileName), true)
	if err != nil {
		return true, nil //nolint:nilerr // same as above
	}
	return !reflect.DeepEqual(currentImages, baselineImages) || !reflect.DeepEqual(currentApprovals, baselineApprovals), nil
}

func decodeTOMLFile[T any](path string, optional bool) (T, error) {
	var snapshot T
	data, err := os.ReadFile(path)
	if err != nil {
		if optional && errors.Is(err, fs.ErrNotExist) {
			return snapshot, nil
		}
		return snapshot, fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := toml.Decode(string(data), &snapshot); err != nil {
		return snapshot, fmt.Errorf("parse %s: %w", path, err)
	}
	return snapshot, nil
}
