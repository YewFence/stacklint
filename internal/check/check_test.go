package check_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/YewFence/stacklint/internal/check"
	"github.com/YewFence/stacklint/internal/config"
	"github.com/YewFence/stacklint/internal/discover"
)

type fixture struct {
	t   *testing.T
	dir string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir()}
	// Both deployment roots must exist for discovery.
	f.write("stacks/edge/.gitkeep", "")
	return f
}

func (f *fixture) write(path, content string) {
	f.t.Helper()
	absolute := filepath.Join(f.dir, path)
	if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(absolute, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) configToml() string {
	return `[repository]
deployment_roots = ["stacks/edge", "stacks/app"]
compose_filenames = ["compose.yaml"]

[ports]
shared_roots = ["stacks/edge"]

[bind_address]
default_variable = "BIND_ADDRESS"
`
}

func (f *fixture) run(t *testing.T, targets ...string) *check.Report {
	t.Helper()
	cfg, err := config.Load(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	stacks, err := discover.FindAll(cfg, f.dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) > 0 {
		var selected []discover.Stack
		for _, target := range targets {
			stack, err := discover.MatchTarget(target, stacks, f.dir)
			if err != nil {
				t.Fatal(err)
			}
			selected = append(selected, stack)
		}
		stacks = selected
	}
	approvals, err := config.LoadApprovals(f.dir, f.dir, cfg)
	if err != nil {
		t.Fatal(err)
	}
	report, err := check.Run(context.Background(), cfg, f.dir, approvals, stacks)
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func rules(report *check.Report) []string {
	var out []string
	for _, d := range report.Diagnostics {
		out = append(out, d.Rule+": "+d.Message)
	}
	return out
}

func hasDiagnostic(report *check.Report, rule, messagePart string) bool {
	for _, d := range report.Diagnostics {
		if d.Rule == rule && strings.Contains(d.Message, messagePart) {
			return true
		}
	}
	return false
}

const pinnedImage = "ghcr.io/org/app:1@sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestCheckStructureAndPinnedImagePass(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n")
	report := f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected clean report, got %v", rules(report))
	}
	if len(report.Results) != 1 || report.Results[0].Status != "ok" {
		t.Fatalf("unexpected results: %+v", report.Results)
	}
}

func TestCheckStructureMissingName(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "services:\n  web:\n    image: "+pinnedImage+"\n")
	report := f.run(t)
	if !hasDiagnostic(report, "structure", "missing top-level name") {
		t.Fatalf("expected structure diagnostic, got %v", rules(report))
	}
}

func TestCheckUnpinnedImageNeedsApproval(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: traefik:v3\n")
	report := f.run(t)
	if !hasDiagnostic(report, "image", "is not pinned") {
		t.Fatalf("expected image diagnostic, got %v", rules(report))
	}
	if report.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %d", report.ExitCode)
	}

	f.write("approvals.toml", `[[image]]
compose = "stacks/app/web/compose.yaml"
service = "web"
repository = "traefik"
rule = "major"
reason = "Traefik major track is reviewed."
`)
	report = f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected approval to waive, got %v", rules(report))
	}
}

func TestCheckImageApprovalMismatch(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: traefik:v3.1\n")
	f.write("approvals.toml", `[[image]]
compose = "stacks/app/web/compose.yaml"
service = "web"
repository = "traefik"
rule = "major"
reason = "Major only."
`)
	report := f.run(t)
	if !hasDiagnostic(report, "image", "does not match rule major") {
		t.Fatalf("expected mismatch diagnostic, got %v", rules(report))
	}
}

func TestCheckImageMalformedNotApprovable(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: traefik:v3@sha256:bad\n")
	f.write("approvals.toml", `[[image]]
compose = "stacks/app/web/compose.yaml"
service = "web"
repository = "traefik"
rule = "major"
reason = "Cannot waive malformed digests."
`)
	report := f.run(t)
	if !hasDiagnostic(report, "image", "sha256 digest") {
		t.Fatalf("expected malformed digest diagnostic, got %v", rules(report))
	}
}

func TestCheckStaleImageApproval(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n")
	f.write("approvals.toml", `[[image]]
compose = "stacks/app/web/compose.yaml"
service = "web"
repository = "ghcr.io/org/app"
rule = "major"
reason = "Now redundant."
`)
	report := f.run(t)
	if !hasDiagnostic(report, "stale_approval", "unnecessary because") {
		t.Fatalf("expected stale approval diagnostic, got %v", rules(report))
	}
}

func TestCheckEnvFileRule(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n    env_file:\n      - path: .env\n        required: false\n")
	report := f.run(t)
	if !hasDiagnostic(report, "env_file", "project .env") {
		t.Fatalf("expected env_file diagnostic, got %v", rules(report))
	}

	f.write("approvals.toml", `[[env_file]]
compose = "stacks/app/web/compose.yaml"
service = "web"
path = ".env"
reason = "Upstream interface is huge."
`)
	report = f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected approval to waive, got %v", rules(report))
	}
}

func TestCheckBindAddressRule(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n    ports:\n      - '${CUSTOM_BIND}:8080:80'\n")
	report := f.run(t)
	if !hasDiagnostic(report, "bind_address", "CUSTOM_BIND") {
		t.Fatalf("expected bind_address diagnostic, got %v", rules(report))
	}

	f.write("approvals.toml", `[[bind_address]]
compose = "stacks/app/web/compose.yaml"
service = "web"
variables = ["CUSTOM_BIND"]
reason = "Dual-homed host."
`)
	report = f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected approval to waive, got %v", rules(report))
	}

	// Approval with a different variable set must not match.
	f.write("approvals.toml", `[[bind_address]]
compose = "stacks/app/web/compose.yaml"
service = "web"
variables = ["OTHER_BIND"]
reason = "Wrong set."
`)
	report = f.run(t)
	if !hasDiagnostic(report, "bind_address", "approval permits OTHER_BIND") {
		t.Fatalf("expected variable set mismatch, got %v", rules(report))
	}
}

func TestCheckPublishedPortConflict(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/edge/proxy/compose.yaml", "name: proxy\nservices:\n  proxy:\n    image: "+pinnedImage+"\n    ports:\n      - published: 8080\n        target: 80\n")
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n    ports:\n      - published: 8080\n        target: 8080\n")
	report := f.run(t)
	if !hasDiagnostic(report, "published_port", "8080/tcp is duplicated") {
		t.Fatalf("expected port conflict, got %v", rules(report))
	}

	f.write("approvals.toml", `[[published_port]]
port = 8080
protocol = "tcp"
reason = "Both proxies intentionally reserve the port."
bindings = [
  { compose = "stacks/edge/proxy/compose.yaml", service = "proxy", host_ip = "*" },
  { compose = "stacks/app/web/compose.yaml", service = "web", host_ip = "*" },
]
`)
	report = f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected approval to waive, got %v", rules(report))
	}
}

func TestCheckPortRangeExpansion(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/edge/a/compose.yaml", "name: a\nservices:\n  a:\n    image: "+pinnedImage+"\n    ports:\n      - published: 9000-9002\n        target: 9000\n")
	f.write("stacks/app/b/compose.yaml", "name: b\nservices:\n  b:\n    image: "+pinnedImage+"\n    ports:\n      - published: 9001\n        target: 9001\n")
	report := f.run(t)
	if !hasDiagnostic(report, "published_port", "9001/tcp is duplicated") {
		t.Fatalf("expected range-expanded conflict, got %v", rules(report))
	}
}

func TestCheckVolumeNaming(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n    volumes:\n      - data:/data\nvolumes:\n  data:\n")
	report := f.run(t)
	if !hasDiagnostic(report, "volume_name", "must set explicit name") {
		t.Fatalf("expected volume_name diagnostic, got %v", rules(report))
	}

	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n    volumes:\n      - data:/data\nvolumes:\n  data:\n    name: web_data\n")
	report = f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected clean report, got %v", rules(report))
	}
}

func TestCheckResourceOwnership(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/edge/owner/compose.yaml", "name: owner\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\n    networks: [shared]\nvolumes:\n  shared:\n    name: shared-volume\n    external: true\nnetworks:\n  shared:\n    name: shared-network\n    external: true\n")
	f.write("stacks/app/consumer/compose.yaml", "name: consumer\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\n    networks: [shared]\nvolumes:\n  shared:\n    name: shared-volume\n    external: true\nnetworks:\n  shared:\n    name: shared-network\n    external: true\n")
	report := f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected host-owned resources to pass by default, got %v", rules(report))
	}

	f.write("stacks/edge/owner/compose.yaml", "name: owner\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\n    networks: [shared]\nvolumes:\n  shared:\n    name: shared-volume\nnetworks:\n  shared:\n    name: shared-network\n")
	report = f.run(t)
	if !hasDiagnostic(report, "resource_owner", "shared-volume") {
		t.Fatalf("expected host-own volume diagnostic, got %v", rules(report))
	}
	if !hasDiagnostic(report, "resource_owner", "shared-network") {
		t.Fatalf("expected host-own network diagnostic, got %v", rules(report))
	}

	f.write("stacks/app/consumer/compose.yaml", "name: consumer\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\n    networks: [shared]\nvolumes:\n  shared:\n    name: shared-volume\nnetworks:\n  shared:\n    name: shared-network\n")
	report = f.run(t)
	if !hasDiagnostic(report, "resource_owner", "shared-volume") {
		t.Fatalf("expected duplicate volume owner diagnostic, got %v", rules(report))
	}
	if !hasDiagnostic(report, "resource_owner", "shared-network") {
		t.Fatalf("expected duplicate network owner diagnostic, got %v", rules(report))
	}
}

func TestCheckResourceOwnershipRespectsPortGroups(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", `[repository]
deployment_roots = ["stacks/edge", "stacks/app", "stacks/service"]
compose_filenames = ["compose.yaml"]

[ports]
shared_roots = ["stacks/edge"]

[resource_ownership]
volumes_mode = "stack-own"
networks_mode = "stack-own"
`)
	f.write("stacks/edge/shared/compose.yaml", "name: shared\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [data:/data]\n    networks: [backend]\nvolumes:\n  data:\n    name: shared-data\n    external: true\nnetworks:\n  backend:\n    name: shared-backend\n    external: true\n")
	for _, stack := range []string{"stacks/app/a", "stacks/service/b"} {
		f.write(stack+"/compose.yaml", "name: app\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [data:/data]\n    networks: [backend]\nvolumes:\n  data:\n    name: shared-data\nnetworks:\n  backend:\n    name: shared-backend\n")
	}
	report := f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected each non-shared root to own the resource in its own group, got %v", rules(report))
	}
}

func TestCheckResourceOwnershipModes(t *testing.T) {
	for _, mode := range []string{"stack-own", "either"} {
		t.Run(mode, func(t *testing.T) {
			f := newFixture(t)
			f.write("stacklint.toml", f.configToml()+"\n[resource_ownership]\nvolumes_mode = \""+mode+"\"\n")
			f.write("stacks/edge/a/compose.yaml", "name: a\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\nvolumes:\n  shared:\n    name: shared-volume\n")
			f.write("stacks/app/b/compose.yaml", "name: b\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\nvolumes:\n  shared:\n    name: shared-volume\n    external: true\n")
			if report := f.run(t); report.ExitCode != 0 {
				t.Fatalf("expected one owner to pass in %s mode, got %v", mode, rules(report))
			}
			f.write("stacks/app/b/compose.yaml", "name: b\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\nvolumes:\n  shared:\n    name: shared-volume\n")
			if report := f.run(t); !hasDiagnostic(report, "resource_owner", "shared-volume") {
				t.Fatalf("expected multiple owners to fail in %s mode, got %v", mode, rules(report))
			}
			if mode == "either" {
				f.write("stacks/edge/a/compose.yaml", "name: a\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\nvolumes:\n  shared:\n    name: shared-volume\n    external: true\n")
				f.write("stacks/app/b/compose.yaml", "name: b\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\nvolumes:\n  shared:\n    name: shared-volume\n    external: true\n")
				if report := f.run(t); report.ExitCode != 0 {
					t.Fatalf("expected all external declarations to pass in either mode, got %v", rules(report))
				}
			}
		})
	}
}

func TestCheckResourceOwnershipRequiresOwner(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+"\n[resource_ownership]\nvolumes_mode = \"stack-own\"\n")
	for _, stack := range []string{"stacks/edge/a", "stacks/app/b"} {
		f.write(stack+"/compose.yaml", "name: app\nservices:\n  app:\n    image: "+pinnedImage+"\n    volumes: [shared:/data]\nvolumes:\n  shared:\n    name: shared-volume\n    external: true\n")
	}
	report := f.run(t)
	if !hasDiagnostic(report, "resource_owner", "shared-volume") {
		t.Fatalf("expected stack-own mode to require an owner, got %v", rules(report))
	}
}

func TestCheckVariantCoverage(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+`
[[env.variant]]
stack = "stacks/app/web"
name = "postgres"
files = ["stacks/app/web/compose.yaml", "stacks/app/web/compose.postgres.yaml"]
output = ".env.postgres.example"
`)
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n")
	f.write("stacks/app/web/compose.postgres.yaml", "services:\n  db:\n    image: postgres:17\n")
	report := f.run(t)
	// The variant-only unpinned postgres image must be caught.
	if !hasDiagnostic(report, "image", "postgres:17 is not pinned") {
		t.Fatalf("expected variant image diagnostic, got %v", rules(report))
	}
	foundVariant := false
	for _, d := range report.Diagnostics {
		if d.Variant == "postgres" {
			foundVariant = true
		}
	}
	if !foundVariant {
		t.Fatalf("expected diagnostic with variant dimension, got %v", report.Diagnostics)
	}
}

func TestCheckVariantPortsDoNotSelfConflict(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+`
[[env.variant]]
stack = "stacks/app/web"
name = "postgres"
files = ["stacks/app/web/compose.yaml", "stacks/app/web/compose.postgres.yaml"]
output = ".env.postgres.example"
`)
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n    ports:\n      - published: 5432\n        target: 80\n")
	f.write("stacks/app/web/compose.postgres.yaml", "services:\n  db:\n    image: "+pinnedImage+"\n    ports:\n      - published: 5432\n        target: 5432\n")
	report := f.run(t)
	// base publishes 5432 (web), variant publishes 5432 (web + db). web vs db
	// in the variant IS a conflict (same combination, different services);
	// web(base) vs web(variant) must not double-report.
	count := 0
	for _, d := range report.Diagnostics {
		if d.Rule == "published_port" {
			count++
		}
	}
	if count == 0 {
		t.Fatalf("expected variant-internal conflict, got %v", rules(report))
	}
	for _, d := range report.Diagnostics {
		if d.Rule == "published_port" && d.Service == "web" && strings.Contains(d.Message, "web:web") {
			t.Fatalf("same service across variants must not conflict: %v", d.Message)
		}
	}
}

func TestCheckRenderFailureContinues(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/broken/compose.yaml", "name: broken\nservices:\n  bad:\n    image: ${MISSING_IMAGE:?required}\n")
	f.write("stacks/app/good/compose.yaml", "name: good\nservices:\n  good:\n    image: "+pinnedImage+"\n")
	report := f.run(t)
	if !hasDiagnostic(report, "render", "could not render") {
		t.Fatalf("expected render diagnostic, got %v", rules(report))
	}
	// The good stack must still be evaluated.
	found := false
	for _, r := range report.Results {
		if r.Stack == "stacks/app/good" && r.Status == "ok" {
			found = true
		}
	}
	if !found {
		t.Fatalf("good stack was not checked: %+v", report.Results)
	}
}

const moduleToml = `
[[module]]
compose = "stacks/compose/litestream/compose.yaml"
wiring_file = ".env.litestream"
wiring_template = ".env.litestream.example"
optional_wiring_variables = ["LITESTREAM_REPLICA_SCOPE"]
required_environment = { service = "litestream", prefix = "LITESTREAM_" }
`

func (f *fixture) writeModule() {
	f.write("stacks/compose/litestream/compose.yaml", "services:\n  litestream:\n    image: "+pinnedImage+"\n    environment:\n      LITESTREAM_DB: ${LITESTREAM_DB:?required}\n")
	f.write("stacks/compose/litestream/.env.litestream.example", "LITESTREAM_DB=\nLITESTREAM_REPLICA_SCOPE=\n")
}

func TestCheckWiringRule(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+moduleToml)
	f.writeModule()
	f.write("stacks/app/web/compose.yaml", "name: web\ninclude:\n  - path: ../../compose/litestream/compose.yaml\n    env_file: .env.litestream\nservices:\n  web:\n    image: "+pinnedImage+"\n")

	// Missing wiring file.
	report := f.run(t)
	if !hasDiagnostic(report, "wiring", "requires .env.litestream") {
		t.Fatalf("expected wiring diagnostic, got %v", rules(report))
	}

	// Complete wiring file passes.
	f.write("stacks/app/web/.env.litestream", "LITESTREAM_DB=/data/app.db\n")
	report = f.run(t)
	if report.ExitCode != 0 {
		t.Fatalf("expected clean report, got %v", rules(report))
	}

	// Extra variable in wiring file fails.
	f.write("stacks/app/web/.env.litestream", "LITESTREAM_DB=/data/app.db\nLITESTREAM_BOGUS=1\n")
	report = f.run(t)
	if !hasDiagnostic(report, "wiring", "variables missing from the module template") {
		t.Fatalf("expected extra variable diagnostic, got %v", rules(report))
	}

	// An @env-example marker on a wiring-provided variable would shadow it.
	f.write("stacks/app/web/.env.litestream", "LITESTREAM_DB=/data/app.db\n")
	f.write("stacks/app/web/compose.yaml", "name: web\ninclude:\n  - path: ../../compose/litestream/compose.yaml\n    env_file: .env.litestream\nservices:\n  web:\n    image: "+pinnedImage+"\n    # @env-example LITESTREAM_DB: /tmp/shadow.db\n")
	report = f.run(t)
	if !hasDiagnostic(report, "wiring", "must not provide values") {
		t.Fatalf("expected shadowing diagnostic, got %v", rules(report))
	}
}

func TestCheckOrphanWiringFile(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+moduleToml)
	f.writeModule()
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n")
	f.write("stacks/app/web/.env.litestream", "LITESTREAM_DB=/data/app.db\n")
	report := f.run(t)
	if !hasDiagnostic(report, "wiring", "orphan") {
		t.Fatalf("expected orphan wiring diagnostic, got %v", rules(report))
	}
}

func TestCheckModuleRequiredEnvironment(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml()+moduleToml)
	f.writeModule()
	// LITESTREAM_TOKEN is declared in the module but covered by neither an
	// example marker nor the wiring file, so it renders empty.
	f.write("stacks/compose/litestream/compose.yaml", "services:\n  litestream:\n    image: "+pinnedImage+"\n    environment:\n      LITESTREAM_DB: ${LITESTREAM_DB:?required}\n      LITESTREAM_TOKEN: ${LITESTREAM_TOKEN}\n")
	f.write("stacks/app/web/compose.yaml", "name: web\ninclude:\n  - path: ../../compose/litestream/compose.yaml\n    env_file: .env.litestream\nservices:\n  web:\n    image: "+pinnedImage+"\n")
	f.write("stacks/app/web/.env.litestream", "LITESTREAM_DB=/data/app.db\n")
	report := f.run(t)
	if !hasDiagnostic(report, "module_environment", "missing or empty") {
		t.Fatalf("expected module environment diagnostic, got %v", rules(report))
	}
	if !hasDiagnostic(report, "module_environment", "LITESTREAM_TOKEN") {
		t.Fatalf("expected LITESTREAM_TOKEN to be named, got %v", rules(report))
	}
}

func TestCheckPartialScopeSkipsStaleJudgement(t *testing.T) {
	f := newFixture(t)
	f.write("stacklint.toml", f.configToml())
	f.write("stacks/app/web/compose.yaml", "name: web\nservices:\n  web:\n    image: "+pinnedImage+"\n")
	f.write("stacks/app/other/compose.yaml", "name: other\nservices:\n  other:\n    image: "+pinnedImage+"\n")
	// Approval targets a stack outside the checked scope; must not be judged stale.
	f.write("approvals.toml", `[[image]]
compose = "stacks/app/other/compose.yaml"
service = "other"
repository = "ghcr.io/org/app"
rule = "major"
reason = "Out of scope."
`)
	report := f.run(t, "web")
	if report.ExitCode != 0 {
		t.Fatalf("out-of-scope approval must not be judged, got %v", rules(report))
	}
}
