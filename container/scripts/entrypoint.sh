#!/bin/bash
set -euo pipefail

mkdir -p /opt/leanclash /etc/mihomo /var/lib/mihomo /run/leanclash

if [ ! -f /etc/mihomo/config_general.yaml ]; then
    cp /usr/share/mihomo/config_general.yaml /etc/mihomo/config_general.yaml
fi

if [ "$#" -gt 0 ]; then
    exec "$@"
fi

exec /usr/local/bin/leanclash serve
