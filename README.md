# debeasy

> A lightweight, browser-based DB admin UI — one navigator for **PostgreSQL**,
> **MySQL/MariaDB**, and **SQLite**. Single ~17 MB Go binary, multi-user, neo-brutalist.

## What it does

- Navigate schemas → tables / views / indexes; inspect columns, indexes, FKs, DDL.
- Run ad-hoc SQL in a dialect-aware editor (CodeMirror 6, `⌘/Ctrl + Enter` to execute,
  multi-statement, per-user history).
- Create databases, tables, views, and indexes from guided forms.
- Insert / edit / delete rows from a modal (PK-required).
- Per-user accounts — first run seeds an admin; admins add more users.
- Saved connection credentials are **AES-GCM encrypted** at rest; sessions use
  HMAC-signed cookies + double-submit CSRF; `/login` is rate-limited.
- Multi-session safe — every request gets its own context, so closing a tab cancels
  the running statement on the DB.

## Install

### 1 · One-line installer (preferred)

On a fresh Linux or macOS server:

```sh
curl -fsSL https://raw.githubusercontent.com/pfortini/debeasy/main/scripts/install.sh | bash
```

Prompts for admin credentials, then:

- drops the binary in `/usr/local/bin`
- creates a `debeasy` system user and `/var/lib/debeasy` (mode `0700`)
- seeds the admin directly in the SQLite store — no `/setup` HTTP race
- writes a hardened systemd unit and starts the service on `127.0.0.1:8080`

Unattended installs (CI / IaC):

```sh
ADMIN_USER=alice ADMIN_PASS="$(openssl rand -base64 24)" \
  curl -fsSL .../install.sh | bash
```

Override defaults with `PREFIX`, `DATA_DIR`, `SERVICE_USER`, `ADDR`, `VERSION`.

### 2 · Docker

```sh
docker run --rm -p 8080:8080 -v debeasy-data:/app/data \
  ghcr.io/pfortini/debeasy:latest
```

Or build locally:

```sh
docker build -t debeasy:local .
docker run --rm -p 8080:8080 -v debeasy-data:/app/data debeasy:local
```

### 3 · From source

```sh
git clone https://github.com/pfortini/debeasy && cd debeasy
go build -ldflags="-s -w" -o debeasy ./cmd/debeasy

# seed an admin (skip the /setup screen)
echo -n "supersecret" | ./debeasy admin create --username alice --password-stdin

# run
./debeasy --addr :8080 --data-dir ~/.debeasy
```

Go 1.25+ is required. No other toolchain — templates, CSS, htmx, and the CodeMirror 6
bundle are all committed and embedded via `//go:embed`.

## Put it behind TLS

For anything beyond localhost, terminate TLS in a reverse proxy and forward to
`127.0.0.1:8080`. The session cookie's `Secure` flag is set automatically when the
proxy sends `X-Forwarded-Proto: https`.

```caddy
db.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

## Configuration

| flag / env                         | default        | purpose                                                    |
|------------------------------------|----------------|------------------------------------------------------------|
| `--addr` / `DEBEASY_ADDR`          | `:8080`        | HTTP listen address                                         |
| `--data-dir` / `DEBEASY_DATA_DIR`  | `~/.debeasy`   | app SQLite store + encryption key                           |
| `DEBEASY_APP_SECRET`               | auto-generated | 32 bytes hex. Auto-written to `<data-dir>/secret` if unset. |

### CLI

```sh
debeasy                                              # run server (default)
debeasy version                                       # print build version
debeasy admin create    --username U --password-stdin   # seed / add a user
debeasy admin reset-password --username U --password-stdin
debeasy update --check                                # see if a new release is out
sudo debeasy update                                   # download + swap + systemctl restart
```

All admin subcommands accept `--if-not-exists` (idempotent) and read passwords from
stdin so they never appear in `argv`.

## Updates

A running instance polls the `pfortini/debeasy` GitHub releases API once a day and
surfaces a banner in the UI (admins only) whenever a newer release is published.
To apply it, run the CLI on the host:

```sh
sudo debeasy update              # latest release, prompts for confirmation
sudo debeasy update --yes        # unattended
sudo debeasy update --check      # just compare versions, don't download
sudo debeasy update --version v1.2.0   # pin a specific tag
sudo debeasy update --no-restart       # swap the binary but leave systemd alone
```

The flow mirrors the installer: downloads `debeasy-${OS}-${ARCH}` from the release,
verifies `sha256` against the release's `checksums.txt`, atomically replaces the
binary (Linux keeps the running process's open inode, so there's no "text file busy"),
then `systemctl restart debeasy.service`.

| env var                        | default             | purpose                                         |
|--------------------------------|---------------------|-------------------------------------------------|
| `DEBEASY_UPDATE_CHECK`         | `1`                 | set `0` to disable the in-server banner poll    |
| `DEBEASY_UPDATE_INTERVAL`      | `24h`               | poll interval (`time.ParseDuration` syntax)     |
| `DEBEASY_UPDATE_REPO`          | `pfortini/debeasy`  | override when running a fork                    |

**Passwordless restart (optional).** If you'd rather not type the sudo password,
add a narrow sudoers rule for the operator account:

```
alice ALL=(root) NOPASSWD: /usr/bin/systemctl restart debeasy.service, /usr/local/bin/debeasy update *
```

## Dev environment

```sh
./scripts/dev.sh
```

Brings up PostgreSQL 16 + MySQL 8 via `docker-compose.dev.yml`, grants `debeasy` user
full privileges on MySQL, and runs the app with `go run` on `:8080`.

Connection parameters for the dev containers:

| kind     | host        | port  | user      | pass      | db         |
|----------|-------------|-------|-----------|-----------|------------|
| postgres | 127.0.0.1   | 55432 | `debeasy` | `debeasy` | `postgres` |
| mysql    | 127.0.0.1   | 53306 | `debeasy` | `debeasy` | `debeasy`  |
| sqlite   | — (any file path, e.g. `/tmp/test.sqlite`) |

## Remote databases

Nothing's pinned to localhost — if a PG/MySQL target accepts connections from the
debeasy host and the credentials permit, it works the same as a local one. Usual
checks: `listen_addresses`/`bind-address`, firewall, `pg_hba` or `user@host` grants,
and set `sslmode=require` (PG) or `tls=true` in extra params (MySQL) for any link
not on a trusted network.

## Release cadence

Tag a semver (`git tag v1.0.1 && git push --tags`) — the workflow in
`.github/workflows/release.yml` cross-compiles for `linux/amd64`, `linux/arm64`,
`darwin/amd64`, `darwin/arm64`, generates checksums, and creates the GitHub Release.
The one-liner installer resolves `…/releases/latest/download/debeasy-${OS}-${ARCH}`
automatically.

## Repo layout

```
cmd/debeasy/      entrypoint + admin subcommand
internal/
  config/         flags, env, data-dir bootstrap
  crypto/         AES-GCM keyring + cookie keys
  dbx/            Driver interface + pg/mysql/sqlite impls + conn pool
  server/         chi router, middleware, handlers, html/template renderer
  store/          app SQLite: users, sessions, connections, history
web/
  templates/      layout + pages + partials
  static/         css, js, vendored htmx + cm6 bundle
  cm6/            CM6 source — `npm run build` regenerates the bundle
scripts/          install.sh, dev.sh, mysql-init.sql
```

## License

MIT.
