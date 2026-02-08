# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.3.x   | Yes       |
| < 0.3   | No        |

## Reporting a Vulnerability

If you discover a security vulnerability in AIB, please report it responsibly.

**Email**: github_issues@zezelj.org

Please include:
- Description of the vulnerability
- Steps to reproduce
- Affected version(s)
- Impact assessment (if known)

You should receive an acknowledgment within 48 hours. Please do not open a public GitHub issue for security vulnerabilities.

## Scope

The following are in scope:
- Command injection via scan paths or CLI arguments
- Path traversal in the scan trigger API
- Authentication bypass on `/api/*` endpoints
- SQL injection in the SQLite store layer
- Sensitive data exposure in API responses or logs

The following are out of scope:
- Denial of service via resource exhaustion (AIB is designed for internal/trusted networks)
- Vulnerabilities in third-party dependencies (report upstream, but feel free to notify us)
- Issues requiring physical access to the host machine
