all:
	make format
	make test
	make start

start:
	go run dtm.go

test:
	go test ./... -cover -count=1

format:
	go mod tidy
	go fmt ./...

testE2E:
	echo "should run `# (make serve)` first"
	cd e2e && npm run test

gql:
	echo "init have run [go run github.com/99designs/gqlgen init --server web/server.go]"
	go run github.com/99designs/gqlgen generate

serve:
	go run dtm.go serve

cli:
	go run dtm.go share -i "sampleInput.csv" -o "sampleOutput.txt"

dev-docker:
	docker run -d --name dtm-pg -e POSTGRES_HOST_AUTH_METHOD=trust -p 5432:5432 postgres
	go run dtm.go migrate -u 
