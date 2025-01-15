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
	"math"
	"net/http"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/rs/zerolog"

	"github.com/mcgillowen/arkime-kafka-indexer/metrics"
)

// WithTransport allows setting a custom http.RoundTripper instance.
func WithTransport(transport http.RoundTripper) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.Transport = transport
	}
}

// WithRetryStatuses sets the HTTP Status Codes that should be retried.
func WithRetryStatuses(statuses ...int) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.RetryOnStatus = statuses
	}
}

// WithMaxRetries allows specifying the maximum number of retries to attempt.
func WithMaxRetries(retries int) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.MaxRetries = retries
	}
}

// WithRetryBackoffFunc sets the retry backoff function, that takes the current
// attempt count and returns a backoff duration.
// The retry backoff func is also the best place to add retry metrics.
func WithRetryBackoffFunc(retryBackoffFunc func(attempt int) time.Duration) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.RetryBackoff = retryBackoffFunc
	}
}

// RetryBackoffFunc returns a function for use as a retry backoff function,
// it uses an exponential backoff and increments the retry metrics.
func RetryBackoffFunc(
	minimumBackoff time.Duration,
	logger zerolog.Logger,
	metrics *metrics.Prometheus,
) func(attempt int) time.Duration {
	return func(attempt int) time.Duration {
		metrics.BulkCallRetry()

		backoffTime := minimumBackoff + time.Duration(math.Exp2(float64(attempt)))*10*time.Millisecond
		logger.Debug().Dur("backoff_time", backoffTime).Int("attempt", attempt).Msg("retrying")

		return backoffTime
	}
}

// WithCustomLogger sets a logger that implements "estransport.Logger" interface on the client.
func WithCustomLogger(logger zerolog.Logger) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.Logger = &Logger{Logger: logger}
	}
}

// WithESClientMetrics enables the client metrics and registers a prometheus collector
// with the default registry.
func WithESClientMetrics(collectorFunc func(*elasticsearch.Client) prometheus.Collector, promReg *prometheus.Registry) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.EnableMetrics = true
		ic.collectorFunc = collectorFunc
		ic.promRegistry = promReg
	}
}

// WithBulkTimeout sets the timeout duration of the bulk calls.
func WithBulkTimeout(timeout time.Duration) IndexerOption {
	return func(ic *indexerConfig) {
		ic.bulkTimeout = timeout
	}
}

// WithFlushInterval sets the interval between flushes to ES.
func WithFlushInterval(interval time.Duration) IndexerOption {
	return func(ic *indexerConfig) {
		ic.flushInterval = interval
	}
}

// WithMaxBufferedMsgs sets the maximum amount of msgs to be buffered before flushing to ES.
func WithMaxBufferedMsgs(maxMsgs int) IndexerOption {
	return func(ic *indexerConfig) {
		ic.maxBufferedMsgs = maxMsgs
	}
}

// WithMaxBufferedBytes sets the maximum amount of bytes to be buffered before flushing to ES.
func WithMaxBufferedBytes(maxBytes int) IndexerOption {
	return func(ic *indexerConfig) {
		ic.maxBufferedBytes = maxBytes
		if bulkMaxBytesLimit < ic.maxBufferedBytes {
			ic.maxBufferedBytes = bulkMaxBytesLimit
		}
	}
}

// WithMaxDeadPercentage sets the maxmimum allowed percentage of dead
// connections before reloading the connection pool.
func WithMaxDeadPercentage(maxPercentage int) IndexerOption {
	return func(ic *indexerConfig) {
		ic.maxDeadPercentage = maxPercentage
	}
}

func WithDiscoverNodesOnStart(enabled bool) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.DiscoverNodesOnStart = enabled
	}
}

func WithDiscoverNodesInterval(discoverInterval time.Duration) IndexerOption {
	return func(ic *indexerConfig) {
		ic.esConfig.DiscoverNodesInterval = discoverInterval
	}
}

// WithBasicAuth sets the username and password for using basic authentication
// to connect to ES.
func WithBasicAuth(username, password string) IndexerOption {
	return func(ic *indexerConfig) {
		if username != "" && password != "" {
			ic.esConfig.Username = username
			ic.esConfig.Password = password
		}
	}
}

// WithAPIKey sets the API key for ES authentication.
func WithAPIKey(apiKey string) IndexerOption {
	return func(ic *indexerConfig) {
		if apiKey != "" {
			ic.esConfig.APIKey = apiKey
		}
	}
}

// WithServiceToken sets the service token for ES authentication.
func WithServiceToken(serviceToken string) IndexerOption {
	return func(ic *indexerConfig) {
		if serviceToken != "" {
			ic.esConfig.ServiceToken = serviceToken
		}
	}
}
