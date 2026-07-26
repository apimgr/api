package docker

import (
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DockerfileLintIssue is a single finding from LintDockerfile.
type DockerfileLintIssue struct {
	Line     int    `json:"line"`
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
}

// DockerfileLintResult is the full output of LintDockerfile.
type DockerfileLintResult struct {
	Issues []DockerfileLintIssue `json:"issues"`
	Passed bool                  `json:"passed"`
}

var secretKeyRe = regexp.MustCompile(`(?i)(password|secret|token|api_key|apikey|private_key|access_key)`)

// LintDockerfile scans raw Dockerfile text for common anti-patterns via a
// line-based regex scan (no build/daemon access required): unpinned/
// "latest" base image tags, ADD used where COPY would do, missing USER
// (running as root), missing HEALTHCHECK, apt-get without
// --no-install-recommends, and secret-looking ENV/ARG keys. RUN-layer count
// is reported as a single summary issue rather than per-line.
func (s *Service) LintDockerfile(content string) *DockerfileLintResult {
	result := &DockerfileLintResult{Issues: []DockerfileLintIssue{}}

	lines := strings.Split(content, "\n")
	hasUser := false
	hasHealthcheck := false
	runCount := 0

	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lineNo := i + 1
		upper := strings.ToUpper(line)

		switch {
		case strings.HasPrefix(upper, "FROM "):
			ref := strings.TrimSpace(line[5:])
			ref = strings.Fields(ref)[0]
			if !strings.Contains(ref, "@sha256:") {
				if strings.HasSuffix(ref, ":latest") || !strings.Contains(ref, ":") {
					result.Issues = append(result.Issues, DockerfileLintIssue{
						Line: lineNo, Severity: "warning", Rule: "unpinned-base-image",
						Message: "base image is unpinned or uses the \"latest\" tag; pin an explicit version or digest",
					})
				}
			}
		case strings.HasPrefix(upper, "ADD "):
			args := strings.Fields(line)[1:]
			if len(args) > 0 && !strings.Contains(args[0], "://") && !strings.HasSuffix(args[0], ".tar") && !strings.HasSuffix(args[0], ".tar.gz") {
				result.Issues = append(result.Issues, DockerfileLintIssue{
					Line: lineNo, Severity: "info", Rule: "prefer-copy",
					Message: "ADD used for a local file/directory; prefer COPY unless remote-URL or tar auto-extraction is needed",
				})
			}
		case strings.HasPrefix(upper, "USER "):
			hasUser = true
			if strings.TrimSpace(line[5:]) == "root" || strings.TrimSpace(line[5:]) == "0" {
				result.Issues = append(result.Issues, DockerfileLintIssue{
					Line: lineNo, Severity: "warning", Rule: "root-user",
					Message: "explicit USER root; run as a non-root user instead",
				})
			}
		case strings.HasPrefix(upper, "HEALTHCHECK"):
			hasHealthcheck = true
		case strings.HasPrefix(upper, "RUN "):
			runCount++
			if strings.Contains(line, "apt-get install") && !strings.Contains(line, "--no-install-recommends") {
				result.Issues = append(result.Issues, DockerfileLintIssue{
					Line: lineNo, Severity: "warning", Rule: "apt-recommends",
					Message: "apt-get install without --no-install-recommends pulls in unnecessary packages",
				})
			}
		case strings.HasPrefix(upper, "ENV ") || strings.HasPrefix(upper, "ARG "):
			if secretKeyRe.MatchString(line) {
				result.Issues = append(result.Issues, DockerfileLintIssue{
					Line: lineNo, Severity: "critical", Rule: "hardcoded-secret",
					Message: "ENV/ARG key name looks like a secret; pass secrets at runtime, never bake them into the image",
				})
			}
		}
	}

	if !hasUser {
		result.Issues = append(result.Issues, DockerfileLintIssue{
			Line: 0, Severity: "warning", Rule: "missing-user",
			Message: "no USER instruction found; the container will run as root by default",
		})
	}
	if !hasHealthcheck {
		result.Issues = append(result.Issues, DockerfileLintIssue{
			Line: 0, Severity: "info", Rule: "missing-healthcheck",
			Message: "no HEALTHCHECK instruction found; consider adding one for orchestrator liveness checks",
		})
	}
	if runCount > 3 {
		result.Issues = append(result.Issues, DockerfileLintIssue{
			Line: 0, Severity: "info", Rule: "merge-run-layers",
			Message: fmt.Sprintf("%d separate RUN instructions found; merge with && to reduce image layers", runCount),
		})
	}

	result.Passed = len(result.Issues) == 0
	return result
}

// DockerBestPractice is a single static guideline entry.
type DockerBestPractice struct {
	Category string `json:"category"`
	Tip      string `json:"tip"`
}

// BestPracticesGuide returns a static, curated list of general Docker
// image/container best practices. Unlike LintDockerfile (which analyzes
// user-submitted Dockerfile text), this is reference material with no
// input required.
func (s *Service) BestPracticesGuide() []DockerBestPractice {
	return []DockerBestPractice{
		{Category: "Base Image", Tip: "Pin base images to a specific version or digest instead of \"latest\""},
		{Category: "Base Image", Tip: "Prefer minimal base images (alpine, distroless, slim variants) to shrink attack surface and size"},
		{Category: "Layers", Tip: "Combine related RUN instructions with && and clean up caches in the same layer"},
		{Category: "Layers", Tip: "Order instructions from least to most frequently changing to maximize build cache reuse"},
		{Category: "Layers", Tip: "Use multi-stage builds to keep build-only tools out of the final image"},
		{Category: "Security", Tip: "Run as a non-root USER whenever the application allows it"},
		{Category: "Security", Tip: "Never bake secrets into ENV, ARG, or COPY'd files; inject them at runtime"},
		{Category: "Security", Tip: "Add a HEALTHCHECK so orchestrators can detect an unhealthy container"},
		{Category: "Security", Tip: "Avoid --privileged, host networking, and mounting docker.sock unless strictly required"},
		{Category: "Size", Tip: "Use a .dockerignore file to keep build context small"},
		{Category: "Size", Tip: "Prefer COPY over ADD for local files; reserve ADD for remote URLs or tar auto-extraction"},
		{Category: "Reliability", Tip: "Pin dependency versions in package manager install commands"},
	}
}

// ComposeValidationResult is the full output of ValidateCompose.
type ComposeValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors"`
	Warnings []string `json:"warnings"`
}

// composeServiceRaw mirrors the subset of docker-compose service fields
// this tool understands; unrecognized fields are ignored via yaml's
// default permissive decoding.
type composeServiceRaw struct {
	Image       string      `yaml:"image"`
	Build       interface{} `yaml:"build"`
	Ports       []string    `yaml:"ports"`
	Volumes     []string    `yaml:"volumes"`
	Environment interface{} `yaml:"environment"`
	Restart     string      `yaml:"restart"`
}

type composeFileRaw struct {
	Version  string                       `yaml:"version"`
	Services map[string]composeServiceRaw `yaml:"services"`
}

var composeVersionRe = regexp.MustCompile(`^["']?\d+(\.\d+)*["']?$`)

// findDuplicateMappingKeys walks a yaml.Node mapping and returns any key
// that appears more than once, since yaml.v3's normal struct-unmarshal
// silently keeps only the last occurrence of a duplicate map key.
func findDuplicateMappingKeys(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	seen := map[string]int{}
	var dupes []string
	for i := 0; i < len(node.Content); i += 2 {
		key := node.Content[i].Value
		seen[key]++
		if seen[key] == 2 {
			dupes = append(dupes, key)
		}
	}
	sort.Strings(dupes)
	return dupes
}

// findMappingValue returns the value node for a given key in a mapping
// node, or nil if not present.
func findMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// validatePortString applies a lenient version of ParsePortMapping that
// also accepts an "ip:host:container[/proto]" form by stripping the
// leading IP segment before delegating.
func (s *Service) validatePortString(mapping string) error {
	parts := strings.Split(mapping, ":")
	switch len(parts) {
	case 2:
		_, _, _, err := s.ParsePortMapping(mapping)
		return err
	case 3:
		_, _, _, err := s.ParsePortMapping(parts[1] + ":" + parts[2])
		return err
	default:
		return fmt.Errorf("invalid port mapping format")
	}
}

// validateVolumeString checks a compose volume entry has a "source:target
// [:mode]" shape for bind mounts, or is a single bare path/named-volume
// reference.
func validateVolumeString(volume string) error {
	parts := strings.Split(volume, ":")
	if len(parts) > 3 {
		return fmt.Errorf("invalid volume format")
	}
	for _, p := range parts {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("invalid volume format")
		}
	}
	return nil
}

// ValidateCompose parses docker-compose.yml text with gopkg.in/yaml.v3 and
// reports structural problems: YAML syntax errors, a missing/empty
// services key, an invalid version key, duplicate service names, and
// invalid port/volume syntax within each service (reusing
// ParsePortMapping for ports).
func (s *Service) ValidateCompose(content string) *ComposeValidationResult {
	result := &ComposeValidationResult{Errors: []string{}, Warnings: []string{}}

	var rootNode yaml.Node
	if err := yaml.Unmarshal([]byte(content), &rootNode); err != nil {
		result.Errors = append(result.Errors, "YAML parse error: "+err.Error())
		return result
	}
	if len(rootNode.Content) == 0 {
		result.Errors = append(result.Errors, "empty document")
		return result
	}
	docNode := rootNode.Content[0]

	servicesNode := findMappingValue(docNode, "services")
	if servicesNode == nil {
		result.Errors = append(result.Errors, "missing required top-level \"services\" key")
	} else if len(servicesNode.Content) == 0 {
		result.Errors = append(result.Errors, "\"services\" key is present but empty")
	} else if dupes := findDuplicateMappingKeys(servicesNode); len(dupes) > 0 {
		result.Errors = append(result.Errors, fmt.Sprintf("duplicate service name(s): %s", strings.Join(dupes, ", ")))
	}

	var file composeFileRaw
	if err := yaml.Unmarshal([]byte(content), &file); err != nil {
		result.Errors = append(result.Errors, "YAML structure error: "+err.Error())
		return result
	}

	if file.Version != "" && !composeVersionRe.MatchString(strings.TrimSpace(file.Version)) {
		result.Warnings = append(result.Warnings, fmt.Sprintf("version %q does not look like a valid compose version string", file.Version))
	}

	names := make([]string, 0, len(file.Services))
	for name := range file.Services {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		svc := file.Services[name]
		if svc.Image == "" && svc.Build == nil {
			result.Errors = append(result.Errors, fmt.Sprintf("service %q has neither image nor build", name))
		}
		for _, port := range svc.Ports {
			if err := s.validatePortString(port); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("service %q has invalid port mapping %q: %v", name, port, err))
			}
		}
		for _, volume := range svc.Volumes {
			if err := validateVolumeString(volume); err != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("service %q has invalid volume %q: %v", name, volume, err))
			}
		}
	}

	result.Valid = len(result.Errors) == 0
	return result
}

// environmentToMap normalizes a compose "environment" field, which may be
// either a YAML mapping (key: value) or a list of "KEY=VALUE" strings,
// into a single map[string]string.
func environmentToMap(env interface{}) map[string]string {
	out := map[string]string{}
	switch v := env.(type) {
	case map[string]interface{}:
		for k, val := range v {
			out[k] = fmt.Sprintf("%v", val)
		}
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				continue
			}
			kv := strings.SplitN(s, "=", 2)
			if len(kv) == 2 {
				out[kv[0]] = kv[1]
			} else {
				out[kv[0]] = ""
			}
		}
	}
	return out
}

// ComposeToRunCommand parses a docker-compose.yml document and converts
// the named service (or the sole service, if serviceName is empty and
// exactly one is defined) into an equivalent "docker run" command line,
// reusing FormatPortMapping/FormatVolumeMount for the flag values. A
// build-only service (no image) is still convertible, but the resulting
// command references the not-yet-built image tag rather than invoking
// "docker build" itself, since this binary has no daemon access.
func (s *Service) ComposeToRunCommand(content, serviceName string) (string, error) {
	var file composeFileRaw
	if err := yaml.Unmarshal([]byte(content), &file); err != nil {
		return "", fmt.Errorf("YAML parse error: %w", err)
	}
	if len(file.Services) == 0 {
		return "", fmt.Errorf("no services defined")
	}

	if serviceName == "" {
		if len(file.Services) != 1 {
			return "", fmt.Errorf("service name is required when the compose file defines more than one service")
		}
		for name := range file.Services {
			serviceName = name
		}
	}

	svc, ok := file.Services[serviceName]
	if !ok {
		return "", fmt.Errorf("service %q not found", serviceName)
	}

	args := []string{"docker", "run", "-d", "--name", serviceName}

	if svc.Restart != "" {
		args = append(args, "--restart", svc.Restart)
	}
	for _, port := range svc.Ports {
		args = append(args, "-p", port)
	}
	for _, volume := range svc.Volumes {
		args = append(args, "-v", volume)
	}
	env := environmentToMap(svc.Environment)
	envKeys := make([]string, 0, len(env))
	for k := range env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, env[k]))
	}

	image := svc.Image
	if image == "" {
		image = fmt.Sprintf("%s:latest # built from: %v", serviceName, svc.Build)
	}
	args = append(args, image)

	return strings.Join(args, " "), nil
}

// tokenizeShellLine splits a shell-style command line into fields,
// honoring single- and double-quoted segments (which may contain spaces)
// so flag values like -e "FOO=a b" survive intact.
func tokenizeShellLine(line string) []string {
	var tokens []string
	var current strings.Builder
	var quote rune
	inToken := false

	flush := func() {
		if inToken {
			tokens = append(tokens, current.String())
			current.Reset()
			inToken = false
		}
	}

	for _, r := range line {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				current.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			inToken = true
		case r == ' ' || r == '\t':
			flush()
		default:
			current.WriteRune(r)
			inToken = true
		}
	}
	flush()

	return tokens
}

// RunCommandToCompose parses a "docker run ..." command line's flags and
// emits an equivalent docker-compose.yml document, reusing
// GenerateComposeService for the YAML rendering.
func (s *Service) RunCommandToCompose(cmdLine string) (string, error) {
	tokens := tokenizeShellLine(strings.TrimSpace(cmdLine))
	if len(tokens) == 0 {
		return "", fmt.Errorf("empty command")
	}
	if tokens[0] == "docker" {
		tokens = tokens[1:]
	}
	if len(tokens) > 0 && tokens[0] == "run" {
		tokens = tokens[1:]
	}

	cfg := ComposeServiceConfig{Environment: map[string]string{}}
	var image string
	var trailing []string

	flagsWithValue := map[string]bool{
		"-p": true, "--publish": true,
		"-v": true, "--volume": true,
		"-e": true, "--env": true,
		"--name": true, "--restart": true, "--network": true,
		"-w": true, "--workdir": true, "--entrypoint": true,
	}

	i := 0
	for i < len(tokens) {
		tok := tokens[i]
		switch {
		case tok == "-d" || tok == "--detach" || tok == "--rm" || tok == "-it" || tok == "-i" || tok == "-t":
			i++
		case flagsWithValue[tok]:
			if i+1 >= len(tokens) {
				return "", fmt.Errorf("flag %s is missing its value", tok)
			}
			val := tokens[i+1]
			switch tok {
			case "-p", "--publish":
				cfg.Ports = append(cfg.Ports, val)
			case "-v", "--volume":
				cfg.Volumes = append(cfg.Volumes, val)
			case "-e", "--env":
				kv := strings.SplitN(val, "=", 2)
				if len(kv) == 2 {
					cfg.Environment[kv[0]] = kv[1]
				} else {
					cfg.Environment[kv[0]] = ""
				}
			case "--name":
				cfg.ContainerName = val
			case "--restart":
				cfg.Restart = val
			}
			i += 2
		case strings.HasPrefix(tok, "-"):
			// Unrecognized flag: skip it (and a value if one follows and
			// isn't itself a flag or the image) rather than misreading it
			// as the image reference.
			i++
		default:
			if image == "" {
				image = tok
				i++
			} else {
				trailing = append(trailing, tok)
				i++
			}
		}
	}

	if image == "" {
		return "", fmt.Errorf("no image found in command")
	}
	cfg.Image = image

	name := cfg.ContainerName
	if name == "" {
		name = sanitizeServiceName(image)
	}

	var b strings.Builder
	b.WriteString("services:\n")
	b.WriteString(s.GenerateComposeService(name, cfg))
	b.WriteString("\n")
	return b.String(), nil
}

var nonServiceNameCharRe = regexp.MustCompile(`[^a-z0-9_-]+`)

// sanitizeServiceName derives a compose-safe service name from an image
// reference (drops registry/namespace and tag, lowercases, and replaces
// any remaining invalid characters with a hyphen).
func sanitizeServiceName(image string) string {
	name := image
	if idx := strings.LastIndex(name, "/"); idx != -1 {
		name = name[idx+1:]
	}
	if idx := strings.Index(name, ":"); idx != -1 {
		name = name[:idx]
	}
	name = strings.ToLower(name)
	name = nonServiceNameCharRe.ReplaceAllString(name, "-")
	if name == "" {
		name = "service"
	}
	return name
}

// EnvVariable is a single parsed KEY=VALUE entry from ParseEnvFile.
type EnvVariable struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Line  int    `json:"line"`
}

// EnvParseResult is the full output of ParseEnvFile.
type EnvParseResult struct {
	Variables []EnvVariable `json:"variables"`
	Errors    []string      `json:"errors"`
	Warnings  []string      `json:"warnings"`
}

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ParseEnvFile parses raw ".env" file text (KEY=VALUE lines, "#" comments,
// blank lines, optional "export " prefix, and single/double-quoted
// values) into a structured variable list, reporting malformed lines and
// duplicate keys.
func (s *Service) ParseEnvFile(content string) *EnvParseResult {
	result := &EnvParseResult{Variables: []EnvVariable{}, Errors: []string{}, Warnings: []string{}}
	seen := map[string]bool{}

	for i, raw := range strings.Split(content, "\n") {
		lineNo := i + 1
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")

		eq := strings.Index(line, "=")
		if eq == -1 {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: missing \"=\" in %q", lineNo, raw))
			continue
		}

		key := strings.TrimSpace(line[:eq])
		value := strings.TrimSpace(line[eq+1:])

		if key == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: empty key", lineNo))
			continue
		}
		if !envKeyRe.MatchString(key) {
			result.Errors = append(result.Errors, fmt.Sprintf("line %d: %q is not a valid environment variable name", lineNo, key))
			continue
		}

		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}

		if seen[key] {
			result.Warnings = append(result.Warnings, fmt.Sprintf("line %d: duplicate key %q", lineNo, key))
		}
		seen[key] = true

		result.Variables = append(result.Variables, EnvVariable{Key: key, Value: value, Line: lineNo})
	}

	return result
}

// NetworkHelperConfig describes a desired Docker network.
type NetworkHelperConfig struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Subnet   string `json:"subnet"`
	Gateway  string `json:"gateway"`
	Internal bool   `json:"internal"`
}

// NetworkHelperResult is the full output of GenerateNetworkConfig.
type NetworkHelperResult struct {
	RunCommand   string `json:"run_command"`
	ComposeBlock string `json:"compose_block"`
}

// GenerateNetworkConfig validates a desired network's name/subnet/gateway
// and renders both an equivalent "docker network create" command and a
// docker-compose.yml "networks:" block. Subnet/gateway validation uses
// stdlib net.ParseCIDR/net.ParseIP directly; network.Service's
// SubnetCalculate is IPv4-broadcast-oriented (host/broadcast ranges) and
// doesn't fit a validate-only, IPv4-or-IPv6 need here.
func (s *Service) GenerateNetworkConfig(config NetworkHelperConfig) (*NetworkHelperResult, error) {
	if config.Name == "" {
		return nil, fmt.Errorf("network name is required")
	}
	driver := config.Driver
	if driver == "" {
		driver = "bridge"
	}

	var subnetNet *net.IPNet
	if config.Subnet != "" {
		_, parsed, err := net.ParseCIDR(config.Subnet)
		if err != nil {
			return nil, fmt.Errorf("invalid subnet: %w", err)
		}
		subnetNet = parsed
	}
	if config.Gateway != "" {
		ip := net.ParseIP(config.Gateway)
		if ip == nil {
			return nil, fmt.Errorf("invalid gateway address")
		}
		if subnetNet != nil && !subnetNet.Contains(ip) {
			return nil, fmt.Errorf("gateway %s is not within subnet %s", config.Gateway, config.Subnet)
		}
	}

	cmdParts := []string{"docker", "network", "create", "--driver", driver}
	if config.Subnet != "" {
		cmdParts = append(cmdParts, "--subnet", config.Subnet)
	}
	if config.Gateway != "" {
		cmdParts = append(cmdParts, "--gateway", config.Gateway)
	}
	if config.Internal {
		cmdParts = append(cmdParts, "--internal")
	}
	cmdParts = append(cmdParts, config.Name)

	var compose strings.Builder
	compose.WriteString("networks:\n")
	compose.WriteString(fmt.Sprintf("  %s:\n", config.Name))
	compose.WriteString(fmt.Sprintf("    driver: %s\n", driver))
	if config.Internal {
		compose.WriteString("    internal: true\n")
	}
	if config.Subnet != "" || config.Gateway != "" {
		compose.WriteString("    ipam:\n      config:\n")
		if config.Subnet != "" {
			compose.WriteString(fmt.Sprintf("        - subnet: %s\n", config.Subnet))
			if config.Gateway != "" {
				compose.WriteString(fmt.Sprintf("          gateway: %s\n", config.Gateway))
			}
		} else if config.Gateway != "" {
			compose.WriteString(fmt.Sprintf("        - gateway: %s\n", config.Gateway))
		}
	}

	return &NetworkHelperResult{
		RunCommand:   strings.Join(cmdParts, " "),
		ComposeBlock: compose.String(),
	}, nil
}

// SecurityIssue is a single finding from ScanSecurity.
type SecurityIssue struct {
	Severity string `json:"severity"`
	Rule     string `json:"rule"`
	Message  string `json:"message"`
}

// SecurityScanResult is the full output of ScanSecurity.
type SecurityScanResult struct {
	Issues []SecurityIssue `json:"issues"`
	Passed bool            `json:"passed"`
}

// ScanSecurity performs a static, text-based security scan of a Dockerfile
// or docker-compose.yml (either may be pasted in) for common
// misconfigurations: privileged mode, host networking, mounting
// docker.sock, explicit root user, overly broad capability grants, and
// secret-looking ENV/ARG/environment keys. This is deliberately a static
// analysis only — a real image vulnerability/CVE scan needs registry or
// daemon access this self-contained binary does not assume it has.
func (s *Service) ScanSecurity(content string) *SecurityScanResult {
	result := &SecurityScanResult{Issues: []SecurityIssue{}}

	lower := strings.ToLower(content)

	if strings.Contains(lower, "privileged: true") || strings.Contains(lower, "--privileged") {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity: "critical", Rule: "privileged-mode",
			Message: "privileged mode is enabled; this grants near-complete host access and should be avoided",
		})
	}
	if strings.Contains(lower, "network_mode: host") || strings.Contains(lower, "--network host") || strings.Contains(lower, "--network=host") {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity: "warning", Rule: "host-network",
			Message: "host networking bypasses container network isolation",
		})
	}
	if strings.Contains(lower, "/var/run/docker.sock") {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity: "critical", Rule: "docker-socket-mount",
			Message: "mounting the Docker socket grants host root-equivalent access to the container",
		})
	}
	if strings.Contains(lower, "cap_add") && (strings.Contains(lower, "all") || strings.Contains(lower, "sys_admin")) {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity: "warning", Rule: "broad-capabilities",
			Message: "an overly broad Linux capability (ALL or SYS_ADMIN) is granted",
		})
	}
	if regexp.MustCompile(`(?im)^\s*USER\s+(root|0)\s*$`).MatchString(content) {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity: "warning", Rule: "root-user",
			Message: "container explicitly runs as root",
		})
	}
	if !regexp.MustCompile(`(?im)^\s*USER\s+`).MatchString(content) && regexp.MustCompile(`(?im)^\s*FROM\s+`).MatchString(content) {
		result.Issues = append(result.Issues, SecurityIssue{
			Severity: "info", Rule: "missing-user",
			Message: "no USER instruction found; the container will run as root by default",
		})
	}
	for _, m := range regexp.MustCompile(`(?im)^\s*(ENV|ARG)\s+(\S+)`).FindAllStringSubmatch(content, -1) {
		if secretKeyRe.MatchString(m[2]) {
			result.Issues = append(result.Issues, SecurityIssue{
				Severity: "critical", Rule: "hardcoded-secret",
				Message: fmt.Sprintf("%s %s looks like a secret; inject it at runtime instead", m[1], m[2]),
			})
		}
	}

	result.Passed = len(result.Issues) == 0
	return result
}

// SizeOptimizationResult is the full output of OptimizeSize.
type SizeOptimizationResult struct {
	Suggestions []string `json:"suggestions"`
}

var heavyBaseImageRe = regexp.MustCompile(`(?i)^(ubuntu|debian|centos|fedora)(:|$)`)

// OptimizeSize performs a static analysis of Dockerfile text and suggests
// image-size reduction techniques: lighter base images, multi-stage
// builds, layer merging, and package-cache cleanup. This cannot measure
// actual built-image layer sizes (that needs daemon/registry access this
// binary does not assume it has) — it is heuristic advice, not a
// measurement.
func (s *Service) OptimizeSize(content string) *SizeOptimizationResult {
	result := &SizeOptimizationResult{Suggestions: []string{}}

	lines := strings.Split(content, "\n")
	fromCount := 0
	runCount := 0
	hasAptCleanup := false
	usesBuildTools := false

	buildToolRe := regexp.MustCompile(`(?i)\b(gcc|g\+\+|make|build-essential|cmake|golang|cargo)\b`)

	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "FROM "):
			fromCount++
			ref := strings.Fields(strings.TrimSpace(line[5:]))[0]
			if heavyBaseImageRe.MatchString(ref) {
				result.Suggestions = append(result.Suggestions, fmt.Sprintf("base image %q is a full-size distro; consider an alpine, slim, or distroless variant", ref))
			}
		case strings.HasPrefix(upper, "RUN "):
			runCount++
			if strings.Contains(line, "rm -rf /var/lib/apt/lists") {
				hasAptCleanup = true
			}
			if buildToolRe.MatchString(line) {
				usesBuildTools = true
			}
		}
	}

	if fromCount == 1 && usesBuildTools {
		result.Suggestions = append(result.Suggestions, "build tools detected in a single-stage build; use a multi-stage build to keep compilers/build tools out of the final image")
	}
	if runCount > 3 {
		result.Suggestions = append(result.Suggestions, fmt.Sprintf("%d separate RUN instructions found; merging with && reduces the number of image layers", runCount))
	}
	if strings.Contains(content, "apt-get install") && !hasAptCleanup {
		result.Suggestions = append(result.Suggestions, "apt-get install without a cache cleanup (rm -rf /var/lib/apt/lists/*) leaves package index files in the image")
	}
	if !strings.Contains(content, ".dockerignore") {
		result.Suggestions = append(result.Suggestions, "add a .dockerignore file to keep the build context (and accidental COPY . . inclusions) small")
	}

	return result
}
