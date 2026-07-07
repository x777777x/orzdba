.PHONY: build test vet fmt lint clean mod-tidy

build:
	go build -o bin/orzdba ./cmd/orzdba

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -s -w .

lint: vet
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run ./... || echo "golangci-lint not installed, skipping"

mod-tidy:
	go mod tidy

clean:
	rm -rf bin/ dist/
