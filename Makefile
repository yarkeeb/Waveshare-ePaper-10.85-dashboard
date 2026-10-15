.PHONY: build test preview pi-zero pi-zero2 package
build:
	go build -trimpath -o bin/dashboard ./cmd/dashboard
test:
	go test -race ./...
	go vet ./...
preview:
	go run ./cmd/dashboard -demo -once -output dashboard.png
pi-zero:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=6 go build -trimpath -o bin/dashboard-linux-armv6 ./cmd/dashboard
pi-zero2:
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -o bin/dashboard-linux-arm64 ./cmd/dashboard
package:
	go run ./cmd/package
