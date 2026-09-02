# syntax=docker/dockerfile:1.7
# ============================================================
# melete-api — Go 后端 (chi + sqlx)
# ============================================================
# 用法:
#   docker buildx build \
#     --platform linux/amd64,linux/arm64 \
#     -t bukahou/melete-api:v1.0.0-<sha7> \
#     -f backend/deploy/docker/Dockerfile backend --push
#
# 与 geass-v3 的 Dockerfile 同构，去掉了 ARG SERVICE ——
# melete 后端是单体，只有一个 cmd/melete-api。
# ============================================================

ARG GO_VERSION=1.26

# -----------------------------------------------------------
# Stage 1: build
#
# --platform=$BUILDPLATFORM 是这里最关键的一行：让编译在**构建机的原生架构**上跑，
# 再交叉编译到目标架构。若不加，buildx 会在 QEMU 模拟的 arm64 里跑 go build，
# 慢 5-10 倍 —— 双架构构建的绝大部分时间都会耗在这上面。
# Go 的交叉编译是一等公民，没有任何理由用模拟。
# -----------------------------------------------------------
FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build

# buildx 自动注入的目标平台变量
ARG TARGETOS
ARG TARGETARCH

WORKDIR /src

# 先拷依赖清单单独下载：源码改动不会让这一层缓存失效
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO_ENABLED=0 静态编译，才能跑在 distroless/static（无 libc）上
# -trimpath      去掉构建机的绝对路径
# -ldflags="-s -w" 去符号表与 DWARF，镜像更小
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" \
    -o /out/melete-api \
    ./cmd/melete-api

# -----------------------------------------------------------
# Stage 2: runtime
#
# distroless/static:nonroot — ~2MB，无 shell、无包管理器、默认非 root。
# 攻击面小到极致：即便进程被攻破，容器里也没有可用的工具。
#
# ⚠️ 需要 ca-certificates：本服务要 HTTPS 访问 Akasha 的 JWKS 端点验 id_token，
#    distroless/static 自带根证书，无需额外安装（换成 scratch 就会 x509 失败）。
# -----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/melete-api /melete-api

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/melete-api"]
