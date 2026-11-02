package check_test

import (
	"fmt"
	"strings"
	"testing"
)

const traefikAlternatives = `
[[deployment.alternatives]]
name = "traefik"
stacks = ["stacks/edge/traefik", "stacks/app/traefik"]
`

func TestAlternativeStacksDoNotConflictButKeepOtherChecks(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+traefikAlternatives)
	for _, stack := range []string{"stacks/edge/traefik", "stacks/app/traefik"} {
		f.write(stack+"/compose.yaml", "name: traefik\nservices:\n  traefik:\n    image: "+pinnedImage+"\n    ports: ['443:443']\n")
	}
	if report := f.run(t); report.ExitCode != 0 {
		t.Fatalf("alternatives must not conflict: %v", rules(report))
	}
	f.write("stacks/app/other/compose.yaml", "name: other\nservices:\n  other:\n    image: "+pinnedImage+"\n    ports: ['443:443']\n")
	report := f.run(t)
	for _, diagnostic := range report.Diagnostics {
		if diagnostic.Rule != "published_port" {
			t.Fatalf("unexpected diagnostic: %+v", diagnostic)
		}
		if diagnostic.Stack == "stacks/edge/traefik" && strings.Contains(diagnostic.Message, "stacks/app/traefik/") ||
			diagnostic.Stack == "stacks/app/traefik" && strings.Contains(diagnostic.Message, "stacks/edge/traefik/") {
			t.Fatalf("diagnostic names mutually exclusive stacks as conflicting: %+v", diagnostic)
		}
	}
	if !hasDiagnostic(report, "published_port", "stacks/app/other/compose.yaml") {
		t.Fatalf("third-party conflicts must remain checked: %v", rules(report))
	}
}

func TestAlternativeStacksKeepInternalPortConflicts(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+traefikAlternatives)
	f.write("stacks/edge/traefik/compose.yaml", "name: traefik\nservices:\n  a:\n    image: "+pinnedImage+"\n    ports: ['443:443']\n  b:\n    image: "+pinnedImage+"\n    ports: ['443:443']\n")
	f.write("stacks/app/traefik/compose.yaml", "name: traefik\nservices:\n  a:\n    image: "+pinnedImage+"\n")
	if report := f.run(t); !hasDiagnostic(report, "published_port", "443/tcp is duplicated") {
		t.Fatalf("internal conflict was hidden: %v", rules(report))
	}
}

func TestAlternativeResources(t *testing.T) {
	for _, mode := range []string{"host-own", "stack-own", "either"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			f.write("stacklint.toml", f.configToml()+traefikAlternatives+fmt.Sprintf("\n[resource_ownership]\nvolumes_mode = %q\nnetworks_mode = %q\n", mode, mode))
			resource := func(external bool) string {
				return fmt.Sprintf("name: app\nservices:\n  app:\n    image: %s\n    volumes: [data:/data]\n    networks: [shared]\nvolumes:\n  data:\n    name: shared-data\n    external: %t\nnetworks:\n  shared:\n    name: shared-network\n    external: %t\n", pinnedImage, external, external)
			}
			for _, stack := range []string{"stacks/edge/traefik", "stacks/app/traefik"} {
				f.write(stack+"/compose.yaml", resource(false))
			}
			if report := f.run(t); report.ExitCode != 0 {
				t.Fatalf("exclusive owners must not conflict: %v", rules(report))
			}
			f.write("stacks/app/consumer/compose.yaml", resource(true))
			report := f.run(t)
			if got := hasDiagnostic(report, "resource_owner", ""); got != (mode == "host-own") {
				t.Fatalf("unexpected alternative ownership verdict: %v", rules(report))
			}
			f.write("stacks/app/consumer/compose.yaml", resource(false))
			if report := f.run(t); !hasDiagnostic(report, "resource_owner", "") {
				t.Fatalf("coexisting owner conflict was hidden: %v", rules(report))
			}
		})
	}
}

func TestAlternativeOwnerCannotHideOwnerlessCoexistingConsumers(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+traefikAlternatives+"\n[resource_ownership]\nvolumes_mode = 'stack-own'\n")
	f.write("stacks/edge/traefik/compose.yaml", "name: edge\nservices:\n  app:\n    image: "+pinnedImage+"\nvolumes:\n  data:\n    name: shared-data\n")
	f.write("stacks/app/traefik/compose.yaml", "name: local\nservices:\n  app:\n    image: "+pinnedImage+"\n")
	for _, stack := range []string{"stacks/app/a", "stacks/app/b"} {
		f.write(stack+"/compose.yaml", "name: consumer\nservices:\n  app:\n    image: "+pinnedImage+"\nvolumes:\n  data:\n    name: shared-data\n    external: true\n")
	}
	if report := f.run(t); !hasDiagnostic(report, "resource_owner", "exactly one stack must own it") {
		t.Fatalf("alternative without the resource owner must still be checked: %v", rules(report))
	}
}

func TestIndependentAlternativeGroupsCanConflict(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+traefikAlternatives+`
[[deployment.alternatives]]
name = "gateway"
stacks = ["stacks/edge/gateway", "stacks/app/gateway"]

[resource_ownership]
volumes_mode = "either"
`)
	for _, stack := range []string{"stacks/edge/traefik", "stacks/app/traefik", "stacks/edge/gateway", "stacks/app/gateway"} {
		f.write(stack+"/compose.yaml", "name: app\nservices:\n  app:\n    image: "+pinnedImage+"\n    ports: ['443:443']\nvolumes:\n  data:\n    name: shared-data\n")
	}
	report := f.run(t)
	if !hasDiagnostic(report, "published_port", "443/tcp is duplicated") || !hasDiagnostic(report, "resource_owner", "shared-data") {
		t.Fatalf("members of independent alternative groups can coexist and must be compared: %v", rules(report))
	}
}

func TestAlternativePortApprovalBecomesStale(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+traefikAlternatives)
	for _, stack := range []string{"stacks/edge/traefik", "stacks/app/traefik"} {
		f.write(stack+"/compose.yaml", "name: traefik\nservices:\n  traefik:\n    image: "+pinnedImage+"\n    ports: ['443:443']\n")
	}
	f.write("stacklint.toml", `[[approvals.published_port]]
port = 443
protocol = "tcp"
reason = "Previously required"
bindings = [
  {compose = "stacks/edge/traefik/compose.yaml", service = "traefik", host_ip = "*"},
  {compose = "stacks/app/traefik/compose.yaml", service = "traefik", host_ip = "*"},
]
`)
	if report := f.run(t); !hasDiagnostic(report, "stale_approval", "no longer duplicated") {
		t.Fatalf("obsolete approval must fail: %v", rules(report))
	}
	if report := f.run(t, "stacks/edge/traefik"); report.ExitCode != 0 {
		t.Fatalf("scoped check judged out-of-scope approval: %v", rules(report))
	}
}
