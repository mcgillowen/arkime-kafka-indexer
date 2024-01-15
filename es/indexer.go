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

package es

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v7"
	"github.com/elastic/go-elasticsearch/v7/esapi"
	"github.com/elastic/go-elasticsearch/v7/estransport"
	"github.com/elastic/go-elasticsearch/v7/esutil"
	"github.com/goccy/go-json"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"
	"github.com/valyala/bytebufferpool"
)

type Indexer struct {
	indexerConfig indexerConfig
	bulkPool      bufferPool
	msgPool       bufferPool
	metrics       indexerMetrics
	logger        zerolog.Logger
	client        *elasticsearch.Client
}

var errPingingCluster = errors.New("error pinging cluster, got a non-200 status code back")

func testClientConnection(client *elasticsearch.Client) error {
	// Ping the elastic cluster to ensure that the client can communicate before
	// trying to read a message from the channel.
	resp, err := client.Ping()
	if err != nil {
		return fmt.Errorf("error pinging cluster: %w", err)
	}
	// Response body needs to be fully read before closing, so that
	// the connection can be reused as described here https://golang.org/pkg/net/http/#Response
	// It verifies the network connection did not fail
	_, err = io.Copy(io.Discard, resp.Body)
	if err != nil {
		return fmt.Errorf("error discarding response body: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to test client connection: %w", errPingingCluster)
	}

	return nil
}

func newElasticClient(cfg *elasticsearch.Config) (*elasticsearch.Client, error) {
	client, err := elasticsearch.NewClient(*cfg)
	if err != nil {
		return nil, fmt.Errorf("error creating elastic client: %w", err)
	}

	err = testClientConnection(client)
	if err != nil {
		return nil, fmt.Errorf("error testing client connection: %w", err)
	}

	return client, err
}

type indexerConfig struct {
	esConfig          *elasticsearch.Config
	collectorFunc     func(*elasticsearch.Client) prometheus.Collector
	promRegistry      *prometheus.Registry
	bulkTimeout       time.Duration
	flushInterval     time.Duration
	maxBufferedMsgs   int
	maxBufferedBytes  int
	maxDeadPercentage int
}

const (
	bulkMaxBytesLimit        = 90 * 1024 * 1024 // ES bulk call limit is 100mb, so setting limit below that
	defaultBulkTimeout       = 100 * time.Millisecond
	defaultFlushTimeout      = 10 * time.Second
	defaultMaxBufferedMsgs   = 100
	defaultMaxBufferedBytes  = 10 * 1024 * 1024 // 10Mb
	defaultMaxDeadPercentage = 10
	defaultDiscoverInterval  = time.Hour
	minimumBufferSize        = 5 // chose 5 since it's the minimum required for a bulk action, 2 empty JSON bodies ({}) with a line feed

	httpScheme  = "http"
	httpsScheme = "https"
)

// NewIndexer creates a new Indexer instance with the provided client.
func NewIndexer(
	endpoint, port string,
	useHTTPS bool,
	msgPool *bytebufferpool.Pool,
	metrics indexerMetrics,
	logger zerolog.Logger,
	opts ...IndexerOption,
) (*Indexer, error) {
	addressScheme := httpScheme
	if useHTTPS {
		addressScheme = httpsScheme
	}

	indexerCfg := indexerConfig{
		esConfig: &elasticsearch.Config{
			DiscoverNodesOnStart:  true,
			DiscoverNodesInterval: defaultDiscoverInterval,
			Addresses:             []string{fmt.Sprintf("%s://%s", addressScheme, net.JoinHostPort(endpoint, port))},
		},
		bulkTimeout:       defaultBulkTimeout,
		flushInterval:     defaultFlushTimeout,
		maxBufferedMsgs:   defaultMaxBufferedMsgs,
		maxBufferedBytes:  defaultMaxBufferedBytes,
		maxDeadPercentage: defaultMaxDeadPercentage,
	}

	for _, opt := range opts {
		opt(&indexerCfg)
	}

	client, err := newElasticClient(indexerCfg.esConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating new elastic client: %w", err)
	}

	indexer := &Indexer{
		bulkPool:      &bytebufferpool.Pool{},
		msgPool:       msgPool,
		client:        client,
		indexerConfig: indexerCfg,
		metrics:       metrics,
		logger:        logger,
	}

	if indexerCfg.esConfig.EnableMetrics {
		esCollector := indexerCfg.collectorFunc(client)

		err = indexerCfg.promRegistry.Register(esCollector)
		if err != nil {
			return nil, fmt.Errorf("error registering Prometheus collector: %w", err)
		}
	}

	return indexer, nil
}

// Start starts the indexing of the messages coming from the sending channel.
//
// If the inbound/sending channel is closed the indexer shutsdown.
//
// This should be run in a Goroutine, preferably using the golang.org/x/sync/errgroup for
// grouping all the concurrent Goroutines.
func (i *Indexer) Start(
	consumerChan <-chan *bytebufferpool.ByteBuffer,
	errorChan chan<- *bytebufferpool.ByteBuffer,
	instanceIndex int,
) error {
	logger := i.logger.With().Int("instance", instanceIndex).Logger()

	if errorChan != nil {
		defer close(errorChan)
	}

	defer logger.Info().Msg("indexer has stopped")

	ticker := time.NewTicker(i.indexerConfig.flushInterval)
	defer ticker.Stop()

	sendErrors := func(in *bytebufferpool.ByteBuffer) {
		if in == nil {
			return
		}

		if errorChan == nil {
			i.msgPool.Put(in)
			return
		}

		if in.Len() < 1 {
			i.msgPool.Put(in)
			return
		}
		errorChan <- in
	}

	flushBuffer := func(buffer *bytebufferpool.ByteBuffer) {
		// sends the buffer to ES and any indexing errors are then sent further on
		sendErrors(i.sendToES(buffer, logger))
		ticker.Reset(i.indexerConfig.flushInterval)
	}

	bulker := newBulker(
		flushBuffer,
		i.indexerConfig.maxBufferedMsgs,
		i.indexerConfig.maxBufferedBytes,
		i.indexerConfig.flushInterval,
		i.msgPool,
		i.bulkPool,
		i.metrics,
		i.logger.With().Str("component", "bulker").Logger(),
	)

	for {
		select {
		case msg, ok := <-consumerChan:
			if !ok {
				logger.Info().Msg("consumer channel is closed, final flush of buffer to ES")
				bulker.finalFlush()

				return nil
			}

			logger.Debug().Msg("read message from channel")

			if logger.GetLevel() == zerolog.TraceLevel {
				logger.Trace().Bytes("msg", msg.B).Send()
			}

			if err := bulker.bulk(msg); err != nil {
				return fmt.Errorf("bulking after reading from channel: %w", err)
			}
		case <-ticker.C:
			if err := i.CheckESStatus(); err != nil {
				return fmt.Errorf("checking ES status after ticker ticked: %w", err)
			}

			if err := bulker.bulk(nil); err != nil {
				return fmt.Errorf("bulking after ticker ticked: %w", err)
			}

			ticker.Reset(i.indexerConfig.flushInterval)
		}
	}
}

func (i *Indexer) sendToES(in *bytebufferpool.ByteBuffer, logger zerolog.Logger) *bytebufferpool.ByteBuffer {
	defer i.bulkPool.Put(in)

	indexedDocsBytes := len(in.B)
	if indexedDocsBytes < minimumBufferSize {
		return nil
	}

	i.metrics.BulkIndexCountInc()

	resp, err := i.client.Bulk(bytes.NewReader(in.B), i.client.Bulk.WithTimeout(i.indexerConfig.bulkTimeout))
	if err != nil {
		logger.Error().Err(err).Msgf("error sending bulk request")
		i.metrics.BulkIndexErrorCountInc()

		return nil
	}

	defer resp.Body.Close()
	logger.Debug().Msg("sent bulk request")

	if resp.IsError() {
		i.handleResponseError(resp, logger)
	}

	jsonBody := esutil.BulkIndexerResponse{}

	err = json.NewDecoder(resp.Body).Decode(&jsonBody)
	if err != nil {
		logger.Error().Err(err).Msg("error unmarshaling response body")
		return nil
	}

	if !jsonBody.HasErrors {
		i.metrics.IndexedDocumentsCountAdd(float64(len(jsonBody.Items)))
		i.metrics.IndexedDocumentsBytesAdd(float64(indexedDocsBytes))

		return nil
	}

	return i.parseESErrors(in.B, jsonBody, logger)
}

func (i *Indexer) handleResponseError(resp *esapi.Response, logger zerolog.Logger) {
	i.metrics.BulkIndexErrorCountInc()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Error().Err(err).Msgf("error reading response body")
		return
	}

	logger.Error().Bytes("body", body).Msgf("request failed")
}

func (i *Indexer) parseESErrors(bulkBytes []byte, jsonBody esutil.BulkIndexerResponse, logger zerolog.Logger) *bytebufferpool.ByteBuffer {
	out := i.msgPool.Get()
	failedItems := 0
	bulkLines := bytes.SplitAfter(bulkBytes, []byte{'\n'})

	for index, item := range jsonBody.Items {
		for action, actionItem := range item {
			if actionItem.Error.Type == "" {
				continue
			}

			_, err := out.Write(bulkLines[index*2])
			if err != nil {
				logger.Error().Err(err).Msg("failed to write ES action line from error to buffer")
			}

			_, err = out.Write(bulkLines[index*2+1])
			if err != nil {
				logger.Error().Err(err).Msg("failed to write ES content line from error to buffer")
			}

			logger.Error().
				Str("es_action", action).
				Str("es_index", actionItem.Index).
				Str("es_id", actionItem.DocumentID).
				Any("error", actionItem.Error).
				Msg("error in bulk request")

			failedItems++

			i.metrics.SessionIndexingFailCountInc()
		}
	}

	i.metrics.IndexedDocumentsCountAdd(float64(len(jsonBody.Items) - failedItems))
	i.metrics.IndexedDocumentsBytesAdd(float64(len(bulkBytes) - out.Len()))

	return out
}

func (i *Indexer) CheckESStatus() error {
	m, err := i.client.Metrics()
	if err != nil {
		return fmt.Errorf("getting metrics: %w", err)
	}

	totalConnections := len(m.Connections)
	deadConnections := 0

	for _, connectionStringer := range m.Connections {
		cm, ok := connectionStringer.(estransport.ConnectionMetric)
		if !ok {
			deadConnections++
		}

		if cm.IsDead {
			deadConnections++
		}
	}

	deadPercentage := int(float32(deadConnections) / float32(totalConnections) * 100) //nolint:gomnd // standard multiplier for percentage

	i.logger.Debug().
		Int("total_connections", totalConnections).
		Int("dead_connections", deadConnections).
		Int("dead_percentage", deadPercentage).
		Msg("ES connections status")

	// if percentage of dead connections is less than the maxmimum do nothing
	if deadPercentage < i.indexerConfig.maxDeadPercentage {
		i.logger.Debug().Msg("dead percentage below threshold")
		return nil
	}

	i.logger.Info().Msg("reloading client because of too many dead connections")

	// if percentage of dead connections is higher than the allowed maximum
	// percentage then reload the client
	err = i.client.DiscoverNodes()
	if err != nil {
		return fmt.Errorf("reloading client: %w", err)
	}

	i.logger.Debug().Msg("client reloaded because too many dead connections")

	return nil
}
