# Despliegue en el Moto G20 (fase 7)

El scaffold ya corre manualmente. Este documento deja el camino para
instalarlo como servicio cuando el MVP tenga los endpoints principales.

## 1. Usuario y directorios

```bash
useradd --system --home /var/lib/agentbank --shell /usr/sbin/nologin agentbank
install -d -o agentbank -g agentbank -m 0750 /var/lib/agentbank
install -d -o root -g root -m 0750 /etc/agentbank
```

## 2. Binario

```bash
cd /root/agentbank-go
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/agentbank ./cmd/agentbank
install -o root -g root -m 0755 bin/agentbank /usr/local/bin/agentbank
```

## 3. Configuración

```bash
install -o root -g agentbank -m 0640 \
  deploy/agentbank.env.example /etc/agentbank/env
# editar /etc/agentbank/env si hace falta
```

## 4. Servicio

```bash
install -o root -g root -m 0644 \
  deploy/agentbank.service /etc/systemd/system/agentbank.service
systemctl daemon-reload
systemctl enable --now agentbank
systemctl status agentbank --no-pager
```

Verificar:

```bash
curl -s localhost:8090/healthz
curl -s localhost:8090/v1/meta
journalctl -u agentbank -n 30 --no-pager
```

## 5. Límites

- `MemoryHigh=96M`, `MemoryMax=128M`: el scaffold usa ~6 MB; hay margen.
- `Nice=10`: no compite con OpenCode.
- `CPUQuota` no se usa porque este kernel no expone el controlador `cpu` en
  cgroup v2.
- `ProtectSystem=strict` + `ReadWritePaths=/var/lib/agentbank`: el binario no
  escribe fuera de su estado.

## 6. Backup

Con el servicio detenido (o usando una futura orden `agentbank backup`):

```bash
systemctl stop agentbank
cp -a /var/lib/agentbank/agentbank.db /var/backups/agentbank-$(date +%F).db
systemctl start agentbank
```

Cuando el MVP tenga `VACUUM INTO`, el backup se puede hacer en caliente.

## 7. Exposición

- Default: `127.0.0.1:8090`, solo local.
- Para la PC/ZeroTier: `AGENTBANK_ADDR=10.27.103.203:8090` y firewall/SSH
  restringido.
- No exponer `0.0.0.0` a Internet sin TLS y autenticación delante.
