# glinet-cli

Small CLI for GL.iNet routers on firmware 4.8+ (tested against 4.9.0).
It shows a router overview and lists, enables and disables VPN client
tunnels from the VPN Dashboard (WireGuard and OpenVPN alike).

## Install

With [Homebrew](https://brew.sh) on macOS or Linux:

```sh
brew install antavik/apps/glinet-cli
```

Using the full name trusts only this formula, not the whole
[tap](https://github.com/antavik/homebrew-apps). `brew upgrade` picks up new
releases.

Prebuilt archives for macOS, Linux and Windows (amd64 and arm64) are on the
[releases page](https://github.com/antavik/glinet-cli/releases), with a
`checksums.txt` to check them against. The binaries are not signed, so on
macOS Gatekeeper blocks one downloaded with a browser; Homebrew avoids that.

From a checkout:

```sh
make build                       # or: go build -o glinet-cli ./src
make release                     # same, without debug info (smaller binary)
go run ./src <command>
```

All Go code lives in `src/`. Go names a binary after its folder, so
`go install .../src` would install it as `src`; use `go build -o` instead.
`glinet-cli -version` prints `dev` unless the build sets
`-ldflags "-X main.version=1.2.3"`; `make build` sets it from `git describe`.

## Log in

```console
$ glinet-cli auth            # same as: glinet-cli auth login
Password for root@http://192.168.8.1:
Logged in. Password saved to the OS keychain.

$ glinet-cli auth logout
Password removed from the OS keychain.
```

`auth` checks the password against the router before saving it, so a typo
never gets stored. Input is hidden on a terminal; piped input works too,
e.g. `op read op://Private/router/password | glinet-cli auth`.

The password lives in the OS keychain under service `glinet-cli`, account
`<user>@<url>`, so each router has its own entry:

- macOS: Keychain
- Windows: Credential Manager
- Linux: Secret Service (GNOME Keyring, KWallet). Headless boxes usually lack
  it; use `GLINET_PASSWORD` there.

Other commands take the password from `GLINET_PASSWORD` if set, otherwise
from the keychain. The env var suits CI and scripts.

The keychain encrypts the password at rest and keeps it out of dotfiles,
shell history and backups. It does not hide it from other programs running as
your user: on macOS any of them can read it through `/usr/bin/security`
without a prompt.

## Configure

| Setting  | Flag       | Env               | Default              |
|----------|------------|-------------------|----------------------|
| Router   | `-url`     | `GLINET_URL`      | `http://192.168.8.1` |
| Username | `-user`    | `GLINET_USER`     | `root`               |
| Password | –          | `GLINET_PASSWORD` | OS keychain          |
| Timeout  | `-timeout` | –                 | 30 seconds           |

There is no password flag, so the password never shows up in `ps` output or
shell history.

## Use

```console
$ glinet-cli status
Model: mt3000
Hostname: GL-MT3000-49a
MAC: 94:83:C4:0C:74:9A
Firmware: 4.9.0 (update available: 4.10.0)
Uptime: 1d 1h 1m
Load: 0.12 0.34 0.56
Memory: 45% (230 MiB / 512 MiB)
Flash: 12% (15 MiB / 128 MiB)
Ethernet: dhcp 192.168.1.5 gw 192.168.1.1 dns 1.1.1.1,8.8.8.8 (connected)
VPN: Home/WG connected, Travel/WG connecting

$ glinet-cli vpn
ID    NAME            ENABLED  STATUS
2001  Mullvad/se-sto  on       connected
2002  Proton/nl-free  off      -

$ glinet-cli vpn on 2002          # by tunnel ID
$ glinet-cli vpn off "Proton/nl-free"  # by name, case-insensitive
$ glinet-cli vpn off -all
$ glinet-cli vpn restart 2001     # turn off, then on
$ glinet-cli vpn restart -all
$ glinet-cli vpn on 2002 -wait     # return once the tunnel is connected
$ glinet-cli vpn restart -all -wait -timeout 2m

$ glinet-cli web                  # open the router web UI in the default browser
```

`on` and `off` leave tunnels already in the requested state alone. `restart`
cycles each tunnel off, then on, one at a time, regardless of its current
state, so `restart -all` leaves every tunnel on, including ones that were off.
With `-all`, a failure on one tunnel does not stop the others; the command exits
non-zero and lists every error. If a tunnel fails to turn off, it is not turned
on.

With `-wait`, `on` and `restart` poll `vpn-client.get_status` every second
until each tunnel reports connected (status `1`), one tunnel at a time,
including tunnels that were already on. `-timeout` bounds the whole command,
so raise it for slow tunnels; a tunnel still connecting when it expires fails
the command.

## How it works

The router exposes JSON-RPC 2.0 at `POST /rpc`:

1. `challenge` returns `alg`, `salt`, `nonce` and, on newer firmware,
   `hash-method`.
2. The client computes `crypt(password, "$<alg>$<salt>")` (MD5/SHA-256/SHA-512
   crypt) and hashes `username:<crypt>:nonce` with `hash-method`
   (MD5 when absent).
3. `login` with that hash returns a session ID.
4. Everything else is `call` with `[sid, module, function, args]`:
   `system.get_info`, `system.get_status`, `cable.get_status`,
   `upgrade.check_firmware_online`, `vpn-client.get_status`,
   `vpn-client.set_tunnel`.
5. `logout` ends the session when the command is done.

`set_tunnel` returns as soon as the router accepts the change; the tunnel
then connects in the background. `get_status` reports each tunnel's `status`:
`0` not started, `1` connected, `2` connecting.

The password itself never goes over the wire, but the session ID does, in
plain HTTP, and anyone who records the challenge and login hash can guess
the password offline. Use it on a network you trust, with a strong router
password. Logging out after every command,
even one cut short by Ctrl+C, SIGTERM or the timeout, keeps a captured session
ID from being reused. The client does not follow redirects, rejects a salt
that tries to raise the crypt round count, and replaces control and other
non-printable characters in router text with `?` before printing it.

## Sources

The API is only partly documented. The client is built from these sources and
checked against a router on firmware 4.9.0:

- [GL.iNet SDK 4.x API docs](https://dev.gl-inet.com/router-4.x-api/):
  the official reference for `challenge`, `login`, `logout` and `call`, and
  the 5-minute idle session timeout, and the VPN status codes (`0` not
  started, `1` connected, `2` connecting) of `wg-client` and `ovpn-client`.
  The site is offline since January 2024; an
  [archived copy](https://web.archive.org/web/20240121142533/https://dev.gl-inet.com/router-4.x-api/)
  remains. It predates the 4.8 `vpn-client` module.
- [gli4py](https://github.com/HarvsG/gli4py): Python client whose code and
  mock fixtures show the `vpn-client` module (`get_status`, `set_tunnel`) and
  the tunnel status codes.
- [python-glinet](https://pypi.org/project/python-glinet/)
  ([docs](https://python-glinet.readthedocs.io/)): Python client whose code
  shows `logout` taking `{"sid": ...}` and errors arriving in the JSON-RPC
  `error` object.

The `sha256` hash method is not in older docs or forum posts, which describe
MD5 only. It was found by calling `challenge` on the router.

## Develop

```sh
make          # lint, test, then build ./glinet-cli
make test     # go test -race, fails below the coverage minimum
make lint     # golangci-lint and go mod tidy -diff
make vuln     # govulncheck; release and publish run it too
make fmt
make help     # every target
```

`make lint` runs the golangci-lint version pinned in the `Makefile` through
`go run`, so it needs no install and is built with your Go. The first run
takes a while. Linters are set in `.golangci.yml`.

### Tests

`make test` runs every test with the race detector and fails when total
coverage drops below `COVERAGE_MIN` in the `Makefile`. Coverage counts what
any test reaches in any package, the scripts below included, so a behavior
needs one test, at the cheapest level that can see it.

- Unit tests sit next to the code. Router work runs against
  `glinettest.NewRouter`, a fake router on firmware 4.9.0 that issues a new
  session per login; `glinettest.NewVPN` adds a vpn-client module that keeps
  tunnel state and records every `set_tunnel` call.
- `src/testdata/script/*.txtar` are end-to-end scripts
  ([testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)):
  each runs the real `glinet-cli` against its own fake router and checks
  stdout, stderr and exit codes (`exits <code> glinet-cli ...`). A script can
  bring its own `tunnels.json`. The keychain is an in-memory mock there, so
  tests never touch the OS keychain. Run one with
  `go test ./src -run TestScript/vpn`; add a behavior by adding a script.

### Layout

- `src/main.go`: flags, help text, picks the command.
- `src/cmd/`: what commands share: the `Command` type, the registry
  (`Register`, `Lookup`, `All`) and `WithClient`, which logs in to the router.
- `src/cmd/auth`, `src/cmd/status`, `src/cmd/vpn`, `src/cmd/web`: one package per command.
- `src/internal/glinet/`: router API client. `client.go` has JSON-RPC and
  login; every other file covers one router API module (`system.go`,
  `vpnclient.go`).
- `src/internal/glinet/glinettest/`: fake router for tests.
- `src/internal/config/`: global flags (`-url`, `-user`, `-timeout`).
- `src/internal/keychain/`: the saved password, or `GLINET_PASSWORD`.

### Add a command

1. Add the API call to `src/internal/glinet`, in the file for its router module
   (e.g. `wifi.go` for `wifi.*`), with a test against `glinettest.NewRouter`.
2. Create `src/cmd/<name>/<name>.go` with an `init` that calls
   `cmd.Register(cmd.Command{...})`: its name, one usage line per form, and a
   `Parse` that only checks the arguments and returns an action. Wrap router
   work in `cmd.WithClient`, which logs in first and always logs out.
3. Add a blank import of the package (`_ ".../src/cmd/<name>"`) in
   `src/main.go`. The help text lists commands sorted by name.

## Release

Releases are automated. Every merge into `main` runs the checks, tags HEAD
with the next version (a `feat:` commit bumps the minor version, anything
else the patch version; the first release is `v0.1.0`), builds the platform
archives, publishes a GitHub release, and pushes the matching formula to
[antavik/homebrew-apps](https://github.com/antavik/homebrew-apps). The formula
installs the prebuilt binary for macOS or Linux (arm64/amd64), verified
against the release's `checksums.txt`. The tap update needs the
`HOMEBREW_TAP_TOKEN` repository secret (a fine-grained PAT with Contents
write access to the tap); without it releases still publish and the
Homebrew step skips itself.

The version bump reads commit subjects, so PRs are squash-merged and the PR
title becomes the subject. The title must be a
[Conventional Commit](https://www.conventionalcommits.org/):
`<type>[(scope)][!]: <summary>`, with type one of `feat fix perf refactor
docs test build ci chore revert`, e.g. `feat(vpn): add restart command`. The
`PR title` workflow checks it. Title a PR that bundles features `feat: …`,
not `release: vX.Y.Z`: the version comes from the type, not the title text.

To release a specific version by hand, tag the commit on `main` and push the
tag: `git tag -a v1.2.3 -m "glinet-cli 1.2.3" && git push origin v1.2.3`.
The next workflow run reuses a tag that already sits at HEAD. For a fully
local release, `make publish` builds `dist/` and uploads it to the tag's
GitHub release with `gh` (run `gh auth login` once); it stops if the tree has
changes or HEAD is not the tag. Note the Homebrew formula is only updated by
the CI pipeline.

Tags must start with `v` and follow semantic versioning.

## License

[MIT](LICENSE)
