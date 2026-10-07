package check

import (
	"fmt"
	"strings"

	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/imageref"
)

// checkImages requires every service image to be pinned to a sha256 digest or
// covered by a matching [[image]] approval. Empty tags and malformed digests
// are never approvable.
func checkImages(c *ruleContext) []Diagnostic {
	var diagnostics []Diagnostic
	compose := c.composeRel()
	for _, serviceName := range c.render.Project.ServiceNames() {
		service := c.render.Project.Services[serviceName]
		image := strings.TrimSpace(service.Image)
		if image == "" {
			continue
		}
		c.reviewed.observedImages[imageKey(compose, serviceName)] = image
		ref, violation := imageref.Parse(image)
		if violation == nil {
			// Pinned images need no approval; an existing one is redundant and
			// reported by the stale-approval pass.
			continue
		}
		if !violation.Approvable {
			diagnostics = append(diagnostics, c.diagnose("image", serviceName,
				fmt.Sprintf("image %s %s", image, violation.Reason),
				"Fix the image reference because approvals cannot waive malformed tags or digests."))
			continue
		}
		approval := findImageApproval(c.approvals, compose, serviceName)
		if approval == nil {
			diagnostics = append(diagnostics, c.diagnose("image", serviceName,
				fmt.Sprintf("image %s %s", image, violation.Reason),
				"Use a valid sha256 digest, or add an [[image]] approval for this compose/service/repository with an explicit rule (exact-tag, floating-tag, major, major-minor) and a reason in approvals.toml."))
			continue
		}
		c.reviewed.images[imageKey(compose, serviceName)] = true
		if mismatch := imageref.MatchApproval(imageref.Approval{
			Repository: approval.Repository,
			Rule:       approval.Rule,
			Tag:        approval.Tag,
			Suffix:     approval.Suffix,
		}, ref); mismatch != "" {
			diagnostics = append(diagnostics, c.diagnose("image", serviceName,
				fmt.Sprintf("image %s %s; existing approval does not match because %s", image, violation.Reason, mismatch),
				"Use a valid sha256 digest, or update the [[image]] rule in approvals.toml after reviewing the new policy."))
		}
	}
	return diagnostics
}

func findImageApproval(approvals *config.Approvals, compose, service string) *config.ImageApproval {
	for i := range approvals.Images {
		if approvals.Images[i].Compose == compose && approvals.Images[i].Service == service {
			return &approvals.Images[i]
		}
	}
	return nil
}
