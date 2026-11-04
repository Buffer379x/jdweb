# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-11-04

First release.

### Added

- Web app (Preact) for JDownloader: downloads, link grabber, captchas and
  questions, extraction, hoster accounts, settings, dark and light mode, phone
  layout, German and English by browser language.
- Live updates over server-sent events. A JDownloader restart is shown, the
  connection heals by itself and the lists catch up.
- JDownloader client, state mirror and sync engine with backoff and
  self-healing after JDownloader restarts.
- REST API in front of JDownloader's API (`docs/api.md`); the browser never
  talks to JDownloader.
- Account with Argon2id password, optional two-factor (TOTP), sessions, setup
  with a one-time token, `reset-password`, rate limiting and CSRF protection.
- The `jdweb` binary with `serve`, `healthcheck`, `reset-password` and
  `version`. Started as root, it gives `/data` to `PUID:PGID` and drops root.
- Docker image (distroless, about 15 MB, linux/amd64 and linux/arm64), Compose
  example, Unraid template, smoke test, CI and release workflows.
- README with installation for Unraid and Compose, the two JDownloader
  settings and the notes about the missing login of JDownloader's API.
- Licence notices of the bundled Go modules and npm packages
  (`web/public/licenses/THIRD_PARTY_NOTICES.txt`).

### Known limits

- Interactive captchas (reCAPTCHA, hCaptcha and the like) cannot be solved in
  JDWeb. It explains them and can skip them; solve them in JDownloader itself.
- Captchas are tested against a simulated JDownloader only. The real instance
  used for the tests never asked for one, so image captchas have not been seen
  on a real job, and the recognition of interactive ones is a guess from the
  type name.
- DLC and other container files are not tested against real files.
- JDownloader in Unraid's default `bridge` network needs a change of its
  network setup, see the README.
- JDWeb needs JDownloader's old API (`Deprecated Api Enabled`), which has no
  login. See the security section of the README.
