/*
 * Copyright 2018-present Open Networking Foundation

 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at

 * http://www.apache.org/licenses/LICENSE-2.0

 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// File contains utility functions to support Open Tracing in conjunction with
// Enhanced Logging based on context propagation

package log

import (
	"context"
	"errors"
	"github.com/opentracing/opentracing-go"
	jtracing "github.com/uber/jaeger-client-go"
	jcfg "github.com/uber/jaeger-client-go/config"
	jmetrics "github.com/uber/jaeger-lib/metrics"
	"io"
	"os"
)

// This method will start the Tracing for a component using Component name injected from the Chart
// The close() method on returned Closer instance should be called in defer mode to gracefully
// terminate tracing on component shutdown
func StartTracing() (io.Closer, error) {
	componentName := os.Getenv("COMPONENT_NAME")
	if componentName == "" {
		return nil, errors.New("Unable to retrieve PoD Component Name from Runtime env")
	}

	// Use basic configuration to start with; will extend later to support dynamic config updates
	cfg := jcfg.Configuration{}

	jLoggerCfgOption := jcfg.Logger(jtracing.StdLogger)
	jMetricsFactoryCfgOption := jcfg.Metrics(jmetrics.NullFactory)

	return cfg.InitGlobalTracer(componentName, jLoggerCfgOption, jMetricsFactoryCfgOption)
}

// Extracts details of Execution Context as log fields from the Tracing Span injected into the
// context instance. Following log fields are extracted:
// 1. Operation Name : key as 'op-name' and value as Span operation name
// 2. Operation Id : key as 'op-id' and value as 64 bit Span Id in hex digits string
//
// Additionally, any tags present in Span are also extracted to use as log fields e.g. device-id.
//
// If no Span is found associated with context, blank slice is returned without any log fields
func ExtractContextAttributes(ctx context.Context) []interface{} {
	attrMap := make(map[string]interface{})

	if ctx != nil {
		if span := opentracing.SpanFromContext(ctx); span != nil {
			if jspan, ok := span.(*jtracing.Span); ok {
				opname := jspan.OperationName()
				spanid := jspan.SpanContext().SpanID().String()

				attrMap["op-id"] = spanid
				attrMap["op-name"] = opname

				for k, v := range jspan.Tags() {
					attrMap[k] = v
				}
			}
		}
	}

	return serializeMap(attrMap)
}

// Utility method to convert log Fields into array of interfaces expected by zap logger methods
func serializeMap(fields Fields) []interface{} {
	data := make([]interface{}, len(fields)*2)
	i := 0
	for k, v := range fields {
		data[i] = k
		data[i+1] = v
		i = i + 2
	}
	return data
}
