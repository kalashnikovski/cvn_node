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
# Only a plain file name is accepted. The file is looked for in /opt/cvn_node
# and in each seed's folder under /var/lib/cvn_seed.
#
# To move to one particular release and not the newest source, name its tag:
#
#   CVN_VERSION=v5.0.0 bash /opt/cvn_node/deploy/update_seed.sh
set -euo pipefail

DIR=/opt/cvn_node
if [ "$(id -u)" -ne 0 ]; then
  echo "Run this as root (or with sudo)."; exit 1
fi

# Every name is checked before anything is stopped or built. It must be a
# plain file name ending in .db: letters, digits, _ - and . only, no folder,
# no "..", and not starting with a dot or a dash.
for database in "$@"; do
  case "$database" in
    ''|*[!A-Za-z0-9_.-]*|.*|-*|*..*) bad=yes ;;
    *.db) bad="" ;;
    *) bad=yes ;;
  esac
  if [ -n "$bad" ]; then
    echo "Not changing anything: '$database' is not a database file name. Give a plain name such as cvn_devnet.db: letters, digits, _ - and . only, ending in .db, with no folder in front."; exit 1
  fi
done

echo "== Fetching and building"
# The new program is built beside the old one while the seeds go on running.
# If the source cannot be fetched or does not build, this stops here and the
# seeds are left running the program they had.
git -C "$DIR" fetch --tags --quiet origin
if [ -n "${CVN_VERSION:-}" ]; then
  git -C "$DIR" checkout --quiet "refs/tags/$CVN_VERSION"
else
  git -C "$DIR" checkout --quiet main
  git -C "$DIR" pull --ff-only
fi
(cd "$DIR" && GOTOOLCHAIN=auto GOFLAGS=-buildvcs=false go build -o cvn_node.new .)
# The seed runs as a user of its own and must be able to run the program,
# whatever this server gives new files by default.
chmod 755 "$DIR/cvn_node.new"

echo "== Stopping the seed services"
# Only the services that are switched on. One that was switched off, such
# as the service of a network that has been retired, is left alone.
UNITS=$(systemctl list-unit-files 'cvn-seed-*.service' --state=enabled --no-legend | awk '{print $1}')
for unit in $UNITS; do systemctl stop "$unit"; done
mv -f "$DIR/cvn_node.new" "$DIR/cvn_node"

# A seed made by install_seed.sh keeps its database in /var/lib/cvn_seed/NETWORK.
# An older one kept it in $DIR. The name is looked for in both places.
for database in "$@"; do
  echo "== Removing $database"
  removed=""
  for place in "$DIR" /var/lib/cvn_seed/*; do
    if [ -f "$place/$database" ]; then
      rm -f "$place/$database"
      echo "   Removed $place/$database"
      removed=yes
    fi
    # A copy of it that install_seed.sh was cut off while moving goes too,
    # so that it is never finished into place later.
    rm -f "$place/$database.moving"
  done
  if [ -z "$removed" ]; then
    echo "   No file called $database was found, so nothing was removed."
  fi
done

echo "== Starting the seed services"
for unit in $UNITS; do systemctl start "$unit"; done
sleep 5
for unit in $UNITS; do
  systemctl --no-pager --lines=3 status "$unit" || true
done
echo
echo "Updated to: $(git -C "$DIR" log --oneline -1)"
