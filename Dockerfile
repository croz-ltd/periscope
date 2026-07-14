# syntax=docker/dockerfile:1

# Base images are overridable so CI can point them at internal mirrors (Harbor).
ARG NODE_IMAGE=node:20-alpine
ARG GO_IMAGE=golang:1.26
ARG RUNTIME_IMAGE=registry.access.redhat.com/ubi9/ubi-micro:latest

# Version stamped into the binary (-X pkg/version.Raw).
ARG VERSION=dev

# 1) Build the PatternFly UI into web/dist
FROM ${NODE_IMAGE} AS web
WORKDIR /src/web/app
COPY web/app/package*.json ./
RUN npm ci
COPY web/app/ ./
RUN npm run build   # vite emits to ../dist (web/dist)

# 2) Build the Go binary with the UI embedded
FROM ${GO_IMAGE} AS build
ARG VERSION
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/croz-ltd/periscope/pkg/version.Raw=${VERSION}" \
    -o /out/periscope ./cmd/periscope

# 3) Minimal runtime
FROM ${RUNTIME_IMAGE}
COPY --from=build /out/periscope /usr/local/bin/periscope
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/periscope"]
CMD ["serve"]
