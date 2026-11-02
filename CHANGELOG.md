# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Repository base: license, version file, API contract, test fixture server and
  a Docker-based JDownloader test stack.
- REST and server-sent-events API (`internal/api`) and the `jdweb` binary with
  `serve`, `healthcheck`, `reset-password` and `version`. Started as root, it
  gives `/data` to `PUID:PGID` and drops root.
- JDownloader client, state mirror and sync engine with live updates, backoff
  and self-healing after JDownloader restarts.
- Account with Argon2id password, optional two-factor (TOTP), sessions, setup
  with a one-time token, rate limiting and CSRF protection.
- Web app (Preact): downloads, link grabber, captchas and questions, extraction,
  accounts, settings, dark and light mode, phone layout, German and English.
- Docker image (distroless, about 15 MB, linux/amd64 and linux/arm64), Compose
  example, Unraid template, smoke test, CI and release workflows.
- README with installation for Unraid and Compose, the two JDownloader
  settings and the security notes about JDownloader's API.

### Fixed

- Memory of password hashing is returned to the system at once: idle memory
  after the first login was about 45 MiB, now about 6 MiB.
