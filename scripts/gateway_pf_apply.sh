#!/bin/bash
#
# gateway_pf_apply.sh
# FAST apply of pf "transport" rules (safe for frequent recovery calls).
# It ONLY rewrites the xrouter_transport anchor file and reloads pf.
#
# Args:
#   $1 = TRANSPORT_IF (utunX)      [required]
#   $2 = WAN_IF (e.g. en7)         [optional; default en7]
#   $3 = LAN_IF (e.g. en5)         [optional; default en5]
#   $4 = TRANSPORT_SERVER_IPS CSV  [required; e.g. "1.2.3.4,5.6.7.8"]
#   $5 = WAN_DNS_IPS CSV           [optional; e.g. "1.1.1.1,8.8.8.8"]
#   $6 = ALLOW_WAN_NTP             [optional; "true" or "false"]

set -e

echo "=== PF APPLY (fast) ==="

# Next Step: Config-driven shell execution / Remove hardcoded interface roles from shell scripts
# TEMPORARY:
# Interface values are duplicated with config.yaml.
# Next version: xrouter must pass WAN_IF/LAN_IF/TUN_IF from config.yaml.

#-TRANSPORT_IF="$1"
#-WAN_IF="${2:-en7}"
#-LAN_IF="${3:-en5}"
#-TRANSPORT_SERVER_IPS_CSV="$4"
#-WAN_DNS_IPS_CSV="${5:-}"
#-ALLOW_WAN_NTP="${6:-false}"

 # xrouter may pass either positional values:
 #   utun66 en7 en5 "XX.XX.XX.121" "1.1.1.1,8.8.8.8" true
 # or key=value:
 #   utun=utun66 wan=en7 lan=en5 transport_server_ips=... wan_dns=... allow_ntp=true
 strip_kv() {
   case "${1:-}" in
     *=*) echo "${1#*=}" ;;
     *)   echo "${1:-}" ;;
   esac
 }

 TRANSPORT_IF="$(strip_kv "${1:-}")"
 WAN_IF="$(strip_kv "${2:-en7}")"
 LAN_IF="$(strip_kv "${3:-en5}")"
 TRANSPORT_SERVER_IPS_CSV="$(strip_kv "${4:-}")"
 WAN_DNS_IPS_CSV="$(strip_kv "${5:-}")"
 ALLOW_WAN_NTP="$(strip_kv "${6:-false}")"

LAN_CIDR="192.168.50.0/24"
LAN_IP="192.168.50.1"

PF_ANCHOR_TRANSPORT="/etc/pf.anchors/xrouter_transport"

if [ -z "$TRANSPORT_IF" ]; then
  echo "ERROR: Transport interface not provided (expected utunX)"
  exit 1
fi

# Wait briefly for utun to appear (transport can create it asynchronously).
deadline=$((SECONDS+5))
while ! ifconfig "$TRANSPORT_IF" >/dev/null 2>&1; do
  if (( SECONDS >= deadline )); then
    echo "ERROR: Provided transport interface utun=$TRANSPORT_IF does not exist" >&2
    exit 1
  fi
  sleep 0.1
done

if [ -z "$TRANSPORT_SERVER_IPS_CSV" ]; then
  echo "ERROR: Transport server IP list is empty (pass CSV in arg #4)"
  exit 1
fi

echo "Transport: $TRANSPORT_IF  WAN: $WAN_IF  LAN: $LAN_IF"
echo "Transport servers (WAN allowlist): $TRANSPORT_SERVER_IPS_CSV"

# Convert CSV -> pf table list: "1.2.3.4,5.6.7.8" -> "1.2.3.4 5.6.7.8"
# pf tables want space-separated entries inside { ... } (commas break parsing)
TRANSPORT_SERVER_IPS_PF="$(printf "%s" "$TRANSPORT_SERVER_IPS_CSV" | tr ', ' ' ' | xargs)"

WAN_DNS_IPS_PF=""
if [ -n "$WAN_DNS_IPS_CSV" ]; then
  echo "WAN DNS allowed: $WAN_DNS_IPS_CSV"
  # "1.1.1.1,8.8.8.8" -> "1.1.1.1 8.8.8.8"
  WAN_DNS_IPS_PF="$(printf "%s" "$WAN_DNS_IPS_CSV" | tr ', ' ' ' | xargs)"
fi

echo "Writing dynamic anchor: $PF_ANCHOR_TRANSPORT ..."

sudo tee "$PF_ANCHOR_TRANSPORT" >/dev/null <<EOF
# xrouter_transport (DYNAMIC)
# Rewritten by gateway_pf_apply.sh
# Transport: $TRANSPORT_IF
# WAN: $WAN_IF
# LAN: $LAN_IF

# Tables (WAN allowlist)
table <xrouter_transport_servers> persist { $TRANSPORT_SERVER_IPS_PF }
EOF

# Optional WAN DNS table
if [ -n "$WAN_DNS_IPS_PF" ]; then
  sudo tee -a "$PF_ANCHOR_TRANSPORT" >/dev/null <<EOF
table <xrouter_wan_dns> persist { $WAN_DNS_IPS_PF }
EOF
fi

# Rules block (append)
sudo tee -a "$PF_ANCHOR_TRANSPORT" >/dev/null <<EOF

# --- NAT ---
# NAT LAN -> Transport tunnel
nat on $TRANSPORT_IF from $LAN_CIDR to any -> ($TRANSPORT_IF)

# --- LAN -> Transport allowed ---
pass in  quick on $LAN_IF inet from $LAN_CIDR to any keep state
pass out quick on $TRANSPORT_IF inet from $LAN_CIDR to any keep state

# --- This Mac: allow WAN ONLY to transport servers (to maintain the tunnel) ---
pass out quick on $WAN_IF inet from ($WAN_IF) to <xrouter_transport_servers> keep state

EOF

# Optional: allow WAN DNS (helpful before tunnel comes up)
if [ -n "$WAN_DNS_IPS_PF" ]; then
  sudo tee -a "$PF_ANCHOR_TRANSPORT" >/dev/null <<EOF
pass out quick on $WAN_IF inet proto { udp, tcp } from ($WAN_IF) to <xrouter_wan_dns> port 53 keep state
EOF
fi

# Optional: allow WAN NTP
if [ "$ALLOW_WAN_NTP" = "true" ]; then
  echo "WAN NTP allowed: true"
  sudo tee -a "$PF_ANCHOR_TRANSPORT" >/dev/null <<EOF
pass out quick on $WAN_IF inet proto udp from ($WAN_IF) to any port 123 keep state
EOF
else
  echo "WAN NTP allowed: false"
fi

echo "Reloading pf..."
sudo pfctl -f /etc/pf.conf

echo
echo "=== PF APPLY complete ==="
echo "Useful checks:"
echo "  sudo pfctl -s nat"
echo "  sudo pfctl -s rules | head -n 60"

