#!/bin/sh
# Removes pistar-os-extras and puts the dashboard back on port 80. Keeps tunnel.token unless --all.
[ "$(id -u)" -eq 0 ] || { echo "run with sudo"; exit 1; }
D=/data/opt/pistar-extras
sed -i '/pistar-extras/d' /data/etc/crontabs/root 2>/dev/null
killall pistar-extras cloudflared 2>/dev/null
if grep -q '^listen_http=:8080$' /etc/pistar-mcp/pistar-mcp.ini; then
    sed -i 's/^listen_http=:8080$/listen_http=:80/' /etc/pistar-mcp/pistar-mcp.ini
    rc-service pistar-mcp restart
fi
if [ "$1" = "--all" ]; then rm -rf $D; else find $D -mindepth 1 ! -name tunnel.token -exec rm -rf {} + 2>/dev/null; fi
echo "removed"
