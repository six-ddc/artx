# Security

`artx` is a local-first tool. It serves files from a vault on your own machine and has no remote component, no telemetry, and no account system.

## Threat model

The content in a vault is produced by an agent running on your machine with your privileges. `artx` treats it as equivalent in trust to files you wrote yourself, and does not sanitize markdown before rendering. Two boundaries carry the security weight instead.

**Network exposure.** `artx serve` binds `127.0.0.1` by default. Passing `--host` to bind any other address requires `--token`; without it the server refuses to start rather than exposing an unauthenticated vault. The token is accepted as an `Authorization: Bearer` header, a `?token=` query parameter, or an `artx_token` HttpOnly cookie — the cookie exists because `EventSource` and `<iframe>` cannot set request headers. Non-GET requests carrying an `Origin` that does not match the server's own are rejected with 403.

**HTML artifacts.** Every HTML artifact is loaded in an `<iframe sandbox="allow-scripts">` without `allow-same-origin`, so its scripts cannot reach the shell page's DOM, storage, or cookies. The shell page itself sets a Content-Security-Policy. The reviewer script injected into the frame communicates with the shell only over `postMessage`.

**Filesystem.** The server reads and writes only within the vault directory. Paths from requests are validated against traversal (`../`) and symlink escape.

The serve lockfile at `.artx/serve.lock` contains the `--token` value, so local CLI invocations need no configuration. It is written mode `0600`, and `artx`'s own auto-commits are scoped to the artifact directory and `.artx/comments/` and never include it. It is not, however, added to a vault `.gitignore` — if you run `git add -A` in a vault by hand while a token-mode serve is live, check that you are not committing the token.

## Supported versions

`artx` is pre-1.0. Only the latest release receives security fixes.

## Reporting a vulnerability

> **TODO** — a private reporting channel has not been set up yet. Until it is, open a GitHub issue for anything that is not exploitable against a default (`127.0.0.1`-only) configuration, and hold anything more sensitive until this section names a contact.

Please do not open a public issue for a vulnerability that is exploitable against a default configuration.
