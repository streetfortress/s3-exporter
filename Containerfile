# Static binary on scratch: the exporter makes outbound HTTPS to an S3
# endpoint and serves plain HTTP, so it needs CA roots and nothing else.
# VERSION is stamped by the release workflow from the tag; a local build
# says "dev".
#
# The build stage runs on the builder's own architecture and cross-compiles
# to TARGETARCH, so a multi-arch `docker buildx build --platform
# linux/amd64,linux/arm64` needs no QEMU. A plain `docker build` sets
# TARGETOS and TARGETARCH to the host's values, and an empty value makes go
# use its own defaults, so a local build is unaffected.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG VERSION=dev
ARG REVISION=unknown
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
      -ldflags "-s -w \
        -X github.com/prometheus/common/version.Version=${VERSION} \
        -X github.com/prometheus/common/version.Revision=${REVISION} \
        -X github.com/prometheus/common/version.Branch=sfi \
        -X github.com/prometheus/common/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o /s3_exporter .

FROM scratch
# The image is a distribution of a derivative work under the Apache License
# 2.0, so it carries the license text and the attribution beside the binary.
# A scratch image has no package manager and no docs, and /LICENSE is where
# a reader looks.
LABEL org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.source="https://github.com/streetfortress/s3-exporter" \
      org.opencontainers.image.vendor="Streetfortress Industries"
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /src/LICENSE /src/NOTICE /
COPY --from=build /s3_exporter /s3_exporter
USER 65534:65534
EXPOSE 9340/tcp
ENTRYPOINT ["/s3_exporter"]
