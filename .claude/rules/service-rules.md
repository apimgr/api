# Privilege Escalation & Service Support Rules (PART 23, 24)

⚠️ **These rules are NON-NEGOTIABLE. Violations are bugs.** ⚠️

**IDEA.md override:** None needed — PART 23/24 describe OS-level service
installation and system-user management, which is infrastructure, not a
user-facing account/admin-panel feature. IDEA.md's no-accounts/no-admin-panel
non-goals do not touch this PART; every rule below applies as written in the
base spec.

## CRITICAL - NEVER DO
- Never require the operator to manually create the system user/group —
  the binary creates and configures `{internal_name}` user/group itself
  when run with sufficient privilege
- Never pick a UID/GID outside the safe range (200-899 Linux, 200-399
  macOS) or reuse a reserved/well-known ID (0-99 system, 65534 nobody,
  common daemon UIDs like 999/998/etc.) — always probe downward from 899
  (or 399) and skip anything already taken or reserved
- Never let UID and GID diverge for the service account — they must match
- Never grant the service account a login shell — always `/sbin/nologin`
  (or platform equivalent)
- Never install a service without first detecting the actual init system
  in use — never assume systemd on every Linux host
- Never use `ProtectSystem=strict` (or equivalent) without also granting
  explicit `ReadWritePaths`/equivalent for config, data, cache, and log
  directories — an over-locked-down unit that can't write its own files
  is a bug, not a hardening win
- Never skip privilege-escalation detection before attempting an operation
  that requires root/Administrator — detect via the OS-appropriate method
  chain first, then prompt/escalate accordingly
- Never let escalated (root/service) mode bind to a privileged port and
  keep running as root afterward — drop privileges after binding when
  running as a system service
- Never let user-mode (non-escalated) operation bind to a port ≤1024
- Never leave an uninstalled/disabled service's files behind — uninstall
  removes the unit/script file and reloads the service manager's state
- Never write install/uninstall logic that mutates the working
  installation while `service-rules.md`-covered code is only partially
  implemented — implement per-init-system logic completely or not at all

## CRITICAL - ALWAYS DO
- Detect privilege escalation per OS using the documented method order:
  Linux (`sudo`/`su`/`pkexec`/`doas` env/parent-process signals), macOS
  (`sudo`/`osascript` AppleScript prompt), BSD (`doas`/`sudo`/`su`),
  Windows (UAC elevation token / `runas`)
- Create the system user/group with username=group=`{internal_name}`,
  matching UID=GID selected from the safe range, `/sbin/nologin` shell,
  home directory set to the config or data directory, and system-account
  flags/gecos set per OS convention
- Use the 8-step UID/GID selection algorithm: start at the top of the safe
  range and work downward, skipping any ID already in `/etc/passwd`,
  `/etc/group`, or the reserved-IDs table
- Detect the actual init system before installing a service: systemd
  (`/run/systemd/system`), OpenRC, SysVinit, runit, rc.d (FreeBSD),
  launchd (macOS), Windows Service Manager — install the matching
  template only
- Ship a correct template for every supported init system: systemd unit
  file, OpenRC `openrc-run` script, SysVinit `start-stop-daemon` script,
  runit `run`/`log/run` pair, FreeBSD `rc.subr`-based script, launchd
  plist, and a native Windows Service (via `golang.org/x/sys/windows/svc`)
  using a Virtual Service Account by default
- Escalated/service-mode: bind as root to any port, then drop to the
  `{internal_name}` service account after binding
- User-mode: run as the invoking user, restricted to ports >1024, no
  privilege drop needed (never had elevated privilege)
- Reload the service manager's unit/script cache after install/uninstall
  (`systemctl daemon-reload`, `rc-update`, `update-rc.d`, `sv`, ets.) so
  changes take effect immediately

## KEY DECISIONS (pre-answered)
| Question | Answer | Spec Reference |
|----------|--------|----------------|
| System user fields | username=group=`{internal_name}`, UID=GID matched, shell `/sbin/nologin`, home=config/data dir, system-account type | PART 23 |
| Safe UID/GID range | 200-899 (Linux), 200-399 (macOS) | PART 23 |
| UID/GID selection order | Start at top of range, work downward, skip reserved/taken IDs | PART 23 |
| Windows service account | Virtual Service Account (`NT SERVICE\{internal_name}`) by default | PART 23 |
| Service manager detection — current implementation | `src/sysservice/service.go`'s `DetectServiceManager()` on Linux checks **only** `/run/systemd/system` → systemd, else `/run/runit` → runit, else `/etc/systemd` → systemd, else `ServiceUnknown` — **OpenRC and SysVinit are never detected or installed; a Linux host running OpenRC or SysVinit-only falls through to `ServiceUnknown` and errors with "unsupported service manager"**. This is a confirmed gap against PART 24's required template set, not a documented/intentional scope reduction | PART 24 |
| macOS/BSD/Windows detection | Correctly implemented — `darwin`→launchd, `freebsd/openbsd/netbsd`→BSD rc.d, `windows`→Windows Service, matching `installLaunchd()`, `installBSDRC()`, `installWindows()` in `src/sysservice/service.go` | PART 24 |
| Privilege drop after bind | Required in escalated/service mode — verify this is wired in `main.go`'s startup path when adding new listener code; not directly re-verified in this pass | PART 23 |
| Escalated vs user-mode port range | Escalated: any port, drops after bind. User-mode: >1024 only, never drops (never elevated) | PART 24 |

## TERMINOLOGY
| Term | Meaning |
|------|---------|
| Escalated mode | Service runs as root/Administrator initially, binds any port, then drops privilege to the system service account |
| User-mode | Service runs as the invoking non-privileged user for its whole lifetime, restricted to ports >1024 |
| System user | The dedicated `{internal_name}` account created for running the service, distinct from any human login account |
| Safe UID/GID range | The ID window (200-899 Linux, 200-399 macOS) probed downward when allocating a new system user/group |
| Reserved IDs | Well-known UID/GID values (nobody, systemd/docker ranges, legacy service ranges) that must never be reused |
| Init system detection | Runtime probing of marker files/binaries to pick which service template to install |
| Virtual Service Account | Windows account type (`NT SERVICE\{name}`) requiring no password management, used as the default Windows service identity |

## QUICK REFERENCE
- Service management: `src/sysservice/service.go` — `DetectServiceManager()`,
  `Install()`/`Uninstall()`, `installSystemd()`/`installRunit()`/
  `installLaunchd()`/`installWindows()`/`installBSDRC()` (+ matching
  `uninstall*()`), `Start()`/`Stop()`/`Restart()`/`Disable()`/`Reload()`
- **Missing**: dedicated OpenRC and SysVinit install/uninstall functions —
  Linux hosts without systemd or runit are unsupported today; this is an
  open gap, not a spec deviation, and belongs in `TODO.AI.md` as follow-up
  work rather than being "fixed" by narrowing this rules file to match
- System user/UID-GID allocation logic should live alongside the service
  install code — confirm `reservedIDs` map and `findAvailableSystemID()`
  equivalent exist in `src/sysservice/` before assuming PART 23's
  allocation algorithm is implemented; not independently re-verified line
  by line in this pass beyond the detection/install switch above
- Privilege escalation detection (sudo/su/pkexec/doas/UAC/runas) is a
  precondition check, not a service-manager concern — keep it separate
  from `DetectServiceManager()`

---
For complete details, see AI.md PART 23, PART 24
