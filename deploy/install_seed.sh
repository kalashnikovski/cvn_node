#!/usr/bin/env bash
# Sets up a CVN seed node on a fresh Ubuntu server, as a service that starts
# by itself when the server boots and restarts if it stops.
#
# A seed keeps the chain, checks every block and passes blocks on. It does
# not mine, so it needs no wallet.
#
# Run as root, once for each network the server should serve:
#
#   bash install_seed.sh                  the public test network (testnet-1)
#   bash install_seed.sh cvn_testnet      the CVN Testnet
#   bash install_seed.sh devnet HOST,HOST,...
#                                         the development network, which has
#                                         no seeds of its own and must be
#                                         given the other machines' addresses
#
# Running it again for the same network is safe: it updates and restarts.
set -euo pipefail

NETWORK="${1:-testnet-1}"
SEEDS="${2:-}"
DIR=/opt/cvn_node
REPO=https://github.com/kalashnikovski/cvn_node

case "$NETWORK" in
  testnet-1)   PORTS=8080:8081 ;;
  cvn_testnet) PORTS=18080:18081 ;;
  devnet)      PORTS=28080:28081 ;;
  *) echo "Unknown network: $NETWORK (use testnet-1, cvn_testnet or devnet)"; exit 1 ;;
esac
if [ "$(id -u)" -ne 0 ]; then
  echo "Run this as root (or with sudo)."; exit 1
fi

echo "== Installing what the build needs"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y git golang-go ufw

echo "== Getting the source"
if [ -d "$DIR/.git" ]; then
  git -C "$DIR" pull --ff-only
else
  git clone "$REPO" "$DIR"
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
cat > "/etc/systemd/system/$UNIT.service" <<EOF
[Unit]
Description=Covenant Standard (CVN) seed node, $NETWORK
After=network-online.target
Wants=network-online.target

[Service]
WorkingDirectory=$DIR
ExecStart=$DIR/cvn_node $ARGS
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
echo "  See what it is doing:   journalctl -u $UNIT -f"
echo "  Stop it:                systemctl stop $UNIT"
echo "  Update it later:        bash $DIR/deploy/update_seed.sh"
