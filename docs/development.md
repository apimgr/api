# Development Guide

Guide for contributing to and developing the API Toolkit.

## Prerequisites

- **Docker** - For building and testing (required)
- **Git** - For version control
- **Make** - For build automation

!!! important
    **Go is NOT required on the host system.** All builds and tests run inside Docker containers.

## Getting Started

### Clone the Repository

```bash
git clone https://github.com/apimgr/api.git
cd api
```

### Project Structure

```
api/
├── src/                    # Go source code
│   ├── main.go             # Application entry point
│   ├── config/             # Configuration management
│   ├── server/             # HTTP server
│   ├── swagger/            # OpenAPI/Swagger
│   ├── graphql/            # GraphQL API
│   ├── services/           # Business logic
│   ├── mode/               # Application mode detection
│   ├── paths/              # Path resolution
│   ├── ssl/                # SSL/TLS handling
│   └── scheduler/          # Background tasks
├── docker/                 # Docker files
│   ├── Dockerfile          # Container build
│   └── rootfs/             # Container overlay
├── docs/                   # Documentation
├── tests/                  # Test files
├── Makefile                # Build automation
└── AI.md                   # Project specification
```

## Build System

### Quick Development Build

Fast build to temporary directory (for testing):

```bash
make dev
```

This builds to a random temp directory and outputs the path.

### Full Build

Build for your current platform:

```bash
make build
```

Binary output: `binaries/api`

### Multi-Platform Release

Build for all 8 platforms (Linux, macOS, Windows, FreeBSD × AMD64, ARM64):

```bash
make release
```

Binaries output to `binaries/` directory:
```
api-linux-amd64
api-linux-arm64
api-darwin-amd64
api-darwin-arm64
api-windows-amd64.exe
api-windows-arm64.exe
api-freebsd-amd64
api-freebsd-arm64
```

### Docker Build

Build Docker image:

```bash
make docker
```

Image tagged as: `api:latest`

## Testing

### Run Tests

```bash
make test
```

All tests run in Docker containers (no Go installation required).

### Run Specific Tests

```bash
# Test specific package
docker run --rm -v $(pwd):/build -w /build \
  golang:alpine go test ./src/config

# With verbose output
docker run --rm -v $(pwd):/build -w /build \
  golang:alpine go test -v ./src/server

# With coverage
docker run --rm -v $(pwd):/build -w /build \
  golang:alpine go test -cover ./...
```

## Development Workflow

### 1. Make Changes

Edit source files in `src/` directory.

### 2. Test Changes

```bash
# Quick build
make dev

# Run binary
/tmp/apimgr.XXXXXX/api --mode development --debug
```

### 3. Run Tests

```bash
make test
```

### 4. Build Docker Image

```bash
make docker
```

### 5. Test in Container

```bash
docker run -p 64580:80 api:latest
```

### 6. Update Documentation

Update relevant documentation in `docs/` directory.

## Code Guidelines

### File Organization

- **Lowercase names** - `config.go`, not `Config.go`
- **Snake case** - `user_service.go`, not `userService.go`
- **Singular directories** - `handler/`, not `handlers/`
- **Package per directory** - One package per directory

### Comment Placement

Comments MUST be above code, never inline:

```go
// CORRECT - Comment above
var port = 8080

// WRONG - Don't do this
var port = 8080 // This is wrong
```

### Error Handling

Always handle errors:

```go
// CORRECT
result, err := someFunction()
if err != nil {
    return fmt.Errorf("operation failed: %w", err)
}

// WRONG - Never ignore errors
result, _ := someFunction()
```

### Input Validation

Validate all user input:

```go
// Validate type, length, format, range
if len(input) > maxLength {
    return errors.New("input too long")
}

// Use parameterized queries
db.Query("SELECT * FROM users WHERE id = ?", userID)
```

## Frontend Templates

Frontend pages live under `src/server/template/` and are embedded into the
binary. There are no frameworks or bundlers - one shared `app.js`, four CSS
files (`common.css`, `components.css`, `public.css`, `admin.css`), and Go
`html/template` files.

```
src/server/template/
├── layout/            # public.tmpl
├── partial/           # shared head, scripts, header/nav/footer
├── page/              # one template per category (text.tmpl, crypto.tmpl, ...)
└── page/tools/         # one subdirectory per category
    └── {category}/{tool}.tmpl
```

### Category page pattern

A category page (`page/{category}.tmpl`) lists every tool in that category
as a card linking to `/{category}/{tool}`:

```go
{{define "content"}}
<section class="hero">
  <div class="container">
    <h1 class="hero-title">{Icon} {Category Name}</h1>
    <p class="hero-subtitle">{Count} tools for {purpose}</p>
  </div>
</section>
<section>
  <div class="container">
    <div class="category-grid">
      <a href="/{category}/{tool}" class="category-card">
        <div class="category-icon">{Icon}</div>
        <h3 class="category-title">{Tool Name}</h3>
        <p class="category-description">{Description}</p>
      </a>
      {{/* Repeat for each tool */}}
    </div>
  </div>
</section>
{{end}}
```

### Tool page pattern

A tool page (`page/tools/{category}/{tool}.tmpl`) shows a breadcrumb, a form
that calls the matching API endpoint via `fetch`, a result panel, and a copy
of the `curl` invocation:

```go
{{define "content"}}
<section>
  <div class="container container-sm">
    <nav class="mb-2"><a href="/">Home</a> / <a href="/{category}">{Category}</a> / {Tool Name}</nav>
    <div class="tool-card">
      <h1 class="tool-title">{Tool Name}</h1>
      <p class="tool-description">{Description}</p>
      <form id="{tool}-form" onsubmit="event.preventDefault(); execute{Tool}();">
        <div class="form-group">
          <label class="form-label">{Input Label}</label>
          <input type="text" name="{field}" class="form-input">
        </div>
        <button type="submit" class="btn btn-primary">Execute</button>
      </form>
      <div id="{tool}-result" class="tool-result" style="display:none;"></div>
    </div>
  </div>
</section>
<script>
function execute{Tool}() {
  const form = document.getElementById('{tool}-form');
  const resultDiv = document.getElementById('{tool}-result');
  const params = new URLSearchParams(new FormData(form));
  resultDiv.style.display = 'block';
  fetch(`/api/v1/{category}/{endpoint}?${params}`)
    .then(r => r.text())
    .then(result => { resultDiv.textContent = result; addToHistory('{category}-{tool}'); })
    .catch(err => showToast(`Request failed: ${err.message}`, 'danger'));
}
</script>
{{end}}
```

See `page/text.tmpl` and `page/tools/text/uuid.tmpl` for a complete working
example of each pattern. All routes support [content negotiation](api.md#content-negotiation) -
the same tool endpoint serves HTML to browsers, plain text to `curl`, and
JSON to API clients.

## Debugging

### Debug Mode

Run with debug flag:

```bash
api --debug
```

**Debug endpoints available at:**
- `/debug/pprof` - CPU and memory profiling
- `/debug/vars` - Exported variables
- `/debug/config` - Current configuration
- `/debug/routes` - All registered routes

### Profiling

```bash
# CPU profiling
go tool pprof http://localhost:64580/debug/pprof/profile?seconds=30

# Memory profiling
go tool pprof http://localhost:64580/debug/pprof/heap

# Goroutine profiling
go tool pprof http://localhost:64580/debug/pprof/goroutine
```

### Logs

View logs in development mode:

```bash
tail -f /var/log/apimgr/api/server.log
```

## Contributing

### Workflow

1. **Fork** the repository
2. **Create a branch** - `git checkout -b feature/my-feature`
3. **Make changes** - Implement your feature
4. **Test thoroughly** - Run all tests
5. **Commit** - Follow commit message guidelines
6. **Push** - `git push origin feature/my-feature`
7. **Pull Request** - Create PR with description

### Commit Messages

Use conventional commits:

```
feat: Add new endpoint for URL shortening
fix: Fix timezone conversion bug
docs: Update API documentation
refactor: Simplify authentication logic
test: Add tests for crypto package
```

### Pull Request Guidelines

- **Clear description** - Explain what and why
- **Tests included** - All new code must have tests
- **Documentation updated** - Update docs for user-facing changes
- **No breaking changes** - Without discussion first
- **One feature per PR** - Keep PRs focused

## Building Documentation

### Local Preview

```bash
# Install MkDocs (one-time)
pip install -r docs/requirements.txt

# Serve locally
mkdocs serve
```

Visit `http://localhost:8000` to preview.

### Build Documentation

```bash
mkdocs build
```

Output: `site/` directory

## Release Process

Releases are automated via GitHub Actions.

### Version Numbering

Update `release.txt` with new version:

```bash
echo "1.2.0" > release.txt
git add release.txt
git commit -m "Bump version to 1.2.0"
```

### Create Release

```bash
# Tag the release
git tag v1.2.0
git push origin v1.2.0
```

GitHub Actions will:
1. Build binaries for all 8 platforms
2. Build Docker images
3. Create GitHub release
4. Publish to container registry
5. Update documentation

## Troubleshooting

### Build Fails

```bash
# Clean and rebuild
make clean
make build
```

### Tests Fail

```bash
# Run with verbose output
docker run --rm -v $(pwd):/build -w /build \
  golang:alpine go test -v ./...
```

### Docker Build Fails

```bash
# Check Docker daemon
docker ps

# Clean Docker cache
docker system prune -af
```

## Code Review Checklist

Before submitting a PR:

- [ ] Code builds successfully (`make build`)
- [ ] All tests pass (`make test`)
- [ ] Docker image builds (`make docker`)
- [ ] Documentation updated
- [ ] No hardcoded secrets or credentials
- [ ] Error handling implemented
- [ ] Input validation added
- [ ] Comments above code (not inline)
- [ ] No breaking changes (or discussed)
- [ ] Follows project code style

## Resources

- **Source Code:** [GitHub](https://github.com/apimgr/api)
- **Issues:** [GitHub Issues](https://github.com/apimgr/api/issues)
- **Discussions:** [GitHub Discussions](https://github.com/apimgr/api/discussions)
- **Documentation:** [ReadTheDocs](https://apimgr-api.readthedocs.io)

## Next Steps

- [API Reference](api.md)
- [CLI Reference](cli.md)
- [Configuration Guide](configuration.md)
