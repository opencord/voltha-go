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
	"fmt"
	"github.com/opentracing/opentracing-go"
	jtracing "github.com/uber/jaeger-client-go"
	jcfg "github.com/uber/jaeger-client-go/config"
	"io"
	"os"
	"strings"
	"sync"
)

const (
	RootSpanNameKey = "op-name"
)

// Global Settings governing the Log Correlation and Tracing features. Should only
// be updated through the exposed public methods
type LogFeaturesManager struct {
	isTracePublishingEnabled bool
	isLogCorrelationEnabled  bool
	componentName            string // Name of component extracted from ENV variable
	activeTraceAgentAddress  string
	lock                     sync.Mutex
}

var globalLFM *LogFeaturesManager = &LogFeaturesManager{}

func GetGlobalLFM() *LogFeaturesManager {
	return globalLFM
}

// A Wrapper to utilize currently Active Tracer instance. The middleware library being used for generating
// Spans for GRPC API calls does not support dynamically setting the Active Tracer similar to the SetGlobalTracer method
// provided by OpenTracing API
type ActiveTracerProxy struct {
}

func (atw ActiveTracerProxy) StartSpan(operationName string, opts ...opentracing.StartSpanOption) opentracing.Span {
	return opentracing.GlobalTracer().StartSpan(operationName, opts...)
}

func (atw ActiveTracerProxy) Inject(sm opentracing.SpanContext, format interface{}, carrier interface{}) error {
	return opentracing.GlobalTracer().Inject(sm, format, carrier)
}

func (atw ActiveTracerProxy) Extract(format interface{}, carrier interface{}) (opentracing.SpanContext, error) {
	return opentracing.GlobalTracer().Extract(format, carrier)
}

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

// Wrapper to handle correct Closer call at the time of Process Termination
type traceCloser struct {
}

func (c traceCloser) Close() error {
	currentActiveTracer := opentracing.GlobalTracer()
	if currentActiveTracer != nil {
		if jTracer, ok := currentActiveTracer.(*jtracing.Tracer); ok {
			jTracer.Close()
		}
	}

	return nil
}

// Method to Initialize Jaeger based Tracing client based on initial status of Tracing Publish and Log Correlation
func (lfm *LogFeaturesManager) InitTracingAndLogCorrelation(tracePublishEnabled bool, traceAgentAddress string, logCorrelationEnabled bool) (io.Closer, error) {
	lfm.componentName = os.Getenv("COMPONENT_NAME")
	if lfm.componentName == "" {
		return nil, errors.New("Unable to retrieve PoD Component Name from Runtime env")
	}

	lfm.lock.Lock()
	defer lfm.lock.Unlock()

	// Use NoopTracer when both Tracing Publishing and Log Correlation are disabled
	if !tracePublishEnabled && !logCorrelationEnabled {
		logger.Info(context.Background(), "Skipping Global Tracer initialization as both Trace publish and Log correlation are configured as disabled")
		lfm.isTracePublishingEnabled = false
		lfm.isLogCorrelationEnabled = false
		opentracing.SetGlobalTracer(opentracing.NoopTracer{})
		return traceCloser{}, nil
	}

	tracer, _, err := lfm.constructJaegerTracer(tracePublishEnabled, traceAgentAddress, true)
	if err != nil {
		return nil, err
	}

	// Initialize variables representing Active Status
	opentracing.SetGlobalTracer(tracer)
	lfm.isTracePublishingEnabled = tracePublishEnabled
	lfm.activeTraceAgentAddress = traceAgentAddress
	lfm.isLogCorrelationEnabled = logCorrelationEnabled
	return traceCloser{}, nil
}

// Method to replace Active Tracer along with graceful closer of previous tracer
func (lfm *LogFeaturesManager) replaceActiveTracer(tracer opentracing.Tracer) {
	currentActiveTracer := opentracing.GlobalTracer()
	opentracing.SetGlobalTracer(tracer)

	if currentActiveTracer != nil {
		if jTracer, ok := currentActiveTracer.(*jtracing.Tracer); ok {
			jTracer.Close()
		}
	}
}

func (lfm *LogFeaturesManager) GetLogCorrelationStatus() bool {
	lfm.lock.Lock()
	defer lfm.lock.Unlock()

	return lfm.isLogCorrelationEnabled
}

func (lfm *LogFeaturesManager) SetLogCorrelationStatus(isEnabled bool) {
	lfm.lock.Lock()
	defer lfm.lock.Unlock()

	if isEnabled == lfm.isLogCorrelationEnabled {
		logger.Debugf(context.Background(), "Ignoring Log Correlation Set operation with value %t; current Status same as desired", isEnabled)
		return
	}

	if isEnabled {
		// Construct new Tracer instance if Log Correlation has been enabled and current active tracer is a NoopTracer instance.
		// Continue using the earlier tracer instance in case of any error
		if _, ok := opentracing.GlobalTracer().(opentracing.NoopTracer); ok {
			tracer, _, err := lfm.constructJaegerTracer(lfm.isTracePublishingEnabled, lfm.activeTraceAgentAddress, false)
			if err != nil {
				logger.Warnf(context.Background(), "Log Correlation Enable operation failed with error: %s", err.Error())
				return
			}

			lfm.replaceActiveTracer(tracer)
		}

		lfm.isLogCorrelationEnabled = true
		logger.Info(context.Background(), "Log Correlation has been enabled")

	} else {
		// Switch to NoopTracer when Log Correlation has been disabled and Tracing Publish is already disabled
		if _, ok := opentracing.GlobalTracer().(opentracing.NoopTracer); !ok && !lfm.isTracePublishingEnabled {
			lfm.replaceActiveTracer(opentracing.NoopTracer{})
		}

		lfm.isLogCorrelationEnabled = false
		logger.Info(context.Background(), "Log Correlation has been disabled")
	}
}

func (lfm *LogFeaturesManager) GetTracePublishingStatus() bool {
	lfm.lock.Lock()
	defer lfm.lock.Unlock()

	return lfm.isTracePublishingEnabled
}

func (lfm *LogFeaturesManager) SetTracePublishingStatus(isEnabled bool) {
	lfm.lock.Lock()
	defer lfm.lock.Unlock()

	if isEnabled == lfm.isTracePublishingEnabled {
		logger.Debugf(context.Background(), "Ignoring Trace Publishing Set operation with value %t; current Status same as desired", isEnabled)
		return
	}

	if isEnabled {
		// Construct new Tracer instance if Tracing Publish has been enabled (even if a Jaeger instance is already active)
		// This is needed to ensure that a fresh lookup of Jaeger Agent address is performed again while performing
		// Disable-Enable of Tracing
		tracer, _, err := lfm.constructJaegerTracer(isEnabled, lfm.activeTraceAgentAddress, false)
		if err != nil {
			logger.Warnf(context.Background(), "Trace Publishing Enable operation failed with error: %s", err.Error())
			return
		}
		lfm.replaceActiveTracer(tracer)

		lfm.isTracePublishingEnabled = true
		logger.Info(context.Background(), "Tracing Publishing has been enabled")
	} else {
		// Switch to NoopTracer when Tracing Publish has been disabled and Log Correlation is already disabled
		if !lfm.isLogCorrelationEnabled {
			lfm.replaceActiveTracer(opentracing.NoopTracer{})
		} else {
			// Else construct a new Jaeger Instance with publishing disabled
			tracer, _, err := lfm.constructJaegerTracer(isEnabled, lfm.activeTraceAgentAddress, false)
			if err != nil {
				logger.Warnf(context.Background(), "Trace Publishing Disable operation failed with error: %s", err.Error())
				return
			}
			lfm.replaceActiveTracer(tracer)
		}

		lfm.isTracePublishingEnabled = false
		logger.Info(context.Background(), "Tracing Publishing has been disabled")
	}
}

// Method to contruct a new Jaeger Tracer instance based on given Trace Agent address and Publish status.
// The last attribute indicates whether to use Loopback IP for creating Jaeger Client when the DNS lookup
// of supplied Trace Agent address has failed. It is fine to fallback during the initialization step, but
// not later (when enabling/disabling the status dynamically)
func (lfm *LogFeaturesManager) constructJaegerTracer(tracePublishEnabled bool, traceAgentAddress string, fallbackToLoopbackAllowed bool) (opentracing.Tracer, io.Closer, error) {
	cfg := jcfg.Configuration{ServiceName: lfm.componentName}

	var err error
	var jReporterConfig jcfg.ReporterConfig
	var jReporterCfgOption jtracing.Reporter

	logger.Info(context.Background(), "Constructing new Jaeger Tracer instance")

	// Attempt Trace Agent Address first; will fallback to Loopback IP if it fails
	jReporterConfig = jcfg.ReporterConfig{LocalAgentHostPort: traceAgentAddress, LogSpans: true}
	jReporterCfgOption, err = jReporterConfig.NewReporter(lfm.componentName, jtracing.NewNullMetrics(), traceLogger{logger: logger.(*clogger)})

	if err != nil {
		if !fallbackToLoopbackAllowed {
			return nil, nil, errors.New("Reporter Creation for given Trace Agent address " + traceAgentAddress + " failed with error : " + err.Error())
		}

		logger.Infow(context.Background(), "Unable to create Reporter with given Trace Agent address",
			Fields{"error": err, "address": traceAgentAddress})
		// The Reporter initialization may fail due to Invalid Agent address or non-existent Agent (DNS lookup failure).
		// It is essential for Tracer Instance to still start for correct Span propagation needed for log correlation.
		// Thus, falback to use loopback IP for Reporter initialization before throwing back any error
		tracePublishEnabled = false

		jReporterConfig.LocalAgentHostPort = "127.0.0.1:6831"
		jReporterCfgOption, err = jReporterConfig.NewReporter(lfm.componentName, jtracing.NewNullMetrics(), traceLogger{logger: logger.(*clogger)})
		if err != nil {
			return nil, nil, errors.New("Failed to initialize Jaeger Tracing due to Reporter creation error : " + err.Error())
		}
	}

	// To start with, we are using Constant Sampling type
	samplerParam := 0 // 0: Do not publish span, 1: Publish
	if tracePublishEnabled {
		samplerParam = 1
	}
	jSamplerConfig := jcfg.SamplerConfig{Type: "const", Param: float64(samplerParam)}
	jSamplerCfgOption, err := jSamplerConfig.NewSampler(lfm.componentName, jtracing.NewNullMetrics())
	if err != nil {
		return nil, nil, errors.New("Unable to create Sampler : " + err.Error())
	}

	return cfg.NewTracer(jcfg.Reporter(jReporterCfgOption), jcfg.Sampler(jSamplerCfgOption))
}

func TerminateTracing(c io.Closer) {
	err := c.Close()
	if err != nil {
		logger.Error(context.Background(), "error-while-closing-jaeger-tracer", Fields{"err": err})
	}
}

// Extracts details of Execution Context as log fields from the Tracing Span injected into the
// context instance. Following log fields are extracted:
// 1. Operation Name : key as 'op-name' and value as Span operation name
// 2. Operation Id : key as 'op-id' and value as 64 bit Span Id in hex digits string
//
// Additionally, any tags present in Span are also extracted to use as log fields e.g. device-id.
//
// If no Span is found associated with context, blank slice is returned without any log fields
func (lfm *LogFeaturesManager) ExtractContextAttributes(ctx context.Context) []interface{} {
	if !lfm.isLogCorrelationEnabled {
		return make([]interface{}, 0)
	}

	attrMap := make(map[string]interface{})

	if ctx != nil {
		if span := opentracing.SpanFromContext(ctx); span != nil {
			if jspan, ok := span.(*jtracing.Span); ok {
				// Add Log fields for operation identified by Root Level Span (Trace)
				opId := fmt.Sprintf("%016x", jspan.SpanContext().TraceID().Low) // Using Sprintf to avoid removal of leading 0s
				opName := jspan.BaggageItem(RootSpanNameKey)

				taskId := fmt.Sprintf("%016x", uint64(jspan.SpanContext().SpanID())) // Using Sprintf to avoid removal of leading 0s
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

				processBaggageItems := func(k, v string) bool {
					if k != "rpc-span-name" {
						attrMap[k] = v
					}
					return true
				}

				jspan.SpanContext().ForeachBaggageItem(processBaggageItems)
			}
		}
	}

	return serializeMap(attrMap)
}

// Method to inject additional log fields into Span e.g. device-id
func EnrichSpan(ctx context.Context, keyAndValues ...Fields) {
	span := opentracing.SpanFromContext(ctx)
	if span != nil {
		if jspan, ok := span.(*jtracing.Span); ok {
			// Inject as a BaggageItem when the Span is the Root Span so that it propagates
			// across the components along with Root Span (called as Trace)
			// Else, inject as a Tag so that it is attached to the Child Task
			isRootSpan := false
			if jspan.SpanContext().TraceID().String() == jspan.SpanContext().SpanID().String() {
				isRootSpan = true
			}

			for _, field := range keyAndValues {
				for k, v := range field {
					if isRootSpan {
						span.SetBaggageItem(k, v.(string))
					} else {
						span.SetTag(k, v)
					}
				}
			}
		}
	}
}

// Method to inject Error into the Span in event of any operation failure
func MarkSpanError(ctx context.Context, err error) {
	span := opentracing.SpanFromContext(ctx)
	if span != nil {
		span.SetTag("error", true)
		span.SetTag("err", err)
	}
}

// Creates a Child Span from Parent Span embedded in passed context. Should be used before starting a new major
// operation in Synchronous or Asynchronous mode (go routine), such as following:
// 1. Start of all implemented External API methods unless using a interceptor for auto-injection of Span (Server side impl)
// 2. Just before calling an Third-Party lib which is invoking a External API (etcd, kafka)
// 3. In start of a Go Routine responsible for performing a major task involving significant duration
// 4. Any method which is suspected to be time consuming...
func CreateChildSpan(ctx context.Context, taskName string, keyAndValues ...Fields) (opentracing.Span, context.Context) {
	if !GetGlobalLFM().GetLogCorrelationStatus() && !GetGlobalLFM().GetTracePublishingStatus() {
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
	if !GetGlobalLFM().GetLogCorrelationStatus() && !GetGlobalLFM().GetTracePublishingStatus() {
		return opentracing.NoopTracer{}.StartSpan(taskName), ctx
	}

	var asyncSpan opentracing.Span
	var newCtx context.Context

	parentSpan := opentracing.SpanFromContext(ctx)

	// We should always be creating Aysnc span from a Valid parent span. If not, create a Child span instead
	if parentSpan == nil {
		logger.Warn(context.Background(), "Async span must be created with a Valid parent span only")
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
