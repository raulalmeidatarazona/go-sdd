# syntax=docker/dockerfile:1
# One image, two binaries: /oms-api and /oms-worker. Compose picks the entrypoint.

FROM golang:1.27-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
ARG VERSION=dev
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags "-s -w" -o /out/oms-api ./cmd/api && \
    go build -ldflags "-s -w" -o /out/oms-worker ./cmd/worker

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /
USER nonroot:nonroot
EXPOSE 8080 9090 8081
ENTRYPOINT ["/oms-api"]
