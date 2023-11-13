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
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog"
)

// Logger implements the estransport.Logger interface.
type Logger struct {
	zerolog.Logger
}

// LogRoundTrip prints the information about request and response.
func (l *Logger) LogRoundTrip(
	req *http.Request,
	res *http.Response,
	err error,
	start time.Time,
	dur time.Duration,
) error {
	var (
		level      zerolog.Level
		nReq, nRes int64
	)

	// Set error level.
	//
	switch {
	case err != nil:
		level = zerolog.ErrorLevel
	case res != nil && res.StatusCode > 0 && res.StatusCode < 300:
		level = zerolog.DebugLevel
	case res != nil && res.StatusCode > 299 && res.StatusCode < 500:
		level = zerolog.WarnLevel
	case res != nil && res.StatusCode > 499:
		level = zerolog.ErrorLevel
	default:
		level = zerolog.ErrorLevel
	}

	// Count number of bytes in request and response.
	if req != nil && req.Body != nil && req.Body != http.NoBody {
		nReq, _ = io.Copy(io.Discard, req.Body)
	}

	if res != nil && res.Body != nil && res.Body != http.NoBody {
		nRes, _ = io.Copy(io.Discard, res.Body)
	}

	// Log event.
	//
	l.WithLevel(level).
		Str("component", "elastic-client").
		Str("method", req.Method).
		Int("status_code", res.StatusCode).
		Dur("duration", dur).
		Time("start", start).
		Int64("req_bytes", nReq).
		Int64("res_bytes", nRes).
		Str("url", req.URL.String()).
		Send()

	return nil
}

// RequestBodyEnabled makes the client pass request body to logger.
func (l *Logger) RequestBodyEnabled() bool { return true }

// RequestBodyEnabled makes the client pass response body to logger.
func (l *Logger) ResponseBodyEnabled() bool { return true }
