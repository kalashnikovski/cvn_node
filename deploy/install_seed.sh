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
# The seed runs as a user of its own, cvn-seed, and keeps its chain in
# /var/lib/cvn_seed/NETWORK. A seed set up by an older version of this script
# kept its files in /opt/cvn_node; running this again moves them across.
#
# Running it again for the same network is safe: it updates and restarts.
# On the development network, giving a different Bitcoin height from last
# time (or none, after a rehearsal) starts that network's chain afresh, since
# a chain begun from one first block is not valid under another.
set -euo pipefail

NETWORK="${1:-cvn_testnet}"
SEEDS="${2:-}"
HEIGHT="${3:-}"
DIR=/opt/cvn_node                  # the program and its source: root's
SEED_USER=cvn-seed                 # the user the seed's node runs as
DATA="/var/lib/cvn_seed/$NETWORK"  # the chain and the other files the node writes
REPO=https://github.com/kalashnikovski/cvn_node

case "$NETWORK" in
  cvn_testnet) PORTS=18080:18081; DBFILE=cvn_testnet.db; PEERFILE=peers_testnet.json ;;
  devnet)      PORTS=28080:28081; DBFILE=cvn_devnet.db;   PEERFILE=peers_devnet.json ;;
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
# Go fetch that version if it is older. The program is built beside the old
# one and moved into place, so a seed that is running keeps its own file until
# it is restarted. The source and the program stay root's: the seed's user can
# run them but never change them.
(cd "$DIR" && GOTOOLCHAIN=auto GOFLAGS=-buildvcs=false go build -o cvn_node.new . && mv -f cvn_node.new cvn_node)
chmod 755 "$DIR" "$DIR/cvn_node"

echo "== Making the seed's user and folder"
# The node listens to strangers, so it runs as a user of its own, with a
# folder of its own to write in. It cannot change the program.
if ! id -u "$SEED_USER" >/dev/null 2>&1; then
  useradd --system --user-group --home-dir /var/lib/cvn_seed --shell /usr/sbin/nologin "$SEED_USER"
fi
# The folder that holds every network's folder is root's, so that the seed of
# one network can never make or replace the folder of another. A folder that
# turns out to be a link to somewhere else is not what this script leaves,
# and nothing is done through it.
install -d -o root -g root -m 755 /var/lib/cvn_seed
refuse_links() {
  # The folder, and every name in it that this script moves, copies or
  # writes. The seed's user owns the folder and could put a link under any
  # of them; this script runs as root and must not follow one.
  for path in "$DATA" "$DATA/backups" "$DATA/miner_config.json" \
              "$DATA/$DBFILE" "$DATA/genesis_anchor_$NETWORK.txt" "$DATA/$PEERFILE" \
              "$DATA/$DBFILE.moving" "$DATA/genesis_anchor_$NETWORK.txt.moving" "$DATA/$PEERFILE.moving"; do
    if [ -L "$path" ]; then
      echo "$path is a link to somewhere else. This script does not make it so. Look at it before running this again."; exit 1
    fi
  done
}
refuse_links
install -d -o "$SEED_USER" -g "$SEED_USER" -m 750 "$DATA"

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
systemctl stop "$UNIT" 2>/dev/null || true
# Looked at again now that the seed is stopped and can change nothing.
refuse_links
FRESH=""
if [ "$NETWORK" = devnet ]; then
  # The Bitcoin height decides the first block. If it is not the one this
  # server used last time, the chain it holds belongs to another first block
  # and so does the saved Bitcoin header: both go, from the seed's folder and
  # from the old place in $DIR, and nothing is moved across first.
  PREVIOUS=""
  if [ -f "$UNIT_FILE" ]; then
    PREVIOUS=$(sed -n 's/^Environment=CVN_DEVNET_ANCHOR_HEIGHT=//p' "$UNIT_FILE")
  fi
  if [ "$PREVIOUS" != "$HEIGHT" ]; then
    echo "== The first block has changed (Bitcoin height: was '${PREVIOUS:-none}', now '${HEIGHT:-none}'): starting the devnet chain afresh"
    rm -f "$DATA/$DBFILE" "$DATA/genesis_anchor_devnet.txt" "$DIR/$DBFILE" "$DIR/genesis_anchor_devnet.txt"
    # So does a copy of either that an earlier run was cut off while moving,
    # and a half-moved list of the old chain's peers.
    rm -f "$DATA/$DBFILE.moving" "$DATA/genesis_anchor_devnet.txt.moving" "$DATA/$PEERFILE.moving"
    # The copies of the old chain belong to the old first block. They are
    # moved aside, not deleted, so that they are never taken for copies of
    # the new chain.
    STAMP=$(date +%Y%m%d-%H%M%S)-$$
    if [ -d "$DATA/backups" ]; then
      # -T: the folder is renamed, never moved into something that was put
      # under that name beforehand.
      mv -T "$DATA/backups" "$DATA/backups.before-$STAMP"
    fi
    for copy in "$DIR/backups/${DBFILE%.db}_block"*.db; do
      if [ -f "$copy" ]; then
        install -d -m 700 "$DIR/backups.before-$STAMP"
        mv "$copy" "$DIR/backups.before-$STAMP/"
      fi
    done
    FRESH=yes
  fi
fi
# A seed set up by the older script kept its files in $DIR and ran as root.
# This network's files are moved to the seed's folder, once. A file already
# there is never overwritten, and another network's files are left alone.
MOVED=""
if [ -z "$FRESH" ]; then
  for name in "$DBFILE" "genesis_anchor_$NETWORK.txt" "$PEERFILE"; do
    # A move that was cut off after the file had left $DIR and before it had
    # its real name is finished now. The file is whole: it only leaves $DIR
    # once all of it has arrived.
    if [ -f "$DATA/$name.moving" ] && [ ! -L "$DATA/$name.moving" ] && [ ! -e "$DIR/$name" ] && [ ! -e "$DATA/$name" ] && [ ! -L "$DATA/$name" ]; then
      mv -T "$DATA/$name.moving" "$DATA/$name"
      MOVED="$MOVED $name"
    fi
    if [ -f "$DIR/$name" ] && [ ! -e "$DATA/$name" ] && [ ! -L "$DATA/$name" ]; then
      # A copy left under the moving name by a run that was cut off is never
      # deleted. It is usually half a file. But if the server started the
      # old seed again in between, the file now in $DIR is a new one and the
      # copy left behind is the whole chain. So it is set aside, and said.
      if [ -e "$DATA/$name.moving" ] || [ -L "$DATA/$name.moving" ]; then
        ASIDE="$DATA/$name.cut-off-$(date +%Y%m%d-%H%M%S)-$$"
        mv -T "$DATA/$name.moving" "$ASIDE"
        echo "== An earlier run of this script was cut off while moving $name."
        echo "   What it left is kept as $ASIDE and the file in $DIR is moved in now."
        echo "   If the seed then starts from the first block, stop it and put $ASIDE in the place of $DATA/$name."
        echo "   Delete $ASIDE once the seed is running well."
      fi
      # Moved under another name first, so that a move cut off half-way
      # (a full disk) never leaves half a file under the real name.
      mv -T "$DIR/$name" "$DATA/$name.moving"
      mv -T "$DATA/$name.moving" "$DATA/$name"
      MOVED="$MOVED $name"
    fi
  done
  # The folder of chain copies is shared by networks; only this network's
  # copies are named after its database.
  if [ -d "$DIR/backups" ]; then
    install -d -o "$SEED_USER" -g "$SEED_USER" -m 700 "$DATA/backups"
    for copy in "$DIR/backups/${DBFILE%.db}_block"*.db; do
      if [ -f "$copy" ] && [ ! -e "$DATA/backups/$(basename "$copy")" ]; then
        mv "$copy" "$DATA/backups/"
        MOVED="$MOVED backups/$(basename "$copy")"
      fi
    done
  fi
fi
# A settings file written by hand (miner_config.json, which can name other
# nodes to call) is read from the folder the node runs in. It is shared by
# the networks, so it is copied and the first one is left where it was.
if [ -f "$DIR/miner_config.json" ] && [ ! -e "$DATA/miner_config.json" ] && [ ! -L "$DATA/miner_config.json" ]; then
  cp -p "$DIR/miner_config.json" "$DATA/miner_config.json"
  echo "== Copied miner_config.json from $DIR to $DATA"
fi
if [ -n "$MOVED" ]; then
  echo "== Moved from $DIR to $DATA, where the seed now keeps them:$MOVED"
fi
chown -h -R "$SEED_USER:$SEED_USER" "$DATA"
cat > "$UNIT_FILE" <<EOF
[Unit]
Description=Covenant Standard (CVN) seed node, $NETWORK
After=network-online.target
Wants=network-online.target

[Service]
User=$SEED_USER
Group=$SEED_USER
WorkingDirectory=$DATA
ExecStart=$DIR/cvn_node $ARGS
$REHEARSAL
Restart=always
RestartSec=10
# A seed needs little; this stops a fault from taking the whole server down.
MemoryMax=800M
# The program is told a lower figure, so that it tidies its memory harder as
# it nears the limit and is not stopped by it.
Environment=GOMEMLIMIT=600MiB
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
echo "  Its chain and files:    $DATA (it runs as the user $SEED_USER, not as root)"
echo "  Stop it:                systemctl stop $UNIT"
echo "  Update it later:        bash $DIR/deploy/update_seed.sh"
