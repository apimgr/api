package docker

import (
	"fmt"
	"strings"
)

// Service provides Docker utilities
type Service struct{}

// New creates a new Docker service
func New() *Service {
	return &Service{}
}

// Dockerfile generation helpers
func (s *Service) GenerateDockerfile(config DockerfileConfig) string {
	var lines []string

	// FROM
	if config.BaseImage != "" {
		lines = append(lines, fmt.Sprintf("FROM %s", config.BaseImage))
	}

	// LABEL
	if config.Maintainer != "" {
		lines = append(lines, fmt.Sprintf("LABEL maintainer=\"%s\"", config.Maintainer))
	}

	// WORKDIR
	if config.WorkDir != "" {
		lines = append(lines, fmt.Sprintf("WORKDIR %s", config.WorkDir))
	}

	// COPY
	for _, copy := range config.CopyInstructions {
		lines = append(lines, fmt.Sprintf("COPY %s %s", copy.Source, copy.Dest))
	}

	// RUN
	for _, run := range config.RunCommands {
		lines = append(lines, fmt.Sprintf("RUN %s", run))
	}

	// ENV
	for key, value := range config.Environment {
		lines = append(lines, fmt.Sprintf("ENV %s=%s", key, value))
	}

	// EXPOSE
	for _, port := range config.ExposePorts {
		lines = append(lines, fmt.Sprintf("EXPOSE %d", port))
	}

	// VOLUME
	for _, volume := range config.Volumes {
		lines = append(lines, fmt.Sprintf("VOLUME %s", volume))
	}

	// CMD or ENTRYPOINT
	if config.Cmd != "" {
		lines = append(lines, fmt.Sprintf("CMD %s", config.Cmd))
	}
	if config.Entrypoint != "" {
		lines = append(lines, fmt.Sprintf("ENTRYPOINT %s", config.Entrypoint))
	}

	return strings.Join(lines, "\n")
}

type DockerfileConfig struct {
	BaseImage        string
	Maintainer       string
	WorkDir          string
	CopyInstructions []CopyInstruction
	RunCommands      []string
	Environment      map[string]string
	ExposePorts      []int
	Volumes          []string
	Cmd              string
	Entrypoint       string
}

type CopyInstruction struct {
	Source string
	Dest   string
}

// Docker compose helpers
func (s *Service) GenerateComposeService(name string, config ComposeServiceConfig) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("  %s:", name))

	if config.Image != "" {
		lines = append(lines, fmt.Sprintf("    image: %s", config.Image))
	}

	if config.Build != "" {
		lines = append(lines, fmt.Sprintf("    build: %s", config.Build))
	}

	if config.ContainerName != "" {
		lines = append(lines, fmt.Sprintf("    container_name: %s", config.ContainerName))
	}

	if len(config.Ports) > 0 {
		lines = append(lines, "    ports:")
		for _, port := range config.Ports {
			lines = append(lines, fmt.Sprintf("      - \"%s\"", port))
		}
	}

	if len(config.Volumes) > 0 {
		lines = append(lines, "    volumes:")
		for _, volume := range config.Volumes {
			lines = append(lines, fmt.Sprintf("      - %s", volume))
		}
	}

	if len(config.Environment) > 0 {
		lines = append(lines, "    environment:")
		for key, value := range config.Environment {
			lines = append(lines, fmt.Sprintf("      %s: %s", key, value))
		}
	}

	if config.Restart != "" {
		lines = append(lines, fmt.Sprintf("    restart: %s", config.Restart))
	}

	if len(config.DependsOn) > 0 {
		lines = append(lines, "    depends_on:")
		for _, dep := range config.DependsOn {
			lines = append(lines, fmt.Sprintf("      - %s", dep))
		}
	}

	return strings.Join(lines, "\n")
}

type ComposeServiceConfig struct {
	Image         string
	Build         string
	ContainerName string
	Ports         []string
	Volumes       []string
	Environment   map[string]string
	Restart       string
	DependsOn     []string
}

// Image name parsing
func (s *Service) ParseImageName(image string) *ImageInfo {
	info := &ImageInfo{
		Original: image,
	}

	// Split by : for tag
	parts := strings.Split(image, ":")
	if len(parts) == 2 {
		info.Tag = parts[1]
		image = parts[0]
	} else {
		info.Tag = "latest"
	}

	// Split by / for registry and repository
	slashParts := strings.Split(image, "/")
	if len(slashParts) >= 3 {
		info.Registry = slashParts[0]
		info.Namespace = slashParts[1]
		info.Repository = strings.Join(slashParts[2:], "/")
	} else if len(slashParts) == 2 {
		info.Namespace = slashParts[0]
		info.Repository = slashParts[1]
	} else {
		info.Repository = slashParts[0]
	}

	return info
}

type ImageInfo struct {
	Original   string `json:"original"`
	Registry   string `json:"registry"`
	Namespace  string `json:"namespace"`
	Repository string `json:"repository"`
	Tag        string `json:"tag"`
}

// Container name validation
func (s *Service) IsValidContainerName(name string) bool {
	// Docker container names: alphanumeric, underscore, period, hyphen
	// Must start with alphanumeric
	if len(name) == 0 {
		return false
	}

	firstChar := name[0]
	if !((firstChar >= 'a' && firstChar <= 'z') || (firstChar >= 'A' && firstChar <= 'Z') || (firstChar >= '0' && firstChar <= '9')) {
		return false
	}

	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-') {
			return false
		}
	}

	return true
}

// Port mapping helpers
func (s *Service) FormatPortMapping(hostPort, containerPort int, protocol string) string {
	if protocol == "" {
		protocol = "tcp"
	}
	return fmt.Sprintf("%d:%d/%s", hostPort, containerPort, protocol)
}

func (s *Service) ParsePortMapping(mapping string) (hostPort, containerPort int, protocol string, err error) {
	// Format: "hostPort:containerPort/protocol" or "hostPort:containerPort"
	parts := strings.Split(mapping, "/")
	if len(parts) == 2 {
		protocol = parts[1]
		mapping = parts[0]
	} else {
		protocol = "tcp"
	}

	portParts := strings.Split(mapping, ":")
	if len(portParts) != 2 {
		return 0, 0, "", fmt.Errorf("invalid port mapping format")
	}

	var hp, cp int
	if _, err := fmt.Sscanf(portParts[0], "%d", &hp); err != nil {
		return 0, 0, "", err
	}
	if _, err := fmt.Sscanf(portParts[1], "%d", &cp); err != nil {
		return 0, 0, "", err
	}

	return hp, cp, protocol, nil
}

// Volume helpers
func (s *Service) FormatVolumeMount(hostPath, containerPath string, readOnly bool) string {
	if readOnly {
		return fmt.Sprintf("%s:%s:ro", hostPath, containerPath)
	}
	return fmt.Sprintf("%s:%s", hostPath, containerPath)
}
