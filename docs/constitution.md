# Constitution
Version: 2.0.0

## 1. Mission
Web console for a student team that automates administration tasks on remote Debian GNU/Linux 13 virtual machines, grouped into four modules: system preparation, user management, systemd service management and HAProxy backend management. It MUST NOT become a tool that performs system operations outside bash scripts.

## 2. Core principles
- P1. The backend MUST only validate input, invoke scripts and report results; it MUST NOT perform system operations on the VM directly, and all system-level logic MUST live in bash scripts, the single unit of automation.
- P2. Every script MUST be non-interactive, start with `set -euo pipefail`, receive all inputs as explicit arguments, be idempotent wherever the operation allows it, return a distinct non-zero exit code on each failure type, and write results to stdout and diagnostics to stderr.
- P3. Arguments MUST be passed to scripts as discrete argv items and MUST NOT be concatenated into a shell command string; every input MUST be validated against an allowlist in the UI/backend and again in the script.
- P4. Before key access is configured, communication with the VM MUST go through Guest Additions; afterwards it MUST be exclusively SSH with key authentication, and password login MUST be disabled whenever key access is enabled.
- P5. Private keys MUST NOT be written to logs, MUST NOT be kept after the download is offered to the user, and the UI MUST show a message explaining their correct use.
- P6. Every operation MUST end with a clear success or failure message in the UI; on failure the UI MUST show an understandable message and the logs MUST preserve the command, exit code, stderr and timestamp.
- P7. Code and UI MUST be organized by module and MUST follow the reference UI design: a home with module cards, one screen per module, and forms that confirm the result.
- P8. Every feature MUST be demonstrable with a before/after system state, and every script MUST be runnable and testable on its own, without the web app.
- P9. Solutions MUST be the simplest readable option for a student team, MUST NOT add dependencies or abstractions without a concrete need, and MUST include comments wherever intent is not obvious.

## 3. Amendments
| Date | Version | Change |
|---|---|---|
| 2026-10-19 | 1.0.0 | Initial version |
| 2026-10-22 | 2.0.0 | P4 redefined: the switch from Guest Additions to SSH happens when key access is configured, not when SSH is installed |
