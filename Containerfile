# Static binary on scratch: the exporter makes outbound HTTPS to an S3
# endpoint and serves plain HTTP, so it needs CA roots and nothing else.
# VERSION is stamped by the release workflow from the tag; a local build
# says "dev".
FROM golang:1.26-alpine AS build
ARG VERSION=dev
ARG REVISION=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w \
        -X github.com/prometheus/common/version.Version=${VERSION} \
        -X github.com/prometheus/common/version.Revision=${REVISION} \
        -X github.com/prometheus/common/version.Branch=sfi \
        -X github.com/prometheus/common/version.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      -o /s3_exporter .

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /s3_exporter /s3_exporter
USER 65534:65534
EXPOSE 9340/tcp
ENTRYPOINT ["/s3_exporter"]
