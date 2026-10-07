package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// EnvExemption binds a template exemption to a stack and all its variants.
type EnvExemption struct {
	Stack  string `toml:"stack"`
	Kind   string `toml:"kind"`
	Reason string `toml:"reason"`
	Source string `toml:"source"`
}

type imageApproval struct {
	Compose    string `toml:"compose"`
	Service    string `toml:"service"`
	Repository string `toml:"repository"`
	Rule       string `toml:"rule"`
	Reason     string `toml:"reason"`
	Tag        string `toml:"tag"`
	Suffix     string `toml:"suffix"`
}
type portBinding struct {
	Compose string `toml:"compose"`
	Service string `toml:"service"`
	HostIP  string `toml:"host_ip"`
}
type portApproval struct {
	Port     int           `toml:"port"`
	Protocol string        `toml:"protocol"`
	Reason   string        `toml:"reason"`
	Bindings []portBinding `toml:"bindings"`
}
type bindApproval struct {
	Compose   string   `toml:"compose"`
	Service   string   `toml:"service"`
	Variables []string `toml:"variables"`
	Reason    string   `toml:"reason"`
}
type envFileApproval struct {
	Compose string `toml:"compose"`
	Service string `toml:"service"`
	Path    string `toml:"path"`
	Reason  string `toml:"reason"`
}

// LoadEnvExemptions reads approvals.toml without executing unrelated approval rules.
func LoadEnvExemptions(configDir string) (map[string]EnvExemption, error) {
	path := filepath.Join(configDir, "approvals.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]EnvExemption{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read approvals.toml: %v", ErrLoad, err)
	}
	var approvals struct {
		Images     []imageApproval   `toml:"image"`
		Ports      []portApproval    `toml:"published_port"`
		Binds      []bindApproval    `toml:"bind_address"`
		EnvFiles   []envFileApproval `toml:"env_file"`
		Exemptions []EnvExemption    `toml:"env_exemption"`
	}
	metadata, err := toml.Decode(string(data), &approvals)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot parse approvals.toml", ErrLoad)
	}
	if len(metadata.Undecoded()) > 0 {
		return nil, fmt.Errorf("%w: unknown approvals.toml keys: %v", ErrValidate, metadata.Undecoded())
	}
	result := map[string]EnvExemption{}
	for _, exemption := range approvals.Exemptions {
		if !filepath.IsLocal(exemption.Stack) {
			return nil, fmt.Errorf("%w: env_exemption.stack must be repository-relative", ErrValidate)
		}
		exemption.Stack = filepath.ToSlash(filepath.Clean(exemption.Stack))
		if _, exists := result[exemption.Stack]; exists {
			return nil, fmt.Errorf("%w: duplicate env_exemption for %s", ErrValidate, exemption.Stack)
		}
		switch exemption.Kind {
		case "pending":
		case "env-file-passthrough":
			if strings.TrimSpace(exemption.Reason) == "" {
				return nil, fmt.Errorf("%w: env-file-passthrough exemption for %s requires reason", ErrValidate, exemption.Stack)
			}
		case "upstream":
			if strings.TrimSpace(exemption.Reason) == "" || strings.TrimSpace(exemption.Source) == "" {
				return nil, fmt.Errorf("%w: upstream exemption for %s requires reason and source", ErrValidate, exemption.Stack)
			}
		default:
			return nil, fmt.Errorf("%w: unknown env exemption kind for %s", ErrValidate, exemption.Stack)
		}
		result[exemption.Stack] = exemption
	}
	return result, nil
}
