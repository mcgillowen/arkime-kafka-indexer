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
	"errors"
	"fmt"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
)

type Producer struct {
	flushInterval      time.Duration
	fullQueueCooldown  time.Duration
	logDeliveryReports bool
	topic              string
	bootstrapServer    string
	kafkaConfig        kafka.ConfigMap
	msgPool            *bytebufferpool.Pool
	metrics            producerMetrics
	logger             zerolog.Logger
}

type producerMetrics interface {
	MsgProducedSizeObserve(num float64)
	SuccessfullyProducedMsgInc()
	UnsuccessfullyProducedMsgInc()
}

// NewProducer creates an instance of a kafka.Producer configured with the provided values.
func NewProducer(
	bootstrapServer, topic string,
	flushInterval, msgTimeout, fullQueueCooldown time.Duration,
	msgRetries int,
	logDeliveryReports bool,
	msgPool *bytebufferpool.Pool,
	metrics producerMetrics,
	logger zerolog.Logger,
) *Producer {
	prod := Producer{
		flushInterval:      flushInterval,
		logDeliveryReports: logDeliveryReports,
		fullQueueCooldown:  fullQueueCooldown,
		topic:              topic,
		bootstrapServer:    bootstrapServer,
		kafkaConfig:        kafka.ConfigMap{},
		msgPool:            msgPool,
		metrics:            metrics,
		logger:             logger,
	}

	_ = prod.kafkaConfig.SetKey("bootstrap.servers", bootstrapServer)
	_ = prod.kafkaConfig.SetKey("message.timeout.ms", int(msgTimeout.Milliseconds()))
	_ = prod.kafkaConfig.SetKey("message.send.max.retries", msgRetries)
	_ = prod.kafkaConfig.SetKey("go.delivery.reports", logDeliveryReports)

	return &prod
}

// UseTLS configures librdkafka to use TLS.
func (p *Producer) UseTLS(
	caLocation, certLocation, keyLocation, keyPassword string,
) {
	_ = p.kafkaConfig.SetKey("security.protocol", "ssl")
	_ = p.kafkaConfig.SetKey("ssl.ca.location", caLocation)
	_ = p.kafkaConfig.SetKey("ssl.certificate.location", certLocation)
	_ = p.kafkaConfig.SetKey("ssl.key.location", keyLocation)
	_ = p.kafkaConfig.SetKey("ssl.key.password", keyPassword)
}

// Start starts the production of Kafka messages with the values from the provided channel.
//
// If the inbound channel is closed the production stops.
//
// This should be run in a Goroutine, preferably using the golang.org/g/sync/errgroup for
// grouping all concurrent Goroutines.
func (p *Producer) Start(ctx context.Context, msgChan <-chan *bytebufferpool.ByteBuffer) error {
	defer p.logger.Info().Msg("Stopping producer")

	if msgChan == nil {
		return nil
	}

	var prodMsg *kafka.Message

	p.logger.Debug().Msgf("bootstrap server: %s", p.bootstrapServer)

	producer, err := kafka.NewProducer(&p.kafkaConfig)
	if err != nil {
		return fmt.Errorf("error instantiating kafka producer on %q: %w", p.bootstrapServer, err)
	}

	defer func() {
		unflushedMsgs := producer.Flush(int(p.flushInterval.Milliseconds()))
		if unflushedMsgs > 0 {
			p.logger.Error().Int("unflushed_msgs", unflushedMsgs).Msgf("Failed to flush messages to broker")
		}

		producer.Close()
	}()
	p.logger.Debug().Msg("*kafka.Producer created")

	var deliveryChan chan kafka.Event
	if p.logDeliveryReports {
		deliveryChan = make(chan kafka.Event)
		defer close(deliveryChan)

		go p.deliveryReportsLogger(ctx, deliveryChan)
	}

	for msg := range msgChan {
		func(msg *bytebufferpool.ByteBuffer) {
			defer p.msgPool.Put(msg)
			p.logger.Debug().Msg("read message from channel")

			if p.logger.GetLevel() == zerolog.TraceLevel {
				p.logger.Trace().Msg(msg.String())
			}

			prodMsg = &kafka.Message{
				TopicPartition: kafka.TopicPartition{Topic: &p.topic, Partition: kafka.PartitionAny},
				Timestamp:      time.Now(),
				Value:          msg.Bytes(),
			}

			err := producer.Produce(prodMsg, deliveryChan)
			if err != nil {
				var kafkaErr kafka.Error
				if errors.As(err, &kafkaErr); kafkaErr.Code() == kafka.ErrQueueFull {
					// Producer queue is full, wait for messages
					// to be delivered then try again.
					time.Sleep(p.fullQueueCooldown)

					if err := producer.Produce(prodMsg, deliveryChan); err != nil {
						p.logger.Error().Err(err).Msgf("error producing message async after full queue and waiting %s", p.fullQueueCooldown)
					}

					return
				}

				p.logger.Error().Err(err).Msg("error producing message async")

				return
			}

			log := p.logger.With().Int32("partition", kafka.PartitionAny).Str("topic", p.topic).Logger()
			log.Debug().Msg("produced message")

			if log.GetLevel() == zerolog.TraceLevel {
				log.Trace().Msg(msg.String())
			}

			p.metrics.MsgProducedSizeObserve(float64(msg.Len()))
		}(msg)
	}

	return nil
}

func (p *Producer) deliveryReportsLogger(
	ctx context.Context,
	deliveryChan <-chan kafka.Event,
) {
	logger := p.logger.With().Str("component", "delivery-reports-logger").Logger()

	for {
		select {
		case <-ctx.Done():
			logger.Info().Msg("producer context has been cancelled: shutting down delivery report logging")
			return
		case e := <-deliveryChan:
			switch event := e.(type) {
			case *kafka.Message:
				m := event
				if m.TopicPartition.Error != nil {
					logger.Error().Err(m.TopicPartition.Error).Msg("delivery failed")
					p.metrics.UnsuccessfullyProducedMsgInc()

					continue
				}

				logger.Debug().Any("kafka_message", m).Msg("received a delivery report")
				p.metrics.SuccessfullyProducedMsgInc()
			case kafka.Error:
				logger.Error().Err(event).Msg("generic kafka client error")
				p.metrics.UnsuccessfullyProducedMsgInc()
			default:
				logger.Trace().Any("kafka_event", event).Msg("ignored event")
				p.metrics.UnsuccessfullyProducedMsgInc()
			}
		}
	}
}
