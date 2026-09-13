.PHONY: fmt test vet race tidy-check lint lint-fix ci

fmt:
	go fmt ./...

test:
	go test -v -race --count=1 ./...

vet:
	go vet ./...

lint:
	golangci-lint run

lint-fix:
	golangci-lint run --fix

ci: vet lint test
