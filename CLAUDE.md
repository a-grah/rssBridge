# rssBridge — Project Instructions

## Deployment (Raspberry Pi)

| Key | Value |
|-----|-------|
| Host | `pi@rp4.lan` |
| App dir | `/opt/rssbridge/` |
| Binary | `/opt/rssbridge/rssBridge` |
| Templates | `/opt/rssbridge/templates/` |
| Static files | `/opt/rssbridge/static/` |
| DB | `/var/lib/rssbridge/rssbridge.db` |
| Service | `rssbridge` (systemd) |
| Port | `7171` (direct, no nginx proxy) |

### Cross-compile
```bash
GOOS=linux GOARCH=arm64 go build -o rssBridge-linux-arm64 .
```

### Deploy binary
```bash
scp rssBridge-linux-arm64 pi@rp4.lan:/tmp/rssBridge
ssh pi@rp4.lan "sudo mv /tmp/rssBridge /opt/rssbridge/rssBridge && sudo systemctl restart rssbridge"
```

### Deploy templates
```bash
scp templates/base.html pi@rp4.lan:/tmp/base.html
ssh pi@rp4.lan "sudo cp /tmp/base.html /opt/rssbridge/templates/base.html"
```

### Deploy static files
```bash
rsync -av static/ pi@rp4.lan:/tmp/static/
ssh pi@rp4.lan "sudo cp -r /tmp/static/. /opt/rssbridge/static/"
```

### Check service
```bash
ssh pi@rp4.lan "sudo systemctl status rssbridge --no-pager"
ssh pi@rp4.lan "sudo journalctl -u rssbridge -n 30 --no-pager"
```

## Notes
- `/opt/rssbridge/` is owned by root — always use `/tmp` staging + `sudo mv/cp`
- Static files are served by the Go binary at `/static/`; `staticDir` is derived as a sibling of `templateDir`
