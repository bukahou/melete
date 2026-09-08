# syntax=docker/dockerfile:1.7
# ============================================================
# melete-web — Next.js 16 SSR (standalone)
# ============================================================
# 用法:
#   docker buildx build \
#     --platform linux/amd64,linux/arm64 \
#     -t bukahou/melete-web:v1.0.0-<sha7> \
#     -f web/Dockerfile web --push
#
# ⚠️ 与 geass-v3-web 的关键差异：**没有任何 NEXT_PUBLIC_* build-arg**。
#    melete 的 API 地址只在服务端使用（见 src/lib/api.ts 的 server-only 隔离），
#    浏览器只请求同源的 /api/*。所以这个镜像是**环境无关**的：
#    同一个镜像可跑本地与生产，改 API 地址只需改 ConfigMap 并重启，不必重新构建。
# ============================================================

ARG NODE_VERSION=22

# -----------------------------------------------------------
# Stage 1: deps —— 只装依赖
#
# 单独成层的理由：package*.json 不变时，改任何源码都不会重装 node_modules。
# 双架构构建时这一层的收益尤其明显（npm ci 在 QEMU 里很慢）。
# -----------------------------------------------------------
FROM node:${NODE_VERSION}-alpine AS deps
WORKDIR /app

# Next.js 的原生依赖需要 glibc 兼容层
RUN apk add --no-cache libc6-compat

COPY package.json package-lock.json ./
# npm ci 严格按 lockfile 安装，比 npm install 快且可复现
RUN npm ci

# -----------------------------------------------------------
# Stage 2: build
# -----------------------------------------------------------
FROM node:${NODE_VERSION}-alpine AS build
WORKDIR /app

RUN apk add --no-cache libc6-compat
COPY --from=deps /app/node_modules ./node_modules
COPY . .

# 构建期不需要真实凭证：本项目所有 required 的环境变量都在**运行时**才读
# （src/lib/auth.ts 用 getter 惰性求值，正是为此）。
# 若哪天构建期开始需要某个 env，说明有配置被提前求值了 —— 那是要修的 bug。
ENV NEXT_TELEMETRY_DISABLED=1
RUN npm run build

# -----------------------------------------------------------
# Stage 3: runtime
#
# next.config.ts 里的 output:"standalone" 会产出一个自带最小 node_modules 的
# server.js —— 运行时镜像不需要完整依赖树，体积从数百 MB 降到几十 MB。
# 代价是 static 与 public 必须手动拷进去（Next 的已知行为，不是遗漏）。
# -----------------------------------------------------------
FROM node:${NODE_VERSION}-alpine AS runtime
WORKDIR /app

RUN apk add --no-cache libc6-compat

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

# 版本号 —— 由 CI 传入（release.yml 用镜像 tag 作为值）。
#
# ⭐ 刻意放在【运行阶段】而不是 build 阶段，也刻意【不】用 NEXT_PUBLIC_*：
#   页面是 SSR，服务端在渲染时读 process.env 即可 ——
#   ⓐ 不进客户端 bundle；ⓑ 不需要为了改版本号重新 build。
#   这与本仓「API 地址只在服务端用、走运行时注入」是同一条原则。
#
# 默认值 dev 只用于本地 `docker build` 不传参的情况；
# CI 一定传真值，⛔ 线上不会显示 dev。
ARG APP_VERSION=dev
ENV APP_VERSION=$APP_VERSION

# standalone 产物自带精简后的 node_modules 与 server.js
COPY --from=build --chown=node:node /app/.next/standalone ./
# 这两份 standalone 不含，必须单独拷 —— 少拷 static 的表现是页面能开但无样式
COPY --from=build --chown=node:node /app/.next/static ./.next/static

# node 镜像自带的非 root 用户。Web 进程没有任何写文件的需求，
# 以 root 跑纯属多余的风险。
USER node

EXPOSE 3000
CMD ["node", "server.js"]
