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

package metrics

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/elastic/elastic-transport-go/v8/elastictransport"
	"github.com/elastic/go-elasticsearch/v8"
	"github.com/prometheus/client_golang/prometheus"
)

// ESCollector collects the metrics from the ES client so that we can serve them with the
// rest of the metrics.
// It implements the prometheus.Collector interface.
type ESCollector struct {
	client *elasticsearch.Client

	// metrics descriptions
	invalidMetric            *prometheus.Desc
	clientRequestsMetric     *prometheus.Desc
	clientFailuresMetric     *prometheus.Desc
	clientResponsesMetric    *prometheus.Desc
	invalidConnectionMetric  *prometheus.Desc
	connectionFailuresMetric *prometheus.Desc
	deadConnectionsMetric    *prometheus.Desc
	totalConnectionsMetric   *prometheus.Desc
}

var (
	errInvalidMetrics    = errors.New("error getting metrics from client")
	errInvalidConnection = errors.New("error getting connection-specific metrics")
)

const (
	namespace = "es"
	subsys    = "client"
)

// NewESCollectorFunc creates a function that takes an *elasticsearch.Client and returns a prometheus.Collector,
// the namespace and subsystem for the prometheus metrics must be provided at creation time.
func NewESCollectorFunc() func(*elasticsearch.Client) prometheus.Collector {
	return func(client *elasticsearch.Client) prometheus.Collector {
		return &ESCollector{
			client:                  client,
			invalidMetric:           prometheus.NewInvalidDesc(errInvalidMetrics),
			invalidConnectionMetric: prometheus.NewInvalidDesc(errInvalidConnection),
			clientRequestsMetric: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsys, "requests_total"),
				"Number of requests performed by the ES client",
				nil,
				nil,
			),
			clientFailuresMetric: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsys, "failures_total"),
				"Number of failed requests performed by the ES client",
				nil,
				nil,
			),
			clientResponsesMetric: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsys, "responses_total"),
				"Number of responses received by the ES client, categorised by status code",
				[]string{"status_code"},
				nil,
			),
			connectionFailuresMetric: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsys, "connection_failures_total"),
				"Number of failed requests performed by the ES client, categorised by the URL",
				[]string{"url"},
				nil,
			),
			totalConnectionsMetric: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsys, "total_connections_total"),
				"Number of total connections in connection pool",
				nil,
				nil,
			),
			deadConnectionsMetric: prometheus.NewDesc(
				prometheus.BuildFQName(namespace, subsys, "dead_connections_total"),
				"Number of dead connections in connection pool",
				nil,
				nil,
			),
		}
	}
}

// Describe is the ESCollector implementation of the Describe(chan<- *prometheus.Desc)
// defined by the promtheus.Collector interface.
func (ec *ESCollector) Describe(ch chan<- *prometheus.Desc) {
	metrics, err := ec.client.Metrics()
	if err != nil {
		ch <- ec.invalidMetric
		return
	}

	ch <- ec.clientRequestsMetric
	ch <- ec.clientFailuresMetric

	ch <- ec.clientResponsesMetric

	for _, connectionStringer := range metrics.Connections {
		_, ok := connectionStringer.(elastictransport.ConnectionMetric)
		if !ok {
			ch <- ec.invalidConnectionMetric
		}
		ch <- ec.connectionFailuresMetric
	}

	ch <- ec.totalConnectionsMetric
	ch <- ec.deadConnectionsMetric
}

// Collect is the ESCollector implementation of the Collect(chan<- *prometheus.Collect)
// defined by the promtheus.Collector interface.
func (ec *ESCollector) Collect(ch chan<- prometheus.Metric) {
	metrics, err := ec.client.Metrics()
	if err != nil {
		ch <- prometheus.NewInvalidMetric(ec.invalidMetric, fmt.Errorf("error getting metrics: %w", err))
		return
	}

	ch <- prometheus.MustNewConstMetric(ec.clientRequestsMetric, prometheus.GaugeValue, float64(metrics.Requests))
	ch <- prometheus.MustNewConstMetric(ec.clientFailuresMetric, prometheus.GaugeValue, float64(metrics.Failures))

	for statusCode, count := range metrics.Responses {
		ch <- prometheus.MustNewConstMetric(
			ec.clientResponsesMetric,
			prometheus.GaugeValue,
			float64(count),
			strconv.Itoa(statusCode),
		)
	}

	totalConnections := len(metrics.Connections)
	deadConnections := 0

	for _, connectionStringer := range metrics.Connections {
		connection, ok := connectionStringer.(elastictransport.ConnectionMetric)
		if !ok {
			ch <- prometheus.NewInvalidMetric(ec.invalidConnectionMetric, errInvalidConnection)

			deadConnections++

			continue
		}
		ch <- prometheus.MustNewConstMetric(
			ec.connectionFailuresMetric,
			prometheus.GaugeValue,
			float64(connection.Failures),
			connection.URL,
		)

		if connection.IsDead {
			deadConnections++
		}
	}

	ch <- prometheus.MustNewConstMetric(
		ec.totalConnectionsMetric,
		prometheus.GaugeValue,
		float64(totalConnections),
	)

	ch <- prometheus.MustNewConstMetric(
		ec.deadConnectionsMetric,
		prometheus.GaugeValue,
		float64(deadConnections),
	)
}
