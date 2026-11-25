# pistar-os-extras

🇬🇧 English · 🇧🇷 [Português](README.pt-BR.md)

Extras for [**Pi-Star OS**](https://github.com/MW0MWZ/Pi-Star_OS) (MW0MWZ, Alpine-based, Pi-Star MCP dashboard).

Pi-Star OS does not write MMDVMHost logs to disk and has no Python or nginx, so tools built for the
classic Pi-Star (which read `/var/log/pi-star/MMDVM-*.log` or scrape the dashboard HTML) stop working.
`pistar-os-extras` runs on the hotspot and serves the classic endpoints again, in the classic formats:

| Endpoint | Used by |
|---|---|
| `/cyd/status.json` | [pistar-cyd-panel](https://github.com/danielregis81/pistar-cyd-panel) CYD display, apps |
| `/cyd/flags/<cc>.565` | pistar-cyd-panel (RGB565 flags) |
| `/mmdvmhost/lh.php` | scripts that scrape the classic "Last Heard" table |
| `/images/flags/<cc>.svg` | SVG flags (from flagcdn.com, cached) |
| `/extras/health` | bridge status |

It also (optionally) runs your own **Cloudflare Tunnel** on the hotspot, so the dashboard and the
endpoints above are reachable from anywhere — handy when the hotspot travels with you.

Everything is installed under `/data`, so it **survives Pi-Star OS A/B upgrades**.

## How it works

- `pistar-extras` (Go, single static binary, ~6 MB, ~10 MB RAM on a Pi Zero W) connects to the
  dashboard's own WebSocket (`wss://127.0.0.1/ws`), keeps the recent activity and serves the endpoints.
- Requests that arrive through the Cloudflare Tunnel and are not one of the endpoints above are
  reverse-proxied to the dashboard (HTTPS on 443, WebSocket included). From the LAN they are
  redirected to `https://<hotspot>/`, as the dashboard itself does.
- `watchdog.sh` runs every minute from root's crontab, which is persisted in `/data/etc/crontabs`
  (Pi-Star OS bind-mounts every `/data/etc/<name>/` over `/etc/<name>/` at boot).

## Install

On the hotspot (SSH as `pi-star`):

```sh
cd /tmp
wget -O extras.tar.gz https://github.com/danielregis81/pistar-os-extras/archive/refs/heads/main.tar.gz
tar xzf extras.tar.gz && cd pistar-os-extras-main
sudo sh pistar/install.sh            # extras only
# or
sudo sh pistar/install.sh --tunnel   # also download the official cloudflared (linux-arm)
```

The extras listen on **port 8081** by default: `http://<hotspot>:8081/cyd/status.json`.
Settings are in `/data/opt/pistar-extras/extras.conf`.

### Cloudflare Tunnel (optional)

1. In the Cloudflare dashboard create a tunnel (Networking → Tunnels) and add a public hostname
   pointing to `http://localhost:8081`.
2. Copy the tunnel token (the `eyJ…` string from the install command; do not run that command).
3. On the hotspot:
   ```sh
   sudo vi /data/opt/pistar-extras/tunnel.token   # paste the token, save
   sudo chmod 600 /data/opt/pistar-extras/tunnel.token
   ```
   The watchdog starts `cloudflared` within a minute.

### Port 80 mode

If something must reach the extras on port 80 (for example an existing tunnel route to
`http://localhost:80` shared with a classic Pi-Star card), set in `extras.conf`:

```sh
LISTEN=":80"
TAKE_PORT80="yes"
```

The dashboard's HTTP port then moves to 8080 (`listen_http` in `/etc/pistar-mcp/pistar-mcp.ini`);
HTTPS stays on 443.

## Update / uninstall

Run `install.sh` again from a newer copy to update. `sudo sh pistar/uninstall.sh` removes everything
and puts the dashboard back on port 80 (keeps `tunnel.token`; use `--all` to delete it too).

Log: `/var/log/pistar-extras.log` (RAM).

## Caveats

- The dashboard WebSocket is not a documented API. If a Pi-Star OS update changes its messages,
  the endpoints may stop updating until this project is adjusted.
- The persisted root crontab hides changes a future Pi-Star OS image may make to the default
  `/etc/crontabs/root` (today it only has Alpine's default `run-parts` lines).
- DMR only for now (it is what pistar-cyd-panel shows).
- Unofficial: not affiliated with Pi-Star or MW0MWZ.

## Build

```sh
cd src
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine \
  sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -trimpath -ldflags="-s -w" -o ../dist/pistar-extras-linux-armv6 .'
```

One ARMv6 binary runs on every Raspberry Pi model supported by Pi-Star OS (the image is armhf).

## License

MIT. Country flags: [flagcdn.com](https://flagcdn.com) / [flag-icons](https://github.com/lipis/flag-icons).
