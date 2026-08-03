IMAGE ?= crozltd/periscope
TAG   ?= dev

.PHONY: all web build test vet image run clean

all: web build

# Build the PatternFly UI into web/dist (embedded by the Go build).
web:
	cd web/app && npm install && npm run build

# Build the Go binary (expects web/dist to exist; run `make web` first).
build:
	go build -o bin/periscope ./cmd/periscope

test:
	go test ./...

vet:
	go vet ./...

image:
	docker build -t $(IMAGE):$(TAG) .

# Run locally against your current kubeconfig context.
run: build
	./bin/periscope serve --db ./cc.db

clean:
	rm -rf bin cc.db web/dist/assets
