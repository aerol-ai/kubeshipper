# ---- deps stage: install production dependencies ----
FROM oven/bun:1-alpine AS deps

WORKDIR /app

# Copy only the files needed to resolve deps — leverage layer cache
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile --production

# ---- runtime image ----
FROM oven/bun:1-alpine

WORKDIR /app

# Copy installed modules from deps stage
COPY --from=deps /app/node_modules ./node_modules

# Copy application source and TS config
COPY src/ ./src/
COPY tsconfig.json ./

# SQLite database lives on a mounted PersistentVolume in Kubernetes.
# The directory is pre-created here so local runs also work without a volume.
RUN mkdir -p /data

EXPOSE 3000

ENV PORT=3000 \
    DB_PATH=/data/kubeshipper.sqlite

# Run as the unprivileged bun user (uid 1000)
USER bun

CMD ["bun", "run", "src/index.ts"]
