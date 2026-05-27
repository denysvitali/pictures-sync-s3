# Operational Runbook — pictures-sync-s3

Quick reference for diagnosing and recovering the photo-backup appliance.

---

## 1. `/perm` Partition Backup and Recovery

The `/perm` filesystem persists settings, card history, and rclone config across reboots.

### Backup (SSH/SCP)

```bash
# Gokrazy exposes SSH on port 22 if configured; default user is "gokrazy"
APPLIANCE_IP=192.168.1.42

# Dump the full /perm partition contents
ssh gokrazy@${APPLIANCE_IP} 'tar czf - /perm' > perm-backup-$(date +%Y%m%d).tar.gz

# Or copy specific files
scp gokrazy@${APPLIANCE_IP}:/perm/pictures-sync/settings.json ./settings-backup.json
scp gokrazy@${APPLIANCE_IP}:/perm/rclone/rclone.conf ./rclone-backup.conf
```

### Recovery after `/perm` corruption

Gokrazy formats `/perm` as ext4. If it becomes unreadable:

```bash
# 1. Identify the /perm partition (usually mmcblk0p4 on Raspberry Pi)
ssh gokrazy@${APPLIANCE_IP} 'lsblk'

# 2. Unmount and re-format (DATA LOSS — restore from backup after)
ssh gokrazy@${APPLIANCE_IP} 'umount /perm && mkfs.ext4 /dev/mmcblk0p4 && mount /perm'

# 3. Restore from backup
cat perm-backup-YYYYMMDD.tar.gz | ssh gokrazy@${APPLIANCE_IP} 'tar xzf - -C /'
```

Key `/perm` paths:

| Path | Contents |
|---|---|
| `/perm/pictures-sync/settings.json` | App settings (rclone remote, WiFi, etc.) |
| `/perm/pictures-sync/history.json` | Sync history per card |
| `/perm/pictures-sync/systeminfo.json` | Cached system stats |
| `/perm/rclone/rclone.conf` | rclone remote credentials |

---

## 2. OTA Update Failures

### Trigger an OTA update

```bash
gok -i photo-backup update
```

### Common failures

| Symptom | Likely cause | Fix |
|---|---|---|
| `connection refused` | Device unreachable | Check network/IP; ping appliance |
| `hash mismatch` | Partial download | Re-run `gok update`; check storage space |
| Device unresponsive after update | Bad boot partition | Gokrazy A/B boots — power-cycle to revert |
| `permission denied` | Wrong password in `gokrazy.json` | Re-run `gok -i photo-backup edit` |

### Manual recovery (failed boot)

Gokrazy uses A/B partitions. If the new image fails to boot, the device **automatically reverts** to the previous partition on next power cycle. No manual intervention needed for a single bad update.

If both partitions are corrupted, re-flash the SD card:

```bash
gok -i photo-backup overwrite --full /dev/sdX  # DESTRUCTIVE
```

---

## 3. Sync Failures

### Where to look

**In-app UI** — The WebUI history tab (`/`) shows per-card sync status and timestamps.

**Gokrazy logs** — Gokrazy streams logs via its HTTP interface:

```bash
# Stream live logs from the appliance web UI
curl http://${APPLIANCE_IP}:8080/log        # webui service
curl http://${APPLIANCE_IP}:8080/log?service=pictures-sync

# Or via the Gokrazy built-in port (default 8080 for the gokrazy mgmt interface)
curl http://${APPLIANCE_IP}:8080/status
```

**rclone exit codes** — Non-zero exits from rclone are captured in `pkg/syncmanager` and surfaced in the history JSON. Common codes:

| Code | Meaning |
|---|---|
| 1 | Generic rclone error (check logs) |
| 3 | Directory not found on remote |
| 7 | No files transferred (empty card) |

**Card ID issues** — If a card is not recognized, check for `.pictures-sync-id` on the card root. See `pkg/sdmonitor` for ID generation logic.

---

## 4. Health Endpoints

All endpoints are on the webui service (default port `8080`).

```bash
BASE=http://${APPLIANCE_IP}:8080

# Liveness — returns 200 when the process is alive
curl -s ${BASE}/healthz
# {"status":"ok"}

# Readiness — returns 200 when ready to serve requests
curl -s ${BASE}/readyz
# {"status":"ready"}

# Prometheus metrics (when wired)
curl -s ${BASE}/metrics | grep pictures_sync
# pictures_sync_cards_total 3
# pictures_sync_photos_uploaded_total 1247
# pictures_sync_last_sync_timestamp_seconds 1.716e+09
```

Authentication: if basic auth is configured, pass credentials:

```bash
curl -s -u admin:password ${BASE}/healthz
```

---

## 5. Quick Diagnostics Checklist

1. `curl ${BASE}/healthz` — process alive?
2. `curl ${BASE}/readyz` — service ready?
3. Check WebUI history tab for last sync result
4. Check `/perm/pictures-sync/history.json` for raw error messages
5. Review rclone config: `curl ${BASE}/api/settings` (auth required)
6. Verify network: SD card mounted read-only at `/mnt/sd`
7. Check available space: `df -h /perm`

---

*See also: [`pkg/syncmanager`](../pkg/syncmanager/), [`pkg/sdmonitor`](../pkg/sdmonitor/), [`pkg/state`](../pkg/state/)*
