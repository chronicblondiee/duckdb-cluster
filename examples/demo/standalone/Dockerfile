FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 go build -o /usr/local/bin/duckdb-standalone ./examples/demo/standalone/

FROM debian:bookworm-slim

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

COPY --from=builder /usr/local/bin/duckdb-standalone /usr/local/bin/duckdb-standalone

RUN useradd -r -m -d /data duckdb
USER duckdb
WORKDIR /data

EXPOSE 8090

ENTRYPOINT ["duckdb-standalone"]
