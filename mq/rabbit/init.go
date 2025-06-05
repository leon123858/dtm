package rabbit

import (
	"log"
	"os"

	amqp "github.com/rabbitmq/amqp091-go"
)

func NewRabbitConnection(addr string) *amqp.Connection {
	conn, err := amqp.Dial(addr)
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
		return nil
	}

	return conn
}

func CreateAmqpURL() string {
	amqpURL := "amqp://guest:guest@localhost:5672/"
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		amqpURL = url
	}
	return amqpURL
}
