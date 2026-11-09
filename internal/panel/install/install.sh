#!/bin/sh
# Installs the Bastion agent on this server and joins it to the panel. The panel's "Add a server" gives
# the command that runs this:  curl -fsSL <panel>/install.sh | sudo sh -s -- <panel> <join token>
set -eu

PANEL="${1:?the panel's address, e.g. https://bastion.home.lab}"
TOKEN="${2:?the join token from Add a server}"

if [ "$(id -u)" -ne 0 ]; then
	echo "Run this as root (with sudo)." >&2
	exit 1
fi
if ! command -v docker >/dev/null 2>&1 || ! docker compose version >/dev/null 2>&1; then
	echo "Docker and its compose plugin need to be installed first: https://docs.docker.com/engine/install/" >&2
	exit 1
fi

case "$(uname -m)" in
	x86_64 | amd64) ARCH=amd64 ;;
	aarch64 | arm64) ARCH=arm64 ;;
	*) echo "Bastion's agent runs on amd64 and arm64; this server is $(uname -m)." >&2; exit 1 ;;
esac

echo "Downloading the agent for $ARCH from $PANEL"
TMP="$(mktemp)"
curl -fsSL "$PANEL/downloads/bastion-agent-linux-$ARCH" -o "$TMP"
install -m 0755 "$TMP" /usr/local/bin/bastion-agent
rm -f "$TMP"

echo "Joining the panel"
/usr/local/bin/bastion-agent join "$PANEL" "$TOKEN"

cat >/etc/systemd/system/bastion-agent.service <<'UNIT'
[Unit]
Description=Bastion agent
After=network-online.target docker.service
Wants=network-online.target

[Service]
ExecStart=/usr/local/bin/bastion-agent run
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now bastion-agent
echo "Done. This server shows as online in the panel in a few seconds."
