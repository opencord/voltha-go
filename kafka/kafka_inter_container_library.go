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
	"errors"
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/common/log"
	ic "github.com/opencord/voltha-protos/go/inter_container"
	"reflect"
	"strings"
	"sync"
	"time"
)

// Initialize the logger - gets the default until the main function setup the logger
func init() {
	log.AddPackage(log.JSON, log.DebugLevel, nil)
}

const (
	DefaultMaxRetries     = 3
	DefaultRequestTimeout = 10000 // 10000 milliseconds - to handle a wider latency range
)

const (
	TransactionKey = "transactionID"
	FromTopic      = "fromTopic"
)

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

// InterContainerProxy represents the messaging proxy
type InterContainerProxy struct {
	kafkaHost                      string
	kafkaPort                      int
	DefaultTopic                   *Topic
	defaultRequestHandlerInterface interface{}
	deviceDiscoveryTopic           *Topic
	kafkaClient                    Client
	doneCh                         chan int

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

type InterContainerProxyOption func(*InterContainerProxy)

func InterContainerHost(host string) InterContainerProxyOption {
	return func(args *InterContainerProxy) {
		args.kafkaHost = host
	}
}

func InterContainerPort(port int) InterContainerProxyOption {
	return func(args *InterContainerProxy) {
		args.kafkaPort = port
	}
}

func DefaultTopic(topic *Topic) InterContainerProxyOption {
	return func(args *InterContainerProxy) {
		args.DefaultTopic = topic
	}
}

func DeviceDiscoveryTopic(topic *Topic) InterContainerProxyOption {
	return func(args *InterContainerProxy) {
		args.deviceDiscoveryTopic = topic
	}
}

func RequestHandlerInterface(handler interface{}) InterContainerProxyOption {
	return func(args *InterContainerProxy) {
		args.defaultRequestHandlerInterface = handler
	}
}

func MsgClient(client Client) InterContainerProxyOption {
	return func(args *InterContainerProxy) {
		args.kafkaClient = client
	}
}

func NewInterContainerProxy(opts ...InterContainerProxyOption) (*InterContainerProxy, error) {
	proxy := &InterContainerProxy{
		kafkaHost: DefaultKafkaHost,
		kafkaPort: DefaultKafkaPort,
	}

	for _, option := range opts {
		option(proxy)
	}

	// Create the locks for all the maps
	proxy.lockTopicRequestHandlerChannelMap = sync.RWMutex{}
	proxy.lockTransactionIdToChannelMap = sync.RWMutex{}
	proxy.lockTopicResponseChannelMap = sync.RWMutex{}

	return proxy, nil
}

func (kp *InterContainerProxy) Start() error {
	log.Info("Starting-Proxy")

	// Kafka MsgClient should already have been created.  If not, output fatal error
	if kp.kafkaClient == nil {
		log.Fatal("kafka-client-not-set")
	}

	// Create the Done channel
	kp.doneCh = make(chan int, 1)

	// Start the kafka client
	if err := kp.kafkaClient.Start(); err != nil {
		log.Errorw("Cannot-create-kafka-proxy", log.Fields{"error": err})
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

func (kp *InterContainerProxy) Stop() {
	log.Info("stopping-intercontainer-proxy")
	kp.doneCh <- 1
	// TODO : Perform cleanup
	kp.kafkaClient.Stop()
	//kp.deleteAllTopicRequestHandlerChannelMap()
	//kp.deleteAllTopicResponseChannelMap()
	//kp.deleteAllTransactionIdToChannelMap()
}

// DeviceDiscovered publish the discovered device onto the kafka messaging bus
func (kp *InterContainerProxy) DeviceDiscovered(deviceId string, deviceType string, parentId string, publisher string) error {
	log.Debugw("sending-device-discovery-msg", log.Fields{"deviceId": deviceId})
	//	Simple validation
	if deviceId == "" || deviceType == "" {
		log.Errorw("invalid-parameters", log.Fields{"id": deviceId, "type": deviceType})
		return errors.New("invalid-parameters")
	}
	//	Create the device discovery message
	header := &ic.Header{
		Id:        uuid.New().String(),
		Type:      ic.MessageType_DEVICE_DISCOVERED,
		FromTopic: kp.DefaultTopic.Name,
		ToTopic:   kp.deviceDiscoveryTopic.Name,
		Timestamp: time.Now().UnixNano(),
	}
	body := &ic.DeviceDiscovered{
		Id:         deviceId,
		DeviceType: deviceType,
		ParentId:   parentId,
		Publisher:  publisher,
	}

	var marshalledData *any.Any
	var err error
	if marshalledData, err = ptypes.MarshalAny(body); err != nil {
		log.Errorw("cannot-marshal-request", log.Fields{"error": err})
		return err
	}
	msg := &ic.InterContainerMessage{
		Header: header,
		Body:   marshalledData,
	}

	// Send the message
	if err := kp.kafkaClient.Send(msg, kp.deviceDiscoveryTopic); err != nil {
		log.Errorw("cannot-send-device-discovery-message", log.Fields{"error": err})
		return err
	}
	return nil
}

// InvokeRPC is used to send a request to a given topic
func (kp *InterContainerProxy) InvokeRPC(ctx context.Context, rpc string, toTopic *Topic, replyToTopic *Topic,
	waitForResponse bool, key string, kvArgs ...*KVArg) (bool, *any.Any) {

	//	If a replyToTopic is provided then we use it, otherwise just use the  default toTopic.  The replyToTopic is
	// typically the device ID.
	responseTopic := replyToTopic
	if responseTopic == nil {
		responseTopic = kp.DefaultTopic
	}

	// Encode the request
	protoRequest, err := encodeRequest(rpc, toTopic, responseTopic, key, kvArgs...)
	if err != nil {
		log.Warnw("cannot-format-request", log.Fields{"rpc": rpc, "error": err})
		return false, nil
	}

	// Subscribe for response, if needed, before sending request
	var ch <-chan *ic.InterContainerMessage
	if waitForResponse {
		var err error
		if ch, err = kp.subscribeForResponse(*responseTopic, protoRequest.Header.Id); err != nil {
			log.Errorw("failed-to-subscribe-for-response", log.Fields{"error": err, "toTopic": toTopic.Name})
		}
	}

	// Send request - if the topic is formatted with a device Id then we will send the request using a
	// specific key, hence ensuring a single partition is used to publish the request.  This ensures that the
	// subscriber on that topic will receive the request in the order it was sent.  The key used is the deviceId.
	//key := GetDeviceIdFromTopic(*toTopic)
	log.Debugw("sending-msg", log.Fields{"rpc": rpc, "toTopic": toTopic, "replyTopic": responseTopic, "key": key, "xId": protoRequest.Header.Id})
	go kp.kafkaClient.Send(protoRequest, toTopic, key)

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
		defer kp.unSubscribeForResponse(protoRequest.Header.Id)
		select {
		case msg, ok := <-ch:
			if !ok {
				log.Warnw("channel-closed", log.Fields{"rpc": rpc, "replyTopic": replyToTopic.Name})
				protoError := &ic.Error{Reason: "channel-closed"}
				var marshalledArg *any.Any
				if marshalledArg, err = ptypes.MarshalAny(protoError); err != nil {
					return false, nil // Should never happen
				}
				return false, marshalledArg
			}
			log.Debugw("received-response", log.Fields{"rpc": rpc, "msgHeader": msg.Header})
			var responseBody *ic.InterContainerResponseBody
			var err error
			if responseBody, err = decodeResponse(msg); err != nil {
				log.Errorw("decode-response-error", log.Fields{"error": err})
			}
			return responseBody.Success, responseBody.Result
		case <-ctx.Done():
			log.Debugw("context-cancelled", log.Fields{"rpc": rpc, "ctx": ctx.Err()})
			//	 pack the error as proto any type
			protoError := &ic.Error{Reason: ctx.Err().Error()}
			var marshalledArg *any.Any
			if marshalledArg, err = ptypes.MarshalAny(protoError); err != nil {
				return false, nil // Should never happen
			}
			return false, marshalledArg
		case <-childCtx.Done():
			log.Debugw("context-cancelled", log.Fields{"rpc": rpc, "ctx": childCtx.Err()})
			//	 pack the error as proto any type
			protoError := &ic.Error{Reason: childCtx.Err().Error()}
			var marshalledArg *any.Any
			if marshalledArg, err = ptypes.MarshalAny(protoError); err != nil {
				return false, nil // Should never happen
			}
			return false, marshalledArg
		case <-kp.doneCh:
			log.Infow("received-exit-signal", log.Fields{"toTopic": toTopic.Name, "rpc": rpc})
			return true, nil
		}
	}
	return true, nil
}

// SubscribeWithRequestHandlerInterface allows a caller to assign a target object to be invoked automatically
// when a message is received on a given topic
func (kp *InterContainerProxy) SubscribeWithRequestHandlerInterface(topic Topic, handler interface{}) error {

	// Subscribe to receive messages for that topic
	var ch <-chan *ic.InterContainerMessage
	var err error
	if ch, err = kp.kafkaClient.Subscribe(&topic); err != nil {
		//if ch, err = kp.Subscribe(topic); err != nil {
		log.Errorw("failed-to-subscribe", log.Fields{"error": err, "topic": topic.Name})
	}

	kp.defaultRequestHandlerInterface = handler
	kp.addToTopicRequestHandlerChannelMap(topic.Name, &requestHandlerChannel{requesthandlerInterface: handler, ch: ch})
	// Launch a go routine to receive and process kafka messages
	go kp.waitForMessages(ch, topic, handler)

	return nil
}

// SubscribeWithDefaultRequestHandler allows a caller to add a topic to an existing target object to be invoked automatically
// when a message is received on a given topic.  So far there is only 1 target registered per microservice
func (kp *InterContainerProxy) SubscribeWithDefaultRequestHandler(topic Topic, initialOffset int64) error {
	// Subscribe to receive messages for that topic
	var ch <-chan *ic.InterContainerMessage
	var err error
	if ch, err = kp.kafkaClient.Subscribe(&topic, &KVArg{Key: Offset, Value: initialOffset}); err != nil {
		log.Errorw("failed-to-subscribe", log.Fields{"error": err, "topic": topic.Name})
		return err
	}
	kp.addToTopicRequestHandlerChannelMap(topic.Name, &requestHandlerChannel{requesthandlerInterface: kp.defaultRequestHandlerInterface, ch: ch})

	// Launch a go routine to receive and process kafka messages
	go kp.waitForMessages(ch, topic, kp.defaultRequestHandlerInterface)

	return nil
}

func (kp *InterContainerProxy) UnSubscribeFromRequestHandler(topic Topic) error {
	return kp.deleteFromTopicRequestHandlerChannelMap(topic.Name)
}

// setupTopicResponseChannelMap sets up single consumers channel that will act as a broadcast channel for all
// responses from that topic.
func (kp *InterContainerProxy) setupTopicResponseChannelMap(topic string, arg <-chan *ic.InterContainerMessage) {
	kp.lockTopicResponseChannelMap.Lock()
	defer kp.lockTopicResponseChannelMap.Unlock()
	if _, exist := kp.topicToResponseChannelMap[topic]; !exist {
		kp.topicToResponseChannelMap[topic] = arg
	}
}

func (kp *InterContainerProxy) isTopicSubscribedForResponse(topic string) bool {
	kp.lockTopicResponseChannelMap.RLock()
	defer kp.lockTopicResponseChannelMap.RUnlock()
	_, exist := kp.topicToResponseChannelMap[topic]
	return exist
}

func (kp *InterContainerProxy) deleteFromTopicResponseChannelMap(topic string) error {
	kp.lockTopicResponseChannelMap.Lock()
	defer kp.lockTopicResponseChannelMap.Unlock()
	if _, exist := kp.topicToResponseChannelMap[topic]; exist {
		// Unsubscribe to this topic first - this will close the subscribed channel
		var err error
		if err = kp.kafkaClient.UnSubscribe(&Topic{Name: topic}, kp.topicToResponseChannelMap[topic]); err != nil {
			log.Errorw("unsubscribing-error", log.Fields{"topic": topic})
		}
		delete(kp.topicToResponseChannelMap, topic)
		return err
	} else {
		return errors.New(fmt.Sprintf("%s-Topic-not-found", topic))
	}
}

func (kp *InterContainerProxy) deleteAllTopicResponseChannelMap() error {
	kp.lockTopicResponseChannelMap.Lock()
	defer kp.lockTopicResponseChannelMap.Unlock()
	var err error
	for topic, _ := range kp.topicToResponseChannelMap {
		// Unsubscribe to this topic first - this will close the subscribed channel
		if err = kp.kafkaClient.UnSubscribe(&Topic{Name: topic}, kp.topicToResponseChannelMap[topic]); err != nil {
			log.Errorw("unsubscribing-error", log.Fields{"topic": topic, "error": err})
		}
		delete(kp.topicToResponseChannelMap, topic)
	}
	return err
}

func (kp *InterContainerProxy) addToTopicRequestHandlerChannelMap(topic string, arg *requestHandlerChannel) {
	kp.lockTopicRequestHandlerChannelMap.Lock()
	defer kp.lockTopicRequestHandlerChannelMap.Unlock()
	if _, exist := kp.topicToRequestHandlerChannelMap[topic]; !exist {
		kp.topicToRequestHandlerChannelMap[topic] = arg
	}
}

func (kp *InterContainerProxy) deleteFromTopicRequestHandlerChannelMap(topic string) error {
	kp.lockTopicRequestHandlerChannelMap.Lock()
	defer kp.lockTopicRequestHandlerChannelMap.Unlock()
	if _, exist := kp.topicToRequestHandlerChannelMap[topic]; exist {
		// Close the kafka client client first by unsubscribing to this topic
		kp.kafkaClient.UnSubscribe(&Topic{Name: topic}, kp.topicToRequestHandlerChannelMap[topic].ch)
		delete(kp.topicToRequestHandlerChannelMap, topic)
		return nil
	} else {
		return errors.New(fmt.Sprintf("%s-Topic-not-found", topic))
	}
}

func (kp *InterContainerProxy) deleteAllTopicRequestHandlerChannelMap() error {
	kp.lockTopicRequestHandlerChannelMap.Lock()
	defer kp.lockTopicRequestHandlerChannelMap.Unlock()
	var err error
	for topic, _ := range kp.topicToRequestHandlerChannelMap {
		// Close the kafka client client first by unsubscribing to this topic
		if err = kp.kafkaClient.UnSubscribe(&Topic{Name: topic}, kp.topicToRequestHandlerChannelMap[topic].ch); err != nil {
			log.Errorw("unsubscribing-error", log.Fields{"topic": topic, "error": err})
		}
		delete(kp.topicToRequestHandlerChannelMap, topic)
	}
	return err
}

func (kp *InterContainerProxy) addToTransactionIdToChannelMap(id string, topic *Topic, arg chan *ic.InterContainerMessage) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	if _, exist := kp.transactionIdToChannelMap[id]; !exist {
		kp.transactionIdToChannelMap[id] = &transactionChannel{topic: topic, ch: arg}
	}
}

func (kp *InterContainerProxy) deleteFromTransactionIdToChannelMap(id string) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	if transChannel, exist := kp.transactionIdToChannelMap[id]; exist {
		// Close the channel first
		close(transChannel.ch)
		delete(kp.transactionIdToChannelMap, id)
	}
}

func (kp *InterContainerProxy) deleteTopicTransactionIdToChannelMap(id string) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	for key, value := range kp.transactionIdToChannelMap {
		if value.topic.Name == id {
			close(value.ch)
			delete(kp.transactionIdToChannelMap, key)
		}
	}
}

func (kp *InterContainerProxy) deleteAllTransactionIdToChannelMap() {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	for key, value := range kp.transactionIdToChannelMap {
		close(value.ch)
		delete(kp.transactionIdToChannelMap, key)
	}
}

func (kp *InterContainerProxy) DeleteTopic(topic Topic) error {
	// If we have any consumers on that topic we need to close them
	if err := kp.deleteFromTopicResponseChannelMap(topic.Name); err != nil {
		log.Errorw("delete-from-topic-responsechannelmap-failed", log.Fields{"error": err})
	}
	if err := kp.deleteFromTopicRequestHandlerChannelMap(topic.Name); err != nil {
		log.Errorw("delete-from-topic-requesthandlerchannelmap-failed", log.Fields{"error": err})
	}
	kp.deleteTopicTransactionIdToChannelMap(topic.Name)

	return kp.kafkaClient.DeleteTopic(&topic)
}

func encodeReturnedValue(returnedVal interface{}) (*any.Any, error) {
	// Encode the response argument - needs to be a proto message
	if returnedVal == nil {
		return nil, nil
	}
	protoValue, ok := returnedVal.(proto.Message)
	if !ok {
		log.Warnw("response-value-not-proto-message", log.Fields{"error": ok, "returnVal": returnedVal})
		err := errors.New("response-value-not-proto-message")
		return nil, err
	}

	// Marshal the returned value, if any
	var marshalledReturnedVal *any.Any
	var err error
	if marshalledReturnedVal, err = ptypes.MarshalAny(protoValue); err != nil {
		log.Warnw("cannot-marshal-returned-val", log.Fields{"error": err})
		return nil, err
	}
	return marshalledReturnedVal, nil
}

func encodeDefaultFailedResponse(request *ic.InterContainerMessage) *ic.InterContainerMessage {
	responseHeader := &ic.Header{
		Id:        request.Header.Id,
		Type:      ic.MessageType_RESPONSE,
		FromTopic: request.Header.ToTopic,
		ToTopic:   request.Header.FromTopic,
		Timestamp: time.Now().Unix(),
	}
	responseBody := &ic.InterContainerResponseBody{
		Success: false,
		Result:  nil,
	}
	var marshalledResponseBody *any.Any
	var err error
	// Error should never happen here
	if marshalledResponseBody, err = ptypes.MarshalAny(responseBody); err != nil {
		log.Warnw("cannot-marshal-failed-response-body", log.Fields{"error": err})
	}

	return &ic.InterContainerMessage{
		Header: responseHeader,
		Body:   marshalledResponseBody,
	}

}

//formatRequest formats a request to send over kafka and returns an InterContainerMessage message on success
//or an error on failure
func encodeResponse(request *ic.InterContainerMessage, success bool, returnedValues ...interface{}) (*ic.InterContainerMessage, error) {
	//log.Debugw("encodeResponse", log.Fields{"success": success, "returnedValues": returnedValues})
	responseHeader := &ic.Header{
		Id:        request.Header.Id,
		Type:      ic.MessageType_RESPONSE,
		FromTopic: request.Header.ToTopic,
		ToTopic:   request.Header.FromTopic,
		KeyTopic: request.Header.KeyTopic,
		Timestamp: time.Now().UnixNano(),
	}

	// Go over all returned values
	var marshalledReturnedVal *any.Any
	var err error
	for _, returnVal := range returnedValues {
		if marshalledReturnedVal, err = encodeReturnedValue(returnVal); err != nil {
			log.Warnw("cannot-marshal-response-body", log.Fields{"error": err})
		}
		break // for now we support only 1 returned value - (excluding the error)
	}

	responseBody := &ic.InterContainerResponseBody{
		Success: success,
		Result:  marshalledReturnedVal,
	}

	// Marshal the response body
	var marshalledResponseBody *any.Any
	if marshalledResponseBody, err = ptypes.MarshalAny(responseBody); err != nil {
		log.Warnw("cannot-marshal-response-body", log.Fields{"error": err})
		return nil, err
	}

	return &ic.InterContainerMessage{
		Header: responseHeader,
		Body:   marshalledResponseBody,
	}, nil
}

func CallFuncByName(myClass interface{}, funcName string, params ...interface{}) (out []reflect.Value, err error) {
	myClassValue := reflect.ValueOf(myClass)
	// Capitalize the first letter in the funcName to workaround the first capital letters required to
	// invoke a function from a different package
	funcName = strings.Title(funcName)
	m := myClassValue.MethodByName(funcName)
	if !m.IsValid() {
		return make([]reflect.Value, 0), fmt.Errorf("method-not-found \"%s\"", funcName)
	}
	in := make([]reflect.Value, len(params))
	for i, param := range params {
		in[i] = reflect.ValueOf(param)
	}
	out = m.Call(in)
	return
}

func (kp *InterContainerProxy) addTransactionId(transactionId string, currentArgs []*ic.Argument) []*ic.Argument {
	arg := &KVArg{
		Key:   TransactionKey,
		Value: &ic.StrType{Val: transactionId},
	}

	var marshalledArg *any.Any
	var err error
	if marshalledArg, err = ptypes.MarshalAny(&ic.StrType{Val: transactionId}); err != nil {
		log.Warnw("cannot-add-transactionId", log.Fields{"error": err})
		return currentArgs
	}
	protoArg := &ic.Argument{
		Key:   arg.Key,
		Value: marshalledArg,
	}
	return append(currentArgs, protoArg)
}

func (kp *InterContainerProxy) addFromTopic(fromTopic string, currentArgs []*ic.Argument) []*ic.Argument {
	var marshalledArg *any.Any
	var err error
	if marshalledArg, err = ptypes.MarshalAny(&ic.StrType{Val: fromTopic}); err != nil {
		log.Warnw("cannot-add-transactionId", log.Fields{"error": err})
		return currentArgs
	}
	protoArg := &ic.Argument{
		Key:   FromTopic,
		Value: marshalledArg,
	}
	return append(currentArgs, protoArg)
}

func (kp *InterContainerProxy) handleMessage(msg *ic.InterContainerMessage, targetInterface interface{}) {

	// First extract the header to know whether this is a request - responses are handled by a different handler
	if msg.Header.Type == ic.MessageType_REQUEST {
		var out []reflect.Value
		var err error

		// Get the request body
		requestBody := &ic.InterContainerRequestBody{}
		if err = ptypes.UnmarshalAny(msg.Body, requestBody); err != nil {
			log.Warnw("cannot-unmarshal-request", log.Fields{"error": err})
		} else {
			log.Debugw("received-request", log.Fields{"rpc": requestBody.Rpc, "header": msg.Header})
			// let the callee unpack the arguments as its the only one that knows the real proto type
			// Augment the requestBody with the message Id as it will be used in scenarios where cores
			// are set in pairs and competing
			requestBody.Args = kp.addTransactionId(msg.Header.Id, requestBody.Args)

			// Augment the requestBody with the From topic name as it will be used in scenarios where a container
			// needs to send an unsollicited message to the currently requested container
			requestBody.Args = kp.addFromTopic(msg.Header.FromTopic, requestBody.Args)

			out, err = CallFuncByName(targetInterface, requestBody.Rpc, requestBody.Args)
			if err != nil {
				log.Warn(err)
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
					if goError, ok := out[lastIndex].Interface().(error); ok {
						returnError = &ic.Error{Reason: goError.Error()}
						returnedValues = append(returnedValues, returnError)
					} else { // Should never happen
						returnError = &ic.Error{Reason: "incorrect-error-returns"}
						returnedValues = append(returnedValues, returnError)
					}
				} else if len(out) == 2 && reflect.ValueOf(out[0].Interface()).IsValid() && reflect.ValueOf(out[0].Interface()).IsNil() {
					return // Ignore case - when core is in competing mode
				} else { // Non-error case
					success = true
					for idx, val := range out {
						//log.Debugw("returned-api-response-loop", log.Fields{"idx": idx, "val": val.Interface()})
						if idx != lastIndex {
							returnedValues = append(returnedValues, val.Interface())
						}
					}
				}
			}

			var icm *ic.InterContainerMessage
			if icm, err = encodeResponse(msg, success, returnedValues...); err != nil {
				log.Warnw("error-encoding-response-returning-failure-result", log.Fields{"error": err})
				icm = encodeDefaultFailedResponse(msg)
			}
			// To preserve ordering of messages, all messages to a given topic are sent to the same partition
			// by providing a message key.   The key is encoded in the topic name.  If the deviceId is not
			// present then the key will be empty, hence all messages for a given topic will be sent to all
			// partitions.
			replyTopic := &Topic{Name: msg.Header.FromTopic}
			key := msg.Header.KeyTopic
			log.Debugw("sending-response-to-kafka", log.Fields{"rpc": requestBody.Rpc, "header": icm.Header, "key": key})
			// TODO: handle error response.
			 go kp.kafkaClient.Send(icm, replyTopic, key)
		}
	} else if msg.Header.Type == ic.MessageType_RESPONSE {
		log.Debugw("response-received", log.Fields{"msg-header": msg.Header})
		go kp.dispatchResponse(msg)
	} else {
		log.Warnw("unsupported-message-received", log.Fields{"msg-header": msg.Header})
	}
}

func (kp *InterContainerProxy) waitForMessages(ch <-chan *ic.InterContainerMessage, topic Topic, targetInterface interface{}) {
	//	Wait for messages
	for msg := range ch {
		//log.Debugw("request-received", log.Fields{"msg": msg, "topic": topic.Name, "target": targetInterface})
		go kp.handleMessage(msg, targetInterface)
	}
}

func (kp *InterContainerProxy) dispatchResponse(msg *ic.InterContainerMessage) {
	kp.lockTransactionIdToChannelMap.RLock()
	defer kp.lockTransactionIdToChannelMap.RUnlock()
	if _, exist := kp.transactionIdToChannelMap[msg.Header.Id]; !exist {
		log.Debugw("no-waiting-channel", log.Fields{"transaction": msg.Header.Id})
		return
	}
	kp.transactionIdToChannelMap[msg.Header.Id].ch <- msg
}

// subscribeForResponse allows a caller to subscribe to a given topic when waiting for a response.
// This method is built to prevent all subscribers to receive all messages as is the case of the Subscribe
// API. There is one response channel waiting for kafka messages before dispatching the message to the
// corresponding waiting channel
func (kp *InterContainerProxy) subscribeForResponse(topic Topic, trnsId string) (chan *ic.InterContainerMessage, error) {
	log.Debugw("subscribeForResponse", log.Fields{"topic": topic.Name, "trnsid": trnsId})

	// Create a specific channel for this consumers.  We cannot use the channel from the kafkaclient as it will
	// broadcast any message for this topic to all channels waiting on it.
	ch := make(chan *ic.InterContainerMessage)
	kp.addToTransactionIdToChannelMap(trnsId, &topic, ch)

	return ch, nil
}

func (kp *InterContainerProxy) unSubscribeForResponse(trnsId string) error {
	log.Debugw("unsubscribe-for-response", log.Fields{"trnsId": trnsId})
	kp.deleteFromTransactionIdToChannelMap(trnsId)
	return nil
}

//formatRequest formats a request to send over kafka and returns an InterContainerMessage message on success
//or an error on failure
func encodeRequest(rpc string, toTopic *Topic, replyTopic *Topic, key string, kvArgs ...*KVArg) (*ic.InterContainerMessage, error) {
	requestHeader := &ic.Header{
		Id:        uuid.New().String(),
		Type:      ic.MessageType_REQUEST,
		FromTopic: replyTopic.Name,
		ToTopic:   toTopic.Name,
		KeyTopic: key,
		Timestamp: time.Now().UnixNano(),
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
			log.Warnw("argument-value-not-proto-message", log.Fields{"error": ok, "Value": arg.Value})
			err := errors.New("argument-value-not-proto-message")
			return nil, err
		}
		if marshalledArg, err = ptypes.MarshalAny(protoValue); err != nil {
			log.Warnw("cannot-marshal-request", log.Fields{"error": err})
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
		log.Warnw("cannot-marshal-request", log.Fields{"error": err})
		return nil, err
	}
	request := &ic.InterContainerMessage{
		Header: requestHeader,
		Body:   marshalledData,
	}
	return request, nil
}

func decodeResponse(response *ic.InterContainerMessage) (*ic.InterContainerResponseBody, error) {
	//	Extract the message body
	responseBody := ic.InterContainerResponseBody{}
	if err := ptypes.UnmarshalAny(response.Body, &responseBody); err != nil {
		log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
		return nil, err
	}
	//log.Debugw("response-decoded-successfully", log.Fields{"response-status": &responseBody.Success})

	return &responseBody, nil

}
