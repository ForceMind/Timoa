# syntax=docker/dockerfile:1

# 阶段 1：构建前端
FROM node:24-alpine AS web
WORKDIR /app/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# 阶段 2：构建后端（嵌入前端产物）
FROM golang:1.27-alpine AS go
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /app/web/dist ./internal/webdist/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/xiaozhang ./cmd/xiaozhang

# 阶段 3：运行（非 root，单容器含全部依赖）
FROM alpine:3.21
RUN adduser -D -u 10001 xiaozhang && mkdir -p /data && chown xiaozhang:xiaozhang /data
COPY --from=go /out/xiaozhang /usr/local/bin/xiaozhang
USER xiaozhang
VOLUME ["/data"]
EXPOSE 8787
ENV XIAOZHANG_ADDR=0.0.0.0:8787 \
    XIAOZHANG_DATA_DIR=/data
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s \
  CMD wget -qO- http://127.0.0.1:8787/healthz || exit 1
ENTRYPOINT ["xiaozhang", "serve"]
