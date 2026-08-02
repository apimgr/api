## [ ] Wire backup.encryption_password config schema
AI.md PART 21 "Setting/Changing Backup Password" documents the backup
encryption password as `backup.encryption_password` in `server.yml` (set at
initial config, changeable later, mandatory when
`server.compliance.enabled: true`). `src/config/config.go` has no `Backup`
struct / `encryption_password` field at all, and `src/scheduler/tasks.go`'s
`backupTask()` instead reads the password from an `API_BACKUP_PASSWORD`
environment variable that appears nowhere in AI.md. Result: the documented
config surface doesn't exist, and there is an undocumented env var acting as
a stand-in. Nothing is broken today (backups run unencrypted when unset, per
PART 21's "optional unless compliance"), but the compliance-mandatory
encryption path (block backups / warn when compliance is on and no password
set) is also absent. When implementing: add the `backup.*` config struct
(including `encryption_password`), have `backupTask()` read
`cfg.Server.Backup.EncryptionPassword` (env override optional, but then
document `API_BACKUP_PASSWORD` in AI.md), and enforce the compliance-mode
mandatory-encryption behavior from PART 21.

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
