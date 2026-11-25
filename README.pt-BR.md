# pistar-os-extras

🇬🇧 [English](README.md) · 🇧🇷 Português

Extras para o [**Pi-Star OS**](https://github.com/MW0MWZ/Pi-Star_OS) (MW0MWZ, baseado em Alpine, painel Pi-Star MCP).

O Pi-Star OS não grava o log do MMDVMHost em disco e não tem Python nem nginx. Por isso, ferramentas
feitas para o Pi-Star clássico (que liam `/var/log/pi-star/MMDVM-*.log` ou o HTML do painel) param de
funcionar. O `pistar-os-extras` roda no hotspot e volta a servir os endereços clássicos, nos formatos clássicos:

| Endereço | Quem usa |
|---|---|
| `/cyd/status.json` | display CYD do [pistar-cyd-panel](https://github.com/danielregis81/pistar-cyd-panel), apps |
| `/cyd/flags/<cc>.565` | pistar-cyd-panel (bandeiras RGB565) |
| `/mmdvmhost/lh.php` | scripts que liam a tabela "Last Heard" clássica |
| `/images/flags/<cc>.svg` | bandeiras SVG (flagcdn.com, com cache) |
| `/extras/health` | estado da ponte |

Opcionalmente, também roda o seu próprio **Cloudflare Tunnel** no hotspot, para acessar o painel e os
endereços acima de qualquer lugar. Útil quando o hotspot viaja com você.

Tudo fica em `/data`, então **sobrevive às atualizações A/B** do Pi-Star OS.

## Como funciona

- O `pistar-extras` (Go, binário estático único, ~6 MB, ~10 MB de RAM num Pi Zero W) se conecta ao
  WebSocket do próprio painel (`wss://127.0.0.1/ws`), guarda a atividade recente e serve os endereços.
- O que chega pelo túnel e não é um desses endereços vai para o painel (HTTPS na 443, WebSocket
  incluído). Pela rede local, redireciona para `https://<hotspot>/`, como o próprio painel faz.
- O `watchdog.sh` roda a cada minuto pelo crontab do root, guardado em `/data/etc/crontabs`
  (o Pi-Star OS monta cada `/data/etc/<nome>/` sobre `/etc/<nome>/` no boot).

## Instalar

No hotspot (SSH como `pi-star`):

```sh
cd /tmp
wget -O extras.tar.gz https://github.com/danielregis81/pistar-os-extras/archive/refs/heads/main.tar.gz
tar xzf extras.tar.gz && cd pistar-os-extras-main
sudo sh pistar/install.sh            # só os extras
# ou
sudo sh pistar/install.sh --tunnel   # também baixa o cloudflared oficial (linux-arm)
```

Os extras escutam na **porta 8081** por padrão: `http://<hotspot>:8081/cyd/status.json`.
Configuração em `/data/opt/pistar-extras/extras.conf`.

### Cloudflare Tunnel (opcional)

1. No painel da Cloudflare, crie um túnel (Networking → Tunnels) e um hostname público apontando
   para `http://localhost:8081`.
2. Copie o token do túnel (o trecho `eyJ…` do comando de instalação; não rode o comando).
3. No hotspot:
   ```sh
   sudo vi /data/opt/pistar-extras/tunnel.token   # cole o token e salve
   sudo chmod 600 /data/opt/pistar-extras/tunnel.token
   ```
   Em até um minuto o watchdog sobe o `cloudflared`.

### Modo porta 80

Se algo precisa alcançar os extras na porta 80 (por exemplo, uma rota de túnel para
`http://localhost:80` compartilhada com um cartão do Pi-Star clássico), coloque no `extras.conf`:

```sh
LISTEN=":80"
TAKE_PORT80="yes"
```

A porta HTTP do painel passa para 8080 (`listen_http` em `/etc/pistar-mcp/pistar-mcp.ini`);
o HTTPS continua na 443.

## Atualizar / remover

Rode o `install.sh` de uma cópia mais nova para atualizar. `sudo sh pistar/uninstall.sh` remove tudo
e devolve o painel para a porta 80 (mantém o `tunnel.token`; use `--all` para apagar também).

Log: `/var/log/pistar-extras.log` (RAM).

## Limitações

- O WebSocket do painel não é uma API documentada. Se uma atualização do Pi-Star OS mudar as
  mensagens, os endereços podem parar de atualizar até este projeto ser ajustado.
- O crontab do root guardado em `/data` esconde mudanças que uma imagem futura do Pi-Star OS faça no
  `/etc/crontabs/root` padrão (hoje ele só tem as linhas `run-parts` do Alpine).
- Por enquanto só DMR (é o que o pistar-cyd-panel mostra).
- Não oficial: sem vínculo com o Pi-Star ou o MW0MWZ.

## Compilar

```sh
cd src
docker run --rm -v "$PWD":/src -w /src golang:1.23-alpine \
  sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -trimpath -ldflags="-s -w" -o ../dist/pistar-extras-linux-armv6 .'
```

Um binário ARMv6 roda em todos os Raspberry Pi suportados pelo Pi-Star OS (a imagem é armhf).

## Licença

MIT. Bandeiras: [flagcdn.com](https://flagcdn.com) / [flag-icons](https://github.com/lipis/flag-icons).
