# 构建阶段
FROM golang:1.22-alpine AS builder

# 安装构建依赖
RUN apk add --no-cache git ca-certificates tzdata

# 设置工作目录
WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod tidy

# 复制源代码
COPY . .

# 构建参数
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w -X main.Version=${VERSION} -X main.BuildDate=${BUILD_DATE} -X main.GitCommit=${VCS_REF} -extldflags '-static'" -o pansou .

# 运行阶段
FROM alpine:3.19

# 添加运行时依赖
RUN apk add --no-cache ca-certificates tzdata

# 创建缓存目录
RUN mkdir -p /app/cache

# 从构建阶段复制可执行文件
COPY --from=builder /app/pansou /app/pansou

# 设置工作目录
WORKDIR /app

# 暴露端口
EXPOSE 8888

# 设置环境变量
ENV CACHE_PATH=/app/cache \
    CACHE_ENABLED=true \
    TZ=Asia/Shanghai \
    ASYNC_PLUGIN_ENABLED=true \
    ASYNC_RESPONSE_TIMEOUT=4 \
    ASYNC_MAX_BACKGROUND_WORKERS=20 \
    ASYNC_MAX_BACKGROUND_TASKS=100 \
    ASYNC_CACHE_TTL_HOURS=1

# 构建参数
ARG VERSION=dev
ARG BUILD_DATE=unknown
ARG VCS_REF=unknown

# 添加镜像标签
LABEL org.opencontainers.image.title="PanSou" \
      org.opencontainers.image.description="高性能网盘资源搜索API服务" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.created="${BUILD_DATE}" \
      org.opencontainers.image.revision="${VCS_REF}" \
      org.opencontainers.image.url="https://github.com/fish2018/pansou" \
      org.opencontainers.image.source="https://github.com/fish2018/pansou" \
      maintainer="fish2018"

# 运行应用
CMD ["/app/pansou"] 
