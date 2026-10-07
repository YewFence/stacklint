package imageref_test

import (
	"testing"

	"github.com/YewFence/stacklint/internal/imageref"
)

func TestParse(t *testing.T) {
	pinned := "ghcr.io/org/img:1.2@sha256:" + string(make([]byte, 0, 64))
	_ = pinned
	digest := "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	tests := []struct {
		name       string
		image      string
		repository string
		tag        string
		digest     string
		violation  string
		approvable bool
	}{
		{"pinned", "ghcr.io/org/img:1.2@" + digest, "ghcr.io/org/img", "1.2", digest, "", false},
		{"pinned no tag", "ghcr.io/org/img@" + digest, "ghcr.io/org/img", "", digest, "", false},
		{"unpinned tag", "traefik:v3.1", "traefik:v3.1"[0:7], "v3.1", "", "is not pinned to a sha256 digest", true},
		{"unpinned no tag", "busybox", "busybox", "", "", "is not pinned to a sha256 digest", true},
		{"registry with port", "localhost:5000/img:2", "localhost:5000/img", "2", "", "is not pinned to a sha256 digest", true},
		{"empty tag", "busybox:@", "", "", "", "must use a sha256 digest", false},
		{"bad digest", "busybox:1@sha256:xyz", "", "", "", "must use a sha256 digest", false},
		{"uppercase digest", "busybox:1@sha256:0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF", "", "", "", "must use a sha256 digest", false},
		{"double digest", "busybox@sha256:" + digest[7:] + "@" + digest, "", "", "", "must use a sha256 digest", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ref, violation := imageref.Parse(test.image)
			if test.violation == "" {
				if violation != nil {
					t.Fatalf("unexpected violation: %+v", violation)
				}
			} else {
				if violation == nil {
					t.Fatalf("expected violation %q", test.violation)
				}
				if violation.Approvable != test.approvable {
					t.Fatalf("approvable = %v, want %v", violation.Approvable, test.approvable)
				}
			}
			if violation == nil || violation.Approvable {
				if ref.Repository != test.repository || ref.Tag != test.tag || ref.Digest != test.digest {
					t.Fatalf("Parse(%q) = %+v, want repository=%q tag=%q digest=%q", test.image, ref, test.repository, test.tag, test.digest)
				}
			}
		})
	}
}

func TestMatchApproval(t *testing.T) {
	tests := []struct {
		name     string
		approval imageref.Approval
		ref      imageref.Reference
		want     string
	}{
		{"exact tag match", imageref.Approval{Repository: "img", Rule: "exact-tag", Tag: "1.2.3"}, imageref.Reference{Repository: "img", Tag: "1.2.3"}, ""},
		{"exact tag mismatch", imageref.Approval{Repository: "img", Rule: "exact-tag", Tag: "1.2.3"}, imageref.Reference{Repository: "img", Tag: "1.2.4"}, "tag is"},
		{"floating match", imageref.Approval{Repository: "img", Rule: "floating-tag", Tag: "main"}, imageref.Reference{Repository: "img", Tag: "main"}, ""},
		{"major match", imageref.Approval{Repository: "img", Rule: "major"}, imageref.Reference{Repository: "img", Tag: "v3"}, ""},
		{"major plain digit", imageref.Approval{Repository: "img", Rule: "major"}, imageref.Reference{Repository: "img", Tag: "3"}, ""},
		{"major with suffix", imageref.Approval{Repository: "img", Rule: "major", Suffix: "alpine"}, imageref.Reference{Repository: "img", Tag: "3-alpine"}, ""},
		{"major suffix mismatch", imageref.Approval{Repository: "img", Rule: "major", Suffix: "alpine"}, imageref.Reference{Repository: "img", Tag: "3-debian"}, "suffix is"},
		{"major rejects minor", imageref.Approval{Repository: "img", Rule: "major"}, imageref.Reference{Repository: "img", Tag: "3.1"}, "does not match"},
		{"major-minor match", imageref.Approval{Repository: "img", Rule: "major-minor"}, imageref.Reference{Repository: "img", Tag: "v3.1"}, ""},
		{"major-minor rejects major", imageref.Approval{Repository: "img", Rule: "major-minor"}, imageref.Reference{Repository: "img", Tag: "v3"}, "does not match"},
		{"repository mismatch", imageref.Approval{Repository: "img", Rule: "major"}, imageref.Reference{Repository: "other", Tag: "3"}, "repository is"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := imageref.MatchApproval(test.approval, test.ref)
			if test.want == "" && got != "" {
				t.Fatalf("expected match, got %q", got)
			}
			if test.want != "" && got == "" {
				t.Fatalf("expected mismatch containing %q", test.want)
			}
		})
	}
}

func TestValidateApproval(t *testing.T) {
	if err := imageref.ValidateApproval(imageref.Approval{Repository: "img", Rule: "floating-tag", Tag: "main"}); err != nil {
		t.Fatal(err)
	}
	if err := imageref.ValidateApproval(imageref.Approval{Repository: "img", Rule: "floating-tag"}); err == nil {
		t.Fatal("floating-tag without tag should fail")
	}
	if err := imageref.ValidateApproval(imageref.Approval{Repository: "img", Rule: "major", Tag: "3"}); err == nil {
		t.Fatal("major with tag should fail")
	}
	if err := imageref.ValidateApproval(imageref.Approval{Repository: "img", Rule: "major", Suffix: "alpine"}); err != nil {
		t.Fatal(err)
	}
	if err := imageref.ValidateApproval(imageref.Approval{Repository: "img", Rule: "bogus"}); err == nil {
		t.Fatal("unknown rule should fail")
	}
	if err := imageref.ValidateApproval(imageref.Approval{Repository: "img:1", Rule: "major"}); err == nil {
		t.Fatal("repository with tag should fail")
	}
}
