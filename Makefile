all:
	make start

start:
	go run dtm.go

test:
	go test ./... -v

format:
	go mod tidy
	go fmt ./...

cli:
	go run dtm.go share -i "sampleInput.csv" -o "sampleOutput.txt"