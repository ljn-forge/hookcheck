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
	# Use an execution budget so fuzzing does not end mid-request on a deadline.
	go test -run '^$$' -fuzz FuzzLoad -fuzztime 300000x -parallel 2 -timeout 60s .

smoke: build
	python3 scripts/smoke.py
