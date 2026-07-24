## [ ] Create .claude/rules/config-rules.md covering config security, hot-reload, and Maintenance Mode
Read: AI.md PART 5

## [ ] Create .claude/rules/config-rules.md addendum covering mode/debug priority rules (merge with above or extend)
Read: AI.md PART 6

## [ ] Read PART 12 and extend .claude/rules/config-rules.md with server configuration rules before finalizing it
Read: AI.md PART 12

## [ ] Create .claude/rules/frontend-rules.md
Read: AI.md PART 16

## [ ] Rename mode.go Get()/Set() to intent-revealing names per spec (many call sites, needs a full pass)
Read: AI.md PART 6

## [ ] Fix pre-existing compile error: src/service/network/network.go is empty (package has no exported content, breaks build)
Read: AI.md PART 14

## [ ] Fix pre-existing compile error: src/service/test/ package name conflict
Read: AI.md PART 28

## [ ] Resolve main.go daemon/pid TODO comments (separate feature, not part of PART 0-6 scope)
Read: AI.md PART 8

## [ ] Fix build-info variable naming mismatch (Version/CommitID/BuildDate)
Read: AI.md line 663 (LDFLAGS), lines 31405-31459 (build info variable table)
- src/main.go declares `BuildTime` but Makefile's LDFLAGS targets `-X 'main.BuildDate=...'`;
  the linker silently no-ops on the mismatched symbol name, so the real build
  timestamp is never injected and the binary permanently reports the hardcoded
  "unknown" default instead of the actual build date
- Rename src/main.go's `BuildTime` var to `BuildDate` (spec-mandated name) and
  update every call site that currently reads `BuildTime` (e.g. `handler.BuildDate = BuildTime`,
  `metrics.Get().SetBuildInfo(...)`)
- src/server/server.go also has its own separate local `var (Version, BuildTime)`
  block (never ldflags-injected regardless of name, since ldflags only target
  `main.*`); align it with PageData's `Version`/`BuildTime` fields or remove the
  duplicate block in favor of values passed in from main.go's handler.Version/
  handler.BuildDate assignment — decide during the fix, not before
- Not app-breaking (display-only defect on /server/about's Build Information
  card), so deferred here rather than bundled into the template-wiring fix commit
