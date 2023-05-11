package impl

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/twmb/franz-go/pkg/kgo"
)

type FranzConsumer struct {
	bootstrapServer string
	topic           string
	groupName       string
	pollTimeout     time.Duration
	client          *kgo.Client
	metrics         ConsumerMetrics
	logger          zerolog.Logger
}

func NewFranzConsumer(
	bootstrapServer, topic, groupName string,
	sessionTimeout time.Duration,
	metrics ConsumerMetrics, logger zerolog.Logger,
) (*FranzConsumer, error) {
	cons := FranzConsumer{
		pollTimeout:     defaultPollTimeout,
		topic:           topic,
		groupName:       groupName,
		bootstrapServer: bootstrapServer,
		metrics:         metrics,
		logger:          logger,
	}

	client, err := kgo.NewClient(
		kgo.SeedBrokers(bootstrapServer),
		kgo.ConsumeTopics(topic),
		kgo.ConsumerGroup(groupName),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtEnd()),
		kgo.SessionTimeout(sessionTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("creating client: %w", err)
	}

	cons.client = client

	return &cons, nil
}

func (c *FranzConsumer) Consume(ctx context.Context) ([]Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("consuming: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, c.pollTimeout)
	defer cancel()
	fetches := c.client.PollFetches(ctx)

	if fetches.IsClientClosed() {
		return nil, fmt.Errorf("polling for fetches: %w", context.Canceled)
	}
	if errors.Is(fetches.Err0(), context.Canceled) ||
		errors.Is(fetches.Err0(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("polling for fetches: %w", fetches.Err0())
	}

	fetches.EachError(func(t string, p int32, err error) {
		c.logger.Error().
			Err(err).
			Str("topic", t).
			Int32("partition", p).
			Msg("consumer fetches returned error")
	})

	fetches.Records()

	return nil, nil
}
