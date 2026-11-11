# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/agent ./cmd/agent \
    && mkdir /data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/agent /agent
# The credential volume mounts here. A fresh named volume copies this directory's owner, so the
# non-root agent can write its credential. Without it the volume is root owned and the join fails.
COPY --from=build --chown=65532:65532 /data /data
USER 65532:65532
ENTRYPOINT ["/agent"]
