.PHONY: build run test clean proto

build:
	go build -o bin/duckdb-cluster ./cmd/duckdb-cluster/

run: build
	./bin/duckdb-cluster start

test:
	go test ./...

clean:
	rm -rf bin/ data/

proto:
	PATH="$$PATH:$$HOME/go/bin" protoc --go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/common.proto proto/ingester.proto proto/querier.proto
