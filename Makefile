all:
	make start

start:
	go run main.go

test:
	go test ./... -v

format:
	go mod tidy
	go fmt ./...