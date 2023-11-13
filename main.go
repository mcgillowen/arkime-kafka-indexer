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

package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/kelseyhightower/envconfig"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog"
	"github.com/sourcegraph/conc"
	"github.com/sourcegraph/conc/pool"
	"github.com/valyala/bytebufferpool"

	"github.com/mcgillowen/arkime-kafka-indexer/es"
	"github.com/mcgillowen/arkime-kafka-indexer/kafka"
	"github.com/mcgillowen/arkime-kafka-indexer/metrics"
)

type envConfig struct {
	Port                                  string        `default:"8080"                                                                          desc:"Port for the HTTP server"                                            envconfig:"PORT"`
	LogLevel                              string        `default:"debug"                                                                         desc:"The level to log at"                                                 envconfig:"LOG_LEVEL"`
	KafkaConsumerBrokers                  string        `default:"localhost:9092"                                                                desc:"Kafka Broker to consume from"                                        envconfig:"KAFKA_CONSUMER_BROKERS"`
	KafkaConsumerTopic                    string        `desc:"Kafka topic to consume from"                                                      envconfig:"KAFKA_CONSUMER_TOPIC"                                           required:"true"`
	KafkaConsumerGroupName                string        `desc:"Name of the Kafka consumer group"                                                 envconfig:"KAFKA_CONSUMER_GROUP_NAME"                                      required:"true"`
	KafkaConsumerIncrementalRebalance     bool          `default:"false"                                                                         desc:"If the cooperative rebalancing strategy should be used"              envconfig:"KAFKA_CONSUMER_INCREMENTAL_REBALANCE"`
	KafkaProducerBrokers                  string        `default:"localhost:9092"                                                                desc:"Kafka to produce to"                                                 envconfig:"KAFKA_PRODUCER_BROKERS"`
	KafkaProducerTopic                    string        `desc:"Kafka topic to produce to"                                                        envconfig:"KAFKA_PRODUCER_TOPIC"`
	KafkaProducerMessageTimeout           time.Duration `default:"30s"                                                                           desc:"Produced message timeout"                                            envconfig:"KAFKA_PRODUCER_MSG_TIMEOUT"`
	KafkaProducerMessageRetries           int           `default:"100"                                                                           desc:"Maximum of retries for a produced message"                           envconfig:"KAFKA_PRODUCER_MSG_RETRIES"`
	KafkaProducerQueueFullCooldown        time.Duration `default:"1s"                                                                            desc:"How long to wait after a producer full queue error before retrying"  envconfig:"KAFKA_PRODUCER_FULL_QUEUE_COOLDOWN"`
	KafkaProducerLogDeliveryReports       bool          `default:"true"                                                                          desc:"Should the delivery reports be logged"                               envconfig:"KAFKA_PRODUCER_LOG_DELIVERY_REPORTS"`
	KafkaSessionTimeout                   time.Duration `default:"6000ms"                                                                        desc:"Kafka session timeout length"                                        envconfig:"KAFKA_SESSION_TIMEOUT"`
	KafkaPollTimeout                      time.Duration `default:"100ms"                                                                         desc:"Consumer polling timeout"                                            envconfig:"KAFKA_POLL_TIMEOUT"`
	KafkaFlushInterval                    time.Duration `default:"100ms"                                                                         desc:"Timeout length when flushing Kafka messages at shutdown"             envconfig:"KAFKA_FLUSH_INTERVAL"`
	BulkerFlushInterval                   time.Duration `default:"10s"                                                                           desc:"Maximum amount of time to buffer messages before sending them to ES" envconfig:"BULKER_FLUSH_INTERVAL"`
	BulkerMaxMessages                     int           `default:"100"                                                                           desc:"Maximum number of messages to buffer before sending them to ES"      envconfig:"BULKER_MAX_MESSAGES"`
	BulkerMaxBytes                        int           `default:"10_485_760"                                                                    desc:"Maximum number of bytes to buffer before sending them to ES"         envconfig:"BULKER_MAX_BYTES"` // 10mb = 10 * 1024 * 1024
	ElasticService                        string        `desc:"The address of an Elasticsearch node, the client will discover the rest of nodes" envconfig:"ELASTIC_SERVICE"                                                required:"true"`
	ElasticServicePort                    string        `default:"9200"                                                                          desc:"The ES HTTP port"                                                    envconfig:"ELASTIC_SERVICE_PORT"`
	ElasticIndexerInstances               int           `default:"1"                                                                             desc:"The number of parallel indexers to use"                              envconfig:"ELASTIC_INDEXER_INSTANCES"`
	ElasticClientMaxRetries               int           `default:"10"                                                                            desc:"Number of retries when communicating with ES"                        envconfig:"ELASTIC_CLIENT_MAX_RETRIES"`
	ElasticClientRetryStatuses            []int         `default:"502,503,504,429"                                                               desc:"Which HTTP status codes to retry"                                    envconfig:"ELASTIC_CLIENT_RETRY_STATUSES"`
	ElasticClientDiscoverNodes            bool          `default:"true"                                                                          desc:"Should the client discover the other ES nodes"                       envconfig:"ELASTIC_CLIENT_DISCOVER_NODES"`
	ElasticClientDiscoverInterval         time.Duration `default:"1h"                                                                            desc:"Interval between updates of the list of ES connections"              envconfig:"ELASTIC_CLIENT_DISCOVER_INTERVAL"`
	ElasticClientBulkTimeout              time.Duration `default:"500ms"                                                                         desc:"The timeout duration for the Bulk call"                              envconfig:"ELASTIC_CLIENT_BULK_TIMEOUT"`
	ElasticClientMaxDeadPercentage        int           `default:"20"                                                                            desc:"The maximum percentage of dead ES connections"                       envconfig:"ELASTIC_CLIENT_MAX_DEAD_PERCENTAGE"`
	ElasticTransportDialTimeout           time.Duration `default:"2s"                                                                            envconfig:"ELASTIC_TRANSPORT_DIAL_TIMEOUT"`
	ElasticTransportDialKeepAlive         time.Duration `default:"5s"                                                                            envconfig:"ELASTIC_TRANSPORT_DIAL_KEEPALIVE"`
	ElasticTransportMaxIdleConns          int           `default:"100"                                                                           envconfig:"ELASTIC_TRANSPORT_MAX_IDLE_CONNS"`
	ElasticTransportMaxIdleConnsPerHost   int           `default:"100"                                                                           envconfig:"ELASTIC_TRANSPORT_MAX_IDLE_CONNS_PER_HOST"`
	ElasticTransportMaxConnsPerHost       int           `default:"100"                                                                           envconfig:"ELASTIC_TRANSPORT_MAX_CONNS_PER_HOST"`
	ElasticTransportIdleConnTimeout       time.Duration `default:"10s"                                                                           envconfig:"ELASTIC_TRANSPORT_IDLE_CONN_TIMEOUT"`
	ElasticTransportExpectContinueTimeout time.Duration `default:"1s"                                                                            envconfig:"ELASTIC_TRANSPORT_EXPECT_CONTINUE_TIMEOUT"`
	ConsumerChannelBufferSize             int           `default:"10"                                                                            envconfig:"CONSUMER_CHANNEL_BUFFER_SIZE"`
	ErrorChannelBufferSize                int           `default:"10"                                                                            envconfig:"ERROR_CHANNEL_BUFFER_SIZE"`
	ProducerChannelBufferSize             int           `default:"10"                                                                            envconfig:"PRODUCER_CHANNEL_BUFFER_SIZE"`
	MetricsNamespace                      string        `default:"arkime"                                                                        envconfig:"METRICS_NAMESPACE"`
	MetricsSubsystem                      string        `default:"kafkaindexer"                                                                  envconfig:"METRICS_SUBSYSTEM"`
	MetricsPath                           string        `default:"/metrics"                                                                      envconfig:"METRICS_PATH"`
	FlushedBytesBuckets                   []float64     `default:"50_000,100_000,500_000,1_000_000,5_000_000,25_000_000,50_000_000,75_000_000"   envconfig:"METRICS_FLUSHED_BYTES_BUCKETS"`
	FlushedMsgsBuckets                    []float64     `default:"2,4,8,16,32,64,128,256"                                                        envconfig:"METRICS_FLUSHED_MSGS_BUCKETS"`
}

const (
	defaultHTTPServerReadTimeout     = 5 * time.Second
	defaultHTTPServerWriteTimeout    = 10 * time.Second
	defaultHTTPServerIdleTimeout     = 20 * time.Second
	defaultHTTPServerShutdownTimeout = 10 * time.Second
	defaultMinimumRetryBackoff       = 50 * time.Millisecond
)

func main() {
	logger := zerolog.New(os.Stdout).With().Timestamp().Logger()

	printBuildInfo(logger)
	env := processEnv(logger)
	logger = setLogLevel(env.LogLevel, logger)
	promMetrics, promReg, server := setupMetricsAndServer(env, logger)

	ctx := context.Background()

	ctx, cancel := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	reloadChan := make(chan os.Signal, 1)
	signal.Notify(reloadChan, syscall.SIGHUP)

	defer func() {
		signal.Stop(reloadChan)
	}()

	msgPool := &bytebufferpool.Pool{}
	consumer, producer := setupKafka(env, msgPool, promMetrics, logger)
	indexer := setupIndexer(env, msgPool, promMetrics, promReg, logger)

	ctxPool := pool.New().
		WithContext(ctx).
		WithFirstError().
		WithCancelOnError()

	ctxPool.Go(func(ctx context.Context) error {
		for {
			select {
			case <-reloadChan:
				logger.Debug().Msg("checking ES status")

				if err := indexer.CheckESStatus(); err != nil {
					return fmt.Errorf("error reloading indexer ES client: %w", err)
				}
			case <-ctx.Done():
				logger.Debug().Msg("context is finished")

				ctx, cancel = context.WithTimeout(context.Background(), defaultHTTPServerShutdownTimeout)
				defer cancel()

				if err := server.Shutdown(ctx); err != nil { //nolint:contextcheck,lll // this is a bug https://github.com/kkHAIKE/contextcheck/issues/2
					return fmt.Errorf("error shutting down http server: %w", err)
				}

				return nil
			}
		}
	})
	ctxPool.Go(func(ctx context.Context) error {
		err := server.ListenAndServe() // Blocks!
		if errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http server stopped unexpectedly: %w", err)
		}

		return nil
	})

	consumerChan := make(chan *bytebufferpool.ByteBuffer, env.ConsumerChannelBufferSize)

	ctxPool.Go(func(ctx context.Context) error {
		err := consumer.Start(
			ctx,
			consumerChan,
		)
		if err != nil {
			return fmt.Errorf("error in consumer: %w", err)
		}

		return nil
	})
	logger.Debug().Msg("started consumer goroutine")

	errorChans := make([]<-chan *bytebufferpool.ByteBuffer, env.ElasticIndexerInstances)

	for i := 0; i < env.ElasticIndexerInstances; i++ {
		indexerNum := i

		var errorChan chan *bytebufferpool.ByteBuffer
		if producer != nil {
			errorChan = make(chan *bytebufferpool.ByteBuffer, env.ErrorChannelBufferSize)
			errorChans[indexerNum] = errorChan
		}

		ctxPool.Go(func(ctx context.Context) error {
			err := indexer.Start(
				consumerChan,
				errorChan,
			)
			if err != nil {
				return fmt.Errorf("error in indexer-%d: %w", indexerNum, err)
			}

			return nil
		})
		logger.Debug().Msgf("started indexer %02d goroutine", indexerNum)
	}

	if producer != nil {
		mergedErrorChan := merge(ctxPool, env.ProducerChannelBufferSize, errorChans...)
		ctxPool.Go(func(ctx context.Context) error {
			err := producer.Start(
				ctx,
				mergedErrorChan,
			)
			if err != nil {
				return fmt.Errorf("error in producer: %w", err)
			}

			return nil
		})
		logger.Debug().Msg("started producer goroutine")
	}

	err := ctxPool.Wait()
	if err != nil {
		logger.Fatal().Err(err).Msg("error in goroutine pool")
	}
}

// Inspired by https://blog.golang.org/pipelines
func merge(ctxPool *pool.ContextPool, size int, incomingChans ...<-chan *bytebufferpool.ByteBuffer) <-chan *bytebufferpool.ByteBuffer {
	if len(incomingChans) == 0 {
		return nil
	}

	var wg conc.WaitGroup

	out := make(chan *bytebufferpool.ByteBuffer, size)

	// Start an output goroutine for each input channel in cs.  output
	// copies values from c to out until c is closed, then calls wg.Done.
	output := func(c <-chan *bytebufferpool.ByteBuffer) {
		for n := range c {
			out <- n
		}
	}

	for _, c := range incomingChans {
		c := c

		wg.Go(func() {
			output(c)
		})
	}

	// Start a goroutine to close out once all the output goroutines are
	// done.  This must start after the wg.Add call.
	ctxPool.Go(func(_ context.Context) error {
		recPanic := wg.WaitAndRecover()
		if recPanic != nil {
			return fmt.Errorf("panic in merge wait goroutine %w", recPanic.AsError())
		}

		close(out)

		return nil
	})

	return out
}

func printBuildInfo(logger zerolog.Logger) {
	buildInfo, ok := debug.ReadBuildInfo()
	if !ok {
		logger.Fatal().Msg("unable to read build info from binary")
	}

	var arch, operatingSystem string

	for _, buildSetting := range buildInfo.Settings {
		switch buildSetting.Key {
		case "GOARCH":
			arch = buildSetting.Value
		case "GOOS":
			operatingSystem = buildSetting.Value
		}
	}

	logger.Info().
		Str("go_version", buildInfo.GoVersion).
		Str("GOOS", operatingSystem).
		Str("GOARCH", arch).
		Str("version", version).
		Str("build_time", date).
		Str("commit", commit).
		Str("built_by", builtBy).
		Msgf("Starting %s", buildInfo.Path)
}

func processEnv(logger zerolog.Logger) envConfig {
	var env envConfig
	if err := envconfig.Process("", &env); err != nil {
		logger.Fatal().Err(err).Msg("failed to process ENV variables")
	}

	return env
}

func setLogLevel(level string, logger zerolog.Logger) zerolog.Logger {
	logLevel, err := zerolog.ParseLevel(level)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to parse log level")
	}

	return logger.Level(logLevel)
}

func setupMetricsAndServer(env envConfig, logger zerolog.Logger) (*metrics.Metrics, *prometheus.Registry, *http.Server) {
	mux := http.NewServeMux()

	metrics, promReg, err := metrics.InitPrometheus(
		env.MetricsNamespace,
		env.MetricsSubsystem,
		env.FlushedBytesBuckets,
		env.FlushedMsgsBuckets,
	)
	if err != nil {
		logger.Fatal().Err(err).Msg("failed to initialise prometheus metrics")
	}

	mux.Handle(env.MetricsPath, promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", env.Port),
		Handler:      mux,
		ReadTimeout:  defaultHTTPServerReadTimeout,
		WriteTimeout: defaultHTTPServerWriteTimeout,
		IdleTimeout:  defaultHTTPServerIdleTimeout,
	}

	return metrics, promReg, server
}

func setupIndexer(
	env envConfig,
	msgPool *bytebufferpool.Pool,
	promMetrics *metrics.Metrics, promReg *prometheus.Registry,
	logger zerolog.Logger,
) *es.Indexer {
	esTransport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   env.ElasticTransportDialTimeout,
			KeepAlive: env.ElasticTransportDialKeepAlive,
		}).DialContext,
		MaxIdleConns:          env.ElasticTransportMaxIdleConns,
		MaxIdleConnsPerHost:   env.ElasticTransportMaxConnsPerHost,
		MaxConnsPerHost:       env.ElasticTransportMaxConnsPerHost,
		IdleConnTimeout:       env.ElasticTransportIdleConnTimeout,
		ExpectContinueTimeout: env.ElasticTransportExpectContinueTimeout,
	}

	indexer, err := es.NewIndexer(env.ElasticService, env.ElasticServicePort,
		msgPool,
		promMetrics,
		logger.With().
			Str("component", "indexer").
			Logger(),
		es.WithTransport(esTransport),
		es.WithMaxRetries(env.ElasticClientMaxRetries),
		es.WithRetryStatuses(env.ElasticClientRetryStatuses...),
		es.WithRetryOnTimeout(),
		es.WithRetryBackoffFunc(
			es.RetryBackoffFunc(
				defaultMinimumRetryBackoff,
				logger.With().
					Str("component", "backoff-retry").
					Logger(),
				promMetrics,
			),
		),
		es.WithCustomLogger(logger),
		es.WithESClientMetrics(metrics.NewESCollectorFunc(env.MetricsNamespace, env.MetricsSubsystem), promReg),
		es.WithFlushInterval(env.BulkerFlushInterval),
		es.WithMaxBufferedBytes(env.BulkerMaxBytes),
		es.WithMaxBufferedMsgs(env.BulkerMaxMessages),
		es.WithBulkTimeout(env.ElasticClientBulkTimeout),
		es.WithMaxDeadPercentage(env.ElasticClientMaxDeadPercentage),
		es.WithDiscoverNodesInterval(env.ElasticClientDiscoverInterval),
		es.WithDiscoverNodesOnStart(env.ElasticClientDiscoverNodes),
	)
	if err != nil {
		logger.Fatal().Err(err).Msg("error creating elastic indexer")
	}

	return indexer
}

func setupKafka(
	env envConfig,
	msgPool *bytebufferpool.Pool,
	metrics *metrics.Metrics,
	logger zerolog.Logger,
) (*kafka.Consumer, *kafka.Producer) {
	consumer := kafka.NewConsumer(
		env.KafkaConsumerBrokers,
		env.KafkaConsumerGroupName,
		env.KafkaConsumerTopic,
		env.KafkaConsumerIncrementalRebalance,
		env.KafkaSessionTimeout,
		env.KafkaPollTimeout,
		msgPool,
		metrics,
		logger.With().
			Str("component", "consumer").
			Logger(),
	)

	logger.Debug().Msg("initialised consumer")

	var producer *kafka.Producer
	if env.KafkaProducerTopic != "" {
		producer = kafka.NewProducer(
			env.KafkaProducerBrokers,
			env.KafkaProducerTopic,
			env.KafkaFlushInterval,
			env.KafkaProducerMessageTimeout,
			env.KafkaProducerQueueFullCooldown,
			env.KafkaProducerMessageRetries,
			env.KafkaProducerLogDeliveryReports,
			msgPool,
			metrics,
			logger.With().
				Str("component", "producer").
				Logger(),
		)

		logger.Debug().Msg("initialised producer")
	}

	return consumer, producer
}
