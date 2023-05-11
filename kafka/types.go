package kafka

import (
	"context"

	"github.com/mcgillowen/arkime-kafka-indexer/kafka/impl"
)

type consumer interface {
	Consume(context.Context) ([]impl.Message, error)
	Shutdown() error
}

var _ consumer

type producer interface {
	Produce([]impl.Message) error
	Shutdown() error
}

var _ producer
