FROM golang:1.24-alpine AS build

WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /linkedin-bridge .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /linkedin-bridge /linkedin-bridge
ENTRYPOINT ["/linkedin-bridge"]
