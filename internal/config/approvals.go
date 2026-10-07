package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/YewFence/stacklint/internal/imageref"
)

// EnvExemption binds a template exemption to a stack and all its variants.
type EnvExemption struct {
	Stack  string `toml:"stack"`
	Kind   string `toml:"kind"`
	Reason string `toml:"reason"`
	Source string `toml:"source"`
}

// ImageApproval waives the digest-pinning rule for one service image.
type ImageApproval struct {
	Compose    string `toml:"compose"`
	Service    string `toml:"service"`
	Repository string `toml:"repository"`
	Rule       string `toml:"rule"`
	Reason     string `toml:"reason"`
	Tag        string `toml:"tag"`
	Suffix     string `toml:"suffix"`
}

// PortBinding is one binding covered by a published port approval.
type PortBinding struct {
	Compose string `toml:"compose"`
	Service string `toml:"service"`
	HostIP  string `toml:"host_ip"`
}

// PortApproval waives one duplicated published port within one port group.
type PortApproval struct {
	Port     int           `toml:"port"`
	Protocol string        `toml:"protocol"`
	Reason   string        `toml:"reason"`
	Bindings []PortBinding `toml:"bindings"`
}

// BindApproval waives nonstandard host bind address variables for one service.
type BindApproval struct {
	Compose   string   `toml:"compose"`
	Service   string   `toml:"service"`
	Variables []string `toml:"variables"`
	Reason    string   `toml:"reason"`
}

// EnvFileApproval waives one service-level env_file pointing at the project .env.
type EnvFileApproval struct {
	Compose string `toml:"compose"`
	Service string `toml:"service"`
	Path    string `toml:"path"`
	Reason  string `toml:"reason"`
}

// Approvals is the validated content of approvals.toml.
type Approvals struct {
	Images     []ImageApproval
	Ports      []PortApproval
	Binds      []BindApproval
	EnvFiles   []EnvFileApproval
	Exemptions []EnvExemption
}

type decodedApprovals struct {
	Images     []ImageApproval   `toml:"image"`
	Ports      []PortApproval    `toml:"published_port"`
	Binds      []BindApproval    `toml:"bind_address"`
	EnvFiles   []EnvFileApproval `toml:"env_file"`
	Exemptions []EnvExemption    `toml:"env_exemption"`
}

func decodeApprovals(configDir string) (*decodedApprovals, error) {
	path := filepath.Join(configDir, "approvals.toml")
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &decodedApprovals{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: read approvals.toml: %v", ErrLoad, err)
	}
	var approvals decodedApprovals
	metadata, err := toml.Decode(string(data), &approvals)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot parse approvals.toml", ErrLoad)
	}
	if len(metadata.Undecoded()) > 0 {
		return nil, fmt.Errorf("%w: unknown approvals.toml keys: %v", ErrValidate, metadata.Undecoded())
	}
	return &approvals, nil
}

// LoadEnvExemptions reads approvals.toml without executing unrelated approval rules.
func LoadEnvExemptions(configDir string) (map[string]EnvExemption, error) {
	approvals, err := decodeApprovals(configDir)
	if err != nil {
		return nil, err
	}
	return validateExemptions(approvals.Exemptions)
}

func validateExemptions(exemptions []EnvExemption) (map[string]EnvExemption, error) {
	result := map[string]EnvExemption{}
	for _, exemption := range exemptions {
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

// LoadApprovals reads and validates approvals.toml for the check command.
// A missing file yields empty approvals. Invalid entries fail the load:
// approvals are safety-relevant and never silently ignored.
func LoadApprovals(configDir string, repoRoot string, cfg *Config) (*Approvals, error) {
	decoded, err := decodeApprovals(configDir)
	if err != nil {
		return nil, err
	}
	if _, err := validateExemptions(decoded.Exemptions); err != nil {
		return nil, err
	}
	approvals := &Approvals{Exemptions: decoded.Exemptions}
	seen := map[string]bool{}
	for i, image := range decoded.Images {
		label := fmt.Sprintf("image approval %d", i+1)
		if err := validateImageApproval(image, label, repoRoot); err != nil {
			return nil, err
		}
		key := "image\x00" + image.Compose + "\x00" + image.Service
		if seen[key] {
			return nil, fmt.Errorf("%w: %s duplicates approval for %s:%s", ErrValidate, label, image.Compose, image.Service)
		}
		seen[key] = true
		approvals.Images = append(approvals.Images, image)
	}
	for i, port := range decoded.Ports {
		label := fmt.Sprintf("published port approval %d", i+1)
		if err := validatePortApproval(port, label, repoRoot, cfg); err != nil {
			return nil, err
		}
		group, _ := portApprovalGroup(cfg, port)
		key := fmt.Sprintf("port\x00%s\x00%d\x00%s", group, port.Port, port.Protocol)
		if seen[key] {
			return nil, fmt.Errorf("%w: %s duplicates approval for %d/%s in group %s", ErrValidate, label, port.Port, port.Protocol, group)
		}
		seen[key] = true
		approvals.Ports = append(approvals.Ports, port)
	}
	for i, bind := range decoded.Binds {
		label := fmt.Sprintf("bind address approval %d", i+1)
		if err := validateBindApproval(bind, label, repoRoot, cfg.BindAddress.DefaultVariable); err != nil {
			return nil, err
		}
		key := "bind\x00" + bind.Compose + "\x00" + bind.Service
		if seen[key] {
			return nil, fmt.Errorf("%w: %s duplicates approval for %s:%s", ErrValidate, label, bind.Compose, bind.Service)
		}
		seen[key] = true
		approvals.Binds = append(approvals.Binds, bind)
	}
	for i, envFile := range decoded.EnvFiles {
		label := fmt.Sprintf("env_file approval %d", i+1)
		if err := validateEnvFileApproval(envFile, label, repoRoot); err != nil {
			return nil, err
		}
		key := "envfile\x00" + envFile.Compose + "\x00" + envFile.Service + "\x00" + envFile.Path
		if seen[key] {
			return nil, fmt.Errorf("%w: %s duplicates approval for %s:%s:%s", ErrValidate, label, envFile.Compose, envFile.Service, envFile.Path)
		}
		seen[key] = true
		approvals.EnvFiles = append(approvals.EnvFiles, envFile)
	}
	return approvals, nil
}

func requireField(value, label, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%w: %s field %q must be a non-empty string", ErrValidate, label, field)
	}
	return nil
}

// approvalComposePath validates a canonical repository-relative Compose path
// and confirms the file exists.
func approvalComposePath(value, label, repoRoot string) error {
	if err := requireField(value, label, "compose"); err != nil {
		return err
	}
	if !filepath.IsLocal(value) || filepath.ToSlash(filepath.Clean(value)) != value {
		return fmt.Errorf("%w: %s compose %q must be a canonical repository-relative path", ErrValidate, label, value)
	}
	info, err := os.Stat(filepath.Join(repoRoot, value))
	if err != nil || info.IsDir() {
		return fmt.Errorf("%w: %s compose %q does not exist; remove the stale approval or correct its path", ErrValidate, label, value)
	}
	return nil
}

func validateImageApproval(approval ImageApproval, label, repoRoot string) error {
	if err := approvalComposePath(approval.Compose, label, repoRoot); err != nil {
		return err
	}
	for field, value := range map[string]string{"service": approval.Service, "repository": approval.Repository, "rule": approval.Rule, "reason": approval.Reason} {
		if err := requireField(value, label, field); err != nil {
			return err
		}
	}
	if err := imageref.ValidateApproval(imageref.Approval{
		Repository: approval.Repository,
		Rule:       approval.Rule,
		Tag:        approval.Tag,
		Suffix:     approval.Suffix,
	}); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrValidate, label, err)
	}
	return nil
}

func validatePortApproval(approval PortApproval, label, repoRoot string, cfg *Config) error {
	if approval.Port < 1 || approval.Port > 65535 {
		return fmt.Errorf("%w: %s field \"port\" must be an integer from 1 to 65535", ErrValidate, label)
	}
	if !slices.Contains([]string{"tcp", "udp", "sctp"}, approval.Protocol) {
		return fmt.Errorf("%w: %s protocol %q is not supported; use one of sctp, tcp, udp", ErrValidate, label, approval.Protocol)
	}
	if err := requireField(approval.Reason, label, "reason"); err != nil {
		return err
	}
	if len(approval.Bindings) < 2 {
		return fmt.Errorf("%w: %s field \"bindings\" must contain at least two inline tables", ErrValidate, label)
	}
	for i, binding := range approval.Bindings {
		bindingLabel := fmt.Sprintf("%s binding %d", label, i+1)
		if err := approvalComposePath(binding.Compose, bindingLabel, repoRoot); err != nil {
			return err
		}
		if err := requireField(binding.Service, bindingLabel, "service"); err != nil {
			return err
		}
		if err := requireField(binding.HostIP, bindingLabel, "host_ip"); err != nil {
			return err
		}
	}
	if _, err := portApprovalGroup(cfg, approval); err != nil {
		return fmt.Errorf("%w: %s %v", ErrValidate, label, err)
	}
	return nil
}

// PortGroup derives the approval's port group from its bindings' Compose
// paths: exactly one non-shared deployment root must be present.
func (c *Config) PortGroup(approval PortApproval) (string, error) {
	return portApprovalGroup(c, approval)
}

// portApprovalGroup derives an approval's port group from its bindings'
// Compose paths: exactly one non-shared deployment root must be present.
func portApprovalGroup(cfg *Config, approval PortApproval) (string, error) {
	roots := map[string]bool{}
	for _, binding := range approval.Bindings {
		if root := cfg.DeploymentRootOf(binding.Compose); root != "" {
			roots[root] = true
		}
	}
	if len(roots) != 1 {
		return "", fmt.Errorf("bindings must belong to exactly one non-shared deployment root, optionally alongside shared roots")
	}
	for root := range roots {
		return cfg.PortGroupName(root), nil
	}
	return "", nil
}

func validateBindApproval(approval BindApproval, label, repoRoot string, defaultVariable string) error {
	if err := approvalComposePath(approval.Compose, label, repoRoot); err != nil {
		return err
	}
	if err := requireField(approval.Service, label, "service"); err != nil {
		return err
	}
	if err := requireField(approval.Reason, label, "reason"); err != nil {
		return err
	}
	if len(approval.Variables) == 0 {
		return fmt.Errorf("%w: %s field \"variables\" must be a non-empty array of strings", ErrValidate, label)
	}
	variables := map[string]bool{}
	for i, variable := range approval.Variables {
		variableLabel := fmt.Sprintf("%s variable %d", label, i+1)
		if err := requireField(variable, variableLabel, "variables"); err != nil {
			return err
		}
	if !validVariableName(variable) {
			return fmt.Errorf("%w: %s value %q is not a valid environment variable name", ErrValidate, variableLabel, variable)
		}
		if variable == defaultVariable {
			return fmt.Errorf("%w: %s must not approve %s because it is the default policy", ErrValidate, variableLabel, defaultVariable)
		}
		if variables[variable] {
			return fmt.Errorf("%w: %s duplicates variable %q", ErrValidate, label, variable)
		}
		variables[variable] = true
	}
	return nil
}

func validVariableName(name string) bool {
	if name == "" {
		return false
	}
	for i, char := range name {
		if char == '_' || char >= 'A' && char <= 'Z' || char >= 'a' && char <= 'z' || i > 0 && char >= '0' && char <= '9' {
			continue
		}
		return false
	}
	return true
}

func validateEnvFileApproval(approval EnvFileApproval, label, repoRoot string) error {
	if err := approvalComposePath(approval.Compose, label, repoRoot); err != nil {
		return err
	}
	if err := requireField(approval.Service, label, "service"); err != nil {
		return err
	}
	if err := requireField(approval.Reason, label, "reason"); err != nil {
		return err
	}
	if approval.Path != ".env" {
		return fmt.Errorf("%w: %s path %q must be the canonical project relative path .env", ErrValidate, label, approval.Path)
	}
	return nil
}
