.PHONY: build test vet lint run clean

build:
	go build -o dispatch ./cmd/dispatch

test:
	go test ./... -race -timeout 120s

vet:
	go vet ./...

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run || echo "golangci-lint not installed, skipping"

run: build
	./dispatch

clean:
	rm -f dispatch
	rm -rf dist/
