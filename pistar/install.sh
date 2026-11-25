#!/bin/sh
# pistar-os-extras installer — run ON the Pi-Star OS hotspot, from the unpacked repo:
#   sudo sh pistar/install.sh            (extras only)
#   sudo sh pistar/install.sh --tunnel   (also download cloudflared for a Cloudflare Tunnel)
set -e
[ "$(id -u)" -eq 0 ] || { echo "run with sudo"; exit 1; }
[ -f /etc/pistar-os-release ] || echo "warning: /etc/pistar-os-release not found (is this Pi-Star OS?)"
cd "$(dirname "$0")/.."
D=/data/opt/pistar-extras
mkdir -p $D/flags565
install -m 755 dist/pistar-extras-linux-armv6 $D/pistar-extras.new && mv $D/pistar-extras.new $D/pistar-extras
install -m 755 pistar/watchdog.sh $D/watchdog.sh
cp pistar/flags565/*.565 $D/flags565/
[ -f $D/extras.conf ] || install -m 644 pistar/extras.conf.example $D/extras.conf

if [ "$1" = "--tunnel" ] && [ ! -x $D/cloudflared ]; then
    echo "downloading cloudflared (official build, linux-arm)..."
    wget -q -O $D/cloudflared.new https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-arm
    chmod 755 $D/cloudflared.new && mv $D/cloudflared.new $D/cloudflared
    $D/cloudflared --version
fi
[ -f $D/tunnel.token ] && chmod 600 $D/tunnel.token

# root crontab persisted in /data/etc/crontabs (pistar-bindmounts mounts it over /etc/crontabs at boot)
mkdir -p /data/etc/crontabs
[ -f /data/etc/crontabs/root ] || cp -a /etc/crontabs/root /data/etc/crontabs/root
grep -q pistar-extras /data/etc/crontabs/root || echo '* * * * * /data/opt/pistar-extras/watchdog.sh' >> /data/etc/crontabs/root
chmod 600 /data/etc/crontabs/root
grep -q ' /etc/crontabs ' /proc/mounts || mount --bind /data/etc/crontabs /etc/crontabs
rc-service crond restart >/dev/null 2>&1 || true

# restart with the new binary
killall pistar-extras 2>/dev/null || true
sleep 1
$D/watchdog.sh
sleep 3
. $D/extras.conf
port=${LISTEN##*:}
if wget -q -O /dev/null "http://127.0.0.1:$port/cyd/status.json"; then
    ip=$(grep -B1 "/32 host LOCAL" /proc/net/fib_trie 2>/dev/null | grep -oE "[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+" | grep -v "^127\." | head -1)
    echo "OK: http://${ip:-<pi-star-ip>}:$port/cyd/status.json"
else
    echo "not answering yet, see /var/log/pistar-extras.log"
fi
