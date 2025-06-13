all:
	make format
	make test
	make start

start:
	go mod tidy
	go run dtm.go

test:
	@echo "==> unit test, should set env by docker first"
	go test ./... -cover -v -count=1

format:
	@echo "==> Running initial checks..."
	go fmt ./...
	go vet ./...

testE2E:
	@echo "should run `# (make serve)` first"
	cd e2e && npm run test

build:
	go build -o ./bin/app ./dtm.go

gql:
	@echo "static gql code generate"
	@echo "init have run [go run github.com/99designs/gqlgen init --server web/server.go]"
	go run github.com/99designs/gqlgen generate

serve:
	@echo start web service
	go run dtm.go serve

cli:
	go run dtm.go share -i "sampleInput.csv" -o "sampleOutput.txt"

dev-docker:
	@echo local dev environment
	docker run -d --name dtm-pg -e POSTGRES_HOST_AUTH_METHOD=trust -p 5432:5432 postgres
	docker run -p 5672:5672 -d --hostname dtm-rabbit --name dtm-rabbit rabbitmq:3
	go run dtm.go migrate -u

remote-migration:
	@echo migrate db in cloud sql, please set local ip as auth network in cloud when use this cmd
	@echo not that when include special char in pwd, please use '\' to escape
	go run dtm.go migrate -u -i='<your ip>' -p='<your pwd>'

dev-gcp:
	# gcloud auth application-default login
	# gcloud components install pubsub-emulator
	# gcloud components install beta
	gcloud beta emulators pubsub start --project=test-project

docker-build:
	docker build -t dtm .