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
        docker.io \
    && rm -rf /var/lib/apt/lists/*

COPY --from=docker/buildx-bin /buildx /usr/libexec/docker/cli-plugins/docker-buildx
RUN docker buildx version

# Install Railpack.
RUN curl -sSL https://railpack.com/install.sh | sh

COPY --from=builder /api /api

ENTRYPOINT ["/api"]

EXPOSE 8000
