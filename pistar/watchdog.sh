#!/bin/sh
# pistar-os-extras watchdog — run every minute by root's cron (crontab persisted in /data/etc/crontabs).
# Everything lives in /data, which survives Pi-Star OS A/B upgrades.
D=/data/opt/pistar-extras
INI=/etc/pistar-mcp/pistar-mcp.ini
LOG=/var/log/pistar-extras.log
LISTEN=":8081"; TAKE_PORT80="no"
[ -f $D/extras.conf ] && . $D/extras.conf

log() { echo "$(date '+%F %T') $*" >> $LOG; }

# optional: move the dashboard off port 80 so the extras can use it
if [ "$TAKE_PORT80" = "yes" ] && grep -q '^listen_http=:80$' "$INI"; then
    sed -i 's/^listen_http=:80$/listen_http=:8080/' "$INI"
    log "dashboard HTTP moved to :8080, restarting pistar-mcp"
    rc-service pistar-mcp restart >/dev/null 2>&1
    sleep 5
fi

if ! pidof pistar-extras >/dev/null; then
    log "starting pistar-extras on $LISTEN"
    cd $D && LISTEN="$LISTEN" DATA="$D" setsid ./pistar-extras >> $LOG 2>&1 < /dev/null &
fi

# optional Cloudflare Tunnel (remote-managed; token in tunnel.token)
if [ -s $D/tunnel.token ] && [ -x $D/cloudflared ] && ! pidof cloudflared >/dev/null; then
    log "starting cloudflared"
    setsid $D/cloudflared --no-autoupdate --loglevel warn tunnel run --token-file $D/tunnel.token >> $LOG 2>&1 < /dev/null &
fi

# log lives in RAM (tmpfs): trim above 2 MB
[ "$(wc -c < $LOG 2>/dev/null || echo 0)" -gt 2000000 ] && tail -c 500000 $LOG > $LOG.tmp && mv $LOG.tmp $LOG
exit 0
