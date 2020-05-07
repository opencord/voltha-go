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
	"strings"
	"sync"
	"time"

	"github.com/Shopify/sarama"
	scc "github.com/bsm/sarama-cluster"
	"github.com/eapache/go-resiliency/breaker"
	"github.com/golang/protobuf/proto"
	"github.com/golang/protobuf/ptypes"
	"github.com/google/uuid"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
)

// consumerChannels represents one or more consumers listening on a kafka topic.  Once a message is received on that
// topic, the consumer(s) broadcasts the message to all the listening channels.   The consumer can be a partition
//consumer or a group consumer
type consumerChannels struct {
	consumers []interface{}
	channels  []chan *ic.InterContainerMessage
}

// static check to ensure SaramaClient implements Client
var _ Client = &SaramaClient{}

// SaramaClient represents the messaging proxy
type SaramaClient struct {
	cAdmin                        sarama.ClusterAdmin
	KafkaAddress                  string
	producer                      sarama.AsyncProducer
	consumer                      sarama.Consumer
	groupConsumers                map[string]*scc.Consumer
	lockOfGroupConsumers          sync.RWMutex
	consumerGroupPrefix           string
	consumerType                  int
	consumerGroupName             string
	producerFlushFrequency        int
	producerFlushMessages         int
	producerFlushMaxmessages      int
	producerRetryMax              int
	producerRetryBackOff          time.Duration
	producerReturnSuccess         bool
	producerReturnErrors          bool
	consumerMaxwait               int
	maxProcessingTime             int
	numPartitions                 int
	numReplicas                   int
	autoCreateTopic               bool
	doneCh                        chan int
	metadataCallback              func(fromTopic string, timestamp time.Time)
	topicToConsumerChannelMap     map[string]*consumerChannels
	lockTopicToConsumerChannelMap sync.RWMutex
	topicLockMap                  map[string]*sync.RWMutex
	lockOfTopicLockMap            sync.RWMutex
	metadataMaxRetry              int
	alive                         bool
	liveness                      chan bool
	livenessChannelInterval       time.Duration
	lastLivenessTime              time.Time
	started                       bool
	healthy                       bool
	healthiness                   chan bool
}

type SaramaClientOption func(*SaramaClient)

func Address(address string) SaramaClientOption {
	return func(args *SaramaClient) {
		args.KafkaAddress = address
	}
}

func ConsumerGroupPrefix(prefix string) SaramaClientOption {
	return func(args *SaramaClient) {
		args.consumerGroupPrefix = prefix
	}
}

func ConsumerGroupName(name string) SaramaClientOption {
	return func(args *SaramaClient) {
		args.consumerGroupName = name
	}
}

func ConsumerType(consumer int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.consumerType = consumer
	}
}

func ProducerFlushFrequency(frequency int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.producerFlushFrequency = frequency
	}
}

func ProducerFlushMessages(num int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.producerFlushMessages = num
	}
}

func ProducerFlushMaxMessages(num int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.producerFlushMaxmessages = num
	}
}

func ProducerMaxRetries(num int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.producerRetryMax = num
	}
}

func ProducerRetryBackoff(duration time.Duration) SaramaClientOption {
	return func(args *SaramaClient) {
		args.producerRetryBackOff = duration
	}
}

func ProducerReturnOnErrors(opt bool) SaramaClientOption {
	return func(args *SaramaClient) {
		args.producerReturnErrors = opt
	}
}

func ProducerReturnOnSuccess(opt bool) SaramaClientOption {
	return func(args *SaramaClient) {
		args.producerReturnSuccess = opt
	}
}

func ConsumerMaxWait(wait int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.consumerMaxwait = wait
	}
}

func MaxProcessingTime(pTime int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.maxProcessingTime = pTime
	}
}

func NumPartitions(number int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.numPartitions = number
	}
}

func NumReplicas(number int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.numReplicas = number
	}
}

func AutoCreateTopic(opt bool) SaramaClientOption {
	return func(args *SaramaClient) {
		args.autoCreateTopic = opt
	}
}

func MetadatMaxRetries(retry int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.metadataMaxRetry = retry
	}
}

func LivenessChannelInterval(opt time.Duration) SaramaClientOption {
	return func(args *SaramaClient) {
		args.livenessChannelInterval = opt
	}
}

func NewSaramaClient(opts ...SaramaClientOption) *SaramaClient {
	client := &SaramaClient{
		KafkaAddress: DefaultKafkaAddress,
	}
	client.consumerType = DefaultConsumerType
	client.producerFlushFrequency = DefaultProducerFlushFrequency
	client.producerFlushMessages = DefaultProducerFlushMessages
	client.producerFlushMaxmessages = DefaultProducerFlushMaxmessages
	client.producerReturnErrors = DefaultProducerReturnErrors
	client.producerReturnSuccess = DefaultProducerReturnSuccess
	client.producerRetryMax = DefaultProducerRetryMax
	client.producerRetryBackOff = DefaultProducerRetryBackoff
	client.consumerMaxwait = DefaultConsumerMaxwait
	client.maxProcessingTime = DefaultMaxProcessingTime
	client.numPartitions = DefaultNumberPartitions
	client.numReplicas = DefaultNumberReplicas
	client.autoCreateTopic = DefaultAutoCreateTopic
	client.metadataMaxRetry = DefaultMetadataMaxRetry
	client.livenessChannelInterval = DefaultLivenessChannelInterval

	for _, option := range opts {
		option(client)
	}

	client.groupConsumers = make(map[string]*scc.Consumer)

	client.lockTopicToConsumerChannelMap = sync.RWMutex{}
	client.topicLockMap = make(map[string]*sync.RWMutex)
	client.lockOfTopicLockMap = sync.RWMutex{}
	client.lockOfGroupConsumers = sync.RWMutex{}

	// healthy and alive until proven otherwise
	client.alive = true
	client.healthy = true

	return client
}

func (sc *SaramaClient) Start() error {
	logger.Info("Starting-kafka-sarama-client")

	// Create the Done channel
	sc.doneCh = make(chan int, 1)

	var err error

	// Add a cleanup in case of failure to startup
	defer func() {
		if err != nil {
			sc.Stop()
		}
	}()

	// Create the Cluster Admin
	if err = sc.createClusterAdmin(); err != nil {
		logger.Errorw("Cannot-create-cluster-admin", log.Fields{"error": err})
		return err
	}

	// Create the Publisher
	if err := sc.createPublisher(); err != nil {
		logger.Errorw("Cannot-create-kafka-publisher", log.Fields{"error": err})
		return err
	}

	if sc.consumerType == DefaultConsumerType {
		// Create the master consumers
		if err := sc.createConsumer(); err != nil {
			logger.Errorw("Cannot-create-kafka-consumers", log.Fields{"error": err})
			return err
		}
	}

	// Create the topic to consumers/channel map
	sc.topicToConsumerChannelMap = make(map[string]*consumerChannels)

	logger.Info("kafka-sarama-client-started")

	sc.started = true

	return nil
}

func (sc *SaramaClient) Stop() {
	logger.Info("stopping-sarama-client")

	sc.started = false

	//Send a message over the done channel to close all long running routines
	sc.doneCh <- 1

	if sc.producer != nil {
		if err := sc.producer.Close(); err != nil {
			logger.Errorw("closing-producer-failed", log.Fields{"error": err})
		}
	}

	if sc.consumer != nil {
		if err := sc.consumer.Close(); err != nil {
			logger.Errorw("closing-partition-consumer-failed", log.Fields{"error": err})
		}
	}

	for key, val := range sc.groupConsumers {
		logger.Debugw("closing-group-consumer", log.Fields{"topic": key})
		if err := val.Close(); err != nil {
			logger.Errorw("closing-group-consumer-failed", log.Fields{"error": err, "topic": key})
		}
	}

	if sc.cAdmin != nil {
		if err := sc.cAdmin.Close(); err != nil {
			logger.Errorw("closing-cluster-admin-failed", log.Fields{"error": err})
		}
	}

	//TODO: Clear the consumers map
	//sc.clearConsumerChannelMap()

	logger.Info("sarama-client-stopped")
}

//createTopic is an internal function to create a topic on the Kafka Broker. No locking is required as
// the invoking function must hold the lock
func (sc *SaramaClient) createTopic(topic *Topic, numPartition int, repFactor int) error {
	// Set the topic details
	topicDetail := &sarama.TopicDetail{}
	topicDetail.NumPartitions = int32(numPartition)
	topicDetail.ReplicationFactor = int16(repFactor)
	topicDetail.ConfigEntries = make(map[string]*string)
	topicDetails := make(map[string]*sarama.TopicDetail)
	topicDetails[topic.Name] = topicDetail

	if err := sc.cAdmin.CreateTopic(topic.Name, topicDetail, false); err != nil {
		if err == sarama.ErrTopicAlreadyExists {
			//	Not an error
			logger.Debugw("topic-already-exist", log.Fields{"topic": topic.Name})
			return nil
		}
		logger.Errorw("create-topic-failure", log.Fields{"error": err})
		return err
	}
	// TODO: Wait until the topic has been created.  No API is available in the Sarama clusterAdmin to
	// do so.
	logger.Debugw("topic-created", log.Fields{"topic": topic, "numPartition": numPartition, "replicationFactor": repFactor})
	return nil
}

//CreateTopic is a public API to create a topic on the Kafka Broker.  It uses a lock on a specific topic to
// ensure no two go routines are performing operations on the same topic
func (sc *SaramaClient) CreateTopic(topic *Topic, numPartition int, repFactor int) error {
	sc.lockTopic(topic)
	defer sc.unLockTopic(topic)

	return sc.createTopic(topic, numPartition, repFactor)
}

//DeleteTopic removes a topic from the kafka Broker
func (sc *SaramaClient) DeleteTopic(topic *Topic) error {
	sc.lockTopic(topic)
	defer sc.unLockTopic(topic)

	// Remove the topic from the broker
	if err := sc.cAdmin.DeleteTopic(topic.Name); err != nil {
		if err == sarama.ErrUnknownTopicOrPartition {
			//	Not an error as does not exist
			logger.Debugw("topic-not-exist", log.Fields{"topic": topic.Name})
			return nil
		}
		logger.Errorw("delete-topic-failed", log.Fields{"topic": topic, "error": err})
		return err
	}

	// Clear the topic from the consumer channel.  This will also close any consumers listening on that topic.
	if err := sc.clearTopicFromConsumerChannelMap(*topic); err != nil {
		logger.Errorw("failure-clearing-channels", log.Fields{"topic": topic, "error": err})
		return err
	}
	return nil
}

// Subscribe registers a caller to a topic. It returns a channel that the caller can use to receive
// messages from that topic
func (sc *SaramaClient) Subscribe(topic *Topic, kvArgs ...*KVArg) (<-chan *ic.InterContainerMessage, error) {
	sc.lockTopic(topic)
	defer sc.unLockTopic(topic)

	logger.Debugw("subscribe", log.Fields{"topic": topic.Name})

	// If a consumers already exist for that topic then resuse it
	if consumerCh := sc.getConsumerChannel(topic); consumerCh != nil {
		logger.Debugw("topic-already-subscribed", log.Fields{"topic": topic.Name})
		// Create a channel specific for that consumers and add it to the consumers channel map
		ch := make(chan *ic.InterContainerMessage)
		sc.addChannelToConsumerChannelMap(topic, ch)
		return ch, nil
	}

	// Register for the topic and set it up
	var consumerListeningChannel chan *ic.InterContainerMessage
	var err error

	// Use the consumerType option to figure out the type of consumer to launch
	if sc.consumerType == PartitionConsumer {
		if sc.autoCreateTopic {
			if err = sc.createTopic(topic, sc.numPartitions, sc.numReplicas); err != nil {
				logger.Errorw("create-topic-failure", log.Fields{"error": err, "topic": topic.Name})
				return nil, err
			}
		}
		if consumerListeningChannel, err = sc.setupPartitionConsumerChannel(topic, getOffset(kvArgs...)); err != nil {
			logger.Warnw("create-consumers-channel-failure", log.Fields{"error": err, "topic": topic.Name})
			return nil, err
		}
	} else if sc.consumerType == GroupCustomer {
		// TODO: create topic if auto create is on.  There is an issue with the sarama cluster library that
		// does not consume from a precreated topic in some scenarios
		//if sc.autoCreateTopic {
		//	if err = sc.createTopic(topic, sc.numPartitions, sc.numReplicas); err != nil {
		//		logger.Errorw("create-topic-failure", logger.Fields{"error": err, "topic": topic.Name})
		//		return nil, err
		//	}
		//}
		//groupId := sc.consumerGroupName
		groupId := getGroupId(kvArgs...)
		// Include the group prefix
		if groupId != "" {
			groupId = sc.consumerGroupPrefix + groupId
		} else {
			// Need to use a unique group Id per topic
			groupId = sc.consumerGroupPrefix + topic.Name
		}
		if consumerListeningChannel, err = sc.setupGroupConsumerChannel(topic, groupId, getOffset(kvArgs...)); err != nil {
			logger.Warnw("create-consumers-channel-failure", log.Fields{"error": err, "topic": topic.Name, "groupId": groupId})
			return nil, err
		}

	} else {
		logger.Warnw("unknown-consumer-type", log.Fields{"consumer-type": sc.consumerType})
		return nil, errors.New("unknown-consumer-type")
	}

	return consumerListeningChannel, nil
}

//UnSubscribe unsubscribe a consumer from a given topic
func (sc *SaramaClient) UnSubscribe(topic *Topic, ch <-chan *ic.InterContainerMessage) error {
	sc.lockTopic(topic)
	defer sc.unLockTopic(topic)

	logger.Debugw("unsubscribing-channel-from-topic", log.Fields{"topic": topic.Name})
	var err error
	if err = sc.removeChannelFromConsumerChannelMap(*topic, ch); err != nil {
		logger.Errorw("failed-removing-channel", log.Fields{"error": err})
	}
	if err = sc.deleteFromGroupConsumers(topic.Name); err != nil {
		logger.Errorw("failed-deleting-group-consumer", log.Fields{"error": err})
	}
	return err
}

func (sc *SaramaClient) SubscribeForMetadata(callback func(fromTopic string, timestamp time.Time)) {
	sc.metadataCallback = callback
}

func (sc *SaramaClient) updateLiveness(alive bool) {
	// Post a consistent stream of liveness data to the channel,
	// so that in a live state, the core does not timeout and
	// send a forced liveness message. Production of liveness
	// events to the channel is rate-limited by livenessChannelInterval.
	if sc.liveness != nil {
		if sc.alive != alive {
			logger.Info("update-liveness-channel-because-change")
			sc.liveness <- alive
			sc.lastLivenessTime = time.Now()
		} else if time.Since(sc.lastLivenessTime) > sc.livenessChannelInterval {
			logger.Info("update-liveness-channel-because-interval")
			sc.liveness <- alive
			sc.lastLivenessTime = time.Now()
		}
	}

	// Only emit a log message when the state changes
	if sc.alive != alive {
		logger.Info("set-client-alive", log.Fields{"alive": alive})
		sc.alive = alive
	}
}

// Once unhealthy, we never go back
func (sc *SaramaClient) setUnhealthy() {
	sc.healthy = false
	if sc.healthiness != nil {
		logger.Infow("set-client-unhealthy", log.Fields{"healthy": sc.healthy})
		sc.healthiness <- sc.healthy
	}
}

func (sc *SaramaClient) isLivenessError(err error) bool {
	// Sarama producers and consumers encapsulate the error inside
	// a ProducerError or ConsumerError struct.
	if prodError, ok := err.(*sarama.ProducerError); ok {
		err = prodError.Err
	} else if consumerError, ok := err.(*sarama.ConsumerError); ok {
		err = consumerError.Err
	}

	// Sarama-Cluster will compose the error into a ClusterError struct,
	// which we can't do a compare by reference. To handle that, we the
	// best we can do is compare the error strings.

	switch err.Error() {
	case context.DeadlineExceeded.Error():
		logger.Info("is-liveness-error-timeout")
		return true
	case sarama.ErrOutOfBrokers.Error(): // "Kafka: client has run out of available brokers"
		logger.Info("is-liveness-error-no-brokers")
		return true
	case sarama.ErrShuttingDown.Error(): // "Kafka: message received by producer in process of shutting down"
		logger.Info("is-liveness-error-shutting-down")
		return true
	case sarama.ErrControllerNotAvailable.Error(): // "Kafka: controller is not available"
		logger.Info("is-liveness-error-not-available")
		return true
	case breaker.ErrBreakerOpen.Error(): // "circuit breaker is open"
		logger.Info("is-liveness-error-circuit-breaker-open")
		return true
	}

	if strings.HasSuffix(err.Error(), "connection refused") { // "dial tcp 10.244.1.176:9092: connect: connection refused"
		logger.Info("is-liveness-error-connection-refused")
		return true
	}

	if strings.HasSuffix(err.Error(), "i/o timeout") { // "dial tcp 10.244.1.176:9092: i/o timeout"
		logger.Info("is-liveness-error-io-timeout")
		return true
	}

	// Other errors shouldn't trigger a loss of liveness

	logger.Infow("is-liveness-error-ignored", log.Fields{"err": err})

	return false
}

// send formats and sends the request onto the kafka messaging bus.
func (sc *SaramaClient) Send(msg interface{}, topic *Topic, keys ...string) error {

	// Assert message is a proto message
	var protoMsg proto.Message
	var ok bool
	// ascertain the value interface type is a proto.Message
	if protoMsg, ok = msg.(proto.Message); !ok {
		logger.Warnw("message-not-proto-message", log.Fields{"msg": msg})
		return fmt.Errorf("not-a-proto-msg-%s", msg)
	}

	var marshalled []byte
	var err error
	//	Create the Sarama producer message
	if marshalled, err = proto.Marshal(protoMsg); err != nil {
		logger.Errorw("marshalling-failed", log.Fields{"msg": protoMsg, "error": err})
		return err
	}
	key := ""
	if len(keys) > 0 {
		key = keys[0] // Only the first key is relevant
	}
	kafkaMsg := &sarama.ProducerMessage{
		Topic: topic.Name,
		Key:   sarama.StringEncoder(key),
		Value: sarama.ByteEncoder(marshalled),
	}

	// Send message to kafka
	sc.producer.Input() <- kafkaMsg
	// Wait for result
	// TODO: Use a lock or a different mechanism to ensure the response received corresponds to the message sent.
	select {
	case ok := <-sc.producer.Successes():
		logger.Debugw("message-sent", log.Fields{"status": ok.Topic})
		sc.updateLiveness(true)
	case notOk := <-sc.producer.Errors():
		logger.Debugw("error-sending", log.Fields{"status": notOk})
		if sc.isLivenessError(notOk) {
			sc.updateLiveness(false)
		}
		return notOk
	}
	return nil
}

// Enable the liveness monitor channel. This channel will report
// a "true" or "false" on every publish, which indicates whether
// or not the channel is still live. This channel is then picked up
// by the service (i.e. rw_core / ro_core) to update readiness status
// and/or take other actions.
func (sc *SaramaClient) EnableLivenessChannel(enable bool) chan bool {
	logger.Infow("kafka-enable-liveness-channel", log.Fields{"enable": enable})
	if enable {
		if sc.liveness == nil {
			logger.Info("kafka-create-liveness-channel")
			// At least 1, so we can immediately post to it without blocking
			// Setting a bigger number (10) allows the monitor to fall behind
			// without blocking others. The monitor shouldn't really fall
			// behind...
			sc.liveness = make(chan bool, 10)
			// post intial state to the channel
			sc.liveness <- sc.alive
		}
	} else {
		// TODO: Think about whether we need the ability to turn off
		// liveness monitoring
		panic("Turning off liveness reporting is not supported")
	}
	return sc.liveness
}

// Enable the Healthiness monitor channel. This channel will report "false"
// if the kafka consumers die, or some other problem occurs which is
// catastrophic that would require re-creating the client.
func (sc *SaramaClient) EnableHealthinessChannel(enable bool) chan bool {
	logger.Infow("kafka-enable-healthiness-channel", log.Fields{"enable": enable})
	if enable {
		if sc.healthiness == nil {
			logger.Info("kafka-create-healthiness-channel")
			// At least 1, so we can immediately post to it without blocking
			// Setting a bigger number (10) allows the monitor to fall behind
			// without blocking others. The monitor shouldn't really fall
			// behind...
			sc.healthiness = make(chan bool, 10)
			// post intial state to the channel
			sc.healthiness <- sc.healthy
		}
	} else {
		// TODO: Think about whether we need the ability to turn off
		// liveness monitoring
		panic("Turning off healthiness reporting is not supported")
	}
	return sc.healthiness
}

// send an empty message on the liveness channel to check whether connectivity has
// been restored.
func (sc *SaramaClient) SendLiveness() error {
	if !sc.started {
		return fmt.Errorf("SendLiveness() called while not started")
	}

	kafkaMsg := &sarama.ProducerMessage{
		Topic: "_liveness_test",
		Value: sarama.StringEncoder(time.Now().Format(time.RFC3339)), // for debugging / informative use
	}

	// Send message to kafka
	sc.producer.Input() <- kafkaMsg
	// Wait for result
	// TODO: Use a lock or a different mechanism to ensure the response received corresponds to the message sent.
	select {
	case ok := <-sc.producer.Successes():
		logger.Debugw("liveness-message-sent", log.Fields{"status": ok.Topic})
		sc.updateLiveness(true)
	case notOk := <-sc.producer.Errors():
		logger.Debugw("liveness-error-sending", log.Fields{"status": notOk})
		if sc.isLivenessError(notOk) {
			sc.updateLiveness(false)
		}
		return notOk
	}
	return nil
}

// getGroupId returns the group id from the key-value args.
func getGroupId(kvArgs ...*KVArg) string {
	for _, arg := range kvArgs {
		if arg.Key == GroupIdKey {
			return arg.Value.(string)
		}
	}
	return ""
}

// getOffset returns the offset from the key-value args.
func getOffset(kvArgs ...*KVArg) int64 {
	for _, arg := range kvArgs {
		if arg.Key == Offset {
			return arg.Value.(int64)
		}
	}
	return sarama.OffsetNewest
}

func (sc *SaramaClient) createClusterAdmin() error {
	config := sarama.NewConfig()
	config.Version = sarama.V1_0_0_0

	// Create a cluster Admin
	var cAdmin sarama.ClusterAdmin
	var err error
	if cAdmin, err = sarama.NewClusterAdmin([]string{sc.KafkaAddress}, config); err != nil {
		logger.Errorw("cluster-admin-failure", log.Fields{"error": err, "broker-address": sc.KafkaAddress})
		return err
	}
	sc.cAdmin = cAdmin
	return nil
}

func (sc *SaramaClient) lockTopic(topic *Topic) {
	sc.lockOfTopicLockMap.Lock()
	if _, exist := sc.topicLockMap[topic.Name]; exist {
		sc.lockOfTopicLockMap.Unlock()
		sc.topicLockMap[topic.Name].Lock()
	} else {
		sc.topicLockMap[topic.Name] = &sync.RWMutex{}
		sc.lockOfTopicLockMap.Unlock()
		sc.topicLockMap[topic.Name].Lock()
	}
}

func (sc *SaramaClient) unLockTopic(topic *Topic) {
	sc.lockOfTopicLockMap.Lock()
	defer sc.lockOfTopicLockMap.Unlock()
	if _, exist := sc.topicLockMap[topic.Name]; exist {
		sc.topicLockMap[topic.Name].Unlock()
	}
}

func (sc *SaramaClient) addTopicToConsumerChannelMap(id string, arg *consumerChannels) {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if _, exist := sc.topicToConsumerChannelMap[id]; !exist {
		sc.topicToConsumerChannelMap[id] = arg
	}
}

func (sc *SaramaClient) getConsumerChannel(topic *Topic) *consumerChannels {
	sc.lockTopicToConsumerChannelMap.RLock()
	defer sc.lockTopicToConsumerChannelMap.RUnlock()

	if consumerCh, exist := sc.topicToConsumerChannelMap[topic.Name]; exist {
		return consumerCh
	}
	return nil
}

func (sc *SaramaClient) addChannelToConsumerChannelMap(topic *Topic, ch chan *ic.InterContainerMessage) {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if consumerCh, exist := sc.topicToConsumerChannelMap[topic.Name]; exist {
		consumerCh.channels = append(consumerCh.channels, ch)
		return
	}
	logger.Warnw("consumers-channel-not-exist", log.Fields{"topic": topic.Name})
}

//closeConsumers closes a list of sarama consumers.  The consumers can either be a partition consumers or a group consumers
func closeConsumers(consumers []interface{}) error {
	var err error
	for _, consumer := range consumers {
		//	Is it a partition consumers?
		if partionConsumer, ok := consumer.(sarama.PartitionConsumer); ok {
			if errTemp := partionConsumer.Close(); errTemp != nil {
				logger.Debugw("partition!!!", log.Fields{"err": errTemp})
				if strings.Compare(errTemp.Error(), sarama.ErrUnknownTopicOrPartition.Error()) == 0 {
					// This can occur on race condition
					err = nil
				} else {
					err = errTemp
				}
			}
		} else if groupConsumer, ok := consumer.(*scc.Consumer); ok {
			if errTemp := groupConsumer.Close(); errTemp != nil {
				if strings.Compare(errTemp.Error(), sarama.ErrUnknownTopicOrPartition.Error()) == 0 {
					// This can occur on race condition
					err = nil
				} else {
					err = errTemp
				}
			}
		}
	}
	return err
}

func (sc *SaramaClient) removeChannelFromConsumerChannelMap(topic Topic, ch <-chan *ic.InterContainerMessage) error {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if consumerCh, exist := sc.topicToConsumerChannelMap[topic.Name]; exist {
		// Channel will be closed in the removeChannel method
		consumerCh.channels = removeChannel(consumerCh.channels, ch)
		// If there are no more channels then we can close the consumers itself
		if len(consumerCh.channels) == 0 {
			logger.Debugw("closing-consumers", log.Fields{"topic": topic})
			err := closeConsumers(consumerCh.consumers)
			//err := consumerCh.consumers.Close()
			delete(sc.topicToConsumerChannelMap, topic.Name)
			return err
		}
		return nil
	}
	logger.Warnw("topic-does-not-exist", log.Fields{"topic": topic.Name})
	return errors.New("topic-does-not-exist")
}

func (sc *SaramaClient) clearTopicFromConsumerChannelMap(topic Topic) error {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if consumerCh, exist := sc.topicToConsumerChannelMap[topic.Name]; exist {
		for _, ch := range consumerCh.channels {
			// Channel will be closed in the removeChannel method
			removeChannel(consumerCh.channels, ch)
		}
		err := closeConsumers(consumerCh.consumers)
		//if err == sarama.ErrUnknownTopicOrPartition {
		//	// Not an error
		//	err = nil
		//}
		//err := consumerCh.consumers.Close()
		delete(sc.topicToConsumerChannelMap, topic.Name)
		return err
	}
	logger.Debugw("topic-does-not-exist", log.Fields{"topic": topic.Name})
	return nil
}

//createPublisher creates the publisher which is used to send a message onto kafka
func (sc *SaramaClient) createPublisher() error {
	// This Creates the publisher
	config := sarama.NewConfig()
	config.Producer.Partitioner = sarama.NewRandomPartitioner
	config.Producer.Flush.Frequency = time.Duration(sc.producerFlushFrequency)
	config.Producer.Flush.Messages = sc.producerFlushMessages
	config.Producer.Flush.MaxMessages = sc.producerFlushMaxmessages
	config.Producer.Return.Errors = sc.producerReturnErrors
	config.Producer.Return.Successes = sc.producerReturnSuccess
	//config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.RequiredAcks = sarama.WaitForLocal

	brokers := []string{sc.KafkaAddress}

	if producer, err := sarama.NewAsyncProducer(brokers, config); err != nil {
		logger.Errorw("error-starting-publisher", log.Fields{"error": err})
		return err
	} else {
		sc.producer = producer
	}
	logger.Info("Kafka-publisher-created")
	return nil
}

func (sc *SaramaClient) createConsumer() error {
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	config.Consumer.Fetch.Min = 1
	config.Consumer.MaxWaitTime = time.Duration(sc.consumerMaxwait) * time.Millisecond
	config.Consumer.MaxProcessingTime = time.Duration(sc.maxProcessingTime) * time.Millisecond
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	config.Metadata.Retry.Max = sc.metadataMaxRetry
	brokers := []string{sc.KafkaAddress}

	if consumer, err := sarama.NewConsumer(brokers, config); err != nil {
		logger.Errorw("error-starting-consumers", log.Fields{"error": err})
		return err
	} else {
		sc.consumer = consumer
	}
	logger.Info("Kafka-consumers-created")
	return nil
}

// createGroupConsumer creates a consumers group
func (sc *SaramaClient) createGroupConsumer(topic *Topic, groupId string, initialOffset int64, retries int) (*scc.Consumer, error) {
	config := scc.NewConfig()
	config.ClientID = uuid.New().String()
	config.Group.Mode = scc.ConsumerModeMultiplex
	config.Consumer.Group.Heartbeat.Interval, _ = time.ParseDuration("1s")
	config.Consumer.Return.Errors = true
	//config.Group.Return.Notifications = false
	//config.Consumer.MaxWaitTime = time.Duration(DefaultConsumerMaxwait) * time.Millisecond
	//config.Consumer.MaxProcessingTime = time.Duration(DefaultMaxProcessingTime) * time.Millisecond
	config.Consumer.Offsets.Initial = initialOffset
	//config.Consumer.Offsets.Initial = sarama.OffsetOldest
	brokers := []string{sc.KafkaAddress}

	topics := []string{topic.Name}
	var consumer *scc.Consumer
	var err error

	if consumer, err = scc.NewConsumer(brokers, groupId, topics, config); err != nil {
		logger.Errorw("create-group-consumers-failure", log.Fields{"error": err, "topic": topic.Name, "groupId": groupId})
		return nil, err
	}
	logger.Debugw("create-group-consumers-success", log.Fields{"topic": topic.Name, "groupId": groupId})

	//sc.groupConsumers[topic.Name] = consumer
	sc.addToGroupConsumers(topic.Name, consumer)
	return consumer, nil
}

// dispatchToConsumers sends the intercontainermessage received on a given topic to all subscribers for that
// topic via the unique channel each subscriber received during subscription
func (sc *SaramaClient) dispatchToConsumers(consumerCh *consumerChannels, protoMessage *ic.InterContainerMessage) {
	// Need to go over all channels and publish messages to them - do we need to copy msg?
	sc.lockTopicToConsumerChannelMap.RLock()
	for _, ch := range consumerCh.channels {
		go func(c chan *ic.InterContainerMessage) {
			c <- protoMessage
		}(ch)
	}
	sc.lockTopicToConsumerChannelMap.RUnlock()

	if callback := sc.metadataCallback; callback != nil {
		ts, _ := ptypes.Timestamp(protoMessage.Header.Timestamp)
		callback(protoMessage.Header.FromTopic, ts)
	}
}

func (sc *SaramaClient) consumeFromAPartition(topic *Topic, consumer sarama.PartitionConsumer, consumerChnls *consumerChannels) {
	logger.Debugw("starting-partition-consumption-loop", log.Fields{"topic": topic.Name})
startloop:
	for {
		select {
		case err, ok := <-consumer.Errors():
			if ok {
				if sc.isLivenessError(err) {
					sc.updateLiveness(false)
					logger.Warnw("partition-consumers-error", log.Fields{"error": err})
				}
			} else {
				// Channel is closed
				break startloop
			}
		case msg, ok := <-consumer.Messages():
			//logger.Debugw("message-received", logger.Fields{"msg": msg, "receivedTopic": msg.Topic})
			if !ok {
				// channel is closed
				break startloop
			}
			msgBody := msg.Value
			sc.updateLiveness(true)
			logger.Debugw("message-received", log.Fields{"timestamp": msg.Timestamp, "receivedTopic": msg.Topic})
			icm := &ic.InterContainerMessage{}
			if err := proto.Unmarshal(msgBody, icm); err != nil {
				logger.Warnw("partition-invalid-message", log.Fields{"error": err})
				continue
			}
			go sc.dispatchToConsumers(consumerChnls, icm)
		case <-sc.doneCh:
			logger.Infow("partition-received-exit-signal", log.Fields{"topic": topic.Name})
			break startloop
		}
	}
	logger.Infow("partition-consumer-stopped", log.Fields{"topic": topic.Name})
	sc.setUnhealthy()
}

func (sc *SaramaClient) consumeGroupMessages(topic *Topic, consumer *scc.Consumer, consumerChnls *consumerChannels) {
	logger.Debugw("starting-group-consumption-loop", log.Fields{"topic": topic.Name})

startloop:
	for {
		select {
		case err, ok := <-consumer.Errors():
			if ok {
				if sc.isLivenessError(err) {
					sc.updateLiveness(false)
				}
				logger.Warnw("group-consumers-error", log.Fields{"topic": topic.Name, "error": err})
			} else {
				logger.Warnw("group-consumers-closed-err", log.Fields{"topic": topic.Name})
				// channel is closed
				break startloop
			}
		case msg, ok := <-consumer.Messages():
			if !ok {
				logger.Warnw("group-consumers-closed-msg", log.Fields{"topic": topic.Name})
				// Channel closed
				break startloop
			}
			sc.updateLiveness(true)
			logger.Debugw("message-received", log.Fields{"timestamp": msg.Timestamp, "receivedTopic": msg.Topic})
			msgBody := msg.Value
			icm := &ic.InterContainerMessage{}
			if err := proto.Unmarshal(msgBody, icm); err != nil {
				logger.Warnw("invalid-message", log.Fields{"error": err})
				continue
			}
			go sc.dispatchToConsumers(consumerChnls, icm)
			consumer.MarkOffset(msg, "")
		case ntf := <-consumer.Notifications():
			logger.Debugw("group-received-notification", log.Fields{"notification": ntf})
		case <-sc.doneCh:
			logger.Infow("group-received-exit-signal", log.Fields{"topic": topic.Name})
			break startloop
		}
	}
	logger.Infow("group-consumer-stopped", log.Fields{"topic": topic.Name})
	sc.setUnhealthy()
}

func (sc *SaramaClient) startConsumers(topic *Topic) error {
	logger.Debugw("starting-consumers", log.Fields{"topic": topic.Name})
	var consumerCh *consumerChannels
	if consumerCh = sc.getConsumerChannel(topic); consumerCh == nil {
		logger.Errorw("consumers-not-exist", log.Fields{"topic": topic.Name})
		return errors.New("consumers-not-exist")
	}
	// For each consumer listening for that topic, start a consumption loop
	for _, consumer := range consumerCh.consumers {
		if pConsumer, ok := consumer.(sarama.PartitionConsumer); ok {
			go sc.consumeFromAPartition(topic, pConsumer, consumerCh)
		} else if gConsumer, ok := consumer.(*scc.Consumer); ok {
			go sc.consumeGroupMessages(topic, gConsumer, consumerCh)
		} else {
			logger.Errorw("invalid-consumer", log.Fields{"topic": topic})
			return errors.New("invalid-consumer")
		}
	}
	return nil
}

//// setupConsumerChannel creates a consumerChannels object for that topic and add it to the consumerChannels map
//// for that topic.  It also starts the routine that listens for messages on that topic.
func (sc *SaramaClient) setupPartitionConsumerChannel(topic *Topic, initialOffset int64) (chan *ic.InterContainerMessage, error) {
	var pConsumers []sarama.PartitionConsumer
	var err error

	if pConsumers, err = sc.createPartitionConsumers(topic, initialOffset); err != nil {
		logger.Errorw("creating-partition-consumers-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	consumersIf := make([]interface{}, 0)
	for _, pConsumer := range pConsumers {
		consumersIf = append(consumersIf, pConsumer)
	}

	// Create the consumers/channel structure and set the consumers and create a channel on that topic - for now
	// unbuffered to verify race conditions.
	consumerListeningChannel := make(chan *ic.InterContainerMessage)
	cc := &consumerChannels{
		consumers: consumersIf,
		channels:  []chan *ic.InterContainerMessage{consumerListeningChannel},
	}

	// Add the consumers channel to the map
	sc.addTopicToConsumerChannelMap(topic.Name, cc)

	//Start a consumers to listen on that specific topic
	go func() {
		if err := sc.startConsumers(topic); err != nil {
			logger.Errorw("start-consumers-failed", log.Fields{
				"topic": topic,
				"error": err})
		}
	}()

	return consumerListeningChannel, nil
}

// setupConsumerChannel creates a consumerChannels object for that topic and add it to the consumerChannels map
// for that topic.  It also starts the routine that listens for messages on that topic.
func (sc *SaramaClient) setupGroupConsumerChannel(topic *Topic, groupId string, initialOffset int64) (chan *ic.InterContainerMessage, error) {
	// TODO:  Replace this development partition consumers with a group consumers
	var pConsumer *scc.Consumer
	var err error
	if pConsumer, err = sc.createGroupConsumer(topic, groupId, initialOffset, DefaultMaxRetries); err != nil {
		logger.Errorw("creating-partition-consumers-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}
	// Create the consumers/channel structure and set the consumers and create a channel on that topic - for now
	// unbuffered to verify race conditions.
	consumerListeningChannel := make(chan *ic.InterContainerMessage)
	cc := &consumerChannels{
		consumers: []interface{}{pConsumer},
		channels:  []chan *ic.InterContainerMessage{consumerListeningChannel},
	}

	// Add the consumers channel to the map
	sc.addTopicToConsumerChannelMap(topic.Name, cc)

	//Start a consumers to listen on that specific topic
	go func() {
		if err := sc.startConsumers(topic); err != nil {
			logger.Errorw("start-consumers-failed", log.Fields{
				"topic": topic,
				"error": err})
		}
	}()

	return consumerListeningChannel, nil
}

func (sc *SaramaClient) createPartitionConsumers(topic *Topic, initialOffset int64) ([]sarama.PartitionConsumer, error) {
	logger.Debugw("creating-partition-consumers", log.Fields{"topic": topic.Name})
	partitionList, err := sc.consumer.Partitions(topic.Name)
	if err != nil {
		logger.Warnw("get-partition-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	pConsumers := make([]sarama.PartitionConsumer, 0)
	for _, partition := range partitionList {
		var pConsumer sarama.PartitionConsumer
		if pConsumer, err = sc.consumer.ConsumePartition(topic.Name, partition, initialOffset); err != nil {
			logger.Warnw("consumers-partition-failure", log.Fields{"error": err, "topic": topic.Name})
			return nil, err
		}
		pConsumers = append(pConsumers, pConsumer)
	}
	return pConsumers, nil
}

func removeChannel(channels []chan *ic.InterContainerMessage, ch <-chan *ic.InterContainerMessage) []chan *ic.InterContainerMessage {
	var i int
	var channel chan *ic.InterContainerMessage
	for i, channel = range channels {
		if channel == ch {
			channels[len(channels)-1], channels[i] = channels[i], channels[len(channels)-1]
			close(channel)
			logger.Debug("channel-closed")
			return channels[:len(channels)-1]
		}
	}
	return channels
}

func (sc *SaramaClient) addToGroupConsumers(topic string, consumer *scc.Consumer) {
	sc.lockOfGroupConsumers.Lock()
	defer sc.lockOfGroupConsumers.Unlock()
	if _, exist := sc.groupConsumers[topic]; !exist {
		sc.groupConsumers[topic] = consumer
	}
}

func (sc *SaramaClient) deleteFromGroupConsumers(topic string) error {
	sc.lockOfGroupConsumers.Lock()
	defer sc.lockOfGroupConsumers.Unlock()
	if _, exist := sc.groupConsumers[topic]; exist {
		consumer := sc.groupConsumers[topic]
		delete(sc.groupConsumers, topic)
		if err := consumer.Close(); err != nil {
			logger.Errorw("failure-closing-consumer", log.Fields{"error": err})
			return err
		}
	}
	return nil
}
