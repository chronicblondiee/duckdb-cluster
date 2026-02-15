FROM golang:1.25-bookworm AS builder

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Build
COPY . .
ARG VERSION=0.0.0-dev
RUN CGO_ENABLED=1 go build \
    -ldflags "-X github.com/chronicblondiee/duckdb-cluster/internal/migration.Version=${VERSION}" \
    -o /usr/local/bin/duckdb-cluster \
    ./cmd/duckdb-cluster/

# --- runtime ---
FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/duckdb-cluster /usr/local/bin/duckdb-cluster

RUN useradd -r -m -d /data duckdb
USER duckdb
WORKDIR /data

EXPOSE 8080

ENTRYPOINT ["duckdb-cluster"]
CMD ["start"]
