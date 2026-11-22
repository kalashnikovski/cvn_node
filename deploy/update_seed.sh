#!/usr/bin/env bash
# Updates a CVN seed node set up with install_seed.sh: fetches the newest
# source, rebuilds, and restarts every seed service on this server.
#
# Run as root:   bash /opt/cvn_node/deploy/update_seed.sh
#
# The chain databases are kept. If a release says a network's chain must be
# started afresh, pass that network's database file to delete first:
#
#   bash /opt/cvn_node/deploy/update_seed.sh cvn_devnet.db
#
# To move to one particular release and not the newest source, name its tag:
#
#   CVN_VERSION=v5.0.0 bash /opt/cvn_node/deploy/update_seed.sh
set -euo pipefail

DIR=/opt/cvn_node
if [ "$(id -u)" -ne 0 ]; then
  echo "Run this as root (or with sudo)."; exit 1
fi

echo "== Stopping the seed services"
# Only the services that are switched on. One that was switched off, such
# as the service of a network that has been retired, is left alone.
UNITS=$(systemctl list-unit-files 'cvn-seed-*.service' --state=enabled --no-legend | awk '{print $1}')
for unit in $UNITS; do systemctl stop "$unit"; done

echo "== Fetching and building"
git -C "$DIR" fetch --tags --quiet origin
if [ -n "${CVN_VERSION:-}" ]; then
  git -C "$DIR" checkout --quiet "refs/tags/$CVN_VERSION"
else
  git -C "$DIR" checkout --quiet main
  git -C "$DIR" pull --ff-only
fi
(cd "$DIR" && GOTOOLCHAIN=auto GOFLAGS=-buildvcs=false go build -o cvn_node .)

for database in "$@"; do
  case "$database" in
    *.db) echo "== Removing $database"; rm -f "$DIR/$database" ;;
    *)    echo "Not removing $database: only a .db file in $DIR can be named"; exit 1 ;;
  esac
done

echo "== Starting the seed services"
for unit in $UNITS; do systemctl start "$unit"; done
sleep 5
for unit in $UNITS; do
  systemctl --no-pager --lines=3 status "$unit" || true
done
echo
echo "Updated to: $(git -C "$DIR" log --oneline -1)"
