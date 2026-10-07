.PHONY: build check fuzz smoke

build:
	mkdir -p bin
	go build -trimpath -o bin/hookcheck ./cmd/hookcheck
	go build -trimpath -o bin/payment-demo ./cmd/payment-demo

check:
	test -z "$$(gofmt -l .)"
	go test -race -coverprofile=coverage.out -timeout 60s ./...
	go vet ./...
	go build ./...

fuzz:
	go test -run '^$$' -fuzz FuzzLoad -fuzztime 10s -parallel 2 -timeout 30s .

smoke: build
	python3 scripts/smoke.py
