#!/usr/bin/env bash
# Base hardening for the OpsFlow VPS (Arch Linux).
# Idempotent: safe to re-run. Run as root on the server.
#
#   ADMIN_USER=mehexi bash 01-harden.sh
set -euo pipefail

ADMIN_USER="${ADMIN_USER:-mehexi}"
TIMEZONE="${TIMEZONE:-Asia/Dhaka}"   # matches the app's org_timezone
NEW_HOSTNAME="${NEW_HOSTNAME:-opsflow}"
SSH_PORT="${SSH_PORT:-22}"

log()  { printf '\n\033[1;34m==> %s\033[0m\n' "$*"; }
skip() { printf '    \033[2m(already done: %s)\033[0m\n' "$*"; }

[[ $EUID -eq 0 ]] || { echo "run as root" >&2; exit 1; }

# ---------------------------------------------------------------- 1. identity
log "Timezone, hostname, clock"
timedatectl set-timezone "$TIMEZONE"
timedatectl set-ntp true
hostnamectl set-hostname "$NEW_HOSTNAME"
grep -q "$NEW_HOSTNAME" /etc/hosts || printf '127.0.0.1 %s\n' "$NEW_HOSTNAME" >> /etc/hosts

# ------------------------------------------------------------- 2. the packages
# Arch is a rolling release: this is a FULL upgrade, never a partial one
# (partial upgrades break glibc-linked binaries). A kernel bump needs a reboot.
log "System upgrade + packages"
pacman -Syu --noconfirm --needed \
    docker docker-compose docker-buildx \
    ufw fail2ban \
    git rsync jq bash-completion

# ------------------------------------------------------------ 3. admin account
log "Admin user: $ADMIN_USER"
if id -u "$ADMIN_USER" >/dev/null 2>&1; then
    skip "user exists"
else
    useradd --create-home --shell /bin/bash "$ADMIN_USER"
    passwd --lock "$ADMIN_USER"          # key-only; no password to brute force
fi
usermod -aG wheel,docker "$ADMIN_USER"

# Carry root's authorised key over so the admin can log in before root is locked down.
install -d -m 700 -o "$ADMIN_USER" -g "$ADMIN_USER" "/home/$ADMIN_USER/.ssh"
if [[ -s /root/.ssh/authorized_keys ]]; then
    touch "/home/$ADMIN_USER/.ssh/authorized_keys"
    while read -r key; do
        [[ -n "$key" ]] || continue
        grep -qxF "$key" "/home/$ADMIN_USER/.ssh/authorized_keys" \
            || printf '%s\n' "$key" >> "/home/$ADMIN_USER/.ssh/authorized_keys"
    done < /root/.ssh/authorized_keys
fi
chown "$ADMIN_USER:$ADMIN_USER" "/home/$ADMIN_USER/.ssh/authorized_keys"
chmod 600 "/home/$ADMIN_USER/.ssh/authorized_keys"

# wheel gets sudo (Arch ships this commented out in /etc/sudoers).
# NOPASSWD because the account is key-only with a locked password: there is no
# password to type. It grants nothing that the same SSH key doesn't already get
# through PermitRootLogin prohibit-password.
printf '%%wheel ALL=(ALL:ALL) NOPASSWD: ALL\n' > /etc/sudoers.d/10-wheel
chmod 440 /etc/sudoers.d/10-wheel
visudo -c -q

# --------------------------------------------------------------- 4. ssh daemon
# Drop-in numbered 10- on purpose: sshd takes the FIRST value it sees for each
# keyword, and cloud-init's 50-cloud-init.conf re-enables PasswordAuthentication.
log "SSH: keys only, no password login"
cat > /etc/ssh/sshd_config.d/10-hardening.conf <<EOF
Port $SSH_PORT
PasswordAuthentication no
PermitEmptyPasswords no
KbdInteractiveAuthentication no
# Key-based root stays as a rescue path; password root login is impossible.
PermitRootLogin prohibit-password
PubkeyAuthentication yes
MaxAuthTries 3
MaxSessions 10
LoginGraceTime 30
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding yes
ClientAliveInterval 300
ClientAliveCountMax 2
AllowUsers $ADMIN_USER root
EOF
sshd -t   # refuse to reload a config that would lock us out
systemctl reload sshd
printf '    sshd reloaded; effective settings:\n'
sshd -T | grep -Ei '^(port|passwordauthentication|permitrootlogin|maxauthtries|allowusers)' | sed 's/^/      /'

# ----------------------------------------------------------------- 5. firewall
# NOTE: Docker writes its own iptables rules and published container ports
# BYPASS ufw. The stack therefore publishes nothing but Caddy's 80/443 —
# Postgres, Redis and MinIO are reachable only on the compose network.
log "Firewall (ufw): deny inbound except SSH, HTTP, HTTPS"
ufw --force reset >/dev/null
ufw default deny incoming
ufw default allow outgoing
ufw allow "$SSH_PORT"/tcp comment 'ssh'
ufw allow 80/tcp  comment 'http (ACME + redirect)'
ufw allow 443/tcp comment 'https'
ufw allow 443/udp comment 'https (http3)'
ufw --force enable
systemctl enable --now ufw
ufw status verbose | sed 's/^/      /'

# ----------------------------------------------------------------- 6. fail2ban
log "fail2ban on sshd"
cat > /etc/fail2ban/jail.d/00-sshd.local <<EOF
[DEFAULT]
backend   = systemd
banaction = ufw
bantime   = 1h
findtime  = 10m
maxretry  = 5

[sshd]
enabled = true
port    = $SSH_PORT
EOF
systemctl enable --now fail2ban
systemctl restart fail2ban

# ------------------------------------------------------------------ 7. journal
log "Cap the journal at 500M"
install -d /etc/systemd/journald.conf.d
printf '[Journal]\nSystemMaxUse=500M\nSystemMaxFileSize=50M\n' \
    > /etc/systemd/journald.conf.d/00-size.conf
systemctl restart systemd-journald

# ------------------------------------------------------------------- 8. docker
log "Docker"
install -d /etc/docker
cat > /etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": { "max-size": "20m", "max-file": "5" },
  "live-restore": true
}
EOF
systemctl enable --now docker
docker version --format '      client {{.Client.Version}} / server {{.Server.Version}}'
docker info --format '      storage driver: {{.Driver}}'

log "Done."
echo "    Reboot needed? $( [[ -n $(find /boot -newer /proc/1 -name 'vmlinuz*' 2>/dev/null) ]] && echo 'YES (kernel updated)' || echo 'no' )"
