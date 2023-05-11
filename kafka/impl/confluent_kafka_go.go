//go:build cgo

package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog"
)

type ConfluentConsumer struct {
	bootstrapServer string
	topic           string
	groupName       string
	pollTimeout     time.Duration
	kafkaConfig     kafka.ConfigMap
	client          *kafka.Consumer
	metrics         ConsumerMetrics
	logger          zerolog.Logger
}

func NewCGoConsumer(
	bootstrapServer, topic, groupName string,
	sessionTimeout time.Duration,
	metrics ConsumerMetrics, logger zerolog.Logger,
) (*ConfluentConsumer, error) {
	cons := ConfluentConsumer{
		pollTimeout:     defaultPollTimeout,
		topic:           topic,
		groupName:       groupName,
		bootstrapServer: bootstrapServer,
		kafkaConfig:     kafka.ConfigMap{},
		metrics:         metrics,
		logger:          logger,
	}

	_ = cons.kafkaConfig.SetKey("session.timeout.ms", int(sessionTimeout.Milliseconds()))
	_ = cons.kafkaConfig.SetKey("group.id", groupName)
	_ = cons.kafkaConfig.SetKey("bootstrap.servers", bootstrapServer)

	cons.logger.Debug().Msgf("bootstrap server: %s", cons.bootstrapServer)

	consumer, err := kafka.NewConsumer(&cons.kafkaConfig)
	if err != nil {
		return nil, fmt.Errorf("error instantiating kafka consumer on %q: %w", cons.bootstrapServer, err)
	}

	cons.logger.Debug().Msg("*kafka.Consumer created")

	err = consumer.Subscribe(cons.topic, nil)
	if err != nil {
		return nil, fmt.Errorf("error subscribing to topic %q: %w", cons.topic, err)
	}

	cons.logger.Info().Msgf("subscribed to topic")

	cons.client = consumer

	return &cons, nil
}

func (c *ConfluentConsumer) Consume(ctx context.Context) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("consuming: %w", err)
	}
	msg, err := c.client.ReadMessage(c.pollTimeout)
	if err != nil {
		var kafkaErr kafka.Error
		if errors.As(err, &kafkaErr) && kafkaErr.IsTimeout() {
			return nil, ErrConsumeTimeout
		}
		if msg != nil {
			return nil, fmt.Errorf("partition=%d, offset=%d, topicpartition error=%w: %w",
				msg.TopicPartition.Partition, msg.TopicPartition.Offset, msg.TopicPartition.Error, err)
		}
		return nil, fmt.Errorf("reading message: %w", err)
	}

	c.metrics.ConsumedMsgSize(float64(len(msg.Value)))
	return []Message{
		{
			content: msg.Value,
		},
	}, nil
}
