# syntax=docker/dockerfile:1.7

# 前端: Next.js 静态导出, 产物是纯静态文件, 由后端托管
FROM node:22-alpine AS web-builder
WORKDIR /build/web
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY web/ ./
RUN npm run build

# 后端: 纯 Go 编译 (SQLite 驱动是纯 Go 实现, 不需要 CGO)
FROM golang:1.25-alpine AS server-builder
WORKDIR /build/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o /out/splitdns .

FROM alpine:3.21
# tzdata 必须装: 应用入口显式加载业务时区, 加载失败会直接退出
RUN apk add --no-cache tzdata ca-certificates
ENV TZ=Asia/Shanghai

WORKDIR /app
COPY --from=server-builder /out/splitdns /app/splitdns
COPY --from=web-builder /build/web/out /app/web/out

ENV STATIC_ROOT=/app/web/out \
    DATABASE_PATH=/data/splitdns.db \
    BIND_HOST=0.0.0.0 \
    PORT=8080

EXPOSE 8080
VOLUME ["/data"]
CMD ["/app/splitdns"]
