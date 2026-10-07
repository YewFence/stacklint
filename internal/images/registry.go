package images

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/regclient/regclient"
	regconfig "github.com/regclient/regclient/config"
	"github.com/regclient/regclient/types/errs"
	"github.com/regclient/regclient/types/ref"
)

var digestFormat = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// RegClient is a Client backed by regclient. Credentials come from the
// standard Docker configuration (DOCKER_CONFIG or the default location)
// exclusively; stacklint never decrypts or writes credentials.
type RegClient struct {
	inner   *regclient.RegClient
	hosts   []regconfig.Host
	timeout time.Duration
	retries int
}

// NewRegClient loads the standard Docker configuration and prepares a client
// with the given per-query timeout and retry count for retryable failures.
func NewRegClient(timeout time.Duration, retries int) (*RegClient, error) {
	hosts, err := regconfig.DockerLoad()
	if err != nil {
		return nil, fmt.Errorf("load docker configuration: %w", err)
	}
	return &RegClient{
		inner:   regclient.New(regclient.WithDockerCreds()),
		hosts:   hosts,
		timeout: timeout,
		retries: retries,
	}, nil
}

// HasCredentials reports whether the Docker configuration holds credentials
// (direct or via a credential helper) for the registry.
func (c *RegClient) HasCredentials(registry string) bool {
	for _, host := range c.hosts {
		if normalizeRegistry(host.Name) != normalizeRegistry(registry) {
			continue
		}
		if host.User != "" || host.Token != "" || host.CredHelper != "" {
			return true
		}
	}
	return false
}

func normalizeRegistry(name string) string {
	switch name {
	case "index.docker.io", "registry-1.docker.io", "https://index.docker.io/v1/":
		return "docker.io"
	}
	return name
}

// Digest returns the top-level manifest or image index digest of reference,
// without selecting a platform. Retryable failures (transport errors, 429,
// 5xx and other non-auth non-404 statuses) are retried with backoff;
// authentication errors and 404 are not retried.
func (c *RegClient) Digest(ctx context.Context, reference string) (string, error) {
	parsed, err := ref.New(reference)
	if err != nil {
		return "", &RegistryError{Kind: KindQuery, Message: fmt.Sprintf("invalid reference %q: %v", reference, err)}
	}
	var lastErr error
	for attempt := 0; attempt <= c.retries; attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(time.Duration(1<<(attempt-1)) * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return "", ctx.Err()
			case <-timer.C:
			}
		}
		attemptCtx, cancel := context.WithTimeout(ctx, c.timeout)
		manifest, err := c.inner.ManifestHead(attemptCtx, parsed)
		cancel()
		if err == nil {
			digest := manifest.GetDescriptor().Digest.String()
			if !digestFormat.MatchString(digest) {
				return "", &RegistryError{Kind: KindInvalid, Message: fmt.Sprintf("registry returned an invalid digest for %s: %q", reference, digest)}
			}
			return digest, nil
		}
		registryErr := classifyError(err)
		lastErr = registryErr
		if registryErr.Kind != KindRetryable {
			return "", registryErr
		}
	}
	return "", lastErr
}

// classifyError maps regclient's structured errors onto registry error kinds.
// regclient does not expose the HTTP status beyond the 401/403, 404 and 429
// sentinels, so any other status error is treated as retryable: retrying an
// odd 4xx costs a few HEAD requests, while misclassifying a 5xx as final
// would fail checks on transient registry trouble.
func classifyError(err error) *RegistryError {
	kind := KindRetryable
	switch {
	case errors.Is(err, errs.ErrNotFound):
		kind = KindNotFound
	case errors.Is(err, errs.ErrHTTPUnauthorized):
		kind = KindAuthentication
	case errors.Is(err, errs.ErrHTTPRateLimit):
	case errors.Is(err, errs.ErrHTTPStatus):
	default:
		// Transport errors, timeouts and cancellations are retryable.
	}
	return &RegistryError{Kind: kind, Message: err.Error()}
}
