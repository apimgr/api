# Service Rules (PART 23, 24)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

## CRITICAL - NEVER DO
- Never prompt for privilege escalation if the current user cannot actually escalate (not in sudoers/wheel/admin) — show an informative error instead.
- Never skip the "already root/admin" check — the binary must detect this and skip escalation prompts entirely.
- Never use a UID/GID that isn't identical for both — the service user's UID and GID MUST match.
- Never assign a reserved/well-known UID/GID (65534, 999-980 range, 170-179, 101-110) even if it looks free on the current system.
- Never run permanently as root/Administrator unless explicitly approved in IDEA.md; if approved, document why privilege drop is not possible.
- Never let `--service --uninstall` skip the confirmation prompt — it deletes all data, configs, and the system user.
- Never have `--service --disable` remove data, config, user/group, or the service file — only stop + disable auto-start.
- Never have the `--service --install` flag do more than install/enable/start — user/group creation, directory setup, and privilege drop happen during normal binary startup, not the install flag.
- Never bind privileged ports (<1024) without first checking for root/admin, then dropping privileges after bind on Unix-like systems.

## CRITICAL - ALWAYS DO
- Always check escalation methods in OS-specific order: Linux (root → sudo → su → pkexec → doas), macOS (root → sudo → osascript), BSD (root → doas → sudo → su), Windows (Administrator → UAC → runas).
- Always create the service user as `api` (matches `internal_name`), group `api`, shell `/sbin/nologin` (or `/usr/sbin/nologin`), Gecos `api service account`.
- Always search for a free UID/GID starting at the top of the safe range and working down, skipping reserved IDs: Linux/BSD 200-899 (start 899), macOS 200-399 (start 399).
- Always create the home directory before creating the user, then set ownership.
- On Unix, always start elevated only for privileged port binding, then drop to the dedicated `api` user afterward.
- On Windows, always use a Virtual Service Account (`NT SERVICE\api`) — never Local System, Administrator, or a logged-in user account.
- Always support all detected init systems per platform: Linux (systemd, OpenRC, SysVinit, runit), macOS (launchd), BSD (rc.d), Windows (Windows Service).

## Key Rules Summary
- **Escalation flow**: binary checks EUID==0/admin first; only prompts if the user can actually escalate; otherwise errors out with an explanation. See PART 5 "Privileged Port Binding" for full flow.
- **Service install** (`--service --install`): detect platform/init system, install system service if root/admin (fallback to user service e.g. `systemd --user` otherwise), enable, start. User/group creation and directory setup happen at binary startup, not here.
- **Service uninstall** (`--service --uninstall`): stop, disable, remove service file, delete config/data/cache/log/backup dirs and PID file, delete the system user/group, leave the binary in place with a manual-delete message. Requires `[y/N]` confirmation.
- **Service disable** (`--service --disable`): stop + disable only; everything else (service file, data, user) stays; re-enable via `--service --install`.
- **CLI help surfaces**: `--service --help` (start/stop/restart/reload, --install/--disable/--uninstall, status block), `--maintenance --help` (backup/restore/update/mode/setup), `--shell --help` (completions/init for bash/zsh/fish/etc.), `--update --help` (check/yes/branch stable|beta|daily).
- **System user table**: username `api`, group `api`, UID/GID match required, range 200-899 (Linux/BSD) or 200-399 (macOS), shell nologin, home = config dir (`/etc/apimgr/api`) or data dir (`/var/lib/apimgr/api`), no password/login, system user type.
- **UID/GID selection**: start at top of safe range, skip reserved IDs, verify both UID and GID free in `/etc/passwd` and `/etc/group`, decrement until found or range exhausted (error if none).
- **Reserved IDs**: never use 65534 (nobody), 980-999 (docker/systemd/polkit/etc.), 170-179, 101-110 (sshd/postfix/dovecot etc.) — table in AI.md has the full list.
- **Platform commands**: Linux `groupadd --system --gid`/`useradd --system --uid --gid`; macOS `dscl` (hidden from login, `/usr/bin/false` shell); FreeBSD `pw groupadd`/`pw useradd`; Windows `New-Service` with empty `ServiceStartName` (auto-creates VSA).
- **Service templates** (installation paths): systemd `/etc/systemd/system/api.service` (hardened with `ProtectSystem=strict`, `ProtectHome=yes`, `ReadWritePaths` for config/data/cache/log dirs); OpenRC/SysVinit both use `/etc/init.d/api` (only one installed, detected by presence of `openrc-run`/`systemctl`/`update-rc.d`/`chkconfig`); runit `/etc/sv/api/` (run + log/run scripts); FreeBSD rc.d `/usr/local/etc/rc.d/api`; launchd `/Library/LaunchDaemons/{plist_name}.plist` (root start, binary drops privileges, no UserName/GroupName key); Windows Service via `golang.org/x/sys/windows/svc`, VSA account.
- **Run modes**: Service (escalated) → any port, drops privileges after bind. User mode (`$USER`) → ports >1024 only, no privilege drop needed.

For complete details, see AI.md PART 23, 24
