package images

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeClient struct {
	// digests maps queried references to their digest; missing entries fail
	// with the kind in errors, or not-found when absent there too.
	digests map[string]string
	errors  map[string]*RegistryError
	// credentials is the set of registry hosts with configured credentials.
	credentials map[string]bool
	// queried records every queried reference in order.
	queried []string
}

func (f *fakeClient) HasCredentials(registry string) bool {
	return f.credentials[registry]
}

func (f *fakeClient) Digest(_ context.Context, reference string) (string, error) {
	f.queried = append(f.queried, reference)
	if err, ok := f.errors[reference]; ok {
		return "", err
	}
	if digest, ok := f.digests[reference]; ok {
		return digest, nil
	}
	return "", &RegistryError{Kind: KindNotFound, Message: "not found"}
}

const (
	digestA = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	digestB = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

func checkOne(t *testing.T, client *fakeClient, checker *Checker, reference string) Result {
	t.Helper()
	results := checker.Check(t.Context(), []PlannedImage{{Reference: reference}}, nil)
	require.Len(t, results, 1)
	return results[0]
}

func newChecker(client Client) *Checker {
	return &Checker{
		Client:              client,
		FloatingTags:        []string{"latest", "main"},
		PrivateRepositories: []string{"ghcr.io/example/private"},
		Jobs:                4,
		RegistryJobs:        2,
	}
}

func TestCheckTagAndDigestMatch(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app:1.2.3": digestA}}
	result := checkOne(t, client, newChecker(client), "app:1.2.3@"+digestA)
	require.Equal(t, "ok", result.Outcome)
	require.Equal(t, []string{"app:1.2.3"}, client.queried)
}

func TestCheckTagDriftedButDigestExists(t *testing.T) {
	client := &fakeClient{digests: map[string]string{
		"app:1.2.3":      digestB,
		"app@" + digestA: digestA,
	}}
	result := checkOne(t, client, newChecker(client), "app:1.2.3@"+digestA)
	require.Equal(t, "warning", result.Outcome)
	require.Contains(t, result.Detail, "tag resolves to "+digestB)
	require.Contains(t, result.Detail, "pinned digest still exists")
	require.Equal(t, []string{"app:1.2.3", "app@" + digestA}, client.queried)
}

func TestCheckTagMissingButDigestExists(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app@" + digestA: digestA}}
	result := checkOne(t, client, newChecker(client), "app:1.2.3@"+digestA)
	require.Equal(t, "warning", result.Outcome)
	require.Contains(t, result.Detail, "tag does not exist")
}

func TestCheckTagDriftedAndDigestMissing(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app:1.2.3": digestB}}
	result := checkOne(t, client, newChecker(client), "app:1.2.3@"+digestA)
	require.Equal(t, "error", result.Outcome)
	require.Contains(t, result.Detail, "pinned digest does not exist")
}

func TestCheckFloatingTagOnlyVerifiesDigest(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app@" + digestA: digestA}}
	result := checkOne(t, client, newChecker(client), "app:latest@"+digestA)
	require.Equal(t, "ok", result.Outcome)
	require.Equal(t, "pinned digest exists", result.Detail)
	require.Equal(t, []string{"app@" + digestA}, client.queried)
}

func TestCheckUntaggedDigestOnlyVerifiesDigest(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app@" + digestA: digestA}}
	result := checkOne(t, client, newChecker(client), "app@"+digestA)
	require.Equal(t, "ok", result.Outcome)
	require.Equal(t, []string{"app@" + digestA}, client.queried)
}

func TestCheckUnpinnedVerifiesTag(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app:1.2.3": digestA}}
	result := checkOne(t, client, newChecker(client), "app:1.2.3")
	require.Equal(t, "ok", result.Outcome)
	require.Contains(t, result.Detail, "tag exists (app:1.2.3)")
}

func TestCheckUntaggedDefaultsToLatest(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app:latest": digestA}}
	result := checkOne(t, client, newChecker(client), "app")
	require.Equal(t, "ok", result.Outcome)
	require.Contains(t, result.Detail, "tag exists (app:latest)")
}

func TestCheckUnpinnedTagMissing(t *testing.T) {
	client := &fakeClient{}
	result := checkOne(t, client, newChecker(client), "app:1.2.3")
	require.Equal(t, "error", result.Outcome)
	require.Contains(t, result.Detail, "cannot verify app:1.2.3 (not-found)")
}

func TestCheckPrivateRepositoryWithoutCredentialsSkips(t *testing.T) {
	client := &fakeClient{credentials: map[string]bool{}}
	checker := newChecker(client)
	result := checkOne(t, client, checker, "ghcr.io/example/private:1.0@"+digestA)
	require.Equal(t, "warning", result.Outcome)
	require.Contains(t, result.Detail, "no configured registry credentials")
	require.Empty(t, client.queried)
}

func TestCheckPrivateRepositoryWithCredentialsFailsNormally(t *testing.T) {
	client := &fakeClient{credentials: map[string]bool{"ghcr.io": true}}
	result := checkOne(t, client, newChecker(client), "ghcr.io/example/private:1.0@"+digestA)
	require.Equal(t, "error", result.Outcome)
}

func TestCheckAuthenticationErrorIsNotRetriedByCaller(t *testing.T) {
	client := &fakeClient{errors: map[string]*RegistryError{
		"app:1.2.3": {Kind: KindAuthentication, Message: "unauthorized"},
	}}
	result := checkOne(t, client, newChecker(client), "app:1.2.3@"+digestA)
	require.Equal(t, "error", result.Outcome)
	require.Contains(t, result.Detail, "(authentication)")
	// An authentication failure on the tag must not fall through to the
	// digest query: only not-found does.
	require.Equal(t, []string{"app:1.2.3"}, client.queried)
}

func TestCheckInvalidReferenceFails(t *testing.T) {
	client := &fakeClient{}
	result := checkOne(t, client, newChecker(client), "app:1.2.3@sha256:nothex")
	require.Equal(t, "error", result.Outcome)
	require.Contains(t, result.Detail, "invalid image reference")
	require.Empty(t, client.queried)
}

func TestCheckDeduplicatesIdenticalQueries(t *testing.T) {
	client := &fakeClient{digests: map[string]string{"app:1.2.3": digestA}}
	planned := []PlannedImage{
		{Reference: "app:1.2.3@" + digestA},
		{Reference: "app:1.2.3@" + digestA},
	}
	results := newChecker(client).Check(t.Context(), planned, nil)
	require.Len(t, results, 2)
	for _, result := range results {
		require.Equal(t, "ok", result.Outcome)
	}
	require.Equal(t, []string{"app:1.2.3"}, client.queried)
}

func TestPlanSubtractsBaselineAndKeepsHeadLocations(t *testing.T) {
	head := []Record{
		{Stack: "b", Variant: "base", Service: "svc", Reference: "app:2"},
		{Stack: "a", Variant: "base", Service: "svc", Reference: "app:1"},
		{Stack: "a", Variant: "postgres", Service: "svc", Reference: "app:1"},
	}
	base := []Record{
		{Stack: "a", Variant: "base", Service: "svc", Reference: "app:0"},
		{Stack: "b", Variant: "base", Service: "svc", Reference: "app:2"},
	}
	planned := Plan(head, base)
	require.Len(t, planned, 1)
	require.Equal(t, "app:1", planned[0].Reference)
	require.Equal(t, head[1:], planned[0].Locations)
}

func TestPlanWithoutBaselineSelectsAll(t *testing.T) {
	head := []Record{
		{Stack: "a", Variant: "base", Service: "one", Reference: "app:1"},
		{Stack: "a", Variant: "base", Service: "two", Reference: "app:1"},
	}
	planned := Plan(head, nil)
	require.Len(t, planned, 1)
	require.Len(t, planned[0].Locations, 2)
}
