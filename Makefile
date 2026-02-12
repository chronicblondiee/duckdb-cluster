.PHONY: build run test clean

build:
	go build -o bin/duckdb-cluster ./cmd/duckdb-cluster/

run: build
	./bin/duckdb-cluster start

test:
	go test ./...

clean:
	rm -rf bin/ data/
