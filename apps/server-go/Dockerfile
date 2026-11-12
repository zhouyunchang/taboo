# syntax=docker/dockerfile:1
# taboo（禁制）单镜像：前端构建 + Go 后端编译 + 精简运行时
# 构建（仓库根目录为上下文）：
#   docker build -f apps/server-go/Dockerfile -t taboo .
# 运行：
#   docker run -d --name taboo -p 7100:7100 \
#     -v taboo-data:/data -e TABOO_JWT_SECRET=$(openssl rand -hex 32) taboo

# ---- 阶段 1：构建 React 前端 ----
FROM node:22-alpine AS web
WORKDIR /repo
COPY package.json package-lock.json ./
COPY apps/web/package.json apps/web/
RUN npm ci -w @taboo/web --no-audit --no-fund
COPY apps/web/ apps/web/
RUN npm run build -w @taboo/web

# ---- 阶段 2：编译 Go 后端（前端产物经 embed.FS 打进单二进制） ----
FROM golang:1.27-alpine AS server
WORKDIR /repo
# 先只拷依赖清单，充分利用层缓存
COPY apps/server-go/go.mod apps/server-go/go.sum apps/server-go/
RUN cd apps/server-go && go mod download
COPY apps/server-go/ apps/server-go/
# 前端产物 + API 契约拷入 embed 目录（等价 `just web`）
COPY --from=web /repo/apps/web/dist/ apps/server-go/cmd/server/web/
COPY api/openapi.yaml apps/server-go/cmd/server/web/
# 版本信息经 -X 注入（deploy/dev122/deploy.sh 传入；本地 docker build 缺省为 dev/unknown）
ARG TABOO_VERSION=dev
ARG TABOO_COMMIT=unknown
ARG BUILD_TIME=
RUN cd apps/server-go && BI=github.com/zhouyunchang/taboo/apps/server-go/internal/buildinfo \
    && LDF="-s -w -X $BI.version=${TABOO_VERSION} -X $BI.commit=${TABOO_COMMIT} -X $BI.buildTime=${BUILD_TIME}" \
    && CGO_ENABLED=0 go build -trimpath -ldflags="$LDF" -o /out/taboo-server ./cmd/server

# ---- 阶段 3：运行时 ----
FROM alpine:3.21
ARG TABOO_VERSION=dev
ARG TABOO_COMMIT=unknown
ARG BUILD_TIME=
LABEL org.opencontainers.image.title="taboo" \
      org.opencontainers.image.version="${TABOO_VERSION}" \
      org.opencontainers.image.revision="${TABOO_COMMIT}" \
      org.opencontainers.image.created="${BUILD_TIME}" \
      org.opencontainers.image.source="https://github.com/zhouyunchang/taboo"
RUN apk add --no-cache curl \
    && adduser -D -u 10001 taboo \
    && mkdir -p /data && chown taboo:taboo /data
COPY --from=server /out/taboo-server /usr/local/bin/
ENV TABOO_PORT=7100 \
    TABOO_DATA_DIR=/data \
    TABOO_CORS_ORIGIN=*
VOLUME ["/data"]
EXPOSE 7100
USER taboo
HEALTHCHECK --interval=30s --timeout=5s --start-period=5s \
  CMD curl -fsS http://127.0.0.1:7100/api/v1/readyz >/dev/null || exit 1
ENTRYPOINT ["taboo-server"]
