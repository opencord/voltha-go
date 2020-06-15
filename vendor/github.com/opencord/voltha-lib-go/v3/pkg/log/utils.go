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
	"io"
	"io/ioutil"
	"os"
	"strings"
)

const (
	RootSpanNameKey = "op-name"
)

// Flag indicating whether to extract Log Fields from Span embedded in the received Context
var extractLogFieldsFromContext bool = true

// Flag indicating whether to process Span related operations; to save CPU cycles when disabled
var processSpanOperations bool = true

// Jaeger complaint Logger instance to redirect logs to Default Logger
type traceLogger struct {
	logger *clogger
}

func (tl traceLogger) Error(msg string) {
	tl.logger.Error(context.Background(), msg)
}

func (tl traceLogger) Infof(msg string, args ...interface{}) {
	// Tracing logs should be performed only at Debug Verbosity
	tl.logger.Debugf(context.Background(), msg, args...)
}

// This method will start the Tracing for a component using Component name injected from the Chart
// The close() method on returned Closer instance should be called in defer mode to gracefully
// terminate tracing on component shutdown
func InitTracingAndLogCorrelation(tracePublishEnabled bool, traceAgentAddress string, logCorrelationEnabled bool) (io.Closer, error) {
	if !tracePublishEnabled && !logCorrelationEnabled {
		defaultLogger.Info(context.Background(), "Skipping Global Tracer initialization as both Trace publish and Log correlation are configured as disabled")
		extractLogFieldsFromContext = false
		processSpanOperations = false
		return ioutil.NopCloser(strings.NewReader("")), nil
	}

	if !logCorrelationEnabled {
		defaultLogger.Info(context.Background(), "Disabling Log Fields extraction from context as configured")
		extractLogFieldsFromContext = false
	}

	componentName := os.Getenv("COMPONENT_NAME")
	if componentName == "" {
		return nil, errors.New("Unable to retrieve PoD Component Name from Runtime env")
	}

	// Use basic configuration to start with; will extend later to support dynamic config updates
	cfg := jcfg.Configuration{}

	var err error
	var jReporterConfig jcfg.ReporterConfig
	var jReporterCfgOption jtracing.Reporter

	// Attempt Trace Agent Address only if Trace Publishing is enabled; else directly use Loopback IP
	if tracePublishEnabled {
		jReporterConfig = jcfg.ReporterConfig{LocalAgentHostPort: traceAgentAddress, LogSpans: true}
		jReporterCfgOption, err = jReporterConfig.NewReporter(componentName, jtracing.NewNullMetrics(), traceLogger{logger: defaultLogger})

		if err != nil {
			defaultLogger.Errorw(context.Background(), "Unable to create Reporter with given Trace Agent address",
				Fields{"error": err, "address": traceAgentAddress})
			// The Reporter initialization may fail due to Invalid Agent address or non-existent Agent (DNS lookup failure).
			// It is essential for Tracer Instance to still start for correct Span propagation needed for log correlation.
			// Thus, falback to use loopback IP for Reporter initialization before throwing back any error
			tracePublishEnabled = false
		}
	}

	if !tracePublishEnabled {
		jReporterConfig.LocalAgentHostPort = "127.0.0.1:6831"
		jReporterCfgOption, err = jReporterConfig.NewReporter(componentName, jtracing.NewNullMetrics(), traceLogger{logger: defaultLogger})
		if err != nil {
			return nil, errors.New("Failed to initialize Jaeger Tracing due to Reporter creation error : " + err.Error())
		}
	}

	// To start with, we are using Constant Sampling type
	samplerParam := 0 // 0: Do not publish span, 1: Publish
	if tracePublishEnabled {
		samplerParam = 1
	}
	jSamplerConfig := jcfg.SamplerConfig{Type: "const", Param: float64(samplerParam)}
	jSamplerCfgOption, err := jSamplerConfig.NewSampler(componentName, jtracing.NewNullMetrics())
	if err != nil {
		return nil, errors.New("Unable to create Sampler : " + err.Error())
	}

	return cfg.InitGlobalTracer(componentName, jcfg.Reporter(jReporterCfgOption), jcfg.Sampler(jSamplerCfgOption))
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
	if !extractLogFieldsFromContext {
		return make([]interface{}, 0)
	}

	attrMap := make(map[string]interface{})

	if ctx != nil {
		if span := opentracing.SpanFromContext(ctx); span != nil {
			if jspan, ok := span.(*jtracing.Span); ok {
				// Add Log fields for operation identified by Root Level Span (Trace)
				opId := jspan.SpanContext().TraceID().String()
				opName := jspan.BaggageItem(RootSpanNameKey)

				taskId := jspan.SpanContext().SpanID().String()
				taskName := jspan.OperationName()

				if opName == "" {
					span.SetBaggageItem(RootSpanNameKey, taskName)
					opName = taskName
				}

				attrMap["op-id"] = opId
				attrMap["op-name"] = opName

				// Add Log fields for task identified by Current Span, if it is different
				// than operation
				if taskId != opId {
					attrMap["task-id"] = taskId
					attrMap["task-name"] = taskName
				}

				for k, v := range jspan.Tags() {
					// Ignore the special tags added by Jaeger, middleware (sampler.type, span.*) present in the span
					if strings.HasPrefix(k, "sampler.") || strings.HasPrefix(k, "span.") || k == "component" {
						continue
					}

					attrMap[k] = v
				}
			}
		}
	}

	return serializeMap(attrMap)
}

// Method to inject additional log fields into Span e.g. device-id
func EnrichSpan(ctx context.Context, keyAndValues ...Fields) {
	span := opentracing.SpanFromContext(ctx)
	if span != nil {
		for _, field := range keyAndValues {
			for k, v := range field {
				span.SetTag(k, v)
			}
		}
	}
}

// Method to inject Error into the Span in event of any operation failure
func MarkSpanError(ctx context.Context, err error) {
	span := opentracing.SpanFromContext(ctx)
	span.SetTag("error", true)
	span.SetTag("err", err)
}

// Creates a Child Span from Parent Span embedded in passed context. Should be used before starting a new major
// operation in Synchronous or Asynchronous mode (go routine), such as following:
// 1. Start of all implemented External API methods unless using a interceptor for auto-injection of Span (Server side impl)
// 2. Just before calling an Third-Party lib which is invoking a External API (etcd, kafka)
// 3. In start of a Go Routine responsible for performing a major task involving significant duration
// 4. Any method which is suspected to be time consuming...
func CreateChildSpan(ctx context.Context, taskName string, keyAndValues ...Fields) (opentracing.Span, context.Context) {
	if !processSpanOperations {
		return opentracing.NoopTracer{}.StartSpan(taskName), ctx
	}

	parentSpan := opentracing.SpanFromContext(ctx)
	childSpan, newCtx := opentracing.StartSpanFromContext(ctx, taskName)

	if parentSpan == nil || parentSpan.BaggageItem(RootSpanNameKey) == "" {
		childSpan.SetBaggageItem(RootSpanNameKey, taskName)
	}

	EnrichSpan(newCtx, keyAndValues...)
	return childSpan, newCtx
}

// Creates a Async Child Span with Follows-From relationship from Parent Span embedded in passed context.
// Should be used only in scenarios when
// a) There is dis-continuation in execution and thus result of Child span does not affect the Parent flow at all
// b) The execution of Child Span is guaranteed to start after the completion of Parent Span
// In case of any confusion, use CreateChildSpan method
// Some situations where this method would be suitable includes Kafka Async RPC call, Propagation of Event across
// a channel etc.
func CreateAsyncSpan(ctx context.Context, taskName string, keyAndValues ...Fields) (opentracing.Span, context.Context) {
	if !processSpanOperations {
		return opentracing.NoopTracer{}.StartSpan(taskName), ctx
	}

	var asyncSpan opentracing.Span
	var newCtx context.Context

	parentSpan := opentracing.SpanFromContext(ctx)

	// We should always be creating Aysnc span from a Valid parent span. If not, create a Child span instead
	if parentSpan == nil {
		defaultLogger.Warn(context.Background(), "Async span must be created with a Valid parent span only")
		asyncSpan, newCtx = opentracing.StartSpanFromContext(ctx, taskName)
	} else {
		// Use Background context as the base for Follows-from case; else new span is getting both Child and FollowsFrom relationship
		asyncSpan, newCtx = opentracing.StartSpanFromContext(context.Background(), taskName, opentracing.FollowsFrom(parentSpan.Context()))
	}

	if parentSpan == nil || parentSpan.BaggageItem(RootSpanNameKey) == "" {
		asyncSpan.SetBaggageItem(RootSpanNameKey, taskName)
	}

	EnrichSpan(newCtx, keyAndValues...)
	return asyncSpan, newCtx
}

// Extracts the span from Source context and injects into the supplied Target context.
// This should be used in situations wherein we are calling a time-sensitive operation (etcd update) and hence
// had a context.Background() used earlier to avoid any cancellation/timeout of operation by passed context.
// This will allow propagation of span with a different base context (and not the original context)
func WithSpanFromContext(targetCtx, sourceCtx context.Context) context.Context {
	span := opentracing.SpanFromContext(sourceCtx)
	return opentracing.ContextWithSpan(targetCtx, span)
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
