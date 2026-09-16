# Security Policy

gputop runs on production GPU infrastructure, so security reports are taken
seriously.

## Supported versions

Security fixes are made for the latest release. Before 1.0, users should
upgrade to the newest version.

## Reporting a vulnerability

**Please do not open public issues for security problems.**

Report vulnerabilities privately through
[GitHub Security Advisories](https://github.com/riteshsonawane1372/gputop/security/advisories/new).
Include:

- affected version and platform,
- a description of the issue and its impact,
- steps to reproduce or a proof of concept, if available.

You will receive an acknowledgement within 5 business days. We will agree on a
disclosure timeline with you, typically no more than 90 days.

## Security model

- **Local TUI and one-shot modes** only read local telemetry. gputop never
  executes commands from configuration and spawns no subprocesses while
  collecting.
- **Service mode** (`--service`):
  - listens on `127.0.0.1` by default;
  - refuses to listen on a non-loopback address unless both TLS and bearer-token
    authentication are configured;
  - compares tokens in constant time against a SHA-256 digest
    (`server.token_sha256`), so the plaintext token does not need to exist on
    the agent; supports mutual TLS (`server.client_ca_file`);
  - is read-only (GET/HEAD), sets `nosniff` and `no-store` headers, and has
    request timeouts and header size limits;
  - never serves process command lines; process names and users can be withheld
    with `server.expose_processes: false`.
- **Remote client** (`--remote`) verifies server certificates (system roots or
  `ca_file`), has no option to skip verification, allows plaintext HTTP only to
  loopback addresses (SSH tunnels), and reads tokens from files or environment
  variables, never from the configuration file.
- **Kubernetes** access is read-only and uses the pod's service account; the
  shipped RBAC grants only `get`/`list` on pods and `get` on jobs.
- **Files:** history files and logs are created with `0600` permissions in
  `0700` directories. gputop warns when a token file is readable by other users.

## Hardening recommendations

- Keep agents on loopback and use SSH tunnels when possible.
- If you expose an agent, use certificates from your internal CA, enable mTLS,
  and restrict access with a firewall.
- Rotate tokens (`gputop --gen-token`) and store them with `chmod 600`.
- Use the provided systemd unit (`deploy/systemd/gputop.service`), which runs
  with `DynamicUser`, `NoNewPrivileges` and a read-only filesystem.
