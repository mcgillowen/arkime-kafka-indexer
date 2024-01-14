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

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics struct contains the various prometheus metrics collected by the indexer.
// It implements various *Metrics interfaces defined by the various components of
// the indexer.
type Metrics struct {
	*indexerMetrics
	*bulkerMetrics
	*consumerMetrics
	*producerMetrics
}

type bulkerMetrics struct {
	flushedMsgsHistogram  prometheus.Histogram
	flushedBytesHistogram prometheus.Histogram
	flushReason           prometheus.CounterVec
}

type indexerMetrics struct {
	bulkIndexCounter           prometheus.Counter
	bulkIndexErrorCounter      prometheus.Counter
	bulkIndexRetryCounter      prometheus.Counter
	indexDocumentsCounter      prometheus.Counter
	indexDocumentsBytesCounter prometheus.Counter
	sessionIndexFailureCounter prometheus.Counter
	esClientReloadCounter      prometheus.Counter
}

type consumerMetrics struct {
	msgConsumedSize                prometheus.Histogram
	partitionAssignmentLostCounter prometheus.Counter
	rebalanceCounter               *prometheus.CounterVec
}

type producerMetrics struct {
	msgProducedSize                      prometheus.Histogram
	successfullyProducedMessageCounter   prometheus.Counter
	unsuccessfullyProducedMessageCounter prometheus.Counter
}

// InitPrometheus initialises the Metrics struct with the various metrics and
// registers the metrics with a *prometheus.Registry.
// Returns the initialised Metrics struct and the new Prometheus registry which needs
// to be exposed over HTTP to be collected.
func InitPrometheus(
	namespace, subsys string,
	flushedBytesBuckets, flushedMsgsBuckets []float64,
) (*Metrics, *prometheus.Registry, error) {
	metrics := &Metrics{}
	reg := prometheus.NewPedanticRegistry()
	errs := []error{}

	errs = append(errs,
		reg.Register(collectors.NewBuildInfoCollector()),
		reg.Register(collectors.NewGoCollector()),
		reg.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})),
	)

	bulker, err := initBulkerMetrics(reg, namespace, subsys, flushedMsgsBuckets, flushedBytesBuckets)
	errs = append(errs, err)

	indexer, err := initIndexerMetrics(reg, namespace, subsys)
	errs = append(errs, err)

	consumer, err := initConsumerMetrics(reg, namespace, subsys)
	errs = append(errs, err)

	producer, err := initProducerMetrics(reg, namespace, subsys)
	errs = append(errs, err)

	metrics.bulkerMetrics = bulker
	metrics.indexerMetrics = indexer
	metrics.consumerMetrics = consumer
	metrics.producerMetrics = producer

	if err := errors.Join(errs...); err != nil {
		return nil, nil, fmt.Errorf("failed to register metrics collectors: %w", err)
	}

	return metrics, reg, nil
}

func initBulkerMetrics(registry *prometheus.Registry, namespace, subsys string, flushedMsgsBuckets, flushedBytesBuckets []float64) (*bulkerMetrics, error) {
	metrics := &bulkerMetrics{}

	metrics.flushedMsgsHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:      "bulk_flushed_msgs_total",
		Help:      "Number of msgs flushed when bulking",
		Namespace: namespace,
		Subsystem: subsys,
		Buckets:   flushedMsgsBuckets,
	})

	metrics.flushedBytesHistogram = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:      "bulk_flushed_msgs_bytes",
		Help:      "Amount of bytes flushed when bulking",
		Namespace: namespace,
		Subsystem: subsys,
		Buckets:   flushedBytesBuckets,
	})

	metrics.flushReason = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "bulk_flush_reason_total",
		Help:      "Reason for a bulk flush",
		Namespace: namespace,
		Subsystem: subsys,
	}, []string{"reason"})

	err := errors.Join(
		registry.Register(metrics.flushedMsgsHistogram),
		registry.Register(metrics.flushedBytesHistogram),
		registry.Register(metrics.flushReason),
	)
	if err != nil {
		return nil, fmt.Errorf("registering indexer metrics: %w", err)
	}

	return metrics, nil
}

func initIndexerMetrics(registry *prometheus.Registry, namespace, subsys string) (*indexerMetrics, error) {
	metrics := &indexerMetrics{}

	metrics.bulkIndexCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "bulk_index_total",
		Help:      "Number of Bulk Index calls",
		Namespace: namespace,
		Subsystem: subsys,
	})

	metrics.bulkIndexErrorCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "bulk_index_error_total",
		Help:      "Number of Bulk Index calls with errors",
		Namespace: namespace,
		Subsystem: subsys,
	})

	metrics.bulkIndexRetryCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "bulk_index_retry_total",
		Help:      "Number of Bulk Index calls retried",
		Namespace: namespace,
		Subsystem: subsys,
	})

	metrics.indexDocumentsCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "indexed_documents_total",
		Help:      "Number of indexed documents",
		Namespace: namespace,
		Subsystem: subsys,
	})

	metrics.indexDocumentsBytesCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "indexed_documents_bytes",
		Help:      "Indexed documents total bytes",
		Namespace: namespace,
		Subsystem: subsys,
	})

	metrics.sessionIndexFailureCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "session_index_failure_total",
		Help:      "Session bulk index failure count",
		Namespace: namespace,
		Subsystem: subsys,
	})

	metrics.esClientReloadCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "es_client_reloads_total",
		Help:      "Number of times the ES client was reloaded",
		Namespace: namespace,
		Subsystem: subsys,
	})

	err := errors.Join(
		registry.Register(metrics.bulkIndexCounter),
		registry.Register(metrics.bulkIndexErrorCounter),
		registry.Register(metrics.bulkIndexRetryCounter),
		registry.Register(metrics.indexDocumentsCounter),
		registry.Register(metrics.indexDocumentsBytesCounter),
		registry.Register(metrics.sessionIndexFailureCounter),
		registry.Register(metrics.esClientReloadCounter),
	)
	if err != nil {
		return nil, fmt.Errorf("registering indexer metrics: %w", err)
	}

	return metrics, nil
}

func initConsumerMetrics(registry *prometheus.Registry, namespace, subsys string) (*consumerMetrics, error) {
	metrics := &consumerMetrics{}

	metrics.msgConsumedSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:      "msgs_consumed_bytes",
		Help:      "The size in bytes of the payload consumed",
		Namespace: namespace,
		Subsystem: subsys,
		Buckets:   []float64{256, 384, 512, 768, 1024, 1280, 1536, 1792, 2048, 2560, 3072, 4096},
	})

	metrics.partitionAssignmentLostCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "partition_assignment_lost_total",
		Help:      "Total number of lost partition assignments",
		Namespace: namespace,
		Subsystem: subsys,
	})

	metrics.rebalanceCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "rebalance_total",
		Help:      "Total number of rebalances",
		Namespace: namespace,
		Subsystem: subsys,
	}, []string{"reason"})

	err := errors.Join(
		registry.Register(metrics.msgConsumedSize),
		registry.Register(metrics.partitionAssignmentLostCounter),
		registry.Register(metrics.rebalanceCounter),
	)
	if err != nil {
		return nil, fmt.Errorf("registering consumer metrics: %w", err)
	}

	return metrics, nil
}

func initProducerMetrics(registry *prometheus.Registry, namespace, subsys string) (*producerMetrics, error) {
	metrics := &producerMetrics{}

	metrics.msgProducedSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:      "msgs_produced_bytes",
		Help:      "The size in bytes of the payload produced",
		Namespace: namespace,
		Subsystem: subsys,
		Buckets:   []float64{256, 384, 512, 768, 1024, 1280, 1536, 1792, 2048, 2560, 3072, 4096},
	})

	metrics.successfullyProducedMessageCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "msg_produced_success_total",
		Help:      "How many messages were produced successfully according to delivery reports",
		Namespace: namespace,
		Subsystem: subsys,
	})
	metrics.unsuccessfullyProducedMessageCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "msg_produced_fail_total",
		Help:      "How many messages were produced unsuccessfully according to delivery reports",
		Namespace: namespace,
		Subsystem: subsys,
	})

	err := errors.Join(
		registry.Register(metrics.msgProducedSize),
		registry.Register(metrics.successfullyProducedMessageCounter),
		registry.Register(metrics.unsuccessfullyProducedMessageCounter),
	)
	if err != nil {
		return nil, fmt.Errorf("registering producer metrics: %w", err)
	}

	return metrics, nil
}

// fulfills the es.bulkerMetrics interface.
func (m *bulkerMetrics) FlushReason(reason string)     { m.flushReason.WithLabelValues(reason).Inc() }
func (m *bulkerMetrics) FlushedBytes(numBytes float64) { m.flushedBytesHistogram.Observe(numBytes) }
func (m *bulkerMetrics) FlushedMsgs(numMsgs float64)   { m.flushedMsgsHistogram.Observe(numMsgs) }

// fulfills the es.indexerMetrics interface.
func (m *indexerMetrics) BulkIndexCountInc()                   { m.bulkIndexCounter.Inc() }
func (m *indexerMetrics) BulkIndexErrorCountInc()              { m.bulkIndexErrorCounter.Inc() }
func (m *indexerMetrics) IndexedDocumentsCountAdd(num float64) { m.indexDocumentsCounter.Add(num) }
func (m *indexerMetrics) IndexedDocumentsBytesAdd(num float64) { m.indexDocumentsBytesCounter.Add(num) }
func (m *indexerMetrics) SessionIndexingFailCountInc()         { m.sessionIndexFailureCounter.Inc() }
func (m *indexerMetrics) ESClientReloadInc()                   { m.esClientReloadCounter.Inc() }
func (m *indexerMetrics) BulkIndexRetryCountInc()              { m.bulkIndexRetryCounter.Inc() }

// fulfills the kafka.consumerMetrics interface.
func (m *consumerMetrics) MsgConsumedSizeObserve(num float64) { m.msgConsumedSize.Observe(num) }
func (m *consumerMetrics) LostPartitionAssignmentInc()        { m.partitionAssignmentLostCounter.Inc() }
func (m *consumerMetrics) RebalanceInc(reason string) {
	m.rebalanceCounter.WithLabelValues(reason).Inc()
}

// fulfills the kafka.producerMetrics interface.
func (m *producerMetrics) MsgProducedSizeObserve(num float64) { m.msgProducedSize.Observe(num) }
func (m *producerMetrics) SuccessfullyProducedMsgInc()        { m.successfullyProducedMessageCounter.Inc() }
func (m *producerMetrics) UnsuccessfullyProducedMsgInc() {
	m.unsuccessfullyProducedMessageCounter.Inc()
}
