FROM node:25.2.1 AS web_builder

# 安装 pnpm
RUN npm install -g pnpm@10

WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./

RUN pnpm install --frozen-lockfile --shamefully-hoist

COPY web/ ./
RUN pnpm run build:prod

FROM golang:1.26.9-alpine3.23 AS builder

ENV GO111MODULE=on
WORKDIR /go/release
COPY go.mod go.sum ./
RUN go mod download
COPY app ./app
COPY main ./main
COPY static ./static

COPY --from=web_builder /web/dist ./static/secure

ARG VERSION=unknown

RUN set -x \
    && MODULE_PATH=$(go list -m) \
    && CGO_ENABLED=0 go build -trimpath \
    -ldflags="-X '${MODULE_PATH}/app.Version=${VERSION}' -s -w -buildid=" \
    -o bepusdt ./main

FROM alpine:3.23

ENV TZ=Asia/Shanghai

# 安装所需的依赖
RUN apk add --no-cache tzdata ca-certificates

COPY --from=builder /go/release/bepusdt /usr/local/bin/bepusdt

# 设置时区
RUN ln -fs /usr/share/zoneinfo/Asia/Shanghai /etc/localtime

RUN addgroup -g 10001 bepusdt && adduser -D -H -u 10001 -G bepusdt bepusdt \
    && mkdir -p /var/lib/bepusdt /var/log/bepusdt \
    && chown 10001:10001 /var/lib/bepusdt /var/log/bepusdt \
    && chmod 0700 /var/lib/bepusdt /var/log/bepusdt
COPY deploy/entrypoint.sh /usr/local/bin/bepusdt-entrypoint
RUN chmod 0555 /usr/local/bin/bepusdt-entrypoint /usr/local/bin/bepusdt
USER 10001:10001

EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/bepusdt-entrypoint"]
CMD ["start"]
