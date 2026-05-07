# syntax=docker/dockerfile:1.7

# ---- build stage ---------------------------------------------------------
FROM golang:1.25-alpine AS build
WORKDIR /src

ENV CGO_ENABLED=0 \
    GOFLAGS=-trimpath

# Cache module downloads first.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w" -o /out/porcelain ./cmd/porcelain

# ---- pki bootstrap stage -------------------------------------------------
# Generates a developer CA, server certificate, and client certificate so the
# image can serve mTLS without operator setup. The client artefacts are
# emitted into /pki/client so they can be copied out with `docker cp` and
# imported into the developer's browser.
FROM alpine:3.20 AS pki
RUN apk add --no-cache openssl
WORKDIR /pki
COPY docker/generate-dev-pki.sh /usr/local/bin/generate-dev-pki.sh
RUN chmod +x /usr/local/bin/generate-dev-pki.sh && /usr/local/bin/generate-dev-pki.sh

# ---- runtime stage -------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app

COPY --from=build  /out/porcelain   /app/porcelain
COPY --from=pki    /pki             /pki

ENV PORCELAIN_ADDRESS=":8443" \
    PORCELAIN_SOCKET_ACTIVATION="false" \
    PORCELAIN_AUTH_ENABLED="false" \
    PORCELAIN_DBUS_OPTIONAL="true" \
    PORCELAIN_SECRETS_BACKEND="memory" \
    PORCELAIN_CERTIFICATES_BACKEND="filesystem" \
    PORCELAIN_TLS_DIR="/pki/server" \
    PORCELAIN_TLS_COMMON_NAME="localhost" \
    PORCELAIN_TLS_DNS_NAMES="localhost,127.0.0.1"

EXPOSE 8443
USER nonroot:nonroot
ENTRYPOINT ["/app/porcelain"]
