FROM alpine:3.21 AS downloader

RUN apk add --no-cache ca-certificates curl jq tar

ARG TARGETOS
ARG TARGETARCH
ARG SURGE_VERSION

RUN if [ "${TARGETOS:-linux}" != "linux" ]; then \
      echo "Unsupported target OS: ${TARGETOS}" >&2; \
      exit 1; \
    fi && \
    case "${TARGETARCH}" in \
      amd64) SURGE_ARCH="amd64" ;; \
      arm64) SURGE_ARCH="arm64" ;; \
      arm) SURGE_ARCH="arm" ;; \
      *) echo "Unsupported target architecture: ${TARGETARCH}" >&2; exit 1 ;; \
    esac && \
    if [ -z "${SURGE_VERSION}" ]; then \
      echo "Fetching latest surge release..."; \
      SURGE_VERSION=$(curl -fsSL https://api.github.com/repos/SurgeDM/Surge/releases/latest | jq -r .tag_name | sed 's/^v//'); \
    else \
      echo "Using specified surge version: ${SURGE_VERSION}"; \
    fi && \
    echo "Downloading surge v${SURGE_VERSION} for ${TARGETOS:-linux}/${SURGE_ARCH}" && \
    curl -fsSL -o /tmp/surge.tar.gz \
      "https://github.com/SurgeDM/Surge/releases/download/v${SURGE_VERSION}/Surge_${SURGE_VERSION}_linux_${SURGE_ARCH}.tar.gz" && \
    mkdir -p /tmp/surge-extract /out && \
    tar -xzf /tmp/surge.tar.gz -C /tmp/surge-extract && \
    install -m 0755 /tmp/surge-extract/surge /out/surge

FROM alpine:3.21

ARG SURGE_UID=1000
ARG SURGE_GID=1000

RUN apk add --no-cache ca-certificates && \
    addgroup -g ${SURGE_GID} -S surge && \
    adduser -u ${SURGE_UID} -S -D -h /home/surge -G surge surge && \
    mkdir -p /downloads /var/lib/surge /home/surge && \
    chown -R surge:surge /downloads /var/lib/surge /home/surge

COPY --from=downloader /out/surge /usr/local/bin/surge

ENV HOME=/home/surge \
    XDG_CONFIG_HOME=/var/lib \
    XDG_STATE_HOME=/var/lib

WORKDIR /downloads

VOLUME ["/downloads", "/var/lib/surge"]

LABEL org.opencontainers.image.source="https://github.com/SurgeDM/Surge"
LABEL org.opencontainers.image.description="Surge download manager"
LABEL org.opencontainers.image.licenses="MIT"

USER surge

EXPOSE 1700

HEALTHCHECK --interval=30s --timeout=10s --start-period=5s --retries=3 \
    CMD surge server status || exit 1

CMD ["surge", "server", "start"]
