#!/usr/bin/env bash
# debeasy installer — `curl -fsSL https://your-host/install.sh | bash`
#
# What it does, in order:
#   1. detect OS / arch and download the matching binary (or build from source if Go is present)
#   2. create a system user + a 0700 data dir
#   3. prompt for an admin username + password (or take from env)
#   4. seed the admin via `debeasy admin create --if-not-exists`     -- this avoids the
#      `/setup` HTTP race where the first browser to hit the server becomes admin
#   5. write a systemd unit and start the service
#
# Configuration (env vars; all optional):
#
#   PREFIX=/usr/local                 install prefix (binary goes in $PREFIX/bin)
#   DATA_DIR=/var/lib/debeasy         persistent state (sqlite + secret key)
#   SERVICE_USER=debeasy              system user that owns DATA_DIR + runs the service
#   ADDR=127.0.0.1:8080               listen address (keep it on localhost behind a reverse proxy)
#   VERSION=latest                    GitHub release tag, or "latest"
#   REPO=pfortini/debeasy              GitHub owner/repo for binary download
#   ADMIN_USER=...                    skip the prompt by passing creds via env (CI / IaC)
#   ADMIN_PASS=...
#
# Examples:
#   # interactive (typical):
#   curl -fsSL https://example.com/install.sh | bash
#
#   # fully unattended:
#   ADMIN_USER=alice ADMIN_PASS=$(openssl rand -base64 16) curl -fsSL ... | bash

set -euo pipefail

PREFIX="${PREFIX:-/usr/local}"
DATA_DIR="${DATA_DIR:-/var/lib/debeasy}"
SERVICE_USER="${SERVICE_USER:-debeasy}"
ADDR="${ADDR:-127.0.0.1:8080}"
VERSION="${VERSION:-latest}"
REPO="${REPO:-pfortini/debeasy}"

bold() { printf "\033[1m%s\033[0m\n" "$*"; }
info() { printf "  %s\n" "$*"; }
fail() { printf "\033[31merror:\033[0m %s\n" "$*" >&2; exit 1; }

# ---------- 0. sanity checks ----------
if [ "$(id -u)" -eq 0 ]; then
  SUDO=""
elif command -v sudo >/dev/null 2>&1; then
  SUDO="sudo"
else
  fail "this installer needs root or sudo"
fi

# Run a command as $SERVICE_USER. Works whether we started as root (use runuser,
# falling back to sudo) or as a sudoer (use sudo). $SUDO alone is not enough:
# when we're already root it's empty, and "-u user cmd" is not a valid command.
as_service_user() {
  if [ "$(id -u)" -eq 0 ]; then
    if command -v runuser >/dev/null 2>&1; then
      runuser -u "$SERVICE_USER" -- "$@"
    elif command -v sudo >/dev/null 2>&1; then
      sudo -u "$SERVICE_USER" -- "$@"
    else
      fail "need runuser or sudo to drop privileges to $SERVICE_USER"
    fi
  else
    sudo -u "$SERVICE_USER" -- "$@"
  fi
}

# `read` from /dev/tty so prompts work even when stdin is the curl|bash pipe
prompt() { local var="$1" msg="$2"; read -rp "$msg" "$var" </dev/tty; }
prompt_silent() { local var="$1" msg="$2"; read -rsp "$msg" "$var" </dev/tty; echo; }

# ---------- 1. detect platform ----------
case "$(uname -s)" in
  Linux)  OS=linux ;;
  Darwin) OS=darwin ;;
  *)      fail "unsupported OS: $(uname -s)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

bold "==> installing debeasy ($OS/$ARCH)"

# ---------- 2. download or build the binary ----------
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

BIN_DEST="$PREFIX/bin/debeasy"

download_binary() {
  local url
  if [ "$VERSION" = "latest" ]; then
    url="https://github.com/${REPO}/releases/latest/download/debeasy-${OS}-${ARCH}"
  else
    url="https://github.com/${REPO}/releases/download/${VERSION}/debeasy-${OS}-${ARCH}"
  fi
  info "downloading $url"
  curl -fsSL --retry 3 -o "$TMPDIR/debeasy" "$url"
}

build_from_source() {
  info "downloading via 'go install github.com/${REPO}/cmd/debeasy@${VERSION}' (no release artifact)"
  GOBIN="$TMPDIR" go install "github.com/${REPO}/cmd/debeasy@${VERSION}"
}

if download_binary 2>/dev/null; then
  :
elif command -v go >/dev/null 2>&1; then
  build_from_source
else
  fail "no release artifact at the expected URL and Go is not installed; either install Go or wait for a release"
fi
chmod +x "$TMPDIR/debeasy"

bold "==> installing binary to $BIN_DEST"
$SUDO install -o root -g root -m 755 "$TMPDIR/debeasy" "$BIN_DEST"

# ---------- 3. system user + data dir ----------
if ! id -u "$SERVICE_USER" >/dev/null 2>&1; then
  info "creating system user $SERVICE_USER"
  $SUDO useradd -r -s /usr/sbin/nologin -d "$DATA_DIR" "$SERVICE_USER"
fi
$SUDO install -d -o "$SERVICE_USER" -g "$SERVICE_USER" -m 700 "$DATA_DIR"

# ---------- 4. admin credentials ----------
if [ -z "${ADMIN_USER:-}" ]; then
  prompt ADMIN_USER "admin username: "
fi
if [ -z "${ADMIN_PASS:-}" ]; then
  while :; do
    prompt_silent ADMIN_PASS  "admin password (>= 8 chars): "
    if [ "${#ADMIN_PASS}" -lt 8 ]; then
      echo "  password too short — try again"
      continue
    fi
    prompt_silent ADMIN_PASS2 "confirm password:            "
    [ "$ADMIN_PASS" = "$ADMIN_PASS2" ] && break
    echo "  passwords don't match — try again"
  done
fi

bold "==> seeding admin user"
# Pipe the password via stdin so it never appears in argv / process list.
printf '%s' "$ADMIN_PASS" | \
  as_service_user "$BIN_DEST" admin create \
    --data-dir "$DATA_DIR" \
    --username "$ADMIN_USER" \
    --password-stdin \
    --if-not-exists
unset ADMIN_PASS ADMIN_PASS2

# ---------- 5. systemd unit ----------
bold "==> writing /etc/systemd/system/debeasy.service"
$SUDO tee /etc/systemd/system/debeasy.service >/dev/null <<EOF
[Unit]
Description=debeasy — multi-DB admin UI
After=network.target

[Service]
User=${SERVICE_USER}
Group=${SERVICE_USER}
ExecStart=${BIN_DEST} --addr ${ADDR} --data-dir ${DATA_DIR}
Restart=on-failure
RestartSec=2

# Hardening
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
NoNewPrivileges=true
ReadWritePaths=${DATA_DIR}

[Install]
WantedBy=multi-user.target
EOF

$SUDO systemctl daemon-reload
$SUDO systemctl enable --now debeasy.service

# ---------- 6. wait for healthcheck ----------
HOST_PORT="${ADDR#*:}"
LISTEN_HOST="${ADDR%:*}"
[ "$LISTEN_HOST" = "$ADDR" ] && LISTEN_HOST="127.0.0.1"
HEALTH_URL="http://${LISTEN_HOST}:${HOST_PORT}/healthz"
info "waiting for ${HEALTH_URL}"
for i in $(seq 1 20); do
  if curl -fsS --max-time 1 "$HEALTH_URL" >/dev/null 2>&1; then
    break
  fi
  sleep 0.5
done

bold "==> done"
info "binary:      $BIN_DEST"
info "data dir:    $DATA_DIR  (mode 0700, owner $SERVICE_USER)"
info "listen:      $ADDR"
info "admin user:  $ADMIN_USER"
info "service:     systemctl status debeasy"
echo
info "for public access put nginx/Caddy in front (terminate TLS, forward to $ADDR,"
info "and set X-Forwarded-Proto: https so the session cookie gets the Secure flag)."
