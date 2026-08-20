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
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/mimir /usr/local/bin/mimir
COPY --from=build /out/mimir-mine /usr/local/bin/mimir-mine
# Both hand-maintained data files travel with the image, because a container
# has no repository to read them from. Without effects.json every conditional
# bonus is missing — Raiden reads 43,872 damage per rotation instead of
# 74,678 — and nothing announces it, because a missing effect looks exactly
# like an effect that does nothing.
COPY deploy/supplements.json /etc/mimir/supplements.json
COPY deploy/effects.json /etc/mimir/effects.json

# No USER, on purpose. Yggdrasil always sets the container's user to the
# account that owns the mounted data directory, so a USER baked in here is
# overridden anyway — and baking in uid 1000 only guarantees it is the wrong
# one. The image writes nothing outside /data and looks up no user, so it runs
# as whatever uid it is given.
WORKDIR /data
ENV MIMIR_DATA_DIR=/data \
    MIMIR_ADDR=:8080 \
    MIMIR_DEPLOY=container
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8080/api/healthz || exit 1
# No ENTRYPOINT: the command is fully specified by CMD instead.
#
# An entrypoint of ["mimir"] means anything a host passes as the container's
# command arrives as *arguments to mimir*. Yggdrasil builds its command as
# ["/bin/sh","-c",<startup.command>] whether or not keep_entrypoint is set, so
# the container ran `mimir /bin/sh -c serve`, printed its usage and exited —
# and the restart policy turned that into a boot loop.
#
# With the whole command in CMD, the image does the right thing under a
# cleared entrypoint, under a kept one, and under a plain `docker run`.
CMD ["mimir", "serve"]
