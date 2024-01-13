/**
 * Copyright 2023 Owen McGill
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *	http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package kafka

import (
	"context"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
)

type Consumer struct {
	pollTimeout          time.Duration
	topic                string
	groupName            string
	bootstrapServer      string
	incrementalRebalance bool
	kafkaConfig          kafka.ConfigMap
	msgPool              *bytebufferpool.Pool
	metrics              consumerMetrics
	logger               zerolog.Logger
}

type consumerMetrics interface {
	MsgConsumedSizeObserve(num float64)
	LostPartitionAssignmentInc()
	RebalanceInc(reason string)
}

// NewConsumer creates an instance of a kafka.Consumer configured with the provided values.
func NewConsumer(
	bootstrapServer, groupID, topic string,
	incrementalRebalance bool,
	sessionTimeout, pollTimeout time.Duration,
	msgPool *bytebufferpool.Pool,
	metrics consumerMetrics,
	logger zerolog.Logger,
) *Consumer {
	cons := Consumer{
		pollTimeout:          pollTimeout,
		topic:                topic,
		groupName:            groupID,
		bootstrapServer:      bootstrapServer,
		incrementalRebalance: incrementalRebalance,
		kafkaConfig:          kafka.ConfigMap{},
		msgPool:              msgPool,
		metrics:              metrics,
		logger: logger.With().
			Str("topic", topic).
			Str("consumer_group", groupID).
			Logger(),
	}

	_ = cons.kafkaConfig.SetKey("session.timeout.ms", int(sessionTimeout.Milliseconds()))
	_ = cons.kafkaConfig.SetKey("group.id", groupID)
	_ = cons.kafkaConfig.SetKey("bootstrap.servers", bootstrapServer)
	_ = cons.kafkaConfig.SetKey("go.application.rebalance.enable", true)

	if incrementalRebalance {
		_ = cons.kafkaConfig.SetKey("partition.assignment.strategy", "cooperative-sticky")
	}

	return &cons
}

// UseTLS configures librdkafka to use TLS.
func (c *Consumer) UseTLS(
	caLocation, certLocation, keyLocation, keyPassword string,
) {
	_ = c.kafkaConfig.SetKey("security.protocol", "ssl")
	_ = c.kafkaConfig.SetKey("ssl.ca.location", caLocation)
	_ = c.kafkaConfig.SetKey("ssl.certificate.location", certLocation)
	_ = c.kafkaConfig.SetKey("ssl.key.location", keyLocation)
	_ = c.kafkaConfig.SetKey("ssl.key.password", keyPassword)
}

// Start starts the consumption of messages from Kafka and forwards the values to the provided channel.
//
// If the context is cancelled, it pauses consumption and closes the channel to signal to the
// downstream receivers to shutdown as well.
//
// This should be run in a Goroutine, preferably using the golang.org/x/sync/errgroup for
// grouping all the concurrent Goroutines.
func (c *Consumer) Start(
	ctx context.Context,
	msgChan chan<- *bytebufferpool.ByteBuffer,
) error {
	defer close(msgChan)
	defer c.logger.Info().Msg("stopping consumer")

	c.logger.Debug().Msgf("bootstrap server: %s", c.bootstrapServer)

	consumer, err := kafka.NewConsumer(&c.kafkaConfig)
	if err != nil {
		return fmt.Errorf("error instantiating kafka consumer on %q: %w", c.bootstrapServer, err)
	}
	defer consumer.Close()

	c.logger.Debug().Msg("*kafka.Consumer created")

	err = consumer.Subscribe(c.topic, c.rebalanceCb)
	if err != nil {
		return fmt.Errorf("error subscribing to topic %q: %w", c.topic, err)
	}

	c.logger.Info().Msgf("subscribed to topic")

	for {
		select {
		case <-ctx.Done():
			c.logger.Info().Msgf("context has been cancelled, shutting down consumer: %v", ctx.Err())

			partitions, err := consumer.Assignment()
			if err != nil {
				c.logger.Error().Err(err).Msgf("error getting partition assignments")
			}

			err = consumer.Pause(partitions)
			if err != nil {
				c.logger.Error().Err(err).Msgf("error pausing consumption")
			}

			c.logger.Info().Msg("stopping kafka consumer")

			return nil
		default:
			kafkaEvent := consumer.Poll(int(c.pollTimeout.Milliseconds()))
			if kafkaEvent == nil {
				continue
			}

			switch event := kafkaEvent.(type) {
			case *kafka.Message:
				log := c.logger.With().
					Int32("partition", event.TopicPartition.Partition).
					Int64("offset", int64(event.TopicPartition.Offset)).
					Logger()
				log.Debug().Msg("consumed message")

				if log.GetLevel() == zerolog.TraceLevel {
					log.Trace().Msg(event.String())
				}

				c.metrics.MsgConsumedSizeObserve(float64(len(event.Value)))
				msgBuf := c.msgPool.Get()

				writeLen, _ := msgBuf.Write(event.Value)
				if writeLen != len(event.Value) {
					c.logger.Error().Msg("length written to msg buffer is not equal to message value")
					c.msgPool.Put(msgBuf)

					continue
				}
				msgChan <- msgBuf

				c.logger.Debug().Msg("sent message to channel")

				if c.logger.GetLevel() == zerolog.TraceLevel {
					c.logger.Trace().Msg(msgBuf.String())
				}
			case kafka.Error:
				return fmt.Errorf("kafka consumer error: %w", event)
			}
		}
	}
}

func (c *Consumer) rebalanceCb(kafkaCons *kafka.Consumer, event kafka.Event) error {
	switch kafkaEvent := event.(type) {
	case kafka.AssignedPartitions:
		c.logger.Info().
			Str("rebalance_protocol", kafkaCons.GetRebalanceProtocol()).
			Int("new_partitions", len(kafkaEvent.Partitions)).
			Any("partitions", kafkaEvent.Partitions).
			Msg("assigning new partitions")
		c.metrics.RebalanceInc("assignment")
	case kafka.RevokedPartitions:
		c.logger.Info().
			Str("rebalance_protocol", kafkaCons.GetRebalanceProtocol()).
			Int("revoked_partitions", len(kafkaEvent.Partitions)).
			Any("partitions", kafkaEvent.Partitions).
			Msg("revoking partitions")
		c.metrics.RebalanceInc("revocation")

		if kafkaCons.AssignmentLost() {
			c.logger.Warn().Msg("assignmnent lost involuntarily")
			c.metrics.LostPartitionAssignmentInc()
		}
	}

	return nil
}
