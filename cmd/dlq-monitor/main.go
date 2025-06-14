package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/IBM/sarama"
	"github.com/sirupsen/logrus"
)

func main() {
	logger := logrus.New()
	logger.SetFormatter(&logrus.JSONFormatter{})

	kafkaBrokers := getEnv("KAFKA_BROKERS", "localhost:9092")
	
	// Create consumer for DLQ monitoring
	config := sarama.NewConfig()
	config.Consumer.Group.Rebalance.Strategy = sarama.BalanceStrategyRoundRobin
	config.Consumer.Offsets.Initial = sarama.OffsetOldest
	config.Version = sarama.V2_6_0_0

	consumer, err := sarama.NewConsumerGroup([]string{kafkaBrokers}, "dlq-monitor-group", config)
	if err != nil {
		logger.WithError(err).Fatal("Failed to create DLQ consumer")
	}
	defer consumer.Close()

	// Start monitoring
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handler := &dlqHandler{logger: logger}
	
	go func() {
		for {
			if err := consumer.Consume(ctx, []string{"order.created.dlq"}, handler); err != nil {
				logger.WithError(err).Error("Error consuming from DLQ")
			}
		}
	}()

	logger.Info("DLQ Monitor started - monitoring order.created.dlq topic")

	// Wait for interrupt
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down DLQ monitor...")
}

type dlqHandler struct {
	logger *logrus.Logger
}

func (h *dlqHandler) Setup(sarama.ConsumerGroupSession) error   { return nil }
func (h *dlqHandler) Cleanup(sarama.ConsumerGroupSession) error { return nil }

func (h *dlqHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	for message := range claim.Messages() {
		// Extract metadata
		var metadata map[string]interface{}
		for _, header := range message.Headers {
			if string(header.Key) == "metadata" {
				json.Unmarshal(header.Value, &metadata)
			} else if string(header.Key) == "failure_time" {
				h.logger.WithField("failure_time", string(header.Value)).Info("DLQ message failure time")
			}
		}

		h.logger.WithFields(logrus.Fields{
			"topic":     message.Topic,
			"partition": message.Partition,
			"offset":    message.Offset,
			"key":       string(message.Key),
			"metadata":  metadata,
		}).Warn("DLQ Message Detected")

		// Parse the original order
		var order map[string]interface{}
		if err := json.Unmarshal(message.Value, &order); err == nil {
			h.logger.WithFields(logrus.Fields{
				"order_id":     order["order_id"],
				"customer_id":  order["customer_id"],
				"total_amount": order["total_amount"],
			}).Info("DLQ Order Details")
		}

		fmt.Printf("\n=== DLQ Message ===\n")
		fmt.Printf("Time: %s\n", time.Now().Format(time.RFC3339))
		fmt.Printf("Order Key: %s\n", string(message.Key))
		fmt.Printf("Error: %v\n", metadata["error_message"])
		fmt.Printf("Retry Count: %v\n", metadata["retry_count"])
		fmt.Printf("==================\n\n")

		session.MarkMessage(message, "")
	}
	return nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}