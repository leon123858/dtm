package rabbit

import (
	"fmt"
	"log"
	"os"
	"time"

	"github.com/rabbitmq/amqp091-go"
)

// RabbitMQ 連線字串，可以透過環境變數設定
func CreateAmqpURL() string {
	amqpURL := "amqp://guest:guest@localhost:5672/"
	if url := os.Getenv("RABBITMQ_URL"); url != "" {
		amqpURL = url
		log.Printf("Using RABBITMQ_URL: %s", amqpURL)
	} else {
		log.Printf("Using default RabbitMQ URL: %s", amqpURL)
	}
	return amqpURL
}

// InitRabbitMQ 初始化 RabbitMQ 連線
func InitRabbitMQ(dsn string) (*amqp091.Connection, error) {
	conn, err := amqp091.DialConfig(dsn, amqp091.Config{
		Heartbeat: 10 * time.Second, // 設定心跳間隔
		Locale:    "ASIA/Taipei",    // 設定地區
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to RabbitMQ: %w", err)
	}

	log.Println("Successfully connected to RabbitMQ!")
	return conn, nil
}

// CloseRabbitMQ 關閉 RabbitMQ 連線
func CloseRabbitMQ(conn *amqp091.Connection) {
	if conn != nil {
		err := conn.Close()
		if err != nil {
			log.Printf("Error closing RabbitMQ connection: %v", err)
		} else {
			conn = nil
			log.Println("RabbitMQ connection closed.")
		}
	}
}

// DeclareQueueAndExchange 宣告佇列和交換器
func DeclareQueueAndExchange(ch *amqp091.Channel, queueName, exchangeName, routingKey string) error {
	// 宣告交換器
	err := ch.ExchangeDeclare(
		exchangeName, // name
		"direct",     // type
		false,         // durable
		false,        // auto-deleted
		false,        // internal
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare exchange: %w", err)
	}

	// 宣告佇列
	q, err := ch.QueueDeclare(
		queueName, // name
		true,      // durable
		false,     // delete when unused
		false,     // exclusive
		false,     // no-wait
		nil,       // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to declare queue: %w", err)
	}

	// 綁定佇列到交換器
	err = ch.QueueBind(
		q.Name,       // queue name
		routingKey,   // routing key
		exchangeName, // exchange
		false,        // no-wait
		nil,          // arguments
	)
	if err != nil {
		return fmt.Errorf("failed to bind queue: %w", err)
	}
	return nil
}
