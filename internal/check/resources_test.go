package check_test

import (
	"fmt"
	"strings"
	"testing"
)

func TestCheckResourceOwnershipIndependentModes(t *testing.T) {
	for _, tt := range []struct {
		name          string
		volumesMode   string
		networksMode  string
		volumeOwners  int
		networkOwners int
		volumeError   bool
		networkError  bool
	}{
		{name: "defaults allow all external"},
		{name: "volume override leaves network default", volumesMode: "stack-own", volumeOwners: 1, networkOwners: 1, networkError: true},
		{name: "network override leaves volume default", networksMode: "stack-own", volumeOwners: 1, networkOwners: 1, volumeError: true},
		{name: "volume owner required", volumesMode: "stack-own", networksMode: "either", volumeError: true},
		{name: "network owner required", volumesMode: "either", networksMode: "stack-own", networkError: true},
		{name: "volume either allows one owner", volumesMode: "either", networksMode: "host-own", volumeOwners: 1, networkOwners: 1, networkError: true},
		{name: "network either allows one owner", volumesMode: "host-own", networksMode: "either", volumeOwners: 1, networkOwners: 1, volumeError: true},
		{name: "stack and either reject multiple owners", volumesMode: "stack-own", networksMode: "either", volumeOwners: 2, networkOwners: 2, volumeError: true, networkError: true},
		{name: "either and stack reject multiple owners", volumesMode: "either", networksMode: "stack-own", volumeOwners: 2, networkOwners: 2, volumeError: true, networkError: true},
		{name: "stack and either accept one owner", volumesMode: "stack-own", networksMode: "either", volumeOwners: 1, networkOwners: 1},
		{name: "either and stack accept one owner", volumesMode: "either", networksMode: "stack-own", volumeOwners: 1, networkOwners: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			policy := "\n[resource_ownership]\n"
			if tt.volumesMode != "" {
				policy += fmt.Sprintf("volumes_mode = %q\n", tt.volumesMode)
			}
			if tt.networksMode != "" {
				policy += fmt.Sprintf("networks_mode = %q\n", tt.networksMode)
			}
			f.write("stacklint.toml", f.configToml()+policy)
			for i, stack := range []string{"stacks/edge/a", "stacks/app/b"} {
				f.write(stack+"/compose.yaml", fmt.Sprintf(`name: app
services:
  app:
    image: %s
    volumes: [data:/data]
    networks: [backend]
volumes:
  data:
    name: shared-data
    external: %t
networks:
  backend:
    name: shared-backend
    external: %t
`, pinnedImage, i >= tt.volumeOwners, i >= tt.networkOwners))
			}
			report := f.run(t)
			for _, diagnostic := range report.Diagnostics {
				if diagnostic.Rule != "resource_owner" || (!strings.HasPrefix(diagnostic.Message, "volume ") && !strings.HasPrefix(diagnostic.Message, "network ")) {
					t.Fatalf("unexpected diagnostic: %+v", diagnostic)
				}
			}
			if got := hasDiagnostic(report, "resource_owner", "volume "); got != tt.volumeError {
				t.Errorf("volume error: got %t, want %t; diagnostics: %v", got, tt.volumeError, rules(report))
			}
			if got := hasDiagnostic(report, "resource_owner", "network "); got != tt.networkError {
				t.Errorf("network error: got %t, want %t; diagnostics: %v", got, tt.networkError, rules(report))
			}
		})
	}
}
