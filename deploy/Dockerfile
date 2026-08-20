# Two stages: build the Svelte bundle, then compile it into the Go binary.
#
# The result is one static file with no runtime dependencies, which is what
# lets Mimir ship as a Yggdrasil rune: the container has no node, no CGO, and
# nothing to mount beyond a data directory.

FROM node:22-alpine AS web
WORKDIR /src
COPY web/package.json web/package-lock.json* ./
RUN npm ci --no-audit --no-fund || npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/dist ./web/dist
# CGO off keeps the pure-Go SQLite driver honest and the binaries static.
# The miner ships alongside the server so the nightly game data sync runs in
# the same container — but as its own binary, so a datamine that has moved or
# desynced cannot take the server down with it.
# The version is stamped in, not inferred. An unstamped build reports "dev",
# and the updater refuses to replace one — throwing away a binary somebody
# built on purpose is not an upgrade.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/mimir ./cmd/mimir && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mimir-mine ./cmd/mimir-mine

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 1000 mimir
COPY --from=build /out/mimir /usr/local/bin/mimir
COPY --from=build /out/mimir-mine /usr/local/bin/mimir-mine
COPY deploy/supplements.json /etc/mimir/supplements.json
USER mimir
WORKDIR /data
ENV MIMIR_DATA_DIR=/data \
    MIMIR_ADDR=:8080 \
    MIMIR_DEPLOY=container
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8080/api/healthz || exit 1
ENTRYPOINT ["mimir"]
CMD ["serve"]
