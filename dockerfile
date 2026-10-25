# build stage
FROM golang:1.25 AS builder

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN make format
RUN make build

# exe stage
FROM alpine:latest

RUN addgroup -S appgroup && adduser -S appuser -G appgroup

WORKDIR /home/appuser

# ENV GCIP_API_KEY="<Identity Platform API key>"
# ENV GCIP_PROJECT_ID="<Identity Platform project ID>"
# ENV OAUTH_REDIRECT_PATH="/backend/auth/callback"
# ENV FRONTEND_URL="http://localhost:3000"
# ENV DATABASE_URL="host=localhost user=postgres dbname=postgres port=5432 sslmode=disable TimeZone=Asia/Taipei"
# ENV DATABASE_PASSWORD="<password>"
# ENV DATABASE_USER="postgres"
# ENV DATABASE_HOST="127.0.0.1"
# ENV RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
# ENV GCP_PROJECT_ID="<Pub/Sub project ID>"
# ENV GOOGLE_APPLICATION_CREDENTIALS="<credential JSON path>"
# ENV PUBSUB_EMULATOR_HOST="127.0.0.1:8085"
# ENV ADMIN_KEY="<X-Admin-Key>"
# ENV GRAPHQL_ENDPOINT_HTTP="http://127.0.0.1:8080/query"
# ENV GRAPHQL_ENDPOINT_WS="ws://127.0.0.1:8080/subscription"
# ENV TEST_DATABASE_URL="<test PostgreSQL DSN>"
# ENV DTM_TRIP_BENCH="1"
# ENV DTM_E2E_ID_TOKEN=""
# ENV START_COMMAND=""
# ENV GCP_SA_KEY=""
# ENV REGISTER_NAME=""
# ENV GITHUB_TOKEN=""

COPY --from=builder /app/bin/app .

RUN chown -R appuser:appgroup /home/appuser

USER appuser

EXPOSE 8080

ENTRYPOINT ["./app", "serve", "--dev=false", "--port=8080", "--mq=go_chan"]
# ENTRYPOINT ["./app", "serve", "--dev=true", "--port=8080"]
