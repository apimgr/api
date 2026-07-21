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
