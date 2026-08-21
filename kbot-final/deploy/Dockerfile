# 多阶段构建：前端阶段构建 Admin Console，Go 阶段编译服务，运行阶段只保留静态二进制与前端产物。

# --- 前端：Vite 构建 Admin Console → /web/dist ---
FROM node:22-alpine@sha256:c610fcdfb1d5b4740dd70c284ed3cb16bb857e0f7166196e36a5501df7a3aa32 AS web
WORKDIR /web
COPY web/admin/package.json web/admin/package-lock.json ./
RUN npm ci
COPY web/admin/ ./
RUN npm run build

# --- 后端:先 COPY go.mod/go.sum 再 download,依赖没变时命中缓存、改代码重建飞快。 ---
FROM golang:1.26.6@sha256:0d1d3a794be25f809dd2cb3160d8c73276c4056a9f8242a138e908ddeee7b6b6 AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /bin/kbot ./cmd/server

# --- 运行阶段:distroless,无 shell、攻击面小。 ---
FROM gcr.io/distroless/static-debian12@sha256:6447365a6337c3732f412d1b74357b30a633831955b2bc45552b0086be907687
WORKDIR /app
COPY --from=build /bin/kbot /kbot
# React Admin 构建产物。
COPY --from=web /web/dist /app/web/admin/dist
EXPOSE 8080
ENTRYPOINT ["/kbot"]
