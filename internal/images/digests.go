package images

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/YewFence/stacklint/internal/imageref"
)

// RegistryErrorKind classifies registry query failures without parsing error
// text.
type RegistryErrorKind string

const (
	KindNotFound       RegistryErrorKind = "not-found"
	KindAuthentication RegistryErrorKind = "authentication"
	KindRetryable      RegistryErrorKind = "retryable"
	KindQuery          RegistryErrorKind = "query"
	KindInvalid        RegistryErrorKind = "invalid-response"
)

// RegistryError is a classified registry query failure.
type RegistryError struct {
	Kind    RegistryErrorKind
	Message string
}

func (e *RegistryError) Error() string { return e.Message }

// Client queries remote registries. Registry is the registry host of a
// repository; Digest returns the top-level manifest or image index digest of
// a reference. Query failures are reported as *RegistryError.
type Client interface {
	HasCredentials(registry string) bool
	Digest(ctx context.Context, reference string) (string, error)
}

// PlannedImage is one deduplicated reference to verify, with every position
// it is used at.
type PlannedImage struct {
	Reference string
	Locations []Record
}

// Result is the verification outcome of one planned image.
type Result struct {
	Image     string   `json:"image"`
	Outcome   string   `json:"outcome"` // "ok", "warning", or "error"
	Detail    string   `json:"detail"`
	Locations []Record `json:"locations"`
}

// Plan selects the references to verify: every reference in head, minus those
// already present in base (nil base selects all). Locations come from head
// only. Output is sorted by reference.
func Plan(head []Record, base []Record) []PlannedImage {
	baseline := map[string]bool{}
	for _, record := range base {
		baseline[record.Reference] = true
	}
	locationsByRef := map[string][]Record{}
	for _, record := range head {
		if !baseline[record.Reference] {
			locationsByRef[record.Reference] = append(locationsByRef[record.Reference], record)
		}
	}
	refs := make([]string, 0, len(locationsByRef))
	for ref := range locationsByRef {
		refs = append(refs, ref)
	}
	slices.Sort(refs)
	planned := make([]PlannedImage, 0, len(refs))
	for _, ref := range refs {
		planned = append(planned, PlannedImage{Reference: ref, Locations: locationsByRef[ref]})
	}
	return planned
}

// Checker verifies planned images against remote registries with global and
// per-registry concurrency limits.
type Checker struct {
	Client             Client
	FloatingTags       []string
	PrivateRepositories []string
	Jobs               int
	RegistryJobs       int

	mu             sync.Mutex
	registryLimits map[string]chan struct{}
	queries        map[string]*queryResult
	credentials    map[string]bool
}

type queryResult struct {
	done   chan struct{}
	digest string
	err    error
}

// Check verifies every planned image, invoking progress as each completes.
// Results stay in plan order (sorted by reference).
func (c *Checker) Check(ctx context.Context, images []PlannedImage, progress func(completed, total int, result Result)) []Result {
	c.registryLimits = map[string]chan struct{}{}
	c.queries = map[string]*queryResult{}
	c.credentials = map[string]bool{}
	for _, image := range images {
		if !slices.Contains(c.PrivateRepositories, repositoryOf(image.Reference)) {
			continue
		}
		registry := imageref.Registry(repositoryOf(image.Reference))
		if _, ok := c.credentials[registry]; !ok {
			c.credentials[registry] = c.Client.HasCredentials(registry)
		}
	}

	results := make([]Result, len(images))
	completed := 0
	group, groupCtx := errgroup.WithContext(ctx)
	group.SetLimit(c.Jobs)
	for i, image := range images {
		group.Go(func() error {
			result := c.checkOne(groupCtx, image)
			results[i] = result
			if progress != nil {
				c.mu.Lock()
				completed++
				progress(completed, len(images), result)
				c.mu.Unlock()
			}
			return nil
		})
	}
	// checkOne converts all failures into results, so Wait never errors.
	_ = group.Wait()
	return results
}

func (c *Checker) checkOne(ctx context.Context, image PlannedImage) Result {
	ref, violation := imageref.Parse(image.Reference)
	if violation != nil && !violation.Approvable {
		return Result{Image: image.Reference, Outcome: "error", Detail: "invalid image reference: " + violation.Reason, Locations: image.Locations}
	}
	if slices.Contains(c.PrivateRepositories, ref.Repository) && !c.credentials[imageref.Registry(ref.Repository)] {
		return Result{Image: image.Reference, Outcome: "warning", Detail: "private repository has no configured registry credentials; skipped", Locations: image.Locations}
	}

	if ref.Digest == "" {
		tagRef := tagReference(ref)
		if _, err := c.digest(ctx, ref.Repository, tagRef); err != nil {
			return queryError(image, tagRef, err)
		}
		return Result{Image: image.Reference, Outcome: "ok", Detail: fmt.Sprintf("tag exists (%s)", tagRef), Locations: image.Locations}
	}

	digestRef := ref.Repository + "@" + ref.Digest
	if ref.Tag == "" || slices.Contains(c.FloatingTags, ref.Tag) {
		if _, err := c.digest(ctx, ref.Repository, digestRef); err != nil {
			return queryError(image, digestRef, err)
		}
		return Result{Image: image.Reference, Outcome: "ok", Detail: "pinned digest exists", Locations: image.Locations}
	}

	tagRef := tagReference(ref)
	remoteDigest, err := c.digest(ctx, ref.Repository, tagRef)
	if err != nil {
		if registryErr, ok := err.(*RegistryError); !ok || registryErr.Kind != KindNotFound {
			return queryError(image, tagRef, err)
		}
		return c.classifyMismatch(ctx, image, ref, "", true)
	}
	if remoteDigest == ref.Digest {
		return Result{Image: image.Reference, Outcome: "ok", Detail: "tag and digest match", Locations: image.Locations}
	}
	return c.classifyMismatch(ctx, image, ref, remoteDigest, false)
}

// classifyMismatch handles a tag/digest mismatch or a missing tag: the pinned
// digest still existing downgrades the drift to a warning.
func (c *Checker) classifyMismatch(ctx context.Context, image PlannedImage, ref imageref.Reference, remoteDigest string, tagMissing bool) Result {
	digestRef := ref.Repository + "@" + ref.Digest
	digestExists := false
	if _, err := c.digest(ctx, ref.Repository, digestRef); err != nil {
		if registryErr, ok := err.(*RegistryError); !ok || registryErr.Kind != KindNotFound {
			return queryError(image, digestRef, err)
		}
	} else {
		digestExists = true
	}

	var tagDetail string
	if tagMissing {
		tagDetail = "tag does not exist"
	} else {
		tagDetail = fmt.Sprintf("tag resolves to %s, expected %s", remoteDigest, ref.Digest)
	}
	var digestDetail string
	if digestExists {
		digestDetail = "pinned digest still exists"
	} else {
		digestDetail = "pinned digest does not exist"
	}
	outcome := "warning"
	if !digestExists {
		outcome = "error"
	}
	return Result{Image: image.Reference, Outcome: outcome, Detail: tagDetail + "; " + digestDetail, Locations: image.Locations}
}

// digest answers a manifest digest query, deduplicating identical in-flight
// and completed queries and applying the per-registry concurrency limit.
func (c *Checker) digest(ctx context.Context, repository, reference string) (string, error) {
	c.mu.Lock()
	result, ok := c.queries[reference]
	if !ok {
		result = &queryResult{done: make(chan struct{})}
		c.queries[reference] = result
	}
	c.mu.Unlock()
	if ok {
		select {
		case <-result.done:
			return result.digest, result.err
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}

	limit := c.limit(imageref.Registry(repository))
	limit <- struct{}{}
	digest, err := c.Client.Digest(ctx, reference)
	<-limit
	result.digest, result.err = digest, err
	close(result.done)
	return digest, err
}

type registryLimit chan struct{}

func (c *Checker) limit(registry string) registryLimit {
	c.mu.Lock()
	defer c.mu.Unlock()
	limit, ok := c.registryLimits[registry]
	if !ok {
		limit = make(registryLimit, c.RegistryJobs)
		c.registryLimits[registry] = limit
	}
	return limit
}

func tagReference(ref imageref.Reference) string {
	if ref.Tag == "" {
		return ref.Repository + ":latest"
	}
	return ref.Repository + ":" + ref.Tag
}

func queryError(image PlannedImage, queriedRef string, err error) Result {
	kind := KindQuery
	if registryErr, ok := err.(*RegistryError); ok {
		kind = registryErr.Kind
	}
	return Result{
		Image:     image.Reference,
		Outcome:   "error",
		Detail:    fmt.Sprintf("cannot verify %s (%s): %v", queriedRef, kind, err),
		Locations: image.Locations,
	}
}

// repositoryOf extracts the repository part of a reference as written.
func repositoryOf(reference string) string {
	name, _, _ := strings.Cut(reference, "@")
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		if colon := strings.LastIndex(name, ":"); colon > slash {
			return name[:colon]
		}
		return name
	}
	if colon := strings.LastIndex(name, ":"); colon >= 0 {
		return name[:colon]
	}
	return name
}
