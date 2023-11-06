// Copyright 2023 Owen McGill
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package es

import (
	"github.com/valyala/bytebufferpool"
)

// IndexerOption is the option type for setting Elastic client configuration values.
// See https://dave.cheney.net/2014/10/17/functional-options-for-friendly-apis for more information.
type IndexerOption func(ic *indexerConfig)

// bulkerMetrics is the interface defining what metrics the bulker uses.
// We use an interface since it allows to mock it and check that they are
// called correctly.
type bulkerMetrics interface {
	FlushReason(reason string)
	FlushedBytes(numBytes float64)
	FlushedMsgs(numMsgs float64)
}

// indexerMetrics is the interface defining what metrics the indexer uses.
// We use an interface since it allows to mock it and check that they are
// called correctly.
type indexerMetrics interface {
	// embededded since the indexer provides the metrics object to the bulker
	bulkerMetrics
	BulkIndexCountInc()
	BulkIndexErrorCountInc()
	IndexedDocumentsCountAdd(count float64)
	IndexedDocumentsBytesAdd(bytes float64)
	SessionIndexingFailCountInc()
	ESClientReloadInc()
}

// bufferPool is the interface of the bytebufferpool.Pool.
// We define it so that testing is easier using mocking.
type bufferPool interface {
	Get() *bytebufferpool.ByteBuffer
	Put(buffer *bytebufferpool.ByteBuffer)
}
