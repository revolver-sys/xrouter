#!/bin/bash
#
# gateway_down.sh
# Restore original pf / dnsmasq state and turn off router mode.
#

set -e

echo "=== xrouter DOWN (restore original config) ==="

# dnsmasq paths (Homebrew default)
DNSMASQ_MAIN="/usr/local/etc/dnsmasq.conf"
DNSMASQ_MAIN_BAK="/usr/local/etc/dnsmasq.conf.transportrouter.bak"
DNSMASQ_DIR="/usr/local/etc/dnsmasq.d"
DNSMASQ_LAN_CONF="$DNSMASQ_DIR/lan-dhcp.conf"

# pf backup
PF_BACKUP="/etc/pf.conf.transportrouter.bak"

#############################################
# 1) Restore original pf.conf if backup exists
if [ -f "$PF_BACKUP" ]; then
  echo "Restoring original /etc/pf.conf from $PF_BACKUP ..."
  sudo cp "$PF_BACKUP" /etc/pf.conf
  sudo pfctl -f /etc/pf.conf
else
  echo "WARNING: backup $PF_BACKUP not found."
  echo "Leaving current /etc/pf.conf in place."
fi

# Optionally disable pf entirely (uncomment if you want that)
# echo "Disabling pf ..."
# sudo pfctl -d

#############################################
# 2) Remove our LAN dnsmasq config
if [ -f "$DNSMASQ_LAN_CONF" ]; then
  echo "Removing dnsmasq LAN config $DNSMASQ_LAN_CONF ..."
  sudo rm -f "$DNSMASQ_LAN_CONF"
else
  echo "dnsmasq LAN config $DNSMASQ_LAN_CONF not found (nothing to remove)."
fi

#############################################
# 3) Restore original dnsmasq main config (if we backed it up)

if [ -f "$DNSMASQ_MAIN_BAK" ]; then
  if [ -f "$DNSMASQ_MAIN" ]; then
    echo "Restoring original dnsmasq main config from $DNSMASQ_MAIN_BAK ..."
    sudo cp "$DNSMASQ_MAIN_BAK" "$DNSMASQ_MAIN"
  else
    echo "dnsmasq main config missing; restoring from backup $DNSMASQ_MAIN_BAK ..."
    sudo cp "$DNSMASQ_MAIN_BAK" "$DNSMASQ_MAIN"
  fi
else
  echo "No dnsmasq main backup ($DNSMASQ_MAIN_BAK) found; leaving $DNSMASQ_MAIN as-is."
fi

#############################################
# 4) Restart dnsmasq to apply changes
if command -v dnsmasq >/dev/null 2>&1; then
  echo "Restarting dnsmasq ..."
  sudo brew services restart dnsmasq || echo "dnsmasq restart failed or not managed by brew."
else
  echo "dnsmasq not installed; nothing to restart."
fi

#############################################
# 5) Disable IPv4 forwarding (optional but clean)

echo "Disabling IPv4 forwarding ..."
sudo sysctl -w net.inet.ip.forwarding=0 >/dev/null 2>&1 || true

echo "=== xrouter mode is DOWN ==="
echo "pf and dnsmasq are restored (as far as backups allow),"
echo "and IPv4 forwarding has been turned off."

