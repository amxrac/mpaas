FROM golang:1.26.1 AS builder

WORKDIR /app

ENV CGO_ENABLED=0

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go build -o /api ./cmd


FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
    && rm -rf /var/lib/apt/lists/*

# Install Docker Buildx CLI plugin.
# COPY --from=docker/buildx-bin /buildx \
#     /usr/libexec/docker/cli-plugins/docker-buildx
RUN curl -fsSL https://get.docker.com | sh

# RUN docker buildx version

# Install Railpack.
RUN curl -sSL https://railpack.com/install.sh | RAILPACK_VERSION=0.36.4 sh && which railpack

COPY --from=builder /api /api

EXPOSE 8000

ENTRYPOINT ["/api"]
