FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /postiz-x-cookie-bridge .

FROM ghcr.io/bupd/shitpost:latest

COPY --from=builder /postiz-x-cookie-bridge /usr/local/bin/postiz-x-cookie-bridge

EXPOSE 8080
CMD ["postiz-x-cookie-bridge"]
