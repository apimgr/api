## [ ] Fix release.yml BUILD_DATE format
`.github/workflows/release.yml` line 57 stamps `BUILD_DATE` with the custom
format `"%a %b %d, %Y at %H:%M:%S %Z"` instead of ISO 8601 UTC
(`YYYY-MM-DDTHH:MM:SSZ`), inconsistent with `Makefile` line 10 and AI.md
PART 27's CI/CD conventions. Fix: change the `date` invocation to
`date -u +%Y-%m-%dT%H:%M:%SZ`.

## [ ] Fix docker-compose volume mount paths
`docker/docker-compose.yml` (lines 20-21), `docker/docker-compose.dev.yml`
(lines 21-22), and `docker/docker-compose.test.yml` (lines 19-20) all mount
`./rootfs/config`/`./rootfs/data` instead of the standard
`./volumes/config`/`./volumes/data` required by
`.claude/rules/docker-rules.md` ("Always mount exactly two volumes in
compose: `./volumes/config:/config`... and `./volumes/data:/data`...").
Fix: update all three compose files' volume mounts to `./volumes/config` /
`./volumes/data`, keeping the `:z` suffix on the production file only.
