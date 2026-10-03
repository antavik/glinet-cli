# glinet-cli

Small CLI for GL.iNet routers on firmware 4.8+ (tested against 4.9.0).
It shows router uptime and lists, enables and disables VPN client tunnels
from the VPN Dashboard (WireGuard and OpenVPN alike).

## Install

With [Homebrew](https://brew.sh) on macOS or Linux:

```sh
brew install antavik/tap/glinet-cli
```

Using the full name trusts only this formula, not the whole
[tap](https://github.com/antavik/homebrew-tap). `brew upgrade` picks up new
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
Uptime: 3d 4h 5m

$ glinet-cli vpn
ID    NAME            ENABLED  STATUS
2001  Mullvad/se-sto  on       connected
2002  Proton/nl-free  off      -

$ glinet-cli vpn on 2002          # by tunnel ID
$ glinet-cli vpn off "Proton/nl-free"  # by name, case-insensitive
$ glinet-cli vpn off all

$ glinet-cli web                  # open the router web UI in the default browser
```

Tunnels already in the requested state are left alone. With `all`, a failure
on one tunnel does not stop the others; the command exits non-zero and lists
every error.

## How it works

The router exposes JSON-RPC 2.0 at `POST /rpc`:

1. `challenge` returns `alg`, `salt`, `nonce` and, on newer firmware,
   `hash-method`.
2. The client computes `crypt(password, "$<alg>$<salt>")` (MD5/SHA-256/SHA-512
   crypt) and hashes `username:<crypt>:nonce` with `hash-method`
   (MD5 when absent).
3. `login` with that hash returns a session ID.
4. Everything else is `call` with `[sid, module, function, args]`:
   `system.get_status`, `vpn-client.get_status`, `vpn-client.set_tunnel`.
5. `logout` ends the session when the command is done.

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
  the 5-minute idle session timeout. The site is not always publicly reachable.
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
make test     # go test -race
make lint     # golangci-lint and go mod tidy -diff
make vuln     # govulncheck; release and publish run it too
make fmt
make hooks    # once per clone: version tag prompt after each commit
make help     # every target
```

`make lint` runs the golangci-lint version pinned in the `Makefile` through
`go run`, so it needs no install and is built with your Go. The first run
takes a while. Linters are set in `.golangci.yml`.

`make hooks` points Git at `scripts/hooks`. The `post-commit` hook then asks
after every commit whether to tag it with the next version – patch, minor,
major, a custom number, or skip (the default) – starting from the highest
`vX.Y.Z` tag. It only creates a local annotated tag; nothing is committed or
pushed. It stays quiet without a terminal and during rebase, cherry-pick and
merge; `GLINET_SKIP_VERSION_BUMP=1 git commit` skips a single commit. After
`git commit --amend` of a tagged commit it prints the command that moves the
tag onto the new one.

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

A release is a tag plus binaries on GitHub. The Homebrew formula in
[antavik/homebrew-tap](https://github.com/antavik/homebrew-tap) builds from
the source archive GitHub makes for each tag, not from those binaries.

1. Tag and push: `git tag -a v1.2.3 -m "glinet-cli 1.2.3" && git push origin v1.2.3`,
   or let the `make hooks` prompt create the tag and just push it.
2. Publish binaries: `make publish`. It builds `dist/` and uploads it to a
   GitHub release for the tag with `gh` (run `gh auth login` once). It stops
   if the tree has changes or HEAD is not the tag.
3. The tap's daily `brew bump` workflow opens a pull request with the new URL
   and SHA-256. To skip the wait, run
   `brew bump-formula-pr --version=1.2.3 antavik/tap/glinet-cli`.
4. When the pull request's `brew test-bot` checks pass, publish bottles with
   the tap's `brew pr-pull` workflow (Actions tab), or run
   `brew pr-pull --tap=antavik/tap --head-sha=<reviewed SHA> <PR number>`.

Tags must start with `v` and follow semantic versioning. Homebrew reads the
version from the archive name.

## License

[MIT](LICENSE)
