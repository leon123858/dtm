# build stage
FROM golang:1.23 AS builder

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

# can be set by infra
# ENV DATABASE_URL="host=localhost user=postgres dbname=postgres port=5432 sslmode=disable TimeZone=Asia/Taipei"
# ENV RABBITMQ_URL="amqp://guest:guest@localhost:5672/"
# ENV FRONTEND_URL="http://localhost:3000"
# ENV DATABASE_PASSWORD="<db pwd>"
# ENV DATABASE_USER="postgres"
# ENV CLOUD_SQL_SA_EMAIL="<email>"

COPY --from=builder /app/bin/app .

RUN chown -R appuser:appgroup /home/appuser

USER appuser

EXPOSE 8080

ENTRYPOINT ["./app", "serve", "--dev=false", "--port=8080", "--mq=gochan"]
# ENTRYPOINT ["./app", "serve", "--dev=true", "--port=8080"]