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
	msgConsumedSize prometheus.Histogram
	msgProducedSize prometheus.Histogram

	partitionAssignmentLostCounter prometheus.Counter
	rebalanceCounter               *prometheus.CounterVec
	offsetCommitCounter            prometheus.Counter

	flushedMsgsHistogram  prometheus.Histogram
	flushedBytesHistogram prometheus.Histogram
	flushReason           prometheus.CounterVec

	indexDocumentsCounter      prometheus.Counter
	indexDocumentsBytesCounter prometheus.Counter

	sessionIndexFailureCounter prometheus.Counter

	esClientReloadCounter prometheus.Counter

	bulkIndexCounter      prometheus.Counter
	bulkIndexErrorCounter prometheus.Counter
	bulkIndexRetryCounter prometheus.Counter

	successfullyProducedMessageCounter   prometheus.Counter
	unsuccessfullyProducedMessageCounter prometheus.Counter
}

// InitPrometheus initialises the Metrics struct with the various metrics and
// registers the metrics with a *prometheus.Registry.
// Returns the initialised Metrics struct and the new Prometheus registry which needs
// to be exposed over HTTP to be collected.
//
//nolint:funlen // This function can't really be shorter
func InitPrometheus(
	namespace, subsys string,
	flushedBytesBuckets, flushedMsgsBuckets []float64,
) (*Metrics, *prometheus.Registry, error) {
	metrics := &Metrics{}
	reg := prometheus.NewPedanticRegistry()
	errs := make([]error, 0)

	metrics.msgConsumedSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:      "msgs_consumed_bytes",
		Help:      "The size in bytes of the payload consumed",
		Namespace: namespace,
		Subsystem: subsys,
		Buckets:   []float64{256, 384, 512, 768, 1024, 1280, 1536, 1792, 2048, 2560, 3072, 4096},
	})
	metrics.msgProducedSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:      "msgs_produced_bytes",
		Help:      "The size in bytes of the payload produced",
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

	metrics.offsetCommitCounter = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "offset_commit_total",
		Help:      "Total number of offset commits",
		Namespace: namespace,
		Subsystem: subsys,
	})

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

	errs = append(errs,
		reg.Register(collectors.NewBuildInfoCollector()),
		reg.Register(collectors.NewGoCollector()),
		reg.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})),
		reg.Register(metrics.msgConsumedSize),
		reg.Register(metrics.msgProducedSize),
		reg.Register(metrics.partitionAssignmentLostCounter),
		reg.Register(metrics.rebalanceCounter),
		reg.Register(metrics.offsetCommitCounter),
		reg.Register(metrics.flushedMsgsHistogram),
		reg.Register(metrics.flushedBytesHistogram),
		reg.Register(metrics.flushReason),
		reg.Register(metrics.indexDocumentsCounter),
		reg.Register(metrics.indexDocumentsBytesCounter),
		reg.Register(metrics.sessionIndexFailureCounter),
		reg.Register(metrics.esClientReloadCounter),
		reg.Register(metrics.bulkIndexCounter),
		reg.Register(metrics.bulkIndexErrorCounter),
		reg.Register(metrics.bulkIndexRetryCounter),
		reg.Register(metrics.successfullyProducedMessageCounter),
		reg.Register(metrics.unsuccessfullyProducedMessageCounter),
	)

	if err := errors.Join(errs...); err != nil {
		return nil, nil, fmt.Errorf("failed to register metrics collectors: %w", err)
	}

	return metrics, reg, nil
}

// fulfills the es.bulkerMetrics interface.
func (m *Metrics) FlushReason(reason string)     { m.flushReason.WithLabelValues(reason).Inc() }
func (m *Metrics) FlushedBytes(numBytes float64) { m.flushedBytesHistogram.Observe(numBytes) }
func (m *Metrics) FlushedMsgs(numMsgs float64)   { m.flushedMsgsHistogram.Observe(numMsgs) }

// fulfills the es.indexerMetrics interface.
func (m *Metrics) BulkIndexCountInc()                   { m.bulkIndexCounter.Inc() }
func (m *Metrics) BulkIndexErrorCountInc()              { m.bulkIndexErrorCounter.Inc() }
func (m *Metrics) IndexedDocumentsCountAdd(num float64) { m.indexDocumentsCounter.Add(num) }
func (m *Metrics) IndexedDocumentsBytesAdd(num float64) { m.indexDocumentsBytesCounter.Add(num) }
func (m *Metrics) SessionIndexingFailCountInc()         { m.sessionIndexFailureCounter.Inc() }
func (m *Metrics) ESClientReloadInc()                   { m.esClientReloadCounter.Inc() }

func (m *Metrics) BulkIndexRetryCountInc() { m.bulkIndexRetryCounter.Inc() }

// fulfills the kafka.consumerMetrics interface.
func (m *Metrics) MsgConsumedSizeObserve(num float64) { m.msgConsumedSize.Observe(num) }
func (m *Metrics) LostPartitionAssignmentInc()        { m.partitionAssignmentLostCounter.Inc() }
func (m *Metrics) RebalanceInc(reason string)         { m.rebalanceCounter.WithLabelValues(reason).Inc() }

// fulfills the kafka.producerMetrics interface.
func (m *Metrics) MsgProducedSizeObserve(num float64) { m.msgProducedSize.Observe(num) }
func (m *Metrics) SuccessfullyProducedMsgInc()        { m.successfullyProducedMessageCounter.Inc() }
func (m *Metrics) UnsuccessfullyProducedMsgInc()      { m.unsuccessfullyProducedMessageCounter.Inc() }
