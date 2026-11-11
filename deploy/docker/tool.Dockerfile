# syntax=docker/dockerfile:1
# Builds a small helper from ./tools (demo-app, fake-github).
FROM golang:1.26-alpine AS build
ARG TOOL
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY tools ./tools
COPY internal/fakegithub ./internal/fakegithub
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tool ./tools/${TOOL}

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/tool /tool
USER 65532:65532
ENTRYPOINT ["/tool"]
