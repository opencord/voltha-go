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
package kafka

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/opentracing/opentracing-go"
)

const (
	DefaultMaxRetries     = 3
	DefaultRequestTimeout = 60000 // 60000 milliseconds - to handle a wider latency range
)

const (
	TransactionKey = "transactionID"
	FromTopic      = "fromTopic"
)

var ErrorTransactionNotAcquired = errors.New("transaction-not-acquired")
var ErrorTransactionInvalidId = errors.New("transaction-invalid-id")

// requestHandlerChannel represents an interface associated with a channel.  Whenever, an event is
// obtained from that channel, this interface is invoked.   This is used to handle
// async requests into the Core via the kafka messaging bus
type requestHandlerChannel struct {
	requesthandlerInterface interface{}
	ch                      <-chan *ic.InterContainerMessage
}

// transactionChannel represents a combination of a topic and a channel onto which a response received
// on the kafka bus will be sent to
type transactionChannel struct {
	topic *Topic
	ch    chan *ic.InterContainerMessage
}

type InterContainerProxy interface {
	Start(ctx context.Context) error
	Stop(ctx context.Context)
	GetDefaultTopic() *Topic
	InvokeRPC(ctx context.Context, rpc string, toTopic *Topic, replyToTopic *Topic, waitForResponse bool, key string, kvArgs ...*KVArg) (bool, *any.Any)
	InvokeAsyncRPC(ctx context.Context, rpc string, toTopic *Topic, replyToTopic *Topic, waitForResponse bool, key string, kvArgs ...*KVArg) chan *RpcResponse
	SubscribeWithRequestHandlerInterface(ctx context.Context, topic Topic, handler interface{}) error
	SubscribeWithDefaultRequestHandler(ctx context.Context, topic Topic, initialOffset int64) error
	UnSubscribeFromRequestHandler(ctx context.Context, topic Topic) error
	DeleteTopic(ctx context.Context, topic Topic) error
	EnableLivenessChannel(ctx context.Context, enable bool) chan bool
	SendLiveness(ctx context.Context) error
}

// interContainerProxy represents the messaging proxy
type interContainerProxy struct {
	kafkaAddress                   string
	defaultTopic                   *Topic
	defaultRequestHandlerInterface interface{}
	kafkaClient                    Client
	doneCh                         chan struct{}
	doneOnce                       sync.Once

	// This map is used to map a topic to an interface and channel.   When a request is received
	// on that channel (registered to the topic) then that interface is invoked.
	topicToRequestHandlerChannelMap   map[string]*requestHandlerChannel
	lockTopicRequestHandlerChannelMap sync.RWMutex

	// This map is used to map a channel to a response topic.   This channel handles all responses on that
	// channel for that topic and forward them to the appropriate consumers channel, using the
	// transactionIdToChannelMap.
	topicToResponseChannelMap   map[string]<-chan *ic.InterContainerMessage
	lockTopicResponseChannelMap sync.RWMutex

	// This map is used to map a transaction to a consumers channel.  This is used whenever a request has been
	// sent out and we are waiting for a response.
	transactionIdToChannelMap     map[string]*transactionChannel
	lockTransactionIdToChannelMap sync.RWMutex
}

type InterContainerProxyOption func(*interContainerProxy)

func InterContainerAddress(address string) InterContainerProxyOption {
	return func(args *interContainerProxy) {
		args.kafkaAddress = address
	}
}

func DefaultTopic(topic *Topic) InterContainerProxyOption {
	return func(args *interContainerProxy) {
		args.defaultTopic = topic
	}
}

func RequestHandlerInterface(handler interface{}) InterContainerProxyOption {
	return func(args *interContainerProxy) {
		args.defaultRequestHandlerInterface = handler
	}
}

func MsgClient(client Client) InterContainerProxyOption {
	return func(args *interContainerProxy) {
		args.kafkaClient = client
	}
}

func newInterContainerProxy(opts ...InterContainerProxyOption) *interContainerProxy {
	proxy := &interContainerProxy{
		kafkaAddress: DefaultKafkaAddress,
		doneCh:       make(chan struct{}),
	}

	for _, option := range opts {
		option(proxy)
	}

	return proxy
}

func NewInterContainerProxy(opts ...InterContainerProxyOption) InterContainerProxy {
	return newInterContainerProxy(opts...)
}

func (kp *interContainerProxy) Start(ctx context.Context) error {
	logger.Info(ctx, "Starting-Proxy")

	// Kafka MsgClient should already have been created.  If not, output fatal error
	if kp.kafkaClient == nil {
		logger.Fatal(ctx, "kafka-client-not-set")
	}

	// Start the kafka client
	if err := kp.kafkaClient.Start(ctx); err != nil {
		logger.Errorw(ctx, "Cannot-create-kafka-proxy", log.Fields{"error": err})
		return err
	}

	// Create the topic to response channel map
	kp.topicToResponseChannelMap = make(map[string]<-chan *ic.InterContainerMessage)
	//
	// Create the transactionId to Channel Map
	kp.transactionIdToChannelMap = make(map[string]*transactionChannel)

	// Create the topic to request channel map
	kp.topicToRequestHandlerChannelMap = make(map[string]*requestHandlerChannel)

	return nil
}

func (kp *interContainerProxy) Stop(ctx context.Context) {
	logger.Info(ctx, "stopping-intercontainer-proxy")
	kp.doneOnce.Do(func() { close(kp.doneCh) })
	// TODO : Perform cleanup
	kp.kafkaClient.Stop(ctx)
	err := kp.deleteAllTopicRequestHandlerChannelMap(ctx)
	if err != nil {
		logger.Errorw(ctx, "failed-delete-all-topic-request-handler-channel-map", log.Fields{"error": err})
	}
	err = kp.deleteAllTopicResponseChannelMap(ctx)
	if err != nil {
		logger.Errorw(ctx, "failed-delete-all-topic-response-channel-map", log.Fields{"error": err})
	}
	kp.deleteAllTransactionIdToChannelMap(ctx)
}

func (kp *interContainerProxy) GetDefaultTopic() *Topic {
	return kp.defaultTopic
}

// InvokeAsyncRPC is used to make an RPC request asynchronously
func (kp *interContainerProxy) InvokeAsyncRPC(ctx context.Context, rpc string, toTopic *Topic, replyToTopic *Topic,
	waitForResponse bool, key string, kvArgs ...*KVArg) chan *RpcResponse {

	logger.Debugw(ctx, "InvokeAsyncRPC", log.Fields{"rpc": rpc, "key": key})

	spanArg, span, ctx := kp.embedSpanAsArg(ctx, rpc, !waitForResponse)
	if spanArg != nil {
		kvArgs = append(kvArgs, &spanArg[0])
	}
	defer span.Finish()

	//	If a replyToTopic is provided then we use it, otherwise just use the  default toTopic.  The replyToTopic is
	// typically the device ID.
	responseTopic := replyToTopic
	if responseTopic == nil {
		responseTopic = kp.GetDefaultTopic()
	}

	chnl := make(chan *RpcResponse)

	go func() {

		// once we're done,
		// close the response channel
		defer close(chnl)

		var err error
		var protoRequest *ic.InterContainerMessage

		// Encode the request
		protoRequest, err = encodeRequest(ctx, rpc, toTopic, responseTopic, key, kvArgs...)
		if err != nil {
			logger.Warnw(ctx, "cannot-format-request", log.Fields{"rpc": rpc, "error": err})
			log.MarkSpanError(ctx, errors.New("cannot-format-request"))
			chnl <- NewResponse(RpcFormattingError, err, nil)
			return
		}

		// Subscribe for response, if needed, before sending request
		var ch <-chan *ic.InterContainerMessage
		if ch, err = kp.subscribeForResponse(ctx, *responseTopic, protoRequest.Header.Id); err != nil {
			logger.Errorw(ctx, "failed-to-subscribe-for-response", log.Fields{"error": err, "toTopic": toTopic.Name})
			log.MarkSpanError(ctx, errors.New("failed-to-subscribe-for-response"))
			chnl <- NewResponse(RpcTransportError, err, nil)
			return
		}

		// Send request - if the topic is formatted with a device Id then we will send the request using a
		// specific key, hence ensuring a single partition is used to publish the request.  This ensures that the
		// subscriber on that topic will receive the request in the order it was sent.  The key used is the deviceId.
		logger.Debugw(ctx, "sending-msg", log.Fields{"rpc": rpc, "toTopic": toTopic, "replyTopic": responseTopic, "key": key, "xId": protoRequest.Header.Id})

		// if the message is not sent on kafka publish an event an close the channel
		if err = kp.kafkaClient.Send(ctx, protoRequest, toTopic, key); err != nil {
			chnl <- NewResponse(RpcTransportError, err, nil)
			return
		}

		// if the client is not waiting for a response send the ack and close the channel
		chnl <- NewResponse(RpcSent, nil, nil)
		if !waitForResponse {
			return
		}

		defer func() {
			// Remove the subscription for a response on return
			if err := kp.unSubscribeForResponse(ctx, protoRequest.Header.Id); err != nil {
				logger.Warnw(ctx, "invoke-async-rpc-unsubscriber-for-response-failed", log.Fields{"err": err})
			}
		}()

		// Wait for response as well as timeout or cancellation
		select {
		case msg, ok := <-ch:
			if !ok {
				logger.Warnw(ctx, "channel-closed", log.Fields{"rpc": rpc, "replyTopic": replyToTopic.Name})
				log.MarkSpanError(ctx, errors.New("channel-closed"))
				chnl <- NewResponse(RpcTransportError, status.Error(codes.Aborted, "channel closed"), nil)
			}
			logger.Debugw(ctx, "received-response", log.Fields{"rpc": rpc, "msgHeader": msg.Header})
			if responseBody, err := decodeResponse(ctx, msg); err != nil {
				chnl <- NewResponse(RpcReply, err, nil)
			} else {
				if responseBody.Success {
					chnl <- NewResponse(RpcReply, nil, responseBody.Result)
				} else {
					// response body contains an error
					unpackErr := &ic.Error{}
					if err := ptypes.UnmarshalAny(responseBody.Result, unpackErr); err != nil {
						chnl <- NewResponse(RpcReply, err, nil)
					} else {
						chnl <- NewResponse(RpcReply, status.Error(codes.Internal, unpackErr.Reason), nil)
					}
				}
			}
		case <-ctx.Done():
			logger.Errorw(ctx, "context-cancelled", log.Fields{"rpc": rpc, "ctx": ctx.Err()})
			log.MarkSpanError(ctx, errors.New("context-cancelled"))
			err := status.Error(codes.DeadlineExceeded, ctx.Err().Error())
			chnl <- NewResponse(RpcTimeout, err, nil)
		case <-kp.doneCh:
			chnl <- NewResponse(RpcSystemClosing, nil, nil)
			logger.Warnw(ctx, "received-exit-signal", log.Fields{"toTopic": toTopic.Name, "rpc": rpc})
		}
	}()
	return chnl
}

// Method to extract Open-tracing Span from Context and serialize it for transport over Kafka embedded as a additional argument.
// Additional argument is injected using key as "span" and value as Span marshalled into a byte slice
//
// The span name is automatically constructed using the RPC name with following convention (<rpc-name> represents name of invoked method):
// - RPC invoked in Sync manner (WaitForResponse=true) : kafka-rpc-<rpc-name>
// - RPC invoked in Async manner (WaitForResponse=false) : kafka-async-rpc-<rpc-name>
// - Inter Adapter RPC invoked in Sync manner (WaitForResponse=true) : kafka-inter-adapter-rpc-<rpc-name>
// - Inter Adapter RPC invoked in Async manner (WaitForResponse=false) : kafka-inter-adapter-async-rpc-<rpc-name>
func (kp *interContainerProxy) embedSpanAsArg(ctx context.Context, rpc string, isAsync bool) ([]KVArg, opentracing.Span, context.Context) {
	var err error
	var newCtx context.Context
	var spanToInject opentracing.Span

	var spanName strings.Builder
	spanName.WriteString("kafka-")

	// In case of inter adapter message, use Msg Type for constructing RPC name
	if rpc == "process_inter_adapter_message" {
		if msgType, ok := ctx.Value("inter-adapter-msg-type").(ic.InterAdapterMessageType_Types); ok {
			spanName.WriteString("inter-adapter-")
			rpc = msgType.String()
		}
	}

	if isAsync {
		spanName.WriteString("async-rpc-")
	} else {
		spanName.WriteString("rpc-")
	}
	spanName.WriteString(rpc)

	if isAsync {
		spanToInject, newCtx = log.CreateAsyncSpan(ctx, spanName.String())
	} else {
		spanToInject, newCtx = log.CreateChildSpan(ctx, spanName.String())
	}

	spanToInject.SetBaggageItem("rpc-span-name", spanName.String())

	textMapCarrier := opentracing.TextMapCarrier(make(map[string]string))
	if err = opentracing.GlobalTracer().Inject(spanToInject.Context(), opentracing.TextMap, textMapCarrier); err != nil {
		logger.Warnw(ctx, "unable-to-serialize-span-to-textmap", log.Fields{"span": spanToInject, "error": err})
		return nil, spanToInject, newCtx
	}

	var textMapJson []byte
	if textMapJson, err = json.Marshal(textMapCarrier); err != nil {
		logger.Warnw(ctx, "unable-to-marshal-textmap-to-json-string", log.Fields{"textMap": textMapCarrier, "error": err})
		return nil, spanToInject, newCtx
	}

	spanArg := make([]KVArg, 1)
	spanArg[0] = KVArg{Key: "span", Value: &ic.StrType{Val: string(textMapJson)}}
	return spanArg, spanToInject, newCtx
}

// InvokeRPC is used to send a request to a given topic
func (kp *interContainerProxy) InvokeRPC(ctx context.Context, rpc string, toTopic *Topic, replyToTopic *Topic,
	waitForResponse bool, key string, kvArgs ...*KVArg) (bool, *any.Any) {

	spanArg, span, ctx := kp.embedSpanAsArg(ctx, rpc, false)
	if spanArg != nil {
		kvArgs = append(kvArgs, &spanArg[0])
	}
	defer span.Finish()

	//	If a replyToTopic is provided then we use it, otherwise just use the  default toTopic.  The replyToTopic is
	// typically the device ID.
	responseTopic := replyToTopic
	if responseTopic == nil {
		responseTopic = kp.defaultTopic
	}

	// Encode the request
	protoRequest, err := encodeRequest(ctx, rpc, toTopic, responseTopic, key, kvArgs...)
	if err != nil {
		logger.Warnw(ctx, "cannot-format-request", log.Fields{"rpc": rpc, "error": err})
		log.MarkSpanError(ctx, errors.New("cannot-format-request"))
		return false, nil
	}

	// Subscribe for response, if needed, before sending request
	var ch <-chan *ic.InterContainerMessage
	if waitForResponse {
		var err error
		if ch, err = kp.subscribeForResponse(ctx, *responseTopic, protoRequest.Header.Id); err != nil {
			logger.Errorw(ctx, "failed-to-subscribe-for-response", log.Fields{"error": err, "toTopic": toTopic.Name})
		}
	}

	// Send request - if the topic is formatted with a device Id then we will send the request using a
	// specific key, hence ensuring a single partition is used to publish the request.  This ensures that the
	// subscriber on that topic will receive the request in the order it was sent.  The key used is the deviceId.
	//key := GetDeviceIdFromTopic(*toTopic)
	logger.Debugw(ctx, "sending-msg", log.Fields{"rpc": rpc, "toTopic": toTopic, "replyTopic": responseTopic, "key": key, "xId": protoRequest.Header.Id})
	go func() {
		if err := kp.kafkaClient.Send(ctx, protoRequest, toTopic, key); err != nil {
			log.MarkSpanError(ctx, errors.New("send-failed"))
			logger.Errorw(ctx, "send-failed", log.Fields{
				"topic": toTopic,
				"key":   key,
				"error": err})
		}
	}()

	if waitForResponse {
		// Create a child context based on the parent context, if any
		var cancel context.CancelFunc
		childCtx := context.Background()
		if ctx == nil {
			ctx, cancel = context.WithTimeout(context.Background(), DefaultRequestTimeout*time.Millisecond)
		} else {
			childCtx, cancel = context.WithTimeout(ctx, DefaultRequestTimeout*time.Millisecond)
		}
		defer cancel()

		// Wait for response as well as timeout or cancellation
		// Remove the subscription for a response on return
		defer func() {
			if err := kp.unSubscribeForResponse(ctx, protoRequest.Header.Id); err != nil {
				logger.Errorw(ctx, "response-unsubscribe-failed", log.Fields{
					"id":    protoRequest.Header.Id,
					"error": err})
			}
		}()
		select {
		case msg, ok := <-ch:
			if !ok {
				logger.Warnw(ctx, "channel-closed", log.Fields{"rpc": rpc, "replyTopic": replyToTopic.Name})
				log.MarkSpanError(ctx, errors.New("channel-closed"))
				protoError := &ic.Error{Reason: "channel-closed"}
				var marshalledArg *any.Any
				if marshalledArg, err = ptypes.MarshalAny(protoError); err != nil {
					return false, nil // Should never happen
				}
				return false, marshalledArg
			}
			logger.Debugw(ctx, "received-response", log.Fields{"rpc": rpc, "msgHeader": msg.Header})
			var responseBody *ic.InterContainerResponseBody
			var err error
			if responseBody, err = decodeResponse(ctx, msg); err != nil {
				logger.Errorw(ctx, "decode-response-error", log.Fields{"error": err})
				// FIXME we should return something
			}
			return responseBody.Success, responseBody.Result
		case <-ctx.Done():
			logger.Debugw(ctx, "context-cancelled", log.Fields{"rpc": rpc, "ctx": ctx.Err()})
			log.MarkSpanError(ctx, errors.New("context-cancelled"))
			//	 pack the error as proto any type
			protoError := &ic.Error{Reason: ctx.Err().Error(), Code: ic.ErrorCode_DEADLINE_EXCEEDED}

			var marshalledArg *any.Any
			if marshalledArg, err = ptypes.MarshalAny(protoError); err != nil {
				return false, nil // Should never happen
			}
			return false, marshalledArg
		case <-childCtx.Done():
			logger.Debugw(ctx, "context-cancelled", log.Fields{"rpc": rpc, "ctx": childCtx.Err()})
			log.MarkSpanError(ctx, errors.New("context-cancelled"))
			//	 pack the error as proto any type
			protoError := &ic.Error{Reason: childCtx.Err().Error(), Code: ic.ErrorCode_DEADLINE_EXCEEDED}

			var marshalledArg *any.Any
			if marshalledArg, err = ptypes.MarshalAny(protoError); err != nil {
				return false, nil // Should never happen
			}
			return false, marshalledArg
		case <-kp.doneCh:
			logger.Infow(ctx, "received-exit-signal", log.Fields{"toTopic": toTopic.Name, "rpc": rpc})
			return true, nil
		}
	}
	return true, nil
}

// SubscribeWithRequestHandlerInterface allows a caller to assign a target object to be invoked automatically
// when a message is received on a given topic
func (kp *interContainerProxy) SubscribeWithRequestHandlerInterface(ctx context.Context, topic Topic, handler interface{}) error {

	// Subscribe to receive messages for that topic
	var ch <-chan *ic.InterContainerMessage
	var err error
	if ch, err = kp.kafkaClient.Subscribe(ctx, &topic); err != nil {
		//if ch, err = kp.Subscribe(topic); err != nil {
		logger.Errorw(ctx, "failed-to-subscribe", log.Fields{"error": err, "topic": topic.Name})
		return err
	}

	kp.defaultRequestHandlerInterface = handler
	kp.addToTopicRequestHandlerChannelMap(topic.Name, &requestHandlerChannel{requesthandlerInterface: handler, ch: ch})
	// Launch a go routine to receive and process kafka messages
	go kp.waitForMessages(ctx, ch, topic, handler)

	return nil
}

// SubscribeWithDefaultRequestHandler allows a caller to add a topic to an existing target object to be invoked automatically
// when a message is received on a given topic.  So far there is only 1 target registered per microservice
func (kp *interContainerProxy) SubscribeWithDefaultRequestHandler(ctx context.Context, topic Topic, initialOffset int64) error {
	// Subscribe to receive messages for that topic
	var ch <-chan *ic.InterContainerMessage
	var err error
	if ch, err = kp.kafkaClient.Subscribe(ctx, &topic, &KVArg{Key: Offset, Value: initialOffset}); err != nil {
		logger.Errorw(ctx, "failed-to-subscribe", log.Fields{"error": err, "topic": topic.Name})
		return err
	}
	kp.addToTopicRequestHandlerChannelMap(topic.Name, &requestHandlerChannel{requesthandlerInterface: kp.defaultRequestHandlerInterface, ch: ch})

	// Launch a go routine to receive and process kafka messages
	go kp.waitForMessages(ctx, ch, topic, kp.defaultRequestHandlerInterface)

	return nil
}

func (kp *interContainerProxy) UnSubscribeFromRequestHandler(ctx context.Context, topic Topic) error {
	return kp.deleteFromTopicRequestHandlerChannelMap(ctx, topic.Name)
}

func (kp *interContainerProxy) deleteFromTopicResponseChannelMap(ctx context.Context, topic string) error {
	kp.lockTopicResponseChannelMap.Lock()
	defer kp.lockTopicResponseChannelMap.Unlock()
	if _, exist := kp.topicToResponseChannelMap[topic]; exist {
		// Unsubscribe to this topic first - this will close the subscribed channel
		var err error
		if err = kp.kafkaClient.UnSubscribe(ctx, &Topic{Name: topic}, kp.topicToResponseChannelMap[topic]); err != nil {
			logger.Errorw(ctx, "unsubscribing-error", log.Fields{"topic": topic})
		}
		delete(kp.topicToResponseChannelMap, topic)
		return err
	} else {
		return fmt.Errorf("%s-Topic-not-found", topic)
	}
}

// nolint: unused
func (kp *interContainerProxy) deleteAllTopicResponseChannelMap(ctx context.Context) error {
	logger.Debug(ctx, "delete-all-topic-response-channel")
	kp.lockTopicResponseChannelMap.Lock()
	defer kp.lockTopicResponseChannelMap.Unlock()
	var unsubscribeFailTopics []string
	for topic := range kp.topicToResponseChannelMap {
		// Unsubscribe to this topic first - this will close the subscribed channel
		if err := kp.kafkaClient.UnSubscribe(ctx, &Topic{Name: topic}, kp.topicToResponseChannelMap[topic]); err != nil {
			unsubscribeFailTopics = append(unsubscribeFailTopics, topic)
			logger.Errorw(ctx, "unsubscribing-error", log.Fields{"topic": topic, "error": err})
			// Do not return. Continue to try to unsubscribe to other topics.
		} else {
			// Only delete from channel map if successfully unsubscribed.
			delete(kp.topicToResponseChannelMap, topic)
		}
	}
	if len(unsubscribeFailTopics) > 0 {
		return fmt.Errorf("unsubscribe-errors: %v", unsubscribeFailTopics)
	}
	return nil
}

func (kp *interContainerProxy) addToTopicRequestHandlerChannelMap(topic string, arg *requestHandlerChannel) {
	kp.lockTopicRequestHandlerChannelMap.Lock()
	defer kp.lockTopicRequestHandlerChannelMap.Unlock()
	if _, exist := kp.topicToRequestHandlerChannelMap[topic]; !exist {
		kp.topicToRequestHandlerChannelMap[topic] = arg
	}
}

func (kp *interContainerProxy) deleteFromTopicRequestHandlerChannelMap(ctx context.Context, topic string) error {
	kp.lockTopicRequestHandlerChannelMap.Lock()
	defer kp.lockTopicRequestHandlerChannelMap.Unlock()
	if _, exist := kp.topicToRequestHandlerChannelMap[topic]; exist {
		// Close the kafka client client first by unsubscribing to this topic
		if err := kp.kafkaClient.UnSubscribe(ctx, &Topic{Name: topic}, kp.topicToRequestHandlerChannelMap[topic].ch); err != nil {
			return err
		}
		delete(kp.topicToRequestHandlerChannelMap, topic)
		return nil
	} else {
		return fmt.Errorf("%s-Topic-not-found", topic)
	}
}

// nolint: unused
func (kp *interContainerProxy) deleteAllTopicRequestHandlerChannelMap(ctx context.Context) error {
	logger.Debug(ctx, "delete-all-topic-request-channel")
	kp.lockTopicRequestHandlerChannelMap.Lock()
	defer kp.lockTopicRequestHandlerChannelMap.Unlock()
	var unsubscribeFailTopics []string
	for topic := range kp.topicToRequestHandlerChannelMap {
		// Close the kafka client client first by unsubscribing to this topic
		if err := kp.kafkaClient.UnSubscribe(ctx, &Topic{Name: topic}, kp.topicToRequestHandlerChannelMap[topic].ch); err != nil {
			unsubscribeFailTopics = append(unsubscribeFailTopics, topic)
			logger.Errorw(ctx, "unsubscribing-error", log.Fields{"topic": topic, "error": err})
			// Do not return. Continue to try to unsubscribe to other topics.
		} else {
			// Only delete from channel map if successfully unsubscribed.
			delete(kp.topicToRequestHandlerChannelMap, topic)
		}
	}
	if len(unsubscribeFailTopics) > 0 {
		return fmt.Errorf("unsubscribe-errors: %v", unsubscribeFailTopics)
	}
	return nil
}

func (kp *interContainerProxy) addToTransactionIdToChannelMap(id string, topic *Topic, arg chan *ic.InterContainerMessage) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	if _, exist := kp.transactionIdToChannelMap[id]; !exist {
		kp.transactionIdToChannelMap[id] = &transactionChannel{topic: topic, ch: arg}
	}
}

func (kp *interContainerProxy) deleteFromTransactionIdToChannelMap(id string) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	if transChannel, exist := kp.transactionIdToChannelMap[id]; exist {
		// Close the channel first
		close(transChannel.ch)
		delete(kp.transactionIdToChannelMap, id)
	}
}

func (kp *interContainerProxy) deleteTopicTransactionIdToChannelMap(id string) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	for key, value := range kp.transactionIdToChannelMap {
		if value.topic.Name == id {
			close(value.ch)
			delete(kp.transactionIdToChannelMap, key)
		}
	}
}

// nolint: unused
func (kp *interContainerProxy) deleteAllTransactionIdToChannelMap(ctx context.Context) {
	logger.Debug(ctx, "delete-all-transaction-id-channel-map")
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	for key, value := range kp.transactionIdToChannelMap {
		close(value.ch)
		delete(kp.transactionIdToChannelMap, key)
	}
}

func (kp *interContainerProxy) DeleteTopic(ctx context.Context, topic Topic) error {
	// If we have any consumers on that topic we need to close them
	if err := kp.deleteFromTopicResponseChannelMap(ctx, topic.Name); err != nil {
		logger.Errorw(ctx, "delete-from-topic-responsechannelmap-failed", log.Fields{"error": err})
	}
	if err := kp.deleteFromTopicRequestHandlerChannelMap(ctx, topic.Name); err != nil {
		logger.Errorw(ctx, "delete-from-topic-requesthandlerchannelmap-failed", log.Fields{"error": err})
	}
	kp.deleteTopicTransactionIdToChannelMap(topic.Name)

	return kp.kafkaClient.DeleteTopic(ctx, &topic)
}

func encodeReturnedValue(ctx context.Context, returnedVal interface{}) (*any.Any, error) {
	// Encode the response argument - needs to be a proto message
	if returnedVal == nil {
		return nil, nil
	}
	protoValue, ok := returnedVal.(proto.Message)
	if !ok {
		logger.Warnw(ctx, "response-value-not-proto-message", log.Fields{"error": ok, "returnVal": returnedVal})
		err := errors.New("response-value-not-proto-message")
		return nil, err
	}

	// Marshal the returned value, if any
	var marshalledReturnedVal *any.Any
	var err error
	if marshalledReturnedVal, err = ptypes.MarshalAny(protoValue); err != nil {
		logger.Warnw(ctx, "cannot-marshal-returned-val", log.Fields{"error": err})
		return nil, err
	}
	return marshalledReturnedVal, nil
}

func encodeDefaultFailedResponse(ctx context.Context, request *ic.InterContainerMessage) *ic.InterContainerMessage {
	responseHeader := &ic.Header{
		Id:        request.Header.Id,
		Type:      ic.MessageType_RESPONSE,
		FromTopic: request.Header.ToTopic,
		ToTopic:   request.Header.FromTopic,
		Timestamp: ptypes.TimestampNow(),
	}
	responseBody := &ic.InterContainerResponseBody{
		Success: false,
		Result:  nil,
	}
	var marshalledResponseBody *any.Any
	var err error
	// Error should never happen here
	if marshalledResponseBody, err = ptypes.MarshalAny(responseBody); err != nil {
		logger.Warnw(ctx, "cannot-marshal-failed-response-body", log.Fields{"error": err})
	}

	return &ic.InterContainerMessage{
		Header: responseHeader,
		Body:   marshalledResponseBody,
	}

}

//formatRequest formats a request to send over kafka and returns an InterContainerMessage message on success
//or an error on failure
func encodeResponse(ctx context.Context, request *ic.InterContainerMessage, success bool, returnedValues ...interface{}) (*ic.InterContainerMessage, error) {
	//logger.Debugw(ctx, "encodeResponse", log.Fields{"success": success, "returnedValues": returnedValues})
	responseHeader := &ic.Header{
		Id:        request.Header.Id,
		Type:      ic.MessageType_RESPONSE,
		FromTopic: request.Header.ToTopic,
		ToTopic:   request.Header.FromTopic,
		KeyTopic:  request.Header.KeyTopic,
		Timestamp: ptypes.TimestampNow(),
	}

	// Go over all returned values
	var marshalledReturnedVal *any.Any
	var err error

	// for now we support only 1 returned value - (excluding the error)
	if len(returnedValues) > 0 {
		if marshalledReturnedVal, err = encodeReturnedValue(ctx, returnedValues[0]); err != nil {
			logger.Warnw(ctx, "cannot-marshal-response-body", log.Fields{"error": err})
		}
	}

	responseBody := &ic.InterContainerResponseBody{
		Success: success,
		Result:  marshalledReturnedVal,
	}

	// Marshal the response body
	var marshalledResponseBody *any.Any
	if marshalledResponseBody, err = ptypes.MarshalAny(responseBody); err != nil {
		logger.Warnw(ctx, "cannot-marshal-response-body", log.Fields{"error": err})
		return nil, err
	}

	return &ic.InterContainerMessage{
		Header: responseHeader,
		Body:   marshalledResponseBody,
	}, nil
}

func CallFuncByName(ctx context.Context, myClass interface{}, funcName string, params ...interface{}) (out []reflect.Value, err error) {
	myClassValue := reflect.ValueOf(myClass)
	// Capitalize the first letter in the funcName to workaround the first capital letters required to
	// invoke a function from a different package
	funcName = strings.Title(funcName)
	m := myClassValue.MethodByName(funcName)
	if !m.IsValid() {
		return make([]reflect.Value, 0), fmt.Errorf("method-not-found \"%s\"", funcName)
	}
	in := make([]reflect.Value, len(params)+1)
	in[0] = reflect.ValueOf(ctx)
	for i, param := range params {
		in[i+1] = reflect.ValueOf(param)
	}
	out = m.Call(in)
	return
}

func (kp *interContainerProxy) addTransactionId(ctx context.Context, transactionId string, currentArgs []*ic.Argument) []*ic.Argument {
	arg := &KVArg{
		Key:   TransactionKey,
		Value: &ic.StrType{Val: transactionId},
	}

	var marshalledArg *any.Any
	var err error
	if marshalledArg, err = ptypes.MarshalAny(&ic.StrType{Val: transactionId}); err != nil {
		logger.Warnw(ctx, "cannot-add-transactionId", log.Fields{"error": err})
		return currentArgs
	}
	protoArg := &ic.Argument{
		Key:   arg.Key,
		Value: marshalledArg,
	}
	return append(currentArgs, protoArg)
}

func (kp *interContainerProxy) addFromTopic(ctx context.Context, fromTopic string, currentArgs []*ic.Argument) []*ic.Argument {
	var marshalledArg *any.Any
	var err error
	if marshalledArg, err = ptypes.MarshalAny(&ic.StrType{Val: fromTopic}); err != nil {
		logger.Warnw(ctx, "cannot-add-transactionId", log.Fields{"error": err})
		return currentArgs
	}
	protoArg := &ic.Argument{
		Key:   FromTopic,
		Value: marshalledArg,
	}
	return append(currentArgs, protoArg)
}

// Method to extract the Span embedded in Kafka RPC request on the receiver side. If span is found embedded in the KV args (with key as "span"),
// it is de-serialized and injected into the Context to be carried forward by the RPC request processor thread.
// If no span is found embedded, even then a span is created with name as "kafka-rpc-<rpc-name>" to enrich the Context for RPC calls coming
// from components currently not sending the span (e.g. openonu adapter)
func (kp *interContainerProxy) enrichContextWithSpan(ctx context.Context, rpcName string, args []*ic.Argument) (opentracing.Span, context.Context) {

	for _, arg := range args {
		if arg.Key == "span" {
			var err error
			var textMapString ic.StrType
			if err = ptypes.UnmarshalAny(arg.Value, &textMapString); err != nil {
				logger.Warnw(ctx, "unable-to-unmarshal-kvarg-to-textmap-string", log.Fields{"value": arg.Value})
				break
			}

			spanTextMap := make(map[string]string)
			if err = json.Unmarshal([]byte(textMapString.Val), &spanTextMap); err != nil {
				logger.Warnw(ctx, "unable-to-unmarshal-textmap-from-json-string", log.Fields{"textMapString": textMapString, "error": err})
				break
			}

			var spanContext opentracing.SpanContext
			if spanContext, err = opentracing.GlobalTracer().Extract(opentracing.TextMap, opentracing.TextMapCarrier(spanTextMap)); err != nil {
				logger.Warnw(ctx, "unable-to-deserialize-textmap-to-span", log.Fields{"textMap": spanTextMap, "error": err})
				break
			}

			var receivedRpcName string
			extractBaggage := func(k, v string) bool {
				if k == "rpc-span-name" {
					receivedRpcName = v
					return false
				}
				return true
			}

			spanContext.ForeachBaggageItem(extractBaggage)

			return opentracing.StartSpanFromContext(ctx, receivedRpcName, opentracing.FollowsFrom(spanContext))
		}
	}

	// Create new Child Span with rpc as name if no span details were received in kafka arguments
	var spanName strings.Builder
	spanName.WriteString("kafka-")

	// In case of inter adapter message, use Msg Type for constructing RPC name
	if rpcName == "process_inter_adapter_message" {
		for _, arg := range args {
			if arg.Key == "msg" {
				iamsg := ic.InterAdapterMessage{}
				if err := ptypes.UnmarshalAny(arg.Value, &iamsg); err == nil {
					spanName.WriteString("inter-adapter-")
					rpcName = iamsg.Header.Type.String()
				}
			}
		}
	}

	spanName.WriteString("rpc-")
	spanName.WriteString(rpcName)

	return opentracing.StartSpanFromContext(ctx, spanName.String())
}

func (kp *interContainerProxy) handleMessage(ctx context.Context, msg *ic.InterContainerMessage, targetInterface interface{}) {

	// First extract the header to know whether this is a request - responses are handled by a different handler
	if msg.Header.Type == ic.MessageType_REQUEST {
		var out []reflect.Value
		var err error

		// Get the request body
		requestBody := &ic.InterContainerRequestBody{}
		if err = ptypes.UnmarshalAny(msg.Body, requestBody); err != nil {
			logger.Warnw(ctx, "cannot-unmarshal-request", log.Fields{"error": err})
		} else {
			span, ctx := kp.enrichContextWithSpan(ctx, requestBody.Rpc, requestBody.Args)
			defer span.Finish()

			logger.Debugw(ctx, "received-request", log.Fields{"rpc": requestBody.Rpc, "header": msg.Header})
			// let the callee unpack the arguments as its the only one that knows the real proto type
			// Augment the requestBody with the message Id as it will be used in scenarios where cores
			// are set in pairs and competing
			requestBody.Args = kp.addTransactionId(ctx, msg.Header.Id, requestBody.Args)

			// Augment the requestBody with the From topic name as it will be used in scenarios where a container
			// needs to send an unsollicited message to the currently requested container
			requestBody.Args = kp.addFromTopic(ctx, msg.Header.FromTopic, requestBody.Args)

			out, err = CallFuncByName(ctx, targetInterface, requestBody.Rpc, requestBody.Args)
			if err != nil {
				logger.Warn(ctx, err)
			}
		}
		// Response required?
		if requestBody.ResponseRequired {
			// If we already have an error before then just return that
			var returnError *ic.Error
			var returnedValues []interface{}
			var success bool
			if err != nil {
				returnError = &ic.Error{Reason: err.Error()}
				returnedValues = make([]interface{}, 1)
				returnedValues[0] = returnError
			} else {
				returnedValues = make([]interface{}, 0)
				// Check for errors first
				lastIndex := len(out) - 1
				if out[lastIndex].Interface() != nil { // Error
					if retError, ok := out[lastIndex].Interface().(error); ok {
						if retError.Error() == ErrorTransactionNotAcquired.Error() {
							logger.Debugw(ctx, "Ignoring request", log.Fields{"error": retError, "txId": msg.Header.Id})
							return // Ignore - process is in competing mode and ignored transaction
						}
						returnError = &ic.Error{Reason: retError.Error()}
						returnedValues = append(returnedValues, returnError)
					} else { // Should never happen
						returnError = &ic.Error{Reason: "incorrect-error-returns"}
						returnedValues = append(returnedValues, returnError)
					}
				} else if len(out) == 2 && reflect.ValueOf(out[0].Interface()).IsValid() && reflect.ValueOf(out[0].Interface()).IsNil() {
					logger.Warnw(ctx, "Unexpected response of (nil,nil)", log.Fields{"txId": msg.Header.Id})
					return // Ignore - should not happen
				} else { // Non-error case
					success = true
					for idx, val := range out {
						//logger.Debugw(ctx, "returned-api-response-loop", log.Fields{"idx": idx, "val": val.Interface()})
						if idx != lastIndex {
							returnedValues = append(returnedValues, val.Interface())
						}
					}
				}
			}

			var icm *ic.InterContainerMessage
			if icm, err = encodeResponse(ctx, msg, success, returnedValues...); err != nil {
				logger.Warnw(ctx, "error-encoding-response-returning-failure-result", log.Fields{"error": err})
				icm = encodeDefaultFailedResponse(ctx, msg)
			}
			// To preserve ordering of messages, all messages to a given topic are sent to the same partition
			// by providing a message key.   The key is encoded in the topic name.  If the deviceId is not
			// present then the key will be empty, hence all messages for a given topic will be sent to all
			// partitions.
			replyTopic := &Topic{Name: msg.Header.FromTopic}
			key := msg.Header.KeyTopic
			logger.Debugw(ctx, "sending-response-to-kafka", log.Fields{"rpc": requestBody.Rpc, "header": icm.Header, "key": key})
			// TODO: handle error response.
			go func() {
				if err := kp.kafkaClient.Send(ctx, icm, replyTopic, key); err != nil {
					logger.Errorw(ctx, "send-reply-failed", log.Fields{
						"topic": replyTopic,
						"key":   key,
						"error": err})
				}
			}()
		}
	} else if msg.Header.Type == ic.MessageType_RESPONSE {
		logger.Debugw(ctx, "response-received", log.Fields{"msg-header": msg.Header})
		go kp.dispatchResponse(ctx, msg)
	} else {
		logger.Warnw(ctx, "unsupported-message-received", log.Fields{"msg-header": msg.Header})
	}
}

func (kp *interContainerProxy) waitForMessages(ctx context.Context, ch <-chan *ic.InterContainerMessage, topic Topic, targetInterface interface{}) {
	//	Wait for messages
	for msg := range ch {
		//logger.Debugw(ctx, "request-received", log.Fields{"msg": msg, "topic": topic.Name, "target": targetInterface})
		go kp.handleMessage(context.Background(), msg, targetInterface)
	}
}

func (kp *interContainerProxy) dispatchResponse(ctx context.Context, msg *ic.InterContainerMessage) {
	kp.lockTransactionIdToChannelMap.RLock()
	defer kp.lockTransactionIdToChannelMap.RUnlock()
	if _, exist := kp.transactionIdToChannelMap[msg.Header.Id]; !exist {
		logger.Debugw(ctx, "no-waiting-channel", log.Fields{"transaction": msg.Header.Id})
		return
	}
	kp.transactionIdToChannelMap[msg.Header.Id].ch <- msg
}

// subscribeForResponse allows a caller to subscribe to a given topic when waiting for a response.
// This method is built to prevent all subscribers to receive all messages as is the case of the Subscribe
// API. There is one response channel waiting for kafka messages before dispatching the message to the
// corresponding waiting channel
func (kp *interContainerProxy) subscribeForResponse(ctx context.Context, topic Topic, trnsId string) (chan *ic.InterContainerMessage, error) {
	logger.Debugw(ctx, "subscribeForResponse", log.Fields{"topic": topic.Name, "trnsid": trnsId})

	// Create a specific channel for this consumers.  We cannot use the channel from the kafkaclient as it will
	// broadcast any message for this topic to all channels waiting on it.
	// Set channel size to 1 to prevent deadlock, see VOL-2708
	ch := make(chan *ic.InterContainerMessage, 1)
	kp.addToTransactionIdToChannelMap(trnsId, &topic, ch)

	return ch, nil
}

func (kp *interContainerProxy) unSubscribeForResponse(ctx context.Context, trnsId string) error {
	logger.Debugw(ctx, "unsubscribe-for-response", log.Fields{"trnsId": trnsId})
	kp.deleteFromTransactionIdToChannelMap(trnsId)
	return nil
}

func (kp *interContainerProxy) EnableLivenessChannel(ctx context.Context, enable bool) chan bool {
	return kp.kafkaClient.EnableLivenessChannel(ctx, enable)
}

func (kp *interContainerProxy) EnableHealthinessChannel(ctx context.Context, enable bool) chan bool {
	return kp.kafkaClient.EnableHealthinessChannel(ctx, enable)
}

func (kp *interContainerProxy) SendLiveness(ctx context.Context) error {
	return kp.kafkaClient.SendLiveness(ctx)
}

//formatRequest formats a request to send over kafka and returns an InterContainerMessage message on success
//or an error on failure
func encodeRequest(ctx context.Context, rpc string, toTopic *Topic, replyTopic *Topic, key string, kvArgs ...*KVArg) (*ic.InterContainerMessage, error) {
	requestHeader := &ic.Header{
		Id:        uuid.New().String(),
		Type:      ic.MessageType_REQUEST,
		FromTopic: replyTopic.Name,
		ToTopic:   toTopic.Name,
		KeyTopic:  key,
		Timestamp: ptypes.TimestampNow(),
	}
	requestBody := &ic.InterContainerRequestBody{
		Rpc:              rpc,
		ResponseRequired: true,
		ReplyToTopic:     replyTopic.Name,
	}

	for _, arg := range kvArgs {
		if arg == nil {
			// In case the caller sends an array with empty args
			continue
		}
		var marshalledArg *any.Any
		var err error
		// ascertain the value interface type is a proto.Message
		protoValue, ok := arg.Value.(proto.Message)
		if !ok {
			logger.Warnw(ctx, "argument-value-not-proto-message", log.Fields{"error": ok, "Value": arg.Value})
			err := errors.New("argument-value-not-proto-message")
			return nil, err
		}
		if marshalledArg, err = ptypes.MarshalAny(protoValue); err != nil {
			logger.Warnw(ctx, "cannot-marshal-request", log.Fields{"error": err})
			return nil, err
		}
		protoArg := &ic.Argument{
			Key:   arg.Key,
			Value: marshalledArg,
		}
		requestBody.Args = append(requestBody.Args, protoArg)
	}

	var marshalledData *any.Any
	var err error
	if marshalledData, err = ptypes.MarshalAny(requestBody); err != nil {
		logger.Warnw(ctx, "cannot-marshal-request", log.Fields{"error": err})
		return nil, err
	}
	request := &ic.InterContainerMessage{
		Header: requestHeader,
		Body:   marshalledData,
	}
	return request, nil
}

func decodeResponse(ctx context.Context, response *ic.InterContainerMessage) (*ic.InterContainerResponseBody, error) {
	//	Extract the message body
	responseBody := ic.InterContainerResponseBody{}
	if err := ptypes.UnmarshalAny(response.Body, &responseBody); err != nil {
		logger.Warnw(ctx, "cannot-unmarshal-response", log.Fields{"error": err})
		return nil, err
	}
	//logger.Debugw(ctx, "response-decoded-successfully", log.Fields{"response-status": &responseBody.Success})

	return &responseBody, nil

}
