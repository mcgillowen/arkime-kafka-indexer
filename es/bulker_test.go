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

package es //nolint:testpackage // testing unexported types

import (
	"testing"
	"time"

	"github.com/rzajac/zltest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/valyala/bytebufferpool"
)

type mockBulkerMetrics struct {
	mock.Mock
}

func (m *mockBulkerMetrics) FlushReason(reason string) { m.Called(reason) }
func (m *mockBulkerMetrics) FlushedBytes(num int)      { m.Called(num) }
func (m *mockBulkerMetrics) FlushedMsgs(num int)       { m.Called(num) }

type mockBulkerPool struct{}

func (mockBulkerPool) Get() *bytebufferpool.ByteBuffer  { return &bytebufferpool.ByteBuffer{} }
func (mockBulkerPool) Put(_ *bytebufferpool.ByteBuffer) {}

func Test_bulker_flush(t *testing.T) {
	t.Parallel()

	type fields struct {
		flushCb      func(t *testing.T) func(buffer *bytebufferpool.ByteBuffer)
		buffer       *bytebufferpool.ByteBuffer
		flushReason  string
		flushedBytes int
		flushedMsgs  int
	}

	type args struct {
		reason string
	}

	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		{
			name: "empty buffer",
			fields: fields{
				func(t *testing.T) func(buffer *bytebufferpool.ByteBuffer) {
					t.Helper()
					return func(buffer *bytebufferpool.ByteBuffer) {
						assert.Equal(t, []byte{}, buffer.B, "buffer should be empty")
					}
				},
				&bytebufferpool.ByteBuffer{
					B: []byte{},
				},
				"",
				0,
				0,
			},
			args: args{
				reason: "",
			},
		},
		{
			name: "non-empty buffer",
			fields: fields{
				func(t *testing.T) func(buffer *bytebufferpool.ByteBuffer) {
					t.Helper()
					return func(buffer *bytebufferpool.ByteBuffer) {
						assert.Equal(t, []byte("test"), buffer.B, "buffer should be non-empty")
					}
				},
				&bytebufferpool.ByteBuffer{
					B: []byte("test"),
				},
				"test",
				4,
				0,
			},
			args: args{
				reason: "test",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tst := zltest.New(t)
			mockMetrics := new(mockBulkerMetrics)

			mockMetrics.On("FlushReason", tt.fields.flushReason)
			mockMetrics.On("FlushedBytes", tt.fields.flushedBytes)
			mockMetrics.On("FlushedMsgs", tt.fields.flushedMsgs)

			bulker := &bulker{
				flushCb:    tt.fields.flushCb(t),
				bufferPool: mockBulkerPool{},
				buffer:     tt.fields.buffer,
				metrics:    mockMetrics,
				logger:     tst.Logger(),
			}
			bulker.flush(tt.args.reason)

			mockMetrics.AssertExpectations(t)
		})
	}
}

func Test_bulker_bulk(t *testing.T) {
	t.Parallel()

	type fields struct {
		flushCb      func(t *testing.T) func(buffer *bytebufferpool.ByteBuffer)
		msgLimit     int
		bytesLimit   int
		timeLimit    time.Duration
		flushReason  string
		flushedBytes int
		flushedMsgs  int
	}

	type args struct {
		msgs []*bytebufferpool.ByteBuffer
	}

	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr bool
	}{
		{
			name: "2 empty msgs, flushing because of time limit",
			fields: fields{
				func(t *testing.T) func(buffer *bytebufferpool.ByteBuffer) {
					t.Helper()
					return func(buffer *bytebufferpool.ByteBuffer) {
						assert.Equal(t, []byte{}, buffer.B, "buffer should be empty")
					}
				},
				10,
				1,
				time.Nanosecond,
				"time_limit",
				0,
				1,
			},
			args: args{
				msgs: []*bytebufferpool.ByteBuffer{
					{B: []byte{}},
					{B: []byte{}},
				},
			},
		},
		{
			name: "1 empty msgs, flushing because of msg limit",
			fields: fields{
				func(t *testing.T) func(buffer *bytebufferpool.ByteBuffer) {
					t.Helper()
					return func(buffer *bytebufferpool.ByteBuffer) {
						assert.Equal(t, []byte{}, buffer.B, "buffer should be empty")
					}
				},
				1,
				1,
				time.Hour,
				"msg_limit",
				0,
				1,
			},
			args: args{
				msgs: []*bytebufferpool.ByteBuffer{
					{B: []byte{}},
				},
			},
		},
		{
			name: "2 non-empty msgs, flushing because of bytes limit",
			fields: fields{
				func(t *testing.T) func(buffer *bytebufferpool.ByteBuffer) {
					t.Helper()
					return func(buffer *bytebufferpool.ByteBuffer) {
						assert.Equal(t, []byte("test"), buffer.B, "buffer should contain 'test'")
					}
				},
				10,
				5,
				time.Hour,
				"bytes_limit",
				4,
				1,
			},
			args: args{
				msgs: []*bytebufferpool.ByteBuffer{
					{B: []byte("test")},
					{B: []byte("unflushed")},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tst := zltest.New(t)

			mockMetrics := new(mockBulkerMetrics)

			mockMetrics.On("FlushReason", tt.fields.flushReason)
			mockMetrics.On("FlushedBytes", tt.fields.flushedBytes)
			mockMetrics.On("FlushedMsgs", tt.fields.flushedMsgs)

			bulker := &bulker{
				flushCb:    tt.fields.flushCb(t),
				msgLimit:   tt.fields.msgLimit,
				bytesLimit: tt.fields.bytesLimit,
				timeLimit:  tt.fields.timeLimit,
				lastFlush:  time.Now(),
				msgPool:    mockBulkerPool{},
				bufferPool: mockBulkerPool{},
				buffer:     &bytebufferpool.ByteBuffer{B: []byte{}},
				metrics:    mockMetrics,
				logger:     tst.Logger(),
			}
			for i, msg := range tt.args.msgs {
				if err := bulker.bulk(msg); (err != nil) != tt.wantErr {
					t.Errorf("bulker.bulk() (msg # %d) error = %v, wantErr %v", i, err, tt.wantErr)
				}
			}

			mockMetrics.AssertExpectations(t)
		})
	}
}
