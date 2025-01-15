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

const (
	nativeHistogramBucketFactor = 1.1
	bytesBucketScalingFactor    = 1024
	bytesBucketDensity          = 32
	bytesBucketUpperLimit       = 1280
)

// Prometheus struct contains the various prometheus metrics collected by the indexer.
// It implements various *Prometheus interfaces defined by the various components of
// the indexer.
type Prometheus struct {
	*kafkaPrometheus

	*bulkerPrometheus

	*indexerPrometheus
}

type kafkaPrometheus struct {
	msgConsumedSize  prometheus.Histogram
	msgConsumedError prometheus.Counter
	msgProducedSize  prometheus.Histogram
	msgProducedError prometheus.Counter

	partitionAssignmentLost prometheus.Counter
	rebalance               *prometheus.CounterVec

	successfullyProducedMessage   prometheus.Counter
	unsuccessfullyProducedMessage prometheus.Counter
}

type bulkerPrometheus struct {
	flushedMsgs  prometheus.Histogram
	flushedBytes prometheus.Histogram
	flushReason  prometheus.CounterVec
}

type indexerPrometheus struct {
	sessionIndexFailure prometheus.Histogram

	esClientReload prometheus.Counter

	bulkCall      prometheus.Counter
	bulkCallError prometheus.Counter
	bulkCallRetry prometheus.Counter
}

// NewPrometheus initialises the Metrics struct with the various metrics and
// registers the metrics with a *prometheus.Registry.
// Returns the initialised Metrics struct and the new Prometheus registry which needs
// to be exposed over HTTP to be collected.
func NewPrometheus(
	maxBulkerBytes, maxBulkerMsgs, bulkerBucketsDensity int,
) (*Prometheus, *prometheus.Registry, error) {
	metrics := &Prometheus{}
	reg := prometheus.NewPedanticRegistry()

	kafkaProm, err := setupKafkaPrometheus(reg)
	if err != nil {
		return nil, nil, fmt.Errorf("setting up kafka prometheus metrics: %w", err)
	}

	metrics.kafkaPrometheus = kafkaProm

	bulkerProm, err := setupBulkerPrometheus(reg, maxBulkerBytes, maxBulkerMsgs, bulkerBucketsDensity)
	if err != nil {
		return nil, nil, fmt.Errorf("setting up bulker prometheus metrics: %w", err)
	}

	metrics.bulkerPrometheus = bulkerProm

	indexerProm, err := setupIndexerPrometheus(reg)
	if err != nil {
		return nil, nil, fmt.Errorf("setting up indexer prometheus metrics: %w", err)
	}

	metrics.indexerPrometheus = indexerProm

	if err := errors.Join(
		reg.Register(collectors.NewBuildInfoCollector()),
		reg.Register(collectors.NewGoCollector()),
		reg.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})),
	); err != nil {
		return nil, nil, fmt.Errorf("registering go metrics: %w", err)
	}

	return metrics, reg, nil
}

func setupKafkaPrometheus(promReg prometheus.Registerer) (*kafkaPrometheus, error) {
	metrics := &kafkaPrometheus{}

	metrics.msgConsumedSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:                        "messages_consumed_size_bytes",
		Help:                        "The size in bytes of the payload consumed",
		Namespace:                   "kafka",
		Buckets:                     computeBuckets(bytesBucketUpperLimit*bytesBucketScalingFactor, bytesBucketDensity),
		NativeHistogramBucketFactor: nativeHistogramBucketFactor,
	})

	metrics.msgConsumedError = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "messages_consumed_errors_total",
		Help:      "The number of errors consuming messages from Kafka",
		Namespace: "kafka",
	})

	metrics.msgProducedSize = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:                        "messages_produced_size_bytes",
		Help:                        "The size in bytes of the payload produced",
		Namespace:                   "kafka",
		Buckets:                     computeBuckets(bytesBucketUpperLimit*bytesBucketScalingFactor, bytesBucketDensity),
		NativeHistogramBucketFactor: nativeHistogramBucketFactor,
	})

	metrics.msgProducedError = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "messages_produced_errors_total",
		Help:      "The number of errors producing messages to Kafka",
		Namespace: "kafka",
	})

	metrics.partitionAssignmentLost = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "partition_assignment_lost_total",
		Help:      "Total number of lost partition assignments",
		Namespace: "kafka",
	})

	metrics.rebalance = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "rebalance_total",
		Help:      "Total number of rebalances",
		Namespace: "kafka",
	}, []string{"reason"})

	metrics.successfullyProducedMessage = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "message_produced_success_total",
		Help:      "How many messages were produced successfully according to delivery reports",
		Namespace: "kafka",
	})
	metrics.unsuccessfullyProducedMessage = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "message_produced_fail_total",
		Help:      "How many messages were produced unsuccessfully according to delivery reports",
		Namespace: "kafka",
	})

	if err := errors.Join(
		promReg.Register(metrics.msgConsumedSize),
		promReg.Register(metrics.msgProducedSize),
		promReg.Register(metrics.partitionAssignmentLost),
		promReg.Register(metrics.rebalance),
		promReg.Register(metrics.successfullyProducedMessage),
		promReg.Register(metrics.unsuccessfullyProducedMessage),
	); err != nil {
		return nil, fmt.Errorf("registering kafka metrics: %w", err)
	}

	return metrics, nil
}

func setupBulkerPrometheus(promReg prometheus.Registerer, maxBulkerBytes, maxBulkerMsgs, bulkerBucketsDensity int) (*bulkerPrometheus, error) {
	metrics := &bulkerPrometheus{}

	metrics.flushedMsgs = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:                        "flushed_msgs",
		Help:                        "Number of msgs flushed when bulking",
		Namespace:                   "bulker",
		Buckets:                     computeBuckets(maxBulkerMsgs, bulkerBucketsDensity),
		NativeHistogramBucketFactor: nativeHistogramBucketFactor,
	})

	metrics.flushedBytes = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:                        "flushed_size_bytes",
		Help:                        "Amount of bytes flushed when bulking",
		Namespace:                   "bulker",
		Buckets:                     computeBuckets(maxBulkerBytes, bulkerBucketsDensity),
		NativeHistogramBucketFactor: nativeHistogramBucketFactor,
	})

	metrics.flushReason = *prometheus.NewCounterVec(prometheus.CounterOpts{
		Name:      "flushed_reason_total",
		Help:      "Reason for a bulk flush",
		Namespace: "bulker",
	}, []string{"reason"})

	if err := errors.Join(
		promReg.Register(metrics.flushReason),
		promReg.Register(metrics.flushedBytes),
		promReg.Register(metrics.flushedMsgs),
	); err != nil {
		return nil, fmt.Errorf("registering bulker metrics: %w", err)
	}

	return metrics, nil
}

func setupIndexerPrometheus(promReg prometheus.Registerer) (*indexerPrometheus, error) {
	metrics := &indexerPrometheus{}

	metrics.sessionIndexFailure = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:                        "failed_session_indexing_size_bytes",
		Help:                        "Size in bytes of failed documents/sessions indexed",
		Namespace:                   "indexer",
		Buckets:                     computeBuckets(bytesBucketUpperLimit*bytesBucketScalingFactor, bytesBucketDensity),
		NativeHistogramBucketFactor: nativeHistogramBucketFactor,
	})

	metrics.esClientReload = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "es_client_reloaded_total",
		Help:      "Number of times that the ES client was reloaded",
		Namespace: "indexer",
	})

	metrics.bulkCall = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "bulk_calls_total",
		Help:      "Number of ES bulk action calls",
		Namespace: "indexer",
	})

	metrics.bulkCallError = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "bulk_call_errors_total",
		Help:      "Number of ES bulk action calls with errors",
		Namespace: "indexer",
	})

	metrics.bulkCallRetry = prometheus.NewCounter(prometheus.CounterOpts{
		Name:      "bulk_call_retries_total",
		Help:      "Number of ES bulk action call retries",
		Namespace: "indexer",
	})

	if err := errors.Join(
		promReg.Register(metrics.sessionIndexFailure),
		promReg.Register(metrics.esClientReload),
		promReg.Register(metrics.bulkCall),
		promReg.Register(metrics.bulkCallError),
		promReg.Register(metrics.bulkCallRetry),
	); err != nil {
		return nil, fmt.Errorf("registering indexer metrics: %w", err)
	}

	return metrics, nil
}

// fulfills the es.bulkerMetrics interface.
func (bp *bulkerPrometheus) FlushReason(reason string) { bp.flushReason.WithLabelValues(reason).Inc() }
func (bp *bulkerPrometheus) FlushedBytes(numBytes int) { bp.flushedBytes.Observe(float64(numBytes)) }
func (bp *bulkerPrometheus) FlushedMsgs(numMsgs int)   { bp.flushedMsgs.Observe(float64(numMsgs)) }

// fulfills the es.indexerMetrics interface.
func (ip *indexerPrometheus) BulkCall()      { ip.bulkCall.Inc() }
func (ip *indexerPrometheus) BulkCallError() { ip.bulkCallError.Inc() }
func (ip *indexerPrometheus) BulkCallRetry() { ip.bulkCallRetry.Inc() }
func (ip *indexerPrometheus) FailedSessionIndexing(size int) {
	ip.sessionIndexFailure.Observe(float64(size))
}
func (ip *indexerPrometheus) ESClientReload() { ip.esClientReload.Inc() }

// fulfills the kafka.consumerMetrics interface.
func (kp *kafkaPrometheus) MsgConsumed(size int)     { kp.msgConsumedSize.Observe(float64(size)) }
func (kp *kafkaPrometheus) MsgConsumedError()        { kp.msgConsumedError.Inc() }
func (kp *kafkaPrometheus) LostPartitionAssignment() { kp.partitionAssignmentLost.Inc() }
func (kp *kafkaPrometheus) Rebalanced(reason string) { kp.rebalance.WithLabelValues(reason).Inc() }

// fulfills the kafka.producerMetrics interface.
func (kp *kafkaPrometheus) MsgProduced(size int)       { kp.msgProducedSize.Observe(float64(size)) }
func (kp *kafkaPrometheus) MsgProducedError()          { kp.msgProducedError.Inc() }
func (kp *kafkaPrometheus) SuccessfullyProducedMsg()   { kp.successfullyProducedMessage.Inc() }
func (kp *kafkaPrometheus) UnsuccessfullyProducedMsg() { kp.unsuccessfullyProducedMessage.Inc() }

func computeBuckets(maxBucket, density int) []float64 {
	buckets := make([]float64, 0, density)

	step := maxBucket / density

	for i := step; i <= maxBucket; i += step {
		buckets = append(buckets, float64(i))
	}

	return buckets
}
