# ---- ui build ----
FROM oven/bun:1.2.15-alpine AS ui-build

WORKDIR /src

COPY web ./web

RUN cd web && bun install && bun run build

# ---- build ----
FROM golang:1.22-alpine AS build

RUN apk add --no-cache git ca-certificates

WORKDIR /src

COPY go.mod go.sum* ./
RUN go mod download || true

COPY . .
COPY --from=ui-build /src/internal/ui/static ./internal/ui/static

# Pure-Go SQLite (modernc.org/sqlite) means CGO_ENABLED=0 works fine.
RUN CGO_ENABLED=0 go mod tidy && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/kubeshipper .

# ---- runtime ----
FROM alpine:3.20

RUN apk add --no-cache tini ca-certificates && \
    addgroup -g 1000 ks && adduser -u 1000 -G ks -D ks

COPY --from=build /out/kubeshipper /usr/local/bin/kubeshipper

RUN mkdir -p /data && chown ks:ks /data

USER ks

ENV PORT=3000 \
    DB_PATH=/data/kubeshipper.sqlite

EXPOSE 3000

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/usr/local/bin/kubeshipper"]
