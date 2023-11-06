[![Go Reference](https://pkg.go.dev/badge/github.com/mcgillowen/arkime-kafka-indexer.svg)](https://pkg.go.dev/github.com/mcgillowen/arkime-kafka-indexer)
![Golang workflow](https://github.com/mcgillowen/arkime-kafka-indexer/actions/workflows/go.yaml/badge.svg)
![Release workflow](https://github.com/mcgillowen/arkime-kafka-indexer/actions/workflows/release.yaml/badge.svg)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![CodeQL](https://github.com/mcgillowen/arkime-kafka-indexer/actions/workflows/github-code-scanning/codeql/badge.svg)](https://github.com/mcgillowen/arkime-kafka-indexer/actions/workflows/github-code-scanning/codeql)
[![codebeat badge](https://codebeat.co/badges/885e32a1-4bf3-4222-9e3d-ceccae2f7654)](https://codebeat.co/projects/github-com-mcgillowen-arkime-kafka-indexer-main)
[![goreportcard](https://goreportcard.com/badge/github.com/mcgillowen/arkime-kafka-indexer)](https://goreportcard.com/report/github.com/mcgillowen/arkime-kafka-indexer)
# Arkime Kafka Indexer

An Elasticsearch indexer for Arkime SPI sent through Kafka using the Arkime [Kafka plugin](https://arkime.com/settings#kafka).

This "processor" is part of an ecosystem of "processors" that we are slowly open sourcing, this being the first one and
most important since it is necessary for Arkime to function correctly. For an introduction to this ecosystem and why to
use Kafka with Arkime see the [Arkimeet 2023 - Arkime Stream Processing with Kafka](https://youtu.be/FhNQwTyg218) talk
([slides](https://arkime.com/assets/Arkimeet2023-Arkime-Kafka.pdf)).

## Architecture

This processor is architected in the following way (see diagram below), the main components are:

- Consumer: consumes messages from a Kafka topic as part of a consumer group and then sends them onwards using
  a buffered Go channel
- Indexer: takes the contents of the Kafka messages that are in the Go channel and bulks them together
  , for ES performance reasons, and sends the bulked messages to ES using the [Bulk API](https://www.elastic.co/guide/en/elasticsearch/reference/current/docs-bulk.html). The response is
  then checked for errors and any SPIs that failed to index are then sent into a Go channel.
- Producer: takes the failed SPIs from the Go channel and produces them back into a Kafka topic, for further analysis and reprocessing if desired.

The number of indexers can be configured, this enables parallel sends to the elasticsearch cluster which increases the throughput.

![Architecture](docs/architecture.png)

## Configuration

Configuration of the indexer is done using environment variables following the 12-factor app approach, documented at [12factor.net](https://12factor.net).

| VARIABLE                                  | TYPE          | DEFAULT                                                                     | DESCRIPTION                                                                      |
|-------------------------------------------|---------------|-----------------------------------------------------------------------------|----------------------------------------------------------------------------------|
| PORT                                      | string        | 8080                                                                        | Port for the HTTP server                                                         |
| LOG_LEVEL                                 | string        | debug                                                                       | The level to log at                                                              |
| KAFKA_CONSUMER_BROKERS                    | string        | localhost:9092                                                              | Kafka Broker to consume from                                                     |
| KAFKA_CONSUMER_TOPIC                      | string        |                                                                             | Kafka topic to consume from                                                      |
| KAFKA_CONSUMER_GROUP_NAME                 | string        |                                                                             | Name of the Kafka consumer group                                                 |
| KAFKA_CONSUMER_INCREMENTAL_REBALANCE      | bool          | false                                                                       | If the cooperative rebalancing strategy should be used                           |
| KAFKA_PRODUCER_BROKERS                    | string        | localhost:9092                                                              | Kafka to produce to                                                              |
| KAFKA_PRODUCER_TOPIC                      | string        |                                                                             | Kafka topic to produce to                                                        |
| KAFKA_PRODUCER_MSG_TIMEOUT                | time.Duration | 30s                                                                         | Produced message timeout                                                         |
| KAFKA_PRODUCER_MSG_RETRIES                | int           | 100                                                                         | Maximum of retries for a produced message                                        |
| KAFKA_PRODUCER_FULL_QUEUE_COOLDOWN        | time.Duration | 1s                                                                          | How long to wait after a producer full queue error before retrying               |
| KAFKA_PRODUCER_LOG_DELIVERY_REPORTS       | bool          | true                                                                        | Should the delivery reports be logged                                            |
| KAFKA_SESSION_TIMEOUT                     | time.Duration | 6000ms                                                                      | Kafka session timeout length                                                     |
| KAFKA_POLL_TIMEOUT                        | time.Duration | 100ms                                                                       | Consumer polling timeout                                                         |
| KAFKA_FLUSH_INTERVAL                      | time.Duration | 100ms                                                                       | Timeout length when flushing Kafka messages at shutdown                          |
| BULKER_FLUSH_INTERVAL                     | time.Duration | 10s                                                                         | Maximum amount of time to buffer messages before sending them to ES              |
| BULKER_MAX_MESSAGES                       | int           | 100                                                                         | Maximum number of messages to buffer before sending them to ES                   |
| BULKER_MAX_BYTES                          | int           | 10_485_760                                                                  | Maximum number of bytes to buffer before sending them to ES                      |
| ELASTIC_SERVICE                           | string        |                                                                             | The address of an Elasticsearch node, the client will discover the rest of nodes |
| ELASTIC_SERVICE_PORT                      | string        | 9200                                                                        | The ES HTTP port                                                                 |
| ELASTIC_INDEXER_INSTANCES                 | int           | 1                                                                           | The number of parallel indexers to use                                           |
| ELASTIC_CLIENT_MAX_RETRIES                | int           | 10                                                                          | Number of retries when communicating with ES                                     |
| ELASTIC_CLIENT_RETRY_STATUSES             | []int         | 502,503,504,429                                                             | Which HTTP status codes to retry                                                 |
| ELASTIC_CLIENT_DISCOVER_NODES             | bool          | true                                                                        | Should the client discover the other ES nodes                                    |
| ELASTIC_CLIENT_DISCOVER_INTERVAL          | time.Duration | 1h                                                                          | Interval between updates of the list of ES connections                           |
| ELASTIC_CLIENT_BULK_TIMEOUT               | time.Duration | 500ms                                                                       | The timeout duration for the Bulk call                                           |
| ELASTIC_CLIENT_MAX_DEAD_PERCENTAGE        | int           | 20                                                                          | The maximum percentage of dead ES connections                                    |
| ELASTIC_TRANSPORT_DIAL_TIMEOUT            | time.Duration | 2s                                                                          |                                                                                  |
| ELASTIC_TRANSPORT_DIAL_KEEPALIVE          | time.Duration | 5s                                                                          |                                                                                  |
| ELASTIC_TRANSPORT_MAX_IDLE_CONNS          | int           | 100                                                                         |                                                                                  |
| ELASTIC_TRANSPORT_MAX_IDLE_CONNS_PER_HOST | int           | 100                                                                         |                                                                                  |
| ELASTIC_TRANSPORT_MAX_CONNS_PER_HOST      | int           | 100                                                                         |                                                                                  |
| ELASTIC_TRANSPORT_IDLE_CONN_TIMEOUT       | time.Duration | 10s                                                                         |                                                                                  |
| ELASTIC_TRANSPORT_EXPECT_CONTINUE_TIMEOUT | time.Duration | 1s                                                                          |                                                                                  |
| CONSUMER_CHANNEL_BUFFER_SIZE              | int           | 10                                                                          |                                                                                  |
| ERROR_CHANNEL_BUFFER_SIZE                 | int           | 10                                                                          |                                                                                  |
| PRODUCER_CHANNEL_BUFFER_SIZE              | int           | 10                                                                          |                                                                                  |
| METRICS_NAMESPACE                         | string        | arkime                                                                      |                                                                                  |
| METRICS_SUBSYSTEM                         | string        | kafkaindexer                                                                |                                                                                  |
| METRICS_PATH                              | string        | /metrics                                                                    |                                                                                  |
| METRICS_FLUSHED_BYTES_BUCKETS             | []float64     | 50_000,100_000,500_000,1_000_000,5_000_000,25_000_000,50_000_000,75_000_000 |                                                                                  |
| METRICS_FLUSHED_MSGS_BUCKETS              | []float64     | 2,4,8,16,32,64,128,256                                                      |                                                                                  |


## Tasks

The following subsections define tasks that can be run using the [xc tool](https://xcfile.dev/), this tool executes the commands within the code block of each subsection, with
the name of the subsection being the name of the command, eg. `xc lint` for running the
commands contained within the code block of the "Lint" subsection.

### Deps

Installs the dependencies for building, formatting, linting, etc.

```
# Installs gofumpt for formatting the source code with extra rules
go install mvdan.cc/gofumpt@latest
# Installs golangci-lint for linting, uses the .golangci.yaml
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(go env GOPATH)/bin v1.52.2
# Installs the Go vulnerability checking tool
go install golang.org/x/vuln/cmd/govulncheck@latest
```

### Precommit

Sets up [pre-commit](https://pre-commit.com/) to run when committing.

Requires `pre-commit` to be installed before hand, see [pre-commit/Installation](https://pre-commit.com/#installation)

```
pre-commit run --all-files
pre-commit install
```

### Format

Formats the code to ensure a consistent formatting

```
gofumpt -l -w .
```

### Lint

Lints the source code according to the configuration in the `.golangci.yaml` file

```
govulncheck ./...
golangci-lint run ./...
```

### Test

Runs the unit-tests

```
go test -race github.com/mcgillowen/arkime-kafka-indexer/...
```

### Snapshot-Dry-Run

Creates a snapshot release using GoReleaser and GoReleaser-Cross but without publishing it.

```
docker run \
  --rm \
  -e CGO_ENABLED=1 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v `pwd`:/go/src/github.com/mcgillowen/arkime-kafka-indexer \
  -w /go/src/github.com/mcgillowen/arkime-kafka-indexer \
  ghcr.io/goreleaser/goreleaser-cross:v1.21.3 \
  --clean --skip=publish --snapshot
```

### Snapshot

Creates a snapshot release using GoReleaser and GoReleaser-Cross.

```
docker run \
  --rm \
  -e CGO_ENABLED=1 \
  -v $(HOME)/.docker/config.json:/root/.docker/config.json \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v `pwd`:/go/src/github.com/mcgillowen/arkime-kafka-indexer \
  -w /go/src/github.com/mcgillowen/arkime-kafka-indexer \
  ghcr.io/goreleaser/goreleaser-cross:v1.21.3 \
  --clean --snapshot
```

### Release-Dry-Run

Creates a normal release using GoReleaser and GoReleaser-Cross but without publishing it.

```
docker run \
  --rm \
  -e CGO_ENABLED=1 \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v `pwd`:/go/src/github.com/mcgillowen/arkime-kafka-indexer \
  -w /go/src/github.com/mcgillowen/arkime-kafka-indexer \
  ghcr.io/goreleaser/goreleaser-cross:v1.21.3 \
  --clean --skip=publish
```

### Release

Creates a release using GoReleaser and GoReleaser-Cross.

Inputs: GITHUB_TOKEN
```
docker run \
  --rm \
  -e CGO_ENABLED=1 \
  -e GITHUB_TOKEN=$GITHUB_TOKEN \
  -v $HOME/.docker/config.json:/root/.docker/config.json \
  -v /var/run/docker.sock:/var/run/docker.sock \
  -v `pwd`:/go/src/github.com/mcgillowen/arkime-kafka-indexer \
  -w /go/src/github.com/mcgillowen/arkime-kafka-indexer \
  ghcr.io/goreleaser/goreleaser-cross:v1.21.3 \
  release --clean
```
