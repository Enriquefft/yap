# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | Yes                |
| < 0.1   | No                 |

Only the latest release in the 0.1.x line receives security fixes.
Once newer minor or major versions are released, this table will be updated accordingly.

## Reporting a Vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

### Preferred: GitHub Security Advisories

Report vulnerabilities through [GitHub Security Advisories](https://github.com/Enriquefft/yap/security/advisories/new).
This keeps the report private until a fix is available.

### Fallback: Email

If you cannot use GitHub Security Advisories, email the maintainer directly at the address listed on the [@Enriquefft GitHub profile](https://github.com/Enriquefft).
Use the subject line: `[yap security] <brief description>`.

### What to Include

A good vulnerability report should contain:

- Description of the vulnerability and its potential impact.
- Steps to reproduce, including environment details (OS, display server, yap version).
- Affected component (recording, transcription, injection, daemon IPC, configuration).
- Proof of concept or exploit code, if available.
- Suggested fix, if you have one.

### Response Timeline

yap is maintained by a small team. Expectations:

- **Acknowledgment**: within 72 hours of report receipt.
- **Initial assessment**: within 7 days.
- **Fix timeline**: depends on severity. Critical issues (credential exposure, remote code execution) are prioritized for the next patch release. Lower-severity issues are addressed in the next scheduled release.
- **Disclosure**: coordinated with the reporter. We ask for a reasonable disclosure window (typically 90 days) to develop and release a fix.

## Scope

The following are considered security issues for this project:

- **API key exposure**: leaking Groq, OpenAI, or other transcription service API keys through logs, config files, crash dumps, or IPC.
- **Audio data exposure**: unauthorized access to recorded audio data, failure to clean up temporary audio files, or unintended audio capture.
- **Text injection attacks**: malicious content injected into the active window through crafted transcription responses or manipulated transform pipelines.
- **Privilege escalation via daemon**: the yap daemon running with excessive permissions, or the Unix socket / PID file having incorrect permissions that allow unauthorized control.
- **IPC socket permissions**: unauthorized processes sending commands to or reading data from the daemon socket.
- **Path traversal or arbitrary file access**: through configuration, PID files, or audio file paths.

## Out of Scope

- **Upstream dependency vulnerabilities**: issues in whisper.cpp, Go standard library, or other dependencies should be reported to their respective maintainers. If a dependency vulnerability has a specific impact on yap, report it here.
- **Denial of service via local access**: an attacker with local access to your machine can already disrupt yap in many ways. We do not treat local DoS as a security issue.
- **Social engineering or phishing**: attacks that require tricking the user into running malicious commands.
- **Issues requiring physical access**: to the machine beyond what is needed to operate the software normally.

## Security Design

yap is designed with the following security principles:

- **Local-first**: audio is processed locally when using whisper.cpp. When using cloud transcription (Groq, OpenAI-compatible APIs), audio leaves the machine only for that explicit purpose.
- **No telemetry**: yap collects no usage data and makes no network requests beyond those required for cloud transcription.
- **API keys in environment variables**: API keys are read from environment variables, not stored in configuration files. This avoids accidental exposure through dotfile commits or config file sharing.
- **Minimal daemon privileges**: the daemon runs as the invoking user with no elevated permissions. The Unix socket is created with restrictive permissions.
- **Temporary file cleanup**: audio recordings are cleaned up after transcription completes.
