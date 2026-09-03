package generate

import (
	"fmt"
	"strings"
)

// gitignoreSections holds curated .gitignore boilerplate entries per
// language/platform/editor, covering at minimum go, node, python, rust,
// java, macos, linux, windows, vscode, jetbrains.
var gitignoreSections = map[string]string{
	"go": `# Go
*.exe
*.exe~
*.dll
*.so
*.dylib
*.test
*.out
vendor/
go.work
go.work.sum`,
	"node": `# Node
node_modules/
npm-debug.log*
yarn-debug.log*
yarn-error.log*
.pnpm-debug.log*
dist/
.npm
.env`,
	"python": `# Python
__pycache__/
*.py[cod]
*$py.class
*.egg-info/
.venv/
venv/
.pytest_cache/
.mypy_cache/
dist/
build/`,
	"rust": `# Rust
/target/
Cargo.lock
**/*.rs.bk
*.pdb`,
	"java": `# Java
*.class
*.jar
*.war
*.ear
target/
.gradle/
build/`,
	"macos": `# macOS
.DS_Store
.AppleDouble
.LSOverride
._*
.Spotlight-V100
.Trashes`,
	"linux": `# Linux
*~
.fuse_hidden*
.directory
.Trash-*
.nfs*`,
	"windows": `# Windows
Thumbs.db
Thumbs.db:encryptable
ehthumbs.db
Desktop.ini
$RECYCLE.BIN/
*.lnk`,
	"vscode": `# VSCode
.vscode/*
!.vscode/settings.json
!.vscode/tasks.json
!.vscode/launch.json
!.vscode/extensions.json
*.code-workspace`,
	"jetbrains": `# JetBrains
.idea/
*.iml
*.iws
out/
.idea_modules/`,
}

// Gitignore returns the union of curated .gitignore boilerplate entries for
// a comma-separated list of languages/platforms/editors.
func (s *Service) Gitignore(langs string) (string, error) {
	langs = strings.TrimSpace(langs)
	if langs == "" {
		return "", fmt.Errorf("at least one language must be specified")
	}

	var sections []string
	var unknown []string
	for _, raw := range strings.Split(langs, ",") {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "" {
			continue
		}
		section, ok := gitignoreSections[key]
		if !ok {
			unknown = append(unknown, key)
			continue
		}
		sections = append(sections, section)
	}

	if len(unknown) > 0 {
		return "", fmt.Errorf("unsupported language(s) %s: supported values are go, node, python, rust, java, macos, linux, windows, vscode, jetbrains", strings.Join(unknown, ", "))
	}
	if len(sections) == 0 {
		return "", fmt.Errorf("no valid languages given")
	}

	return strings.Join(sections, "\n\n") + "\n", nil
}
