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
	"github.com/Shopify/sarama"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/google/uuid"
	"github.com/opencord/voltha-go/common/log"
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	"reflect"
	"sync"
	"time"
)

// Initialize the logger - gets the default until the main function setup the logger
func init() {
	log.GetLogger()
}

const (
	DefaultKafkaHost         = "10.100.198.240"
	DefaultKafkaPort         = 9092
	DefaultTopicName         = "Core"
	DefaultSleepOnError      = 1
	DefaultFlushFrequency    = 1
	DefaultFlushMessages     = 1
	DefaultFlushMaxmessages  = 1
	DefaultMaxRetries        = 3
	DefaultReturnSuccess     = false
	DefaultReturnErrors      = true
	DefaultConsumerMaxwait   = 50
	DefaultMaxProcessingTime = 100
	DefaultRequestTimeout    = 50 // 50 milliseconds
)

type consumerChannels struct {
	consumer sarama.PartitionConsumer
	channels []chan *ca.InterContainerMessage
}

// KafkaMessagingProxy represents the messaging proxy
type KafkaMessagingProxy struct {
	KafkaHost                     string
	KafkaPort                     int
	DefaultTopic                  *Topic
	TargetInterface               interface{}
	producer                      sarama.AsyncProducer
	consumer                      sarama.Consumer
	doneCh                        chan int
	topicToConsumerChannelMap     map[string]*consumerChannels
	transactionIdToChannelMap     map[string]chan *ca.InterContainerMessage
	lockTopicToConsumerChannelMap sync.RWMutex
	lockTransactionIdToChannelMap sync.RWMutex
}

type KafkaProxyOption func(*KafkaMessagingProxy)

func KafkaHost(host string) KafkaProxyOption {
	return func(args *KafkaMessagingProxy) {
		args.KafkaHost = host
	}
}

func KafkaPort(port int) KafkaProxyOption {
	return func(args *KafkaMessagingProxy) {
		args.KafkaPort = port
	}
}

func DefaultTopic(topic *Topic) KafkaProxyOption {
	return func(args *KafkaMessagingProxy) {
		args.DefaultTopic = topic
	}
}

func TargetInterface(target interface{}) KafkaProxyOption {
	return func(args *KafkaMessagingProxy) {
		args.TargetInterface = target
	}
}

func NewKafkaMessagingProxy(opts ...KafkaProxyOption) (*KafkaMessagingProxy, error) {
	proxy := &KafkaMessagingProxy{
		KafkaHost:    DefaultKafkaHost,
		KafkaPort:    DefaultKafkaPort,
		DefaultTopic: &Topic{Name: DefaultTopicName},
	}

	for _, option := range opts {
		option(proxy)
	}

	// Create the locks for all the maps
	proxy.lockTopicToConsumerChannelMap = sync.RWMutex{}
	proxy.lockTransactionIdToChannelMap = sync.RWMutex{}

	return proxy, nil
}

func (kp *KafkaMessagingProxy) Start() error {
	log.Info("Starting-Proxy")

	// Create the Done channel
	kp.doneCh = make(chan int, 1)

	// Create the Publisher
	if err := kp.createPublisher(DefaultMaxRetries); err != nil {
		log.Errorw("Cannot-create-kafka-publisher", log.Fields{"error": err})
		return err
	}

	// Create the master consumer
	if err := kp.createConsumer(DefaultMaxRetries); err != nil {
		log.Errorw("Cannot-create-kafka-consumer", log.Fields{"error": err})
		return err
	}

	// Create the topic to consumer/channel map
	kp.topicToConsumerChannelMap = make(map[string]*consumerChannels)

	// Create the transactionId to Channel Map
	kp.transactionIdToChannelMap = make(map[string]chan *ca.InterContainerMessage)

	return nil
}

func (kp *KafkaMessagingProxy) Stop() {
	log.Info("Stopping-Proxy")
	if kp.producer != nil {
		if err := kp.producer.Close(); err != nil {
			panic(err)
		}
	}
	if kp.consumer != nil {
		if err := kp.consumer.Close(); err != nil {
			panic(err)
		}
	}
	//Close the done channel to close all long processing Go routines
	close(kp.doneCh)
}

func (kp *KafkaMessagingProxy) InvokeRPC(ctx context.Context, rpc string, topic *Topic, waitForResponse bool,
	kvArgs ...*KVArg) (bool, *any.Any) {
	// Encode the request
	protoRequest, err := encodeRequest(rpc, topic, kp.DefaultTopic, kvArgs...)
	if err != nil {
		log.Warnw("cannot-format-request", log.Fields{"rpc": rpc, "error": err})
		return false, nil
	}

	// Subscribe for response, if needed, before sending request
	var ch <-chan *ca.InterContainerMessage
	if waitForResponse {
		var err error
		if ch, err = kp.subscribeForResponse(*kp.DefaultTopic, protoRequest.Header.Id); err != nil {
			log.Errorw("failed-to-subscribe-for-response", log.Fields{"error": err, "topic": topic.Name})
		}
	}

	// Send request
	go kp.sendToKafkaTopic(protoRequest, topic)

	if waitForResponse {
		// if ctx is nil use a default timeout ctx to ensure we do not wait forever
		var cancel context.CancelFunc
		if ctx == nil {
			ctx, cancel = context.WithTimeout(context.Background(), DefaultRequestTimeout*time.Millisecond)
			defer cancel()
		}

		// Wait for response as well as timeout or cancellation
		// Remove the subscription for a response on return
		defer kp.unSubscribeForResponse(protoRequest.Header.Id)
		select {
		case msg := <-ch:
			log.Debugw("received-response", log.Fields{"rpc": rpc, "msg": msg})

			var responseBody *ca.InterContainerResponseBody
			var err error
			if responseBody, err = decodeResponse(msg); err != nil {
				log.Errorw("decode-response-error", log.Fields{"error": err})
			}
			return responseBody.Success, responseBody.Result
		case <-ctx.Done():
			log.Debugw("context-cancelled", log.Fields{"rpc": rpc, "ctx": ctx.Err()})
			//	 pack the error as proto any type
			protoError := &ca.Error{Reason: ctx.Err().Error()}
			var marshalledArg *any.Any
			if marshalledArg, err = ptypes.MarshalAny(protoError); err != nil {
				return false, nil // Should never happen
			}
			return false, marshalledArg
		case <-kp.doneCh:
			log.Infow("received-exit-signal", log.Fields{"topic": topic.Name, "rpc": rpc})
			return true, nil
		}
	}
	return true, nil
}

// Subscribe allows a caller to subscribe to a given topic.  A channel is returned to the
// caller to receive messages from that topic.
func (kp *KafkaMessagingProxy) Subscribe(topic Topic) (<-chan *ca.InterContainerMessage, error) {

	log.Debugw("subscribe", log.Fields{"topic": topic.Name})

	if consumerCh := kp.getConsumerChannel(topic); consumerCh != nil {
		log.Debugw("topic-already-subscribed", log.Fields{"topic": topic.Name})
		// Create a channel specific for that consumer and add it to the consumer channel map
		ch := make(chan *ca.InterContainerMessage)
		kp.addChannelToConsumerChannelMap(topic, ch)
		return ch, nil
	}

	// Register for the topic and set it up
	var consumerListeningChannel chan *ca.InterContainerMessage
	var err error
	if consumerListeningChannel, err = kp.setupConsumerChannel(topic); err != nil {
		log.Warnw("create-consumer-channel-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	return consumerListeningChannel, nil
}

func (kp *KafkaMessagingProxy) UnSubscribe(topic Topic, ch <-chan *ca.InterContainerMessage) error {
	log.Debugw("unsubscribing-channel-from-topic", log.Fields{"topic": topic.Name})
	err := kp.removeChannelFromConsumerChannelMap(topic, ch)
	return err
}

// SubscribeWithTarget allows a caller to assign a target object to be invoked automatically
// when a message is received on a given topic
func (kp *KafkaMessagingProxy) SubscribeWithTarget(topic Topic, targetInterface interface{}) error {

	// Subscribe to receive messages for that topic
	var ch <-chan *ca.InterContainerMessage
	var err error
	if ch, err = kp.Subscribe(topic); err != nil {
		log.Errorw("failed-to-subscribe", log.Fields{"error": err, "topic": topic.Name})
	}
	// Launch a go routine to receive and process kafka messages
	go kp.waitForRequest(ch, topic, targetInterface)

	return nil
}

func (kp *KafkaMessagingProxy) UnSubscribeTarget(ctx context.Context, topic Topic, targetInterface interface{}) error {
	// TODO - mostly relevant with multiple interfaces
	return nil
}

func (kp *KafkaMessagingProxy) addToTopicToConsumerChannelMap(id string, arg *consumerChannels) {
	kp.lockTopicToConsumerChannelMap.Lock()
	defer kp.lockTopicToConsumerChannelMap.Unlock()
	if _, exist := kp.topicToConsumerChannelMap[id]; !exist {
		kp.topicToConsumerChannelMap[id] = arg
	}
}

func (kp *KafkaMessagingProxy) deleteFromTopicToConsumerChannelMap(id string) {
	kp.lockTopicToConsumerChannelMap.Lock()
	defer kp.lockTopicToConsumerChannelMap.Unlock()
	if _, exist := kp.topicToConsumerChannelMap[id]; exist {
		delete(kp.topicToConsumerChannelMap, id)
	}
}

func (kp *KafkaMessagingProxy) getConsumerChannel(topic Topic) *consumerChannels {
	kp.lockTopicToConsumerChannelMap.Lock()
	defer kp.lockTopicToConsumerChannelMap.Unlock()

	if consumerCh, exist := kp.topicToConsumerChannelMap[topic.Name]; exist {
		return consumerCh
	}
	return nil
}

func (kp *KafkaMessagingProxy) addChannelToConsumerChannelMap(topic Topic, ch chan *ca.InterContainerMessage) {
	kp.lockTopicToConsumerChannelMap.Lock()
	defer kp.lockTopicToConsumerChannelMap.Unlock()
	if consumerCh, exist := kp.topicToConsumerChannelMap[topic.Name]; exist {
		consumerCh.channels = append(consumerCh.channels, ch)
		return
	}
	log.Warnw("consumer-channel-not-exist", log.Fields{"topic": topic.Name})
}

func (kp *KafkaMessagingProxy) removeChannelFromConsumerChannelMap(topic Topic, ch <-chan *ca.InterContainerMessage) error {
	kp.lockTopicToConsumerChannelMap.Lock()
	defer kp.lockTopicToConsumerChannelMap.Unlock()
	if consumerCh, exist := kp.topicToConsumerChannelMap[topic.Name]; exist {
		// Channel will be closed in the removeChannel method
		consumerCh.channels = removeChannel(consumerCh.channels, ch)
		return nil
	}
	log.Warnw("topic-does-not-exist", log.Fields{"topic": topic.Name})
	return errors.New("topic-does-not-exist")
}

func (kp *KafkaMessagingProxy) addToTransactionIdToChannelMap(id string, arg chan *ca.InterContainerMessage) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	if _, exist := kp.transactionIdToChannelMap[id]; !exist {
		kp.transactionIdToChannelMap[id] = arg
	}
}

func (kp *KafkaMessagingProxy) deleteFromTransactionIdToChannelMap(id string) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	if _, exist := kp.transactionIdToChannelMap[id]; exist {
		delete(kp.transactionIdToChannelMap, id)
	}
}

func (kp *KafkaMessagingProxy) createPublisher(retries int) error {
	// This Creates the publisher
	config := sarama.NewConfig()
	config.Producer.Partitioner = sarama.NewRandomPartitioner
	config.Producer.Flush.Frequency = time.Duration(DefaultFlushFrequency)
	config.Producer.Flush.Messages = DefaultFlushMessages
	config.Producer.Flush.MaxMessages = DefaultFlushMaxmessages
	config.Producer.Return.Errors = DefaultReturnErrors
	config.Producer.Return.Successes = DefaultReturnSuccess
	config.Producer.RequiredAcks = sarama.WaitForAll
	kafkaFullAddr := fmt.Sprintf("%s:%d", kp.KafkaHost, kp.KafkaPort)
	brokers := []string{kafkaFullAddr}

	for {
		producer, err := sarama.NewAsyncProducer(brokers, config)
		if err != nil {
			if retries == 0 {
				log.Errorw("error-starting-publisher", log.Fields{"error": err})
				return err
			} else {
				// If retries is -ve then we will retry indefinitely
				retries--
			}
			log.Info("retrying-after-a-second-delay")
			time.Sleep(time.Duration(DefaultSleepOnError) * time.Second)
		} else {
			kp.producer = producer
			break
		}
	}
	log.Info("Kafka-publisher-created")
	return nil
}

func (kp *KafkaMessagingProxy) createConsumer(retries int) error {
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	config.Consumer.MaxWaitTime = time.Duration(DefaultConsumerMaxwait) * time.Millisecond
	config.Consumer.MaxProcessingTime = time.Duration(DefaultMaxProcessingTime) * time.Millisecond
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	kafkaFullAddr := fmt.Sprintf("%s:%d", kp.KafkaHost, kp.KafkaPort)
	brokers := []string{kafkaFullAddr}

	for {
		consumer, err := sarama.NewConsumer(brokers, config)
		if err != nil {
			if retries == 0 {
				log.Errorw("error-starting-consumer", log.Fields{"error": err})
				return err
			} else {
				// If retries is -ve then we will retry indefinitely
				retries--
			}
			log.Info("retrying-after-a-second-delay")
			time.Sleep(time.Duration(DefaultSleepOnError) * time.Second)
		} else {
			kp.consumer = consumer
			break
		}
	}
	log.Info("Kafka-consumer-created")
	return nil
}

func encodeReturnedValue(request *ca.InterContainerMessage, returnedVal interface{}) (*any.Any, error) {
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

func encodeDefaultFailedResponse(request *ca.InterContainerMessage) *ca.InterContainerMessage {
	responseHeader := &ca.Header{
		Id:        request.Header.Id,
		Type:      ca.MessageType_RESPONSE,
		FromTopic: request.Header.ToTopic,
		ToTopic:   request.Header.FromTopic,
		Timestamp: time.Now().Unix(),
	}
	responseBody := &ca.InterContainerResponseBody{
		Success: false,
		Result:  nil,
	}
	var marshalledResponseBody *any.Any
	var err error
	// Error should never happen here
	if marshalledResponseBody, err = ptypes.MarshalAny(responseBody); err != nil {
		log.Warnw("cannot-marshal-failed-response-body", log.Fields{"error": err})
	}

	return &ca.InterContainerMessage{
		Header: responseHeader,
		Body:   marshalledResponseBody,
	}

}

//formatRequest formats a request to send over kafka and returns an InterContainerMessage message on success
//or an error on failure
func encodeResponse(request *ca.InterContainerMessage, success bool, returnedValues ...interface{}) (*ca.InterContainerMessage, error) {

	log.Infow("encodeResponse", log.Fields{"success": success, "returnedValues": returnedValues})
	responseHeader := &ca.Header{
		Id:        request.Header.Id,
		Type:      ca.MessageType_RESPONSE,
		FromTopic: request.Header.ToTopic,
		ToTopic:   request.Header.FromTopic,
		Timestamp: time.Now().Unix(),
	}

	// Go over all returned values
	var marshalledReturnedVal *any.Any
	var err error
	for _, returnVal := range returnedValues {
		if marshalledReturnedVal, err = encodeReturnedValue(request, returnVal); err != nil {
			log.Warnw("cannot-marshal-response-body", log.Fields{"error": err})
		}
		break // for now we support only 1 returned value - (excluding the error)
	}

	responseBody := &ca.InterContainerResponseBody{
		Success: success,
		Result:  marshalledReturnedVal,
	}

	// Marshal the response body
	var marshalledResponseBody *any.Any
	if marshalledResponseBody, err = ptypes.MarshalAny(responseBody); err != nil {
		log.Warnw("cannot-marshal-response-body", log.Fields{"error": err})
		return nil, err
	}

	return &ca.InterContainerMessage{
		Header: responseHeader,
		Body:   marshalledResponseBody,
	}, nil
}

func CallFuncByName(myClass interface{}, funcName string, params ...interface{}) (out []reflect.Value, err error) {
	myClassValue := reflect.ValueOf(myClass)
	m := myClassValue.MethodByName(funcName)
	if !m.IsValid() {
		return make([]reflect.Value, 0), fmt.Errorf("Method not found \"%s\"", funcName)
	}
	in := make([]reflect.Value, len(params))
	for i, param := range params {
		in[i] = reflect.ValueOf(param)
	}
	out = m.Call(in)
	return
}

func (kp *KafkaMessagingProxy) handleRequest(msg *ca.InterContainerMessage, targetInterface interface{}) {

	// First extract the header to know whether this is a request of a response
	if msg.Header.Type == ca.MessageType_REQUEST {
		log.Debugw("received-request", log.Fields{"header": msg.Header})

		var out []reflect.Value
		var err error

		// Get the request body
		requestBody := &ca.InterContainerRequestBody{}
		if err = ptypes.UnmarshalAny(msg.Body, requestBody); err != nil {
			log.Warnw("cannot-unmarshal-request", log.Fields{"error": err})
		} else {
			// let the callee unpack the arguments as its the only one that knows the real proto type
			out, err = CallFuncByName(targetInterface, requestBody.Rpc, requestBody.Args)
			if err != nil {
				log.Warn(err)
			}
		}
		// Response required?
		if requestBody.ResponseRequired {
			// If we already have an error before then just return that
			var returnError *ca.Error
			var returnedValues []interface{}
			var success bool
			if err != nil {
				returnError = &ca.Error{Reason: err.Error()}
				returnedValues = make([]interface{}, 1)
				returnedValues[0] = returnError
			} else {
				log.Debugw("returned-api-response", log.Fields{"len": len(out), "err": err})
				returnSize := 1 // Minimum array size
				if len(out) > 1 {
					returnSize = len(out) - 1
				}
				returnedValues = make([]interface{}, returnSize)
				for idx, val := range out {
					log.Debugw("returned-api-response-loop", log.Fields{"idx": idx, "val": val.Interface()})
					if idx == 0 {
						if val.Interface() != nil {
							if goError, ok := out[0].Interface().(error); ok {
								returnError = &ca.Error{Reason: goError.Error()}
								returnedValues[0] = returnError
							} // Else should never occur - maybe never say never?
							break
						} else {
							success = true
						}
					} else {
						returnedValues[idx-1] = val.Interface()
					}
				}
			}

			var icm *ca.InterContainerMessage
			if icm, err = encodeResponse(msg, success, returnedValues...); err != nil {
				log.Warnw("error-encoding-response-returning-failure-result", log.Fields{"erroe": err})
				icm = encodeDefaultFailedResponse(msg)
			}
			kp.sendToKafkaTopic(icm, &Topic{Name: msg.Header.FromTopic})
		}

	} else if msg.Header.Type == ca.MessageType_RESPONSE {
		log.Warnw("received-response-on-request-handler", log.Fields{"header": msg.Header})
	} else {
		log.Errorw("invalid-message", log.Fields{"header": msg.Header})
	}
}

func (kp *KafkaMessagingProxy) waitForRequest(ch <-chan *ca.InterContainerMessage, topic Topic, targetInterface interface{}) {
	//	Wait for messages
	for msg := range ch {
		log.Debugw("request-received", log.Fields{"msg": msg, "topic": topic.Name, "target": targetInterface})
		go kp.handleRequest(msg, targetInterface)
	}
}

// dispatchToConsumers sends the intercontainermessage received on a given topic to all subscribers for that
// topic via the unique channel each subsciber received during subscription
func (kp *KafkaMessagingProxy) dispatchToConsumers(consumerCh *consumerChannels, protoMessage *ca.InterContainerMessage) {
	// Need to go over all channels and publish messages to them - do we need to copy msg?
	kp.lockTopicToConsumerChannelMap.Lock()
	defer kp.lockTopicToConsumerChannelMap.Unlock()
	for _, ch := range consumerCh.channels {
		go func(c chan *ca.InterContainerMessage) {
			c <- protoMessage
		}(ch)
	}
}

func (kp *KafkaMessagingProxy) consumeMessagesLoop(topic Topic) {
	log.Debugw("starting-consuming-messages", log.Fields{"topic": topic.Name})
	var consumerCh *consumerChannels
	if consumerCh = kp.getConsumerChannel(topic); consumerCh == nil {
		log.Errorw("consumer-not-exist", log.Fields{"topic": topic.Name})
		return
	}
startloop:
	for {
		select {
		case err := <-consumerCh.consumer.Errors():
			log.Warnw("consumer-error", log.Fields{"error": err})
		case msg := <-consumerCh.consumer.Messages():
			log.Debugw("message-received", log.Fields{"msg": msg})
			// Since the only expected message is a proto intercontainermessage then extract it right away
			// instead of dispatching it to the consumers
			msgBody := msg.Value
			icm := &ca.InterContainerMessage{}
			if err := proto.Unmarshal(msgBody, icm); err != nil {
				log.Warnw("invalid-message", log.Fields{"error": err})
				continue
			}
			log.Debugw("msg-to-consumers", log.Fields{"msg": *icm, "len": len(consumerCh.channels)})

			go kp.dispatchToConsumers(consumerCh, icm)
		case <-kp.doneCh:
			log.Infow("received-exit-signal", log.Fields{"topic": topic.Name})
			break startloop
		}
	}
}

func (kp *KafkaMessagingProxy) dispatchResponse(msg *ca.InterContainerMessage) {
	kp.lockTransactionIdToChannelMap.Lock()
	defer kp.lockTransactionIdToChannelMap.Unlock()
	if _, exist := kp.transactionIdToChannelMap[msg.Header.Id]; !exist {
		log.Debugw("no-waiting-channel", log.Fields{"transaction": msg.Header.Id})
		return
	}
	kp.transactionIdToChannelMap[msg.Header.Id] <- msg
}

func (kp *KafkaMessagingProxy) waitForResponse(ch chan *ca.InterContainerMessage, topic Topic) {
	log.Debugw("starting-consuming-responses-loop", log.Fields{"topic": topic.Name})
startloop:
	for {
		select {
		case msg := <-ch:
			log.Debugw("message-received", log.Fields{"topic": topic.Name, "msg": msg})
			go kp.dispatchResponse(msg)
			//	Need to handle program exit - TODO
		case <-kp.doneCh:
			log.Infow("received-exit-signal", log.Fields{"topic": topic.Name})
			break startloop
		}
	}
}

// createConsumerChannel creates a consumerChannels object for that topic and add it to the consumerChannels map
// for that topic.  It also starts the routine that listens for messages on that topic.
func (kp *KafkaMessagingProxy) setupConsumerChannel(topic Topic) (chan *ca.InterContainerMessage, error) {

	if consumerCh := kp.getConsumerChannel(topic); consumerCh != nil {
		return nil, nil // Already created, so just ignore
	}

	partitionList, err := kp.consumer.Partitions(topic.Name)
	if err != nil {
		log.Warnw("get-partition-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	log.Debugw("partitions", log.Fields{"topic": topic.Name, "partitionList": partitionList, "first": partitionList[0]})
	// Create a partition consumer for that topic - for now just use one partition
	var pConsumer sarama.PartitionConsumer
	if pConsumer, err = kp.consumer.ConsumePartition(topic.Name, partitionList[0], sarama.OffsetNewest); err != nil {
		log.Warnw("consumer-partition-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	// Create the consumer/channel structure and set the consumer and create a channel on that topic - for now
	// unbuffered to verify race conditions.
	consumerListeningChannel := make(chan *ca.InterContainerMessage)
	cc := &consumerChannels{
		consumer: pConsumer,
		channels: []chan *ca.InterContainerMessage{consumerListeningChannel},
	}

	// Add the consumer channel to the map
	kp.addToTopicToConsumerChannelMap(topic.Name, cc)

	//Start a consumer to listen on that specific topic
	go kp.consumeMessagesLoop(topic)

	return consumerListeningChannel, nil
}

// subscribeForResponse allows a caller to subscribe to a given topic when waiting for a response.
// This method is built to prevent all subscribers to receive all messages as is the case of the Subscribe
// API. There is one response channel waiting for kafka messages before dispatching the message to the
// corresponding waiting channel
func (kp *KafkaMessagingProxy) subscribeForResponse(topic Topic, trnsId string) (chan *ca.InterContainerMessage, error) {
	log.Debugw("subscribeForResponse", log.Fields{"topic": topic.Name})

	if consumerCh := kp.getConsumerChannel(topic); consumerCh == nil {
		log.Debugw("topic-not-subscribed", log.Fields{"topic": topic.Name})
		var consumerListeningChannel chan *ca.InterContainerMessage
		var err error
		if consumerListeningChannel, err = kp.setupConsumerChannel(topic); err != nil {
			log.Warnw("create-consumer-channel-failure", log.Fields{"error": err, "topic": topic.Name})
			return nil, err
		}
		// Start a go routine to listen to response messages over the consumer listening channel
		go kp.waitForResponse(consumerListeningChannel, topic)
	}

	ch := make(chan *ca.InterContainerMessage)
	kp.addToTransactionIdToChannelMap(trnsId, ch)

	return ch, nil
}

func removeChannel(channels []chan *ca.InterContainerMessage, ch <-chan *ca.InterContainerMessage) []chan *ca.InterContainerMessage {
	var i int
	var channel chan *ca.InterContainerMessage
	for i, channel = range channels {
		if channel == ch {
			channels[len(channels)-1], channels[i] = channels[i], channels[len(channels)-1]
			close(channel)
			return channels[:len(channels)-1]
		}
	}
	return channels
}

func (kp *KafkaMessagingProxy) unSubscribeForResponse(trnsId string) error {
	log.Debugw("unsubscribe-for-response", log.Fields{"trnsId": trnsId})
	// Close the channel first
	close(kp.transactionIdToChannelMap[trnsId])
	kp.deleteFromTransactionIdToChannelMap(trnsId)
	return nil
}

//formatRequest formats a request to send over kafka and returns an InterContainerMessage message on success
//or an error on failure
func encodeRequest(rpc string, toTopic *Topic, replyTopic *Topic, kvArgs ...*KVArg) (*ca.InterContainerMessage, error) {
	requestHeader := &ca.Header{
		Id:        uuid.New().String(),
		Type:      ca.MessageType_REQUEST,
		FromTopic: replyTopic.Name,
		ToTopic:   toTopic.Name,
		Timestamp: time.Now().Unix(),
	}
	requestBody := &ca.InterContainerRequestBody{
		Rpc:              rpc,
		ResponseRequired: true,
		ReplyToTopic:     replyTopic.Name,
	}

	for _, arg := range kvArgs {
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
		protoArg := &ca.Argument{
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
	request := &ca.InterContainerMessage{
		Header: requestHeader,
		Body:   marshalledData,
	}
	return request, nil
}

// sendRequest formats and sends the request onto the kafka messaging bus.  It waits for the
// response if needed.  This function must, therefore, be run in its own routine.
func (kp *KafkaMessagingProxy) sendToKafkaTopic(msg *ca.InterContainerMessage, topic *Topic) {

	//	Create the Sarama producer message
	time := time.Now().Unix()
	marshalled, _ := proto.Marshal(msg)
	kafkaMsg := &sarama.ProducerMessage{
		Topic: topic.Name,
		Key:   sarama.StringEncoder(time),
		Value: sarama.ByteEncoder(marshalled),
	}

	// Send message to kafka
	kp.producer.Input() <- kafkaMsg

}

func decodeResponse(response *ca.InterContainerMessage) (*ca.InterContainerResponseBody, error) {
	//	Extract the message body
	responseBody := ca.InterContainerResponseBody{}

	log.Debugw("decodeResponse", log.Fields{"icr": &response})
	if err := ptypes.UnmarshalAny(response.Body, &responseBody); err != nil {
		log.Warnw("cannot-unmarshal-response", log.Fields{"error": err})
		return nil, err
	}
	log.Debugw("decodeResponse", log.Fields{"icrbody": &responseBody})

	return &responseBody, nil

}
