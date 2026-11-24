#!/usr/bin/env bash
# Sets up a CVN seed node on a fresh Ubuntu server, as a service that starts
# by itself when the server boots and restarts if it stops.
#
# A seed keeps the chain, checks every block and passes blocks on. It does
# not mine, so it needs no wallet.
#
# Run as root, once for each network the server should serve:
#
#   bash install_seed.sh                  the CVN Testnet
#   bash install_seed.sh cvn_testnet      the same, by name
#   bash install_seed.sh devnet HOST,HOST,...
#                                         the development network, which has
#                                         no seeds of its own and must be
#                                         given the other machines' addresses
#   bash install_seed.sh devnet HOST,HOST,... BITCOIN_HEIGHT
#                                         a rehearsal of a launch on the
#                                         development network: the seed waits
#                                         for that Bitcoin block and starts
#                                         from it. Every machine taking part
#                                         must be given the same height.
#
# To build one particular release and not the newest source, name its tag:
#
#   CVN_VERSION=v5.0.0 bash install_seed.sh cvn_testnet
#
# Running it again for the same network is safe: it updates and restarts.
# On the development network, giving a different Bitcoin height from last
# time (or none, after a rehearsal) starts that network's chain afresh, since
# a chain begun from one first block is not valid under another.
set -euo pipefail

NETWORK="${1:-cvn_testnet}"
SEEDS="${2:-}"
HEIGHT="${3:-}"
DIR=/opt/cvn_node
REPO=https://github.com/kalashnikovski/cvn_node

case "$NETWORK" in
  cvn_testnet) PORTS=18080:18081 ;;
  devnet)      PORTS=28080:28081 ;;
  *) echo "Unknown network: $NETWORK (use cvn_testnet or devnet)"; exit 1 ;;
esac
# The seeds end up in a service file written by root: nothing but
# addresses and the commas between them is let through.
case "$SEEDS" in
  *[!A-Za-z0-9.,:-]*) echo "Give the other machines' addresses alone, with commas between them and nothing else: HOST,HOST,..."; exit 1 ;;
esac
if [ -n "$HEIGHT" ]; then
  if [ "$NETWORK" != devnet ]; then
    echo "A Bitcoin height can only be given for devnet. The other networks' first blocks are fixed in the program."; exit 1
  fi
  case "$HEIGHT" in
    ''|*[!0-9]*|0*) echo "The Bitcoin height must be a whole number, for example 975800. Got: $HEIGHT"; exit 1 ;;
  esac
fi
if [ "$(id -u)" -ne 0 ]; then
  echo "Run this as root (or with sudo)."; exit 1
fi

echo "== Installing what the build needs"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y git golang-go ufw

# The project names the exact Go it needs, and Go fetches it by itself, but
# only from Go 1.21 on. Ubuntu 24.04 has that; 22.04 does not (review of the
# pool, round 3, finding 13).
GO_MINOR=$(go version 2>/dev/null | sed -n 's/.*go1\.\([0-9]*\).*/\1/p')
if [ -z "$GO_MINOR" ] || [ "$GO_MINOR" -lt 21 ]; then
  echo "This needs Go 1.21 or newer to build the program, and this system's Go is $(go version 2>/dev/null || echo missing)."
  echo "Use Ubuntu 24.04 or newer, or install Go from https://go.dev/dl/ and run this again."; exit 1
fi

echo "== Getting the source"
if [ ! -d "$DIR/.git" ]; then
  git clone "$REPO" "$DIR"
fi
git -C "$DIR" fetch --tags --quiet origin
if [ -n "${CVN_VERSION:-}" ]; then
  # A named release: exactly that source, which anyone can check against
  # the release's published sums.
  git -C "$DIR" checkout --quiet "refs/tags/$CVN_VERSION"
  echo "   Building release $CVN_VERSION"
else
  git -C "$DIR" checkout --quiet main
  git -C "$DIR" pull --ff-only
fi

echo "== Building"
# The project names the exact Go version it needs; this lets the installed
# Go fetch that version if it is older.
(cd "$DIR" && GOTOOLCHAIN=auto GOFLAGS=-buildvcs=false go build -o cvn_node .)

echo "== Opening the network's ports ($PORTS)"
# Remote login is allowed first, so that turning the firewall on can never
# lock you out of the server.
ufw allow 22/tcp >/dev/null
ufw allow "$PORTS"/tcp >/dev/null
ufw --force enable >/dev/null

echo "== Installing the service"
ARGS="--network $NETWORK --no-mining"
if [ -n "$SEEDS" ]; then
  ARGS="$ARGS --seeds $SEEDS"
fi
UNIT="cvn-seed-$NETWORK"
UNIT_FILE="/etc/systemd/system/$UNIT.service"
REHEARSAL=""
if [ -n "$HEIGHT" ]; then
  REHEARSAL="Environment=CVN_DEVNET_ANCHOR_HEIGHT=$HEIGHT"
fi
if [ "$NETWORK" = devnet ]; then
  # The Bitcoin height decides the first block. If it is not the one this
  # server used last time, the chain it holds belongs to another first block
  # and so does the saved Bitcoin header: both go.
  PREVIOUS=""
  if [ -f "$UNIT_FILE" ]; then
    PREVIOUS=$(sed -n 's/^Environment=CVN_DEVNET_ANCHOR_HEIGHT=//p' "$UNIT_FILE")
  fi
  if [ "$PREVIOUS" != "$HEIGHT" ]; then
    echo "== The first block has changed (Bitcoin height: was '${PREVIOUS:-none}', now '${HEIGHT:-none}'): starting the devnet chain afresh"
    systemctl stop "$UNIT" 2>/dev/null || true
    rm -f "$DIR/cvn_devnet.db" "$DIR/genesis_anchor_devnet.txt"
  fi
fi
cat > "$UNIT_FILE" <<EOF
[Unit]
Description=Covenant Standard (CVN) seed node, $NETWORK
After=network-online.target
Wants=network-online.target

[Service]
WorkingDirectory=$DIR
ExecStart=$DIR/cvn_node $ARGS
$REHEARSAL
Restart=always
RestartSec=10
# A seed needs little; this stops a fault from taking the whole server down.
MemoryMax=800M
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable "$UNIT" >/dev/null
systemctl restart "$UNIT"

sleep 5
echo
systemctl --no-pager --lines=8 status "$UNIT" || true
echo
echo "Done. The seed for $NETWORK is running and will start by itself after a reboot."
if [ -n "$HEIGHT" ]; then
  echo "  It is rehearsing a launch: it waits for Bitcoin block $HEIGHT and starts from it."
  echo "  Every other machine must be given the same height."
fi
echo "  See what it is doing:   journalctl -u $UNIT -f"
echo "  Stop it:                systemctl stop $UNIT"
echo "  Update it later:        bash $DIR/deploy/update_seed.sh"
