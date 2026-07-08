# Multi-stage build: compile bubblewrap, embed into Go binary, run sandbox tests.
#
# Usage:
#   docker compose up --build --abort-on-container-exit

# ---- Stage 1: Build bubblewrap ----
FROM golang:1.26 AS bwrap-builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    meson ninja-build libcap-dev pkg-config gcc \
    && rm -rf /var/lib/apt/lists/*

# Build bubblewrap from source (uses Meson since v0.9.0)
ARG BWRAP_VERSION=v0.11.0
RUN git clone --depth 1 --branch ${BWRAP_VERSION} \
    https://github.com/containers/bubblewrap.git /tmp/bubblewrap \
    && cd /tmp/bubblewrap \
    && meson setup _build \
       -Dselinux=disabled \
       -Dman=disabled \
       -Dtests=false \
       --default-library=static \
       --prefer-static \
    && meson compile -C _build \
    && cp _build/bwrap /tmp/bwrap

# ---- Stage 2: Run Go sandbox tests ----
FROM golang:1.26

# bwrap needs --cap-drop at runtime, install libcap2 (not libcap-dev)
RUN apt-get update && apt-get install -y --no-install-recommends \
    libcap2 \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

# Copy Go source first
COPY . .

# Overwrite placeholder with real bwrap binary
COPY --from=bwrap-builder /tmp/bwrap ./internal/service/sandbox/bwrap/bwrap_linux_arm64

CMD ["go", "test", "./internal/service/sandbox/", "-v", "-count=1", "-timeout=60s"]
