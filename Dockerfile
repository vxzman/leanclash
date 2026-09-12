FROM debian:bookworm-slim

LABEL maintainer="leanclash"
LABEL description="Mihomo manager container without systemd"

RUN apt-get update && apt-get install -y --no-install-recommends \
        nftables \
        iproute2 \
        ca-certificates \
        procps \
        bash \
        curl \
    && rm -rf /var/lib/apt/lists/*

COPY build/mihomo /usr/bin/mihomo
COPY build/leanclash-container /usr/local/bin/leanclash
COPY container/defaults/config_general.yaml /usr/share/mihomo/config_general.yaml
COPY container/scripts/entrypoint.sh /entrypoint.sh
COPY container/scripts/systemctl /usr/local/bin/systemctl

RUN chmod 0755 /usr/bin/mihomo /usr/local/bin/leanclash /usr/local/bin/systemctl /entrypoint.sh \
    && mkdir -p /opt/leanclash /etc/mihomo /var/lib/mihomo /run/leanclash

VOLUME ["/etc/mihomo", "/var/lib/mihomo", "/opt/leanclash"]

EXPOSE 8081 7890 1053

HEALTHCHECK --interval=30s --timeout=5s --retries=3 \
    CMD curl -sf http://127.0.0.1:8081/api/status || exit 1

ENTRYPOINT ["/entrypoint.sh"]
