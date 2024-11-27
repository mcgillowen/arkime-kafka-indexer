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

package es //nolint:testpackage // testing unexported method

import (
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/rs/zerolog"
	"github.com/rzajac/zltest"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"github.com/valyala/bytebufferpool"
)

type mockIndexerMetrics struct {
	mockBulkerMetrics
	mock.Mock
}

func (m *mockIndexerMetrics) BulkIndexCountInc()                   { m.Called() }
func (m *mockIndexerMetrics) BulkIndexErrorCountInc()              { m.Called() }
func (m *mockIndexerMetrics) IndexedDocumentsCountAdd(num float64) { m.Called(num) }
func (m *mockIndexerMetrics) IndexedDocumentsBytesAdd(num float64) { m.Called(num) }
func (m *mockIndexerMetrics) SessionIndexingFailCountInc()         { m.Called() }
func (m *mockIndexerMetrics) ESClientReloadInc()                   { m.Called() }

type mockIndexerPool struct{}

func (mockIndexerPool) Get() *bytebufferpool.ByteBuffer  { return &bytebufferpool.ByteBuffer{} }
func (mockIndexerPool) Put(_ *bytebufferpool.ByteBuffer) {}

type mockTransport struct {
	Response    *http.Response
	RoundTripFn func(req *http.Request) (*http.Response, error)
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.RoundTripFn(req)
}

func TestIndexer_sendToES(t *testing.T) {
	mocktrans := mockTransport{
		Response: &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(`{}`)),
			Header:     http.Header{"X-Elastic-Product": []string{"Elasticsearch"}},
		},
	}

	esClient, err := elasticsearch.NewClient(elasticsearch.Config{
		Transport: &mocktrans,
	})
	require.NoError(t, err, "there should be no error creating the ES client")

	indexer := &Indexer{
		bulkPool: mockIndexerPool{},
		msgPool:  mockIndexerPool{},
		client:   esClient,
	}

	type buffers struct {
		in, out *bytebufferpool.ByteBuffer
	}

	type testCase struct {
		name               string
		responseBody       io.ReadCloser
		roundtripFn        func(req *http.Request) (*http.Response, error)
		mockCalls          map[string][]interface{}
		buffers            buffers
		numLogErrorEntries int
	}

	testCases := []testCase{
		{
			name:         "no error response",
			responseBody: io.NopCloser(strings.NewReader(`{"took":0,"errors":false}`)),
			roundtripFn:  func(_ *http.Request) (*http.Response, error) { return mocktrans.Response, nil },
			mockCalls:    map[string][]interface{}{},
			buffers: buffers{
				in: &bytebufferpool.ByteBuffer{},
			},
			numLogErrorEntries: 0,
		},
		{
			name: "1 failed index",
			responseBody: io.NopCloser(strings.NewReader(`{
"took":0,
"errors":true,
"items":[
	{
		"index":{
			"_index":"test",
			"_id":"test",
			"status":200,
			"error":{
				"type":"test",
				"reason":"test"
			}
		}
	}
]}`)),
			roundtripFn: func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, req.Method, "request needs to be a POST")
				require.NotNil(t, req.Body, "body cannot be nil")
				reqBody, err := io.ReadAll(req.Body)
				require.NoError(t, err, "error reading body")
				require.Equal(t, []byte(`{"index": {"_index": "test", "_id": "test"}}
{"test":"spi"}`), reqBody, "request body should match sent in buffer")

				return mocktrans.Response, nil
			},
			mockCalls: map[string][]interface{}{
				"BulkIndexCountInc":           {},
				"IndexedDocumentsCountAdd":    {float64(0)},
				"IndexedDocumentsBytesAdd":    {float64(0)},
				"SessionIndexingFailCountInc": {},
			},
			buffers: buffers{
				in: &bytebufferpool.ByteBuffer{B: []byte(`{"index": {"_index": "test", "_id": "test"}}
{"test":"spi"}`)},
				out: &bytebufferpool.ByteBuffer{B: []byte(`{"index": {"_index": "test", "_id": "test"}}
{"test":"spi"}`)},
			},
			numLogErrorEntries: 1,
		},
		{
			name: "1 failed create",
			responseBody: io.NopCloser(strings.NewReader(`{
"took":0,
"errors":true,
"items":[
	{
		"create":{
			"_index":"test",
			"_id":"test",
			"status":200,
			"error":{
				"type":"test",
				"reason":"test"
			}
		}
	}
]}`)),
			roundtripFn: func(req *http.Request) (*http.Response, error) {
				require.Equal(t, http.MethodPost, req.Method, "request needs to be a POST")
				require.NotNil(t, req.Body, "body cannot be nil")
				reqBody, err := io.ReadAll(req.Body)
				require.NoError(t, err, "error reading body")
				require.Equal(t, []byte(`{"create": {"_index": "test", "_id": "test"}}
{"test":"spi"}`), reqBody, "request body should match sent in buffer")

				return mocktrans.Response, nil
			},
			mockCalls: map[string][]interface{}{
				"BulkIndexCountInc":           {},
				"IndexedDocumentsCountAdd":    {float64(0)},
				"IndexedDocumentsBytesAdd":    {float64(0)},
				"SessionIndexingFailCountInc": {},
			},
			buffers: buffers{
				in: &bytebufferpool.ByteBuffer{B: []byte(`{"create": {"_index": "test", "_id": "test"}}
{"test":"spi"}`)},
				out: &bytebufferpool.ByteBuffer{B: []byte(`{"create": {"_index": "test", "_id": "test"}}
{"test":"spi"}`)},
			},
			numLogErrorEntries: 1,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			tst := zltest.New(t)

			mocktrans.Response.Body = testCase.responseBody
			mocktrans.RoundTripFn = testCase.roundtripFn

			mockMetrics := new(mockIndexerMetrics)

			for name, args := range testCase.mockCalls {
				mockMetrics.On(name, args...)
			}

			indexer.metrics = mockMetrics
			indexer.logger = tst.Logger()

			if got := indexer.sendToES(testCase.buffers.in, tst.Logger()); !reflect.DeepEqual(got, testCase.buffers.out) {
				t.Errorf("Indexer.sendToES() = %v, want %v", got, testCase.buffers.out)
			}

			errorEntries := tst.Filter(zerolog.ErrorLevel)
			errorEntries.ExpLen(testCase.numLogErrorEntries)

			mockMetrics.AssertExpectations(t)
		})
	}
}
