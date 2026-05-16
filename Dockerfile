FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

ARG VERSION=docker
ARG TARGETOS
ARG TARGETARCH

RUN apk add --no-cache git ca-certificates && update-ca-certificates

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/hangar .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tini && \
    addgroup -S -g 10001 appgroup && \
    adduser  -S -u 10001 -G appgroup appuser && \
    mkdir -p /data && chown -R appuser:appgroup /data

COPY --from=builder /out/hangar /usr/local/bin/hangar

USER appuser
WORKDIR /data

EXPOSE 8080

VOLUME ["/data"]

ENTRYPOINT ["/sbin/tini","--","/usr/local/bin/hangar"]
CMD ["server","-c","/data/config.toml"]
