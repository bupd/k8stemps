FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /postiz-x-cookie-bridge .

FROM ghcr.io/bupd/shitpost:latest

COPY --from=builder /postiz-x-cookie-bridge /usr/local/bin/postiz-x-cookie-bridge
COPY patch-emusks.mjs /tmp/patch-emusks.mjs
RUN bun /tmp/patch-emusks.mjs && rm /tmp/patch-emusks.mjs

EXPOSE 8080
CMD ["postiz-x-cookie-bridge"]
