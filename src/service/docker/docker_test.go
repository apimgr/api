package docker

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GenerateDockerfile covers a full config (every section populated), a
// minimal/empty config (no lines at all), and confirms optional
// sections are omitted when unset.
func TestGenerateDockerfile(t *testing.T) {
	s := New()

	cfg := DockerfileConfig{
		BaseImage:  "alpine:latest",
		Maintainer: "casjay",
		WorkDir:    "/app",
		CopyInstructions: []CopyInstruction{
			{Source: ".", Dest: "/app"},
		},
		RunCommands: []string{"go build ./..."},
		Environment: map[string]string{"FOO": "bar"},
		ExposePorts: []int{8080},
		Volumes:     []string{"/data"},
		Cmd:         "./app",
	}

	out := s.GenerateDockerfile(cfg)
	assert.Contains(t, out, "FROM alpine:latest")
	assert.Contains(t, out, `LABEL maintainer="casjay"`)
	assert.Contains(t, out, "WORKDIR /app")
	assert.Contains(t, out, "COPY . /app")
	assert.Contains(t, out, "RUN go build ./...")
	assert.Contains(t, out, "ENV FOO=bar")
	assert.Contains(t, out, "EXPOSE 8080")
	assert.Contains(t, out, "VOLUME /data")
	assert.Contains(t, out, "CMD ./app")
	assert.NotContains(t, out, "ENTRYPOINT")

	empty := s.GenerateDockerfile(DockerfileConfig{})
	assert.Equal(t, "", empty)

	// Entrypoint-only config should include ENTRYPOINT but not CMD.
	entry := s.GenerateDockerfile(DockerfileConfig{Entrypoint: "/entrypoint.sh"})
	assert.Contains(t, entry, "ENTRYPOINT /entrypoint.sh")
	assert.NotContains(t, entry, "CMD")
}

// GenerateComposeService covers a fully populated service block, an
// empty one (name line only), and verifies list-valued fields render
// as indented YAML sequences.
func TestGenerateComposeService(t *testing.T) {
	s := New()

	cfg := ComposeServiceConfig{
		Image:         "nginx:latest",
		ContainerName: "web",
		Ports:         []string{"80:80"},
		Volumes:       []string{"./data:/data"},
		Environment:   map[string]string{"ENV": "prod"},
		Restart:       "always",
		DependsOn:     []string{"db"},
	}

	out := s.GenerateComposeService("web", cfg)
	assert.True(t, strings.HasPrefix(out, "  web:\n"))
	assert.Contains(t, out, "    image: nginx:latest")
	assert.Contains(t, out, "    container_name: web")
	assert.Contains(t, out, "    ports:\n      - \"80:80\"")
	assert.Contains(t, out, "    volumes:\n      - ./data:/data")
	assert.Contains(t, out, "    environment:\n      ENV: prod")
	assert.Contains(t, out, "    restart: always")
	assert.Contains(t, out, "    depends_on:\n      - db")

	minimal := s.GenerateComposeService("svc", ComposeServiceConfig{})
	assert.Equal(t, "  svc:", minimal)

	build := s.GenerateComposeService("svc", ComposeServiceConfig{Build: "."})
	assert.Contains(t, build, "    build: .")
}

// ParseImageName covers a bare repository, repo:tag, namespace/repo,
// and a fully qualified registry/namespace/repo:tag.
func TestParseImageName(t *testing.T) {
	s := New()

	cases := []struct {
		name string
		in   string
		want ImageInfo
	}{
		{
			"bare repo, default tag",
			"alpine",
			ImageInfo{Original: "alpine", Repository: "alpine", Tag: "latest"},
		},
		{
			"repo with tag",
			"alpine:3.18",
			ImageInfo{Original: "alpine:3.18", Repository: "alpine", Tag: "3.18"},
		},
		{
			"namespace/repo with tag",
			"library/alpine:3.18",
			ImageInfo{Original: "library/alpine:3.18", Namespace: "library", Repository: "alpine", Tag: "3.18"},
		},
		{
			"registry/namespace/repo with tag",
			"ghcr.io/apimgr/api:v1.0.0",
			ImageInfo{Original: "ghcr.io/apimgr/api:v1.0.0", Registry: "ghcr.io", Namespace: "apimgr", Repository: "api", Tag: "v1.0.0"},
		},
		{
			"deep repository path",
			"registry.example.com/org/team/app:latest",
			ImageInfo{Original: "registry.example.com/org/team/app:latest", Registry: "registry.example.com", Namespace: "org", Repository: "team/app", Tag: "latest"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := s.ParseImageName(c.in)
			require.NotNil(t, got)
			assert.Equal(t, c.want, *got)
		})
	}
}

// IsValidContainerName covers empty, a name starting with a disallowed
// character, all allowed special characters, and a disallowed embedded
// character.
func TestIsValidContainerName(t *testing.T) {
	s := New()

	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"empty", "", false},
		{"starts with underscore", "_bad", false},
		{"starts with hyphen", "-bad", false},
		{"simple alnum", "web1", true},
		{"with allowed specials", "my.web_app-1", true},
		{"embedded space", "my app", false},
		{"embedded slash", "my/app", false},
		{"single char", "a", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, s.IsValidContainerName(c.in))
		})
	}
}

// FormatPortMapping covers the default-protocol path and an explicit
// protocol.
func TestFormatPortMapping(t *testing.T) {
	s := New()

	assert.Equal(t, "8080:80/tcp", s.FormatPortMapping(8080, 80, ""))
	assert.Equal(t, "53:53/udp", s.FormatPortMapping(53, 53, "udp"))
}

// ParsePortMapping covers a mapping with explicit protocol, one
// defaulting to tcp, and malformed input (missing colon, non-numeric
// port).
func TestParsePortMapping(t *testing.T) {
	s := New()

	hp, cp, proto, err := s.ParsePortMapping("8080:80/tcp")
	require.NoError(t, err)
	assert.Equal(t, 8080, hp)
	assert.Equal(t, 80, cp)
	assert.Equal(t, "tcp", proto)

	hp, cp, proto, err = s.ParsePortMapping("53:53")
	require.NoError(t, err)
	assert.Equal(t, 53, hp)
	assert.Equal(t, 53, cp)
	assert.Equal(t, "tcp", proto)

	_, _, _, err = s.ParsePortMapping("8080-80")
	assert.Error(t, err)

	_, _, _, err = s.ParsePortMapping("abc:80")
	assert.Error(t, err)

	_, _, _, err = s.ParsePortMapping("80:abc")
	assert.Error(t, err)
}

// FormatVolumeMount covers the read-only and read-write variants.
func TestFormatVolumeMount(t *testing.T) {
	s := New()

	assert.Equal(t, "/host:/container", s.FormatVolumeMount("/host", "/container", false))
	assert.Equal(t, "/host:/container:ro", s.FormatVolumeMount("/host", "/container", true))
}

// LintDockerfile covers a clean-ish Dockerfile (still flags missing
// HEALTHCHECK) and a Dockerfile that trips every rule at once.
func TestLintDockerfile(t *testing.T) {
	s := New()

	clean := "FROM alpine:3.18\nUSER app\nHEALTHCHECK CMD true\nRUN echo hi\n"
	out := s.LintDockerfile(clean)
	assert.True(t, out.Passed)
	assert.Empty(t, out.Issues)

	bad := "FROM ubuntu\n" +
		"ADD app.tar.gz /app\n" +
		"ADD local.txt /app\n" +
		"ENV API_KEY=abc123\n" +
		"RUN apt-get install foo\n" +
		"RUN echo one\n" +
		"RUN echo two\n" +
		"RUN echo three\n" +
		"RUN echo four\n" +
		"USER root\n"
	out = s.LintDockerfile(bad)
	assert.False(t, out.Passed)

	rules := make(map[string]bool)
	for _, issue := range out.Issues {
		rules[issue.Rule] = true
	}
	assert.True(t, rules["unpinned-base-image"])
	assert.True(t, rules["prefer-copy"])
	assert.True(t, rules["hardcoded-secret"])
	assert.True(t, rules["apt-recommends"])
	assert.True(t, rules["root-user"])
	assert.True(t, rules["missing-healthcheck"])
	assert.True(t, rules["merge-run-layers"])
	assert.False(t, rules["missing-user"])
}

// BestPracticesGuide just verifies the static guide is non-empty and each
// entry has both fields populated.
func TestBestPracticesGuide(t *testing.T) {
	s := New()

	guide := s.BestPracticesGuide()
	require.NotEmpty(t, guide)
	for _, tip := range guide {
		assert.NotEmpty(t, tip.Category)
		assert.NotEmpty(t, tip.Tip)
	}
}

// ValidateCompose covers a valid file, a YAML syntax error, a missing
// services key, a duplicate service name, and a service with an invalid
// port mapping.
func TestValidateCompose(t *testing.T) {
	s := New()

	valid := "services:\n  web:\n    image: nginx:latest\n    ports:\n      - \"80:80\"\n"
	out := s.ValidateCompose(valid)
	assert.True(t, out.Valid)
	assert.Empty(t, out.Errors)

	out = s.ValidateCompose("services: [")
	assert.False(t, out.Valid)
	assert.NotEmpty(t, out.Errors)

	out = s.ValidateCompose("version: \"3\"\n")
	assert.False(t, out.Valid)
	assert.Contains(t, strings.Join(out.Errors, " "), "services")

	dupe := "services:\n  web:\n    image: a\n  web:\n    image: b\n"
	out = s.ValidateCompose(dupe)
	assert.False(t, out.Valid)
	assert.Contains(t, strings.Join(out.Errors, " "), "duplicate")

	badPort := "services:\n  web:\n    image: nginx\n    ports:\n      - \"notaport\"\n"
	out = s.ValidateCompose(badPort)
	assert.False(t, out.Valid)
}

// ComposeToRunCommand covers a single-service file (implicit selection),
// a named-service lookup, and error cases (no services, unknown service
// name, ambiguous multi-service file with no name given).
func TestComposeToRunCommand(t *testing.T) {
	s := New()

	single := "services:\n  web:\n    image: nginx:latest\n    ports:\n      - \"80:80\"\n    restart: always\n"
	out, err := s.ComposeToRunCommand(single, "")
	require.NoError(t, err)
	assert.Contains(t, out, "docker run -d --name web")
	assert.Contains(t, out, "--restart always")
	assert.Contains(t, out, "-p 80:80")
	assert.Contains(t, out, "nginx:latest")

	multi := "services:\n  web:\n    image: nginx\n  db:\n    image: postgres\n"
	_, err = s.ComposeToRunCommand(multi, "")
	assert.Error(t, err)

	out, err = s.ComposeToRunCommand(multi, "db")
	require.NoError(t, err)
	assert.Contains(t, out, "postgres")

	_, err = s.ComposeToRunCommand(multi, "cache")
	assert.Error(t, err)

	_, err = s.ComposeToRunCommand("services: {}\n", "")
	assert.Error(t, err)
}

// RunCommandToCompose covers a typical docker run invocation with
// multiple flag types and error cases (empty command, no image found).
func TestRunCommandToCompose(t *testing.T) {
	s := New()

	cmd := `docker run -d --name web -p 8080:80 -v /data:/data -e FOO=bar --restart always nginx:latest`
	out, err := s.RunCommandToCompose(cmd)
	require.NoError(t, err)
	assert.Contains(t, out, "services:")
	assert.Contains(t, out, "web:")
	assert.Contains(t, out, "image: nginx:latest")
	assert.Contains(t, out, "8080:80")
	assert.Contains(t, out, "/data:/data")
	assert.Contains(t, out, "FOO: bar")
	assert.Contains(t, out, "restart: always")

	_, err = s.RunCommandToCompose("")
	assert.Error(t, err)

	_, err = s.RunCommandToCompose("docker run -d")
	assert.Error(t, err)
}

// ParseEnvFile covers comments/blank lines, quoted values, a malformed
// line, an empty key, and a duplicate key warning.
func TestParseEnvFile(t *testing.T) {
	s := New()

	content := "# comment\n\nexport FOO=bar\nBAZ=\"quoted value\"\nBAD_LINE\n=novalue\nFOO=again\n"
	out := s.ParseEnvFile(content)

	vars := make(map[string]string)
	for _, v := range out.Variables {
		vars[v.Key] = v.Value
	}
	assert.Equal(t, "again", vars["FOO"])
	assert.Equal(t, "quoted value", vars["BAZ"])
	assert.NotEmpty(t, out.Errors)
	assert.NotEmpty(t, out.Warnings)
}

// GenerateNetworkConfig covers a fully specified network, an invalid
// subnet, a gateway outside the subnet, and a missing name.
func TestGenerateNetworkConfig(t *testing.T) {
	s := New()

	out, err := s.GenerateNetworkConfig(NetworkHelperConfig{
		Name: "mynet", Driver: "bridge", Subnet: "172.20.0.0/16", Gateway: "172.20.0.1",
	})
	require.NoError(t, err)
	assert.Contains(t, out.RunCommand, "docker network create --driver bridge --subnet 172.20.0.0/16 --gateway 172.20.0.1 mynet")
	assert.Contains(t, out.ComposeBlock, "networks:")
	assert.Contains(t, out.ComposeBlock, "subnet: 172.20.0.0/16")

	_, err = s.GenerateNetworkConfig(NetworkHelperConfig{Name: "bad", Subnet: "not-a-cidr"})
	assert.Error(t, err)

	_, err = s.GenerateNetworkConfig(NetworkHelperConfig{Name: "bad", Subnet: "10.0.0.0/24", Gateway: "192.168.1.1"})
	assert.Error(t, err)

	_, err = s.GenerateNetworkConfig(NetworkHelperConfig{})
	assert.Error(t, err)
}

// ScanSecurity covers a clean input and one that trips every rule.
func TestScanSecurity(t *testing.T) {
	s := New()

	out := s.ScanSecurity("FROM alpine\nUSER app\n")
	assert.True(t, out.Passed)

	bad := "FROM alpine\n" +
		"ENV DB_PASSWORD=hunter2\n" +
		"USER root\n" +
		"privileged: true\n" +
		"network_mode: host\n" +
		"- /var/run/docker.sock:/var/run/docker.sock\n" +
		"cap_add:\n  - ALL\n"
	out = s.ScanSecurity(bad)
	assert.False(t, out.Passed)

	rules := make(map[string]bool)
	for _, issue := range out.Issues {
		rules[issue.Rule] = true
	}
	assert.True(t, rules["privileged-mode"])
	assert.True(t, rules["host-network"])
	assert.True(t, rules["docker-socket-mount"])
	assert.True(t, rules["broad-capabilities"])
	assert.True(t, rules["root-user"])
	assert.True(t, rules["hardcoded-secret"])
}

// OptimizeSize covers a heavy-base-image, multi-RUN, build-tools
// Dockerfile and confirms suggestions are produced for each concern.
func TestOptimizeSize(t *testing.T) {
	s := New()

	content := "FROM ubuntu:20.04\n" +
		"RUN apt-get install -y build-essential\n" +
		"RUN echo one\n" +
		"RUN echo two\n" +
		"RUN echo three\n" +
		"RUN echo four\n"
	out := s.OptimizeSize(content)
	assert.NotEmpty(t, out.Suggestions)

	joined := strings.Join(out.Suggestions, " ")
	assert.Contains(t, joined, "full-size distro")
	assert.Contains(t, joined, "multi-stage")
	assert.Contains(t, joined, "RUN instructions")
	assert.Contains(t, joined, "cache cleanup")
	assert.Contains(t, joined, "dockerignore")
}
