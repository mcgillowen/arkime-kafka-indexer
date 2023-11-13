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

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/rs/zerolog"
	"github.com/rzajac/zltest"
	"github.com/stretchr/testify/assert"
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
	mocktrans.RoundTripFn = func(req *http.Request) (*http.Response, error) { return mocktrans.Response, nil }

	esClient, err := elasticsearch.NewClient(elasticsearch.Config{
		Transport:            &mocktrans,
		UseResponseCheckOnly: true,
	})
	assert.NoError(t, err, "there should be no error creating the ES client")

	indexer := &Indexer{
		bulkPool: mockIndexerPool{},
		msgPool:  mockIndexerPool{},
		client:   esClient,
	}

	t.Run("no error response", func(t *testing.T) {
		tst := zltest.New(t)

		mocktrans.Response.Body = io.NopCloser(strings.NewReader(`{"took":0,"errors":false}`))

		mockMetrics := new(mockIndexerMetrics)

		indexer.metrics = mockMetrics
		indexer.logger = tst.Logger()

		var (
			inBuffer  = &bytebufferpool.ByteBuffer{}
			outBuffer *bytebufferpool.ByteBuffer
		)

		if got := indexer.sendToES(inBuffer, tst.Logger()); !reflect.DeepEqual(got, outBuffer) {
			t.Errorf("Indexer.sendToES() = %v, want %v", got, outBuffer)
		}

		errorEntries := tst.Filter(zerolog.ErrorLevel)
		errorEntries.ExpLen(0)

		mockMetrics.AssertExpectations(t)
	})

	t.Run("1 failed index", func(t *testing.T) {
		tst := zltest.New(t)

		bufferString := `{}
{testspi}`
		responseString := `{
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
]}`

		mocktrans.Response.Body = io.NopCloser(strings.NewReader(responseString))
		mocktrans.RoundTripFn = func(req *http.Request) (*http.Response, error) {
			require.Equal(t, http.MethodPost, req.Method, "request needs to be a POST")
			require.NotNil(t, req.Body, "body cannot be nil")
			reqBody, err := io.ReadAll(req.Body)
			require.NoError(t, err, "no error reading body")
			require.Equal(t, []byte(bufferString), reqBody, "request body should match sent in buffer")

			return mocktrans.Response, nil
		}

		mockMetrics := new(mockIndexerMetrics)

		mockMetrics.On("BulkIndexCountInc")
		mockMetrics.On("IndexedDocumentsCountAdd", float64(0))
		mockMetrics.On("IndexedDocumentsBytesAdd", float64(0))
		mockMetrics.On("SessionIndexingFailCountInc")

		indexer.metrics = mockMetrics
		indexer.logger = tst.Logger()

		var (
			inBuffer  = &bytebufferpool.ByteBuffer{B: []byte(bufferString)}
			outBuffer = &bytebufferpool.ByteBuffer{B: []byte("{}\n{testspi}")}
		)

		if got := indexer.sendToES(inBuffer, tst.Logger()); !reflect.DeepEqual(got, outBuffer) {
			t.Errorf("Indexer.sendToES() = %v, want %v", got, outBuffer)
		}

		tst.Entries().Print()

		errorEntries := tst.Filter(zerolog.ErrorLevel)
		errorEntries.ExpLen(1)

		mockMetrics.AssertExpectations(t)
	})
}
