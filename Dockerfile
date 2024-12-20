# 使用官方 Go 镜像作为构建环境
FROM golang:1.21-alpine AS builder

# 安装必要的构建工具
RUN apk add --no-cache git

# 设置工作目录
WORKDIR /app

# 复制 go.mod 和 go.sum (如果存在)
COPY go.mod ./
COPY go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 构建应用
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# 使用轻量级的 alpine 作为运行环境
FROM alpine:3.19

# 安装 CA 证书，用于 HTTPS 请求
RUN apk --no-cache add ca-certificates

WORKDIR /app

# 从构建阶段复制编译好的二进制文件
COPY --from=builder /app/main .

# 运行应用
CMD ["./main"] 