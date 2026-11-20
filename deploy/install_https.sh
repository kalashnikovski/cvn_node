#!/usr/bin/env bash
# Gives a CVN seed a name and HTTPS, so that its explorer and wallet address
# open as https://NAME/ and a browser shows the padlock.
#
# It installs Caddy, a small web server that fetches a free certificate from
# Let's Encrypt and renews it by itself. Caddy listens on ports 80 and 443
# and passes each request to the seed on this same machine. The seed itself
# is not changed, and its own ports keep working as before.
#
# Before running it, the name must already point at this server: an "A"
# record for it, holding this server's address, at whoever the domain was
# registered with. The script checks, and stops if it does not.
#
# Run as root on the seed:
#
#   bash install_https.sh singapore.covenantstandard.online devnet
#   bash install_https.sh singapore.covenantstandard.online cvn_testnet
#
# Running it again is safe. To serve another network under the same name
# later, run it again with that network.
set -euo pipefail

NAME="${1:-}"
NETWORK="${2:-devnet}"

case "$NETWORK" in
  cvn_testnet) PORT=18081 ;;
  devnet)      PORT=28081 ;;
  *) echo "Unknown network: $NETWORK (use cvn_testnet or devnet)"; exit 1 ;;
esac
case "$NAME" in
  ''|*[!a-z0-9.-]*|*..*|.*|*.) echo "Give the name this seed is to answer to, for example singapore.covenantstandard.online"; exit 1 ;;
esac
case "$NAME" in
  *.*) ;;
  *) echo "That is not a full name: $NAME"; exit 1 ;;
esac
if [ "$(id -u)" -ne 0 ]; then
  echo "Run this as root (or with sudo)."; exit 1
fi

echo "== Checking that $NAME points at this server"
export DEBIAN_FRONTEND=noninteractive
apt-get update -y
apt-get install -y curl ufw debian-keyring debian-archive-keyring apt-transport-https gnupg
MINE="$(curl -4 -s --max-time 10 https://api.ipify.org || true)"
THEIRS="$(getent ahostsv4 "$NAME" | awk 'NR==1{print $1}' || true)"
if [ -z "$THEIRS" ]; then
  echo "$NAME does not point anywhere yet."
  echo "Add an A record for it holding this server's address${MINE:+ ($MINE)}, wait a few minutes, and run this again."
  exit 1
fi
if [ -n "$MINE" ] && [ "$MINE" != "$THEIRS" ]; then
  echo "$NAME points at $THEIRS, but this server is $MINE."
  echo "Correct the A record, wait a few minutes, and run this again."
  exit 1
fi

echo "== Checking that the seed answers on this machine"
if ! curl -s --max-time 5 "http://127.0.0.1:$PORT/block" | grep -q current_height; then
  echo "Nothing answered on port $PORT. Is the $NETWORK seed running? (systemctl status cvn-seed-$NETWORK)"
  exit 1
fi

echo "== Installing Caddy"
if ! command -v caddy >/dev/null 2>&1; then
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor --yes -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -y
  apt-get install -y caddy
fi

echo "== Writing /etc/caddy/Caddyfile"
cat > /etc/caddy/Caddyfile <<EOF
# Written by deploy/install_https.sh. Running that script again replaces it.
$NAME {
	encode gzip
	reverse_proxy 127.0.0.1:$PORT
}
EOF

echo "== Opening ports 80 and 443"
ufw allow 80/tcp  >/dev/null
ufw allow 443/tcp >/dev/null

systemctl enable caddy >/dev/null 2>&1 || true
systemctl restart caddy

echo "== Waiting for the certificate"
for attempt in $(seq 1 30); do
  if curl -s --max-time 5 "https://$NAME/block" | grep -q current_height; then
    echo
    echo "Done. https://$NAME/ is this seed's explorer, with a certificate that renews by itself."
    exit 0
  fi
  sleep 2
done
echo
echo "Caddy is running, but https://$NAME/ did not answer within a minute."
echo "If the hosting company has a firewall of its own in front of this server,"
echo "open ports 80 and 443 there too. To see what Caddy says:  journalctl -u caddy -n 40"
exit 1
