package impl

import (
	"errors"
	"time"
)

var ErrConsumeTimeout = errors.New("timed out waiting for message")

const defaultPollTimeout = 100 * time.Millisecond

type ConsumerMetrics interface {
	ConsumedMsgSize(size float64)
}

type Message struct {
	content []byte
}
