# ---- helmd-build: compile the Go sidecar that wraps the Helm SDK ----
FROM golang:1.22-alpine AS helmd-build

RUN apk add --no-cache git protoc protobuf-dev

WORKDIR /src

# Fetch deps first for layer caching.
COPY helmd/go.mod helmd/go.sum* helmd/
RUN cd helmd && go mod download || true

# Bring in the rest of the Go source + proto.
COPY helmd/ helmd/

# Generate protobuf stubs into helmd/gen.
RUN go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.34.2 \
    && go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1
ENV PATH=$PATH:/root/go/bin
RUN mkdir -p helmd/gen \
    && protoc \
        --go_out=helmd/gen --go_opt=paths=source_relative \
        --go-grpc_out=helmd/gen --go-grpc_opt=paths=source_relative \
        -I helmd/proto helmd/proto/helmd.proto \
    && cd helmd && go mod tidy

RUN cd helmd && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/helmd .

# ---- ts-deps: install runtime node_modules ----
FROM oven/bun:1-alpine AS ts-deps
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# ---- runtime image ----
FROM oven/bun:1-alpine

# tini for sane signal forwarding (so SIGTERM gracefully stops both bun and helmd)
RUN apk add --no-cache tini

WORKDIR /app

COPY --from=ts-deps /app/node_modules ./node_modules
COPY --from=helmd-build /out/helmd /usr/local/bin/helmd

COPY src/ ./src/
COPY helmd/proto/ ./helmd/proto/
COPY tsconfig.json ./
COPY scripts/start.sh /start.sh
RUN chmod +x /start.sh

RUN mkdir -p /data && chown bun:bun /data

EXPOSE 3000

ENV PORT=3000 \
    DB_PATH=/data/kubeshipper.sqlite \
    HELMD_SOCKET=/tmp/helmd.sock \
    HELMD_PROTO=/app/helmd/proto/helmd.proto

USER bun

ENTRYPOINT ["/sbin/tini", "--"]
CMD ["/start.sh"]
