#!/usr/bin/env bash
# Sets up a CVN pool on a fresh Ubuntu server: a node that takes miners in,
# counts their work, and records what each of them is owed. It starts by
# itself when the server boots and restarts if it stops.
#
# Read POOL_GUIDE.md first. In short:
#
#   - The pool's blocks pay the miners themselves. Only parts too small for
#     a block go to the pool's own address. Make that wallet on a computer
#     of your own, write its 24 words down, and give this script the
#     address. The node set up here never holds the key.
#   - What the pool's address holds for miners is paid on by a timer this
#     script installs: every five hours, counted from the last payment run. The timer does nothing
#     until you have saved the pool's key on this server, locked with a
#     password (see the end of this script's output, and POOL_GUIDE.md).
#     That is done once. Nobody has to run a command to pay after that.
#
# Run as root:
#
#   bash install_pool.sh cvn_testnet CVN_POOL_ADDRESS
#   bash install_pool.sh devnet CVN_POOL_ADDRESS HOST,HOST,...
#                                         the development network, which has
#                                         no seeds of its own and must be
#                                         given the other machines' addresses
#   bash install_pool.sh devnet CVN_POOL_ADDRESS HOST,HOST,... BITCOIN_HEIGHT
#                                         a rehearsal of a launch on the
#                                         development network. Every machine
#                                         taking part must be given the same
#                                         height.
#
# To build one particular release and not the newest source, name its tag:
#
#   CVN_VERSION=v5.0.0 bash install_pool.sh cvn_testnet CVN_POOL_ADDRESS
#
# Payments go out every five hours unless another number of hours is named:
#
#   CVN_POOL_PAY_HOURS=2 bash install_pool.sh cvn_testnet CVN_POOL_ADDRESS
#
# Running it again for the same network is safe: it updates the program and
# restarts the pool. The chain, the pool's record and the saved key are kept.
set -euo pipefail

NETWORK="${1:-}"
ADDRESS="${2:-}"
SEEDS="${3:-}"
HEIGHT="${4:-}"
PAY_HOURS="${CVN_POOL_PAY_HOURS:-5}"
DIR=/opt/cvn_node                  # the program and its source
POOL_USER=cvnpool                  # the user the pool's node runs as
DATA="/var/lib/cvn_pool/$NETWORK"  # the chain and the pool's record
KEYS="/root/cvn_pool_keys/$NETWORK" # the saved key and its password: root only
REPO=https://github.com/kalashnikovski/cvn_node

case "$NETWORK" in
  cvn_testnet) NODE_PORTS=18080:18081; WORK_PORT=18083 ;;
  devnet)      NODE_PORTS=28080:28081; WORK_PORT=28083 ;;
  *) echo "Name the network first: bash install_pool.sh cvn_testnet CVN_POOL_ADDRESS  (or devnet)"; exit 1 ;;
esac
case "$ADDRESS" in
  CVN_*) ;;
  *) echo "Give the pool's address second, exactly as its wallet shows it: CVN_..."; exit 1 ;;
esac
case "${ADDRESS#CVN_}" in
  ''|*[!0-9a-f]*) echo "That is not an address as a wallet shows it: CVN_ followed by digits and the letters a to f. Got: $ADDRESS"; exit 1 ;;
esac
# The seeds end up in a service file written by root: nothing but
# addresses and the commas between them is let through.
case "$SEEDS" in
  *[!A-Za-z0-9.,:-]*) echo "Give the other machines' addresses alone, with commas between them and nothing else: HOST,HOST,..."; exit 1 ;;
esac
if [ "$NETWORK" = devnet ] && [ -z "$SEEDS" ]; then
  echo "The development network has no seeds of its own. Give the other machines' addresses third: HOST,HOST,..."; exit 1
fi
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
case "$PAY_HOURS" in
  ''|*[!0-9]*|0*) echo "The hours between payments must be a whole number from 1 to 168, for example 5. Got: $PAY_HOURS"; exit 1 ;;
esac
if [ "$PAY_HOURS" -gt 168 ]; then
  echo "The hours between payments must be a whole number from 1 to 168, for example 5. Got: $PAY_HOURS"; exit 1
fi

echo "== Installing what the build needs"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y git golang-go ufw

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
# Go fetch that version if it is older. The program is built beside the old
# one and moved into place, so a pool that is running keeps its own file
# until it is restarted.
(cd "$DIR" && GOTOOLCHAIN=auto GOFLAGS=-buildvcs=false go build -o cvn_node.new . && mv -f cvn_node.new cvn_node)

echo "== Making the pool's user and folders"
# The node that listens to miners runs as a user of its own, which can read
# neither root's files nor the saved key.
if ! id -u "$POOL_USER" >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /var/lib/cvn_pool --shell /usr/sbin/nologin "$POOL_USER"
fi
install -d -o "$POOL_USER" -g "$POOL_USER" -m 750 /var/lib/cvn_pool "$DATA"
install -d -o root -g root -m 700 /root/cvn_pool_keys "$KEYS"

echo "== Opening the ports: $NODE_PORTS for the network, $WORK_PORT for miners"
# Remote login is allowed first, so that turning the firewall on can never
# lock you out of the server. The port the paying command uses is not
# opened: it answers on this machine only.
ufw allow 22/tcp >/dev/null
ufw allow "$NODE_PORTS"/tcp >/dev/null
ufw allow "$WORK_PORT"/tcp >/dev/null
ufw --force enable >/dev/null

echo "== Installing the pool's service"
ARGS="--network $NETWORK --pool $ADDRESS"
if [ -n "$SEEDS" ]; then
  ARGS="$ARGS --seeds $SEEDS"
fi
UNIT="cvn-pool-$NETWORK"
UNIT_FILE="/etc/systemd/system/$UNIT.service"
REHEARSAL=""
if [ -n "$HEIGHT" ]; then
  REHEARSAL="Environment=CVN_DEVNET_ANCHOR_HEIGHT=$HEIGHT"
fi
if [ "$NETWORK" = devnet ]; then
  # The Bitcoin height decides the first block. If it is not the one this
  # server used last time, the chain it holds belongs to another first block,
  # and so do the saved Bitcoin header and the pool's record of that chain.
  # The record is moved aside, not deleted: it says what miners were owed.
  PREVIOUS=""
  if [ -f "$UNIT_FILE" ]; then
    PREVIOUS=$(sed -n 's/^Environment=CVN_DEVNET_ANCHOR_HEIGHT=//p' "$UNIT_FILE")
  fi
  if [ -f "$UNIT_FILE" ] && [ "$PREVIOUS" != "$HEIGHT" ]; then
    echo "== The first block has changed (Bitcoin height: was '${PREVIOUS:-none}', now '${HEIGHT:-none}'): starting the devnet chain afresh"
    systemctl stop "$UNIT" 2>/dev/null || true
    rm -f "$DATA/cvn_devnet.db" "$DATA/genesis_anchor_devnet.txt"
    if [ -f "$DATA/pool_devnet.db" ]; then
      mv "$DATA/pool_devnet.db" "$DATA/pool_devnet.db.before-$(date +%Y%m%d-%H%M%S)"
    fi
  fi
fi
cat > "$UNIT_FILE" <<EOF
[Unit]
Description=Covenant Standard (CVN) pool, $NETWORK
After=network-online.target
Wants=network-online.target

[Service]
User=$POOL_USER
Group=$POOL_USER
WorkingDirectory=$DATA
ExecStart=$DIR/cvn_node $ARGS
$REHEARSAL
Restart=always
RestartSec=10
LimitNOFILE=65536
# The node listens to strangers. It may write in its own folder and nowhere
# else, and can never become another user.
NoNewPrivileges=true
ProtectSystem=strict
ReadWritePaths=$DATA
ProtectHome=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

echo "== Installing the payment timer (every $PAY_HOURS hours)"
PAY_UNIT="cvn-pool-pay-$NETWORK"
cat > "/etc/systemd/system/$PAY_UNIT.service" <<EOF
[Unit]
Description=Covenant Standard (CVN) pool, $NETWORK: pay what is owed
After=$UNIT.service
# Nothing is tried until the pool's key has been saved here and its
# password put beside it (POOL_GUIDE.md).
ConditionPathExists=$KEYS/wallet_key.json
ConditionPathExists=$KEYS/password

[Service]
Type=oneshot
WorkingDirectory=$KEYS
ExecStart=$DIR/cvn_node --network $NETWORK --pool-pay --password-file $KEYS/password
EOF
cat > "/etc/systemd/system/$PAY_UNIT.timer" <<EOF
[Unit]
Description=Covenant Standard (CVN) pool, $NETWORK: pay what is owed, every $PAY_HOURS hours

[Timer]
# The hours are counted from the last payment run, and from fifteen minutes
# after the server starts: time for the pool to catch up with the network.
OnBootSec=15min
OnUnitActiveSec=${PAY_HOURS}h
AccuracySec=1min

[Install]
WantedBy=timers.target
EOF

systemctl daemon-reload
systemctl enable "$UNIT" >/dev/null
systemctl restart "$UNIT"
systemctl enable "$PAY_UNIT.timer" >/dev/null
systemctl restart "$PAY_UNIT.timer"

sleep 8
echo
systemctl --no-pager --lines=8 status "$UNIT" || true
echo
if ! systemctl is-active --quiet "$UNIT"; then
  echo "The pool did not stay running. The lines above say why; the commonest cause is an"
  echo "address that is not written exactly as the wallet shows it for this network."
  exit 1
fi
echo "Done. The pool for $NETWORK is running and will start by itself after a reboot."
echo "  Its blocks pay:         the miners themselves; parts too small for a block go to"
echo "                          $ADDRESS"
echo "  Miners connect to:      this server, port $WORK_PORT"
echo "  The pool page:          http://THIS-SERVER:${NODE_PORTS#*:}/pool"
echo "  See what it is doing:   journalctl -u $UNIT -f"
echo "  Copies of its record:   $DATA/backups, one an hour. Fetch them to another machine:"
echo "                          they do not outlive this server (POOL_GUIDE.md, section 6)."
echo "  Update it later:        run this script again with the same words"
echo
if [ -f "$KEYS/wallet_key.json" ] && [ -f "$KEYS/password" ]; then
  echo "Payments go out by themselves every $PAY_HOURS hours. The last run:  journalctl -u $PAY_UNIT -n 30"
else
  echo "ONE THING IS LEFT TO DO, ONCE. The pool's blocks pay the miners themselves, and that"
  echo "works already. Parts too small for a block go to the pool's address; the pool pays"
  echo "those on by itself every $PAY_HOURS hours, and for that it needs the pool's key. Until these"
  echo "three steps are done, those small parts are recorded and owed, and are not sent:"
  echo
  echo "  1. Save the pool's key on this server, locked with a password of at least"
  echo "     8 characters. In the wallet choose 6 (Recovery words and saved key), then c:"
  echo "       cd $KEYS && $DIR/cvn_node --network $NETWORK --wallet"
  echo "  2. Put that password in a file only root can read (it is not shown as you type):"
  echo "       ( umask 077; read -rsp 'Password: ' p; echo; printf '%s\n' \"\$p\" > $KEYS/password )"
  echo "  3. Try it once and read what it says:"
  echo "       systemctl start $PAY_UNIT; journalctl -u $PAY_UNIT -n 30 --no-pager"
  echo
  echo "After that nobody has to run anything to pay. POOL_GUIDE.md says what keeping the key"
  echo "on the server means for the safety of the pool's coins, and how to pay by hand instead."
fi
