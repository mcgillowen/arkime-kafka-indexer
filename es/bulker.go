// Copyright 2023 Owen McGill
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package es

import (
	"fmt"
	"time"

	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
)

// bulker is used for bulking individual messages together.
//
// Bulking messages together helps with ES performance.
//
// The bulker has 3 configurable limits that decide when enough messages
// have been bulked together, elapsed time, number of messages and number
// of bytes bulked together.
type bulker struct {
	flushCb func(buffer *bytebufferpool.ByteBuffer)

	msgLimit   int
	bytesLimit int
	timeLimit  time.Duration

	bulkedMsgCount int

	msgPool    bufferPool
	bufferPool bufferPool
	buffer     *bytebufferpool.ByteBuffer

	lastFlush time.Time

	metrics bulkerMetrics
	logger  zerolog.Logger
}

// newBulker is for creating a bulker instance and initialising required
// fields.
// The flushCb argument should return the `buffer` to the pool
// (bufferPool arg) using the Put(msg *bytebufferpool.ByteBuffer) method.
func newBulker(
	flushCb func(buffer *bytebufferpool.ByteBuffer),
	msgLimit, bytesLimit int,
	timeLimit time.Duration,
	msgPool, bufferPool bufferPool,
	metrics bulkerMetrics,
	logger zerolog.Logger,
) *bulker {
	return &bulker{
		flushCb:    flushCb,
		msgLimit:   msgLimit,
		bytesLimit: bytesLimit,
		timeLimit:  timeLimit,
		msgPool:    msgPool,
		bufferPool: bufferPool,
		buffer:     bufferPool.Get(),
		metrics:    metrics,
		logger:     logger,
	}
}

// bulk adds the msg argument to a buffer and when the limits are reached
// flushes the bufffer and starts a new buffer for the following messages.
func (b *bulker) bulk(msg *bytebufferpool.ByteBuffer) error {
	if b.timeLimit < time.Since(b.lastFlush) && 0 < b.bulkedMsgCount {
		b.flush("time_limit")
	}

	if msg == nil {
		return nil
	}

	if b.bytesLimit < b.buffer.Len()+msg.Len() {
		b.flush("bytes_limit")
	}

	_, err := msg.WriteTo(b.buffer)
	if err != nil {
		return fmt.Errorf("writing to bulk buffer: %w", err)
	}

	b.msgPool.Put(msg)

	b.bulkedMsgCount++

	if b.msgLimit == b.bulkedMsgCount {
		b.flush("msg_limit")
	}

	return nil
}

// flush sets all the correct metrics, logs, flushes the buffer by calling the
// flushCb and resets the buffer for the next batch.
func (b *bulker) flush(reason string) {
	b.metrics.FlushReason(reason)
	flushedBytes := b.buffer.Len()
	b.logger.Debug().
		Int("flushed_msgs", b.bulkedMsgCount).
		Int("flushed_bytes", flushedBytes).
		Str("flush_reason", reason).
		Msg("flushing buffer")
	b.metrics.FlushedBytes(float64(flushedBytes))
	b.metrics.FlushedMsgs(float64(b.bulkedMsgCount))
	b.flushCb(b.buffer)
	b.buffer = b.bufferPool.Get()
	b.lastFlush = time.Now()
	b.bulkedMsgCount = 0
}

func (b *bulker) finalFlush() {
	b.flush("shutdown")
}
