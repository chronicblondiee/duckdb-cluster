.PHONY: build run test clean proto docker-build docker-run docker-test compose-up compose-down compose-distributed-up compose-distributed-down uat

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.0.0-dev")
LDFLAGS = -X github.com/chronicblondiee/duckdb-cluster/internal/migration.Version=$(VERSION)

build:
	go build -ldflags "$(LDFLAGS)" -o bin/duckdb-cluster ./cmd/duckdb-cluster/

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

IMAGE_NAME ?= duckdb-cluster
IMAGE_TAG  ?= $(VERSION)

docker-build:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE_NAME):$(IMAGE_TAG) .

docker-run: docker-build
	docker run --rm -p 8080:8080 -v duckdb-data:/data $(IMAGE_NAME):$(IMAGE_TAG)

docker-test: docker-build
	docker run --rm $(IMAGE_NAME):$(IMAGE_TAG) version

compose-up:
	docker compose -f examples/local/docker-compose.yaml up --build -d

compose-down:
	docker compose -f examples/local/docker-compose.yaml down -v

compose-distributed-up:
	docker compose -f examples/distributed/docker-compose.yaml up --build -d

compose-distributed-down:
	docker compose -f examples/distributed/docker-compose.yaml down -v

uat:
	bash examples/distributed/test.sh
