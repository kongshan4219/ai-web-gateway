# Stage 1: Build Go binary
FROM golang:1.24-alpine AS builder

WORKDIR /build
COPY go.mod ./
COPY *.go ./
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /go-gateway .

# Stage 2: Runtime
FROM nginx:alpine

# Install bash (for start.sh compatibility) and ca-certificates
RUN apk add --no-cache bash ca-certificates curl tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone

# Copy Go binary
COPY --from=builder /go-gateway /usr/local/bin/go-gateway

# Copy nginx config
COPY nginx/default.conf /etc/nginx/conf.d/default.conf

# Create required directories
RUN mkdir -p /sites /sites/.runtime /sites/.runtime/ssl /etc/nginx/conf.d/projects && \
    chown -R nginx:nginx /sites

# Copy entrypoint
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

EXPOSE 80 443

ENTRYPOINT ["/entrypoint.sh"]
