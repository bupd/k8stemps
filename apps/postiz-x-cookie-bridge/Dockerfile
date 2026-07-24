FROM golang:1.25-alpine AS builder

WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /postiz-x-cookie-bridge .

FROM ghcr.io/bupd/shitpost@sha256:1f9e65d361957c6b644dea77ba246819a6889683c101f3e32ae497f9769982c1

COPY --from=builder /postiz-x-cookie-bridge /usr/local/bin/postiz-x-cookie-bridge
COPY patch-emusks.mjs /tmp/patch-emusks.mjs
RUN cd /opt/crosspost/node_modules/emusks \
    && bun add --production --exact x-client-transaction-id@0.3.1 \
    && test "$(bun -e 'console.log(require("./node_modules/x-client-transaction-id/package.json").version)')" = "0.3.1" \
    && bun /tmp/patch-emusks.mjs \
    && rm /tmp/patch-emusks.mjs

EXPOSE 8080
CMD ["postiz-x-cookie-bridge"]
