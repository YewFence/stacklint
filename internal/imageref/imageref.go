// Package imageref is the single place that parses and validates image
// references, built on distribution/reference. Approval matching semantics
// mirror the original Python compose-checks tooling.
package imageref

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/distribution/reference"
)

var (
	sha256DigestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	majorTagRe     = regexp.MustCompile(`^v?\d+(?:-(?P<suffix>[0-9A-Za-z][0-9A-Za-z._-]*))?$`)
	majorMinorTagRe = regexp.MustCompile(`^v?\d+\.\d+(?:-(?P<suffix>[0-9A-Za-z][0-9A-Za-z._-]*))?$`)
)

// ApprovalRules are the valid [[image]] approval rules.
var ApprovalRules = []string{"exact-tag", "floating-tag", "major", "major-minor"}

// Reference is a parsed image reference.
type Reference struct {
	// Repository is the repository as written in the Compose file, so approval
	// entries match the text authors see. It is validated through
	// distribution/reference but not normalized.
	Repository string
	// Tag is the explicit tag, empty when the reference carries none.
	Tag string
	// Digest is the digest part (e.g. "sha256:..."), empty when absent.
	Digest string
}

// Violation describes why a reference breaks the pinning policy.
type Violation struct {
	Reason     string
	Approvable bool
}

// Parse splits image into repository, tag and digest, reporting policy violations.
// A malformed digest or an empty tag is never approvable; a missing digest is.
func Parse(image string) (Reference, *Violation) {
	namePart, digest, hasDigest := strings.Cut(image, "@")
	if hasDigest && (strings.Contains(digest, "@") || !sha256DigestRe.MatchString(digest)) {
		return Reference{}, &Violation{
			Reason:     "must use a sha256 digest with exactly 64 lowercase hexadecimal digits",
			Approvable: false,
		}
	}

	ref := Reference{Digest: digest}
	if strings.HasSuffix(namePart, ":") {
		ref.Repository = strings.TrimSuffix(namePart, ":")
		return ref, &Violation{Reason: "has an empty image tag", Approvable: false}
	}

	named, err := reference.ParseNormalizedNamed(namePart)
	if err != nil {
		return Reference{}, &Violation{Reason: fmt.Sprintf("is not a valid image reference: %v", err), Approvable: false}
	}
	// Keep the repository as written: approvals bind to the literal text.
	if tagged, ok := named.(reference.Tagged); ok {
		ref.Tag = tagged.Tag()
		ref.Repository = strings.TrimSuffix(namePart, ":"+ref.Tag)
	} else {
		ref.Repository = namePart
	}

	if hasDigest {
		return ref, nil
	}
	return ref, &Violation{Reason: "is not pinned to a sha256 digest", Approvable: true}
}

// Approval is a validated [[image]] approval entry.
type Approval struct {
	Repository string
	Rule       string
	Tag        string
	Suffix     string
}

// MatchApproval reports why the reference does not satisfy the approval, or ""
// when it does. Repository must match exactly; the rule then constrains the tag.
func MatchApproval(approval Approval, ref Reference) string {
	if ref.Repository != approval.Repository {
		return fmt.Sprintf("repository is %q instead of approved %q", ref.Repository, approval.Repository)
	}
	tag := ref.Tag
	if tag == "" {
		tag = "none"
	}
	switch approval.Rule {
	case "floating-tag", "exact-tag":
		if ref.Tag != approval.Tag {
			kind := map[string]string{"floating-tag": "floating tag", "exact-tag": "exact tag"}[approval.Rule]
			return fmt.Sprintf("tag is %q instead of approved %s %q", tag, kind, approval.Tag)
		}
		return ""
	case "major", "major-minor":
		pattern := majorTagRe
		if approval.Rule == "major-minor" {
			pattern = majorMinorTagRe
		}
		match := pattern.FindStringSubmatch(ref.Tag)
		if match == nil {
			return fmt.Sprintf("tag %q does not match rule %s", tag, approval.Rule)
		}
		suffix := match[pattern.SubexpIndex("suffix")]
		if suffix != approval.Suffix {
			displayed := suffix
			if displayed == "" {
				displayed = "none"
			}
			approved := approval.Suffix
			if approved == "" {
				approved = "none"
			}
			return fmt.Sprintf("suffix is %q instead of approved %q", displayed, approved)
		}
		return ""
	}
	return fmt.Sprintf("unknown approval rule %q", approval.Rule)
}

// ValidateApproval checks approval field combinations, mirroring the oracle:
// floating-tag/exact-tag require tag and reject suffix; major/major-minor
// reject tag and accept optional suffix.
func ValidateApproval(approval Approval) error {
	switch approval.Rule {
	case "floating-tag", "exact-tag":
		if approval.Tag == "" {
			return fmt.Errorf("rule %q requires a non-empty tag; record the exact tag this policy permits", approval.Rule)
		}
		if approval.Suffix != "" {
			return fmt.Errorf("rule %q does not accept suffix; suffix only constrains major or major-minor version tags", approval.Rule)
		}
	case "major", "major-minor":
		if approval.Tag != "" {
			return fmt.Errorf("rule %q does not accept tag; remove tag because the numeric version is the allowed variable", approval.Rule)
		}
	default:
		return fmt.Errorf("rule %q is not supported; use one of %s", approval.Rule, strings.Join(ApprovalRules, ", "))
	}
	lastSlash := strings.LastIndex(approval.Repository, "/")
	lastColon := strings.LastIndex(approval.Repository, ":")
	if strings.Contains(approval.Repository, "@") || lastColon > lastSlash {
		return fmt.Errorf("repository %q must not include a tag or digest", approval.Repository)
	}
	return nil
}
