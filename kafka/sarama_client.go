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
	"errors"
	"fmt"
	"github.com/Shopify/sarama"
	scc "github.com/bsm/sarama-cluster"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	ca "github.com/opencord/voltha-go/protos/core_adapter"
	"sync"
	"time"
)

// consumerChannels represents a consumer listening on a kafka topic.  Once it receives a message on that
// topic it broadcasts the message to all the listening channels
type consumerChannels struct {
	consumer sarama.PartitionConsumer
	//consumer *sc.Consumer
	channels []chan *ca.InterContainerMessage
}

// SaramaClient represents the messaging proxy
type SaramaClient struct {
	broker                        *sarama.Broker
	client                        sarama.Client
	KafkaHost                     string
	KafkaPort                     int
	producer                      sarama.AsyncProducer
	consumer                      sarama.Consumer
	groupConsumer                 *scc.Consumer
	doneCh                        chan int
	topicToConsumerChannelMap     map[string]*consumerChannels
	lockTopicToConsumerChannelMap sync.RWMutex
}

type SaramaClientOption func(*SaramaClient)

func Host(host string) SaramaClientOption {
	return func(args *SaramaClient) {
		args.KafkaHost = host
	}
}

func Port(port int) SaramaClientOption {
	return func(args *SaramaClient) {
		args.KafkaPort = port
	}
}

func NewSaramaClient(opts ...SaramaClientOption) *SaramaClient {
	client := &SaramaClient{
		KafkaHost: DefaultKafkaHost,
		KafkaPort: DefaultKafkaPort,
	}

	for _, option := range opts {
		option(client)
	}

	client.lockTopicToConsumerChannelMap = sync.RWMutex{}

	return client
}

func (sc *SaramaClient) Start(retries int) error {
	log.Info("Starting-Proxy")

	// Create the Done channel
	sc.doneCh = make(chan int, 1)

	// Create the Publisher
	if err := sc.createPublisher(retries); err != nil {
		log.Errorw("Cannot-create-kafka-publisher", log.Fields{"error": err})
		return err
	}

	// Create the master consumer
	if err := sc.createConsumer(retries); err != nil {
		log.Errorw("Cannot-create-kafka-consumer", log.Fields{"error": err})
		return err
	}

	// Create the topic to consumer/channel map
	sc.topicToConsumerChannelMap = make(map[string]*consumerChannels)

	return nil
}

func (sc *SaramaClient) Stop() {
	log.Info("stopping-sarama-client")

	//Send a message over the done channel to close all long running routines
	sc.doneCh <- 1

	// Clear the consumer map
	//sc.clearConsumerChannelMap()

	if sc.producer != nil {
		if err := sc.producer.Close(); err != nil {
			panic(err)
		}
	}
	if sc.consumer != nil {
		if err := sc.consumer.Close(); err != nil {
			panic(err)
		}
	}
}

func (sc *SaramaClient) addTopicToConsumerChannelMap(id string, arg *consumerChannels) {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if _, exist := sc.topicToConsumerChannelMap[id]; !exist {
		sc.topicToConsumerChannelMap[id] = arg
	}
}

func (sc *SaramaClient) deleteFromTopicToConsumerChannelMap(id string) {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if _, exist := sc.topicToConsumerChannelMap[id]; exist {
		delete(sc.topicToConsumerChannelMap, id)
	}
}

func (sc *SaramaClient) getConsumerChannel(topic Topic) *consumerChannels {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()

	if consumerCh, exist := sc.topicToConsumerChannelMap[topic.Name]; exist {
		return consumerCh
	}
	return nil
}

func (sc *SaramaClient) addChannelToConsumerChannelMap(topic Topic, ch chan *ca.InterContainerMessage) {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if consumerCh, exist := sc.topicToConsumerChannelMap[topic.Name]; exist {
		consumerCh.channels = append(consumerCh.channels, ch)
		return
	}
	log.Warnw("consumer-channel-not-exist", log.Fields{"topic": topic.Name})
}

func (sc *SaramaClient) removeChannelFromConsumerChannelMap(topic Topic, ch <-chan *ca.InterContainerMessage) error {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	if consumerCh, exist := sc.topicToConsumerChannelMap[topic.Name]; exist {
		// Channel will be closed in the removeChannel method
		consumerCh.channels = removeChannel(consumerCh.channels, ch)
		// If there are no more channels then we can close the consumer itself
		if len(consumerCh.channels) == 0 {
			log.Debugw("closing-consumer", log.Fields{"topic": topic})
			err := consumerCh.consumer.Close()
			delete(sc.topicToConsumerChannelMap, topic.Name)
			return err
		}
		return nil
	}
	log.Warnw("topic-does-not-exist", log.Fields{"topic": topic.Name})
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
		err := consumerCh.consumer.Close()
		delete(sc.topicToConsumerChannelMap, topic.Name)
		return err
	}
	log.Warnw("topic-does-not-exist", log.Fields{"topic": topic.Name})
	return errors.New("topic-does-not-exist")
}

func (sc *SaramaClient) clearConsumerChannelMap() error {
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	var err error
	for topic, consumerCh := range sc.topicToConsumerChannelMap {
		for _, ch := range consumerCh.channels {
			// Channel will be closed in the removeChannel method
			removeChannel(consumerCh.channels, ch)
		}
		err = consumerCh.consumer.Close()
		delete(sc.topicToConsumerChannelMap, topic)
	}
	return err
}

func (sc *SaramaClient) CreateTopic(topic *Topic, numPartition int, repFactor int, retries int) error {
	// This Creates the kafka topic
	// Set broker configuration
	kafkaFullAddr := fmt.Sprintf("%s:%d", sc.KafkaHost, sc.KafkaPort)
	broker := sarama.NewBroker(kafkaFullAddr)

	// Additional configurations. Check sarama doc for more info
	config := sarama.NewConfig()
	config.Version = sarama.V1_0_0_0

	// Open broker connection with configs defined above
	broker.Open(config)

	// check if the connection was OK
	_, err := broker.Connected()
	if err != nil {
		return err
	}

	topicDetail := &sarama.TopicDetail{}
	topicDetail.NumPartitions = int32(numPartition)
	topicDetail.ReplicationFactor = int16(repFactor)
	topicDetail.ConfigEntries = make(map[string]*string)

	topicDetails := make(map[string]*sarama.TopicDetail)
	topicDetails[topic.Name] = topicDetail

	request := sarama.CreateTopicsRequest{
		Timeout:      time.Second * 1,
		TopicDetails: topicDetails,
	}

	for {
		// Send request to Broker
		if response, err := broker.CreateTopics(&request); err != nil {
			if retries == 0 {
				log.Errorw("error-creating-topic", log.Fields{"error": err})
				return err
			} else {
				// If retries is -ve then we will retry indefinitely
				retries--
			}
			log.Debug("retrying-after-a-second-delay")
			time.Sleep(time.Duration(DefaultSleepOnError) * time.Second)
		} else {
			log.Debug("topic-response", log.Fields{"response": response})
			break
		}
	}

	log.Debug("topic-created", log.Fields{"topic": topic, "numPartition": numPartition, "replicationFactor": repFactor})
	return nil
}

func (sc *SaramaClient) createPublisher(retries int) error {
	// This Creates the publisher
	config := sarama.NewConfig()
	config.Producer.Partitioner = sarama.NewRandomPartitioner
	config.Producer.Flush.Frequency = time.Duration(DefaultFlushFrequency)
	config.Producer.Flush.Messages = DefaultFlushMessages
	config.Producer.Flush.MaxMessages = DefaultFlushMaxmessages
	config.Producer.Return.Errors = DefaultReturnErrors
	config.Producer.Return.Successes = DefaultReturnSuccess
	config.Producer.RequiredAcks = sarama.WaitForAll
	kafkaFullAddr := fmt.Sprintf("%s:%d", sc.KafkaHost, sc.KafkaPort)
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
			sc.producer = producer
			break
		}
	}
	log.Info("Kafka-publisher-created")
	return nil
}

func (sc *SaramaClient) createConsumer(retries int) error {
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true
	config.Consumer.Fetch.Min = 1
	config.Consumer.MaxWaitTime = time.Duration(DefaultConsumerMaxwait) * time.Millisecond
	config.Consumer.MaxProcessingTime = time.Duration(DefaultMaxProcessingTime) * time.Millisecond
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	kafkaFullAddr := fmt.Sprintf("%s:%d", sc.KafkaHost, sc.KafkaPort)
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
			sc.consumer = consumer
			break
		}
	}
	log.Info("Kafka-consumer-created")
	return nil
}

// createGroupConsumer creates a consumer group
func (sc *SaramaClient) createGroupConsumer(topic *Topic, groupId *string, retries int) (*scc.Consumer, error) {
	config := scc.NewConfig()
	config.Group.Mode = scc.ConsumerModeMultiplex
	config.Consumer.Return.Errors = true
	config.Group.Return.Notifications = true
	config.Consumer.MaxWaitTime = time.Duration(DefaultConsumerMaxwait) * time.Millisecond
	config.Consumer.MaxProcessingTime = time.Duration(DefaultMaxProcessingTime) * time.Millisecond
	config.Consumer.Offsets.Initial = sarama.OffsetNewest
	kafkaFullAddr := fmt.Sprintf("%s:%d", sc.KafkaHost, sc.KafkaPort)
	brokers := []string{kafkaFullAddr}

	if groupId == nil {
		g := DefaultGroupName
		groupId = &g
	}
	topics := []string{topic.Name}
	var consumer *scc.Consumer
	var err error

	// Create the topic with default attributes
	// TODO: needs to be revisited
	//sc.CreateTopic(&Topic{Name:topic.Name}, 3, 1, 1)

	if consumer, err = scc.NewConsumer(brokers, *groupId, topics, config); err != nil {
		log.Errorw("create-consumer-failure", log.Fields{"error": err, "topic": topic.Name, "groupId": groupId})
		return nil, err
	}
	log.Debugw("create-consumer-success", log.Fields{"topic": topic.Name, "groupId": groupId})
	//time.Sleep(10*time.Second)
	sc.groupConsumer = consumer
	return consumer, nil
}

// send formats and sends the request onto the kafka messaging bus.
func (sc *SaramaClient) Send(msg interface{}, topic *Topic, keys ...string) {

	// Assert message is a proto message
	var protoMsg proto.Message
	var ok bool
	// ascertain the value interface type is a proto.Message
	if protoMsg, ok = msg.(proto.Message); !ok {
		log.Warnw("message-not-proto-message", log.Fields{"msg": msg})
		return
	}

	//	Create the Sarama producer message
	marshalled, _ := proto.Marshal(protoMsg)
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
}

// Subscribe registers a caller to a topic.   It returns a channel that the caller can use to receive
// messages from that topic
func (sc *SaramaClient) Subscribe(topic *Topic, retries int) (<-chan *ca.InterContainerMessage, error) {
	log.Debugw("subscribe", log.Fields{"topic": topic.Name})

	// If a consumer already exist for that topic then resuse it
	if consumerCh := sc.getConsumerChannel(*topic); consumerCh != nil {
		log.Debugw("topic-already-subscribed", log.Fields{"topic": topic.Name})
		// Create a channel specific for that consumer and add it to the consumer channel map
		ch := make(chan *ca.InterContainerMessage)
		sc.addChannelToConsumerChannelMap(*topic, ch)
		return ch, nil
	}

	// Register for the topic and set it up
	var consumerListeningChannel chan *ca.InterContainerMessage
	var err error
	if consumerListeningChannel, err = sc.setupConsumerChannel(topic); err != nil {
		log.Warnw("create-consumer-channel-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	return consumerListeningChannel, nil
}

// dispatchToConsumers sends the intercontainermessage received on a given topic to all subscribers for that
// topic via the unique channel each subsciber received during subscription
func (sc *SaramaClient) dispatchToConsumers(consumerCh *consumerChannels, protoMessage *ca.InterContainerMessage) {
	// Need to go over all channels and publish messages to them - do we need to copy msg?
	sc.lockTopicToConsumerChannelMap.Lock()
	defer sc.lockTopicToConsumerChannelMap.Unlock()
	for _, ch := range consumerCh.channels {
		go func(c chan *ca.InterContainerMessage) {
			c <- protoMessage
		}(ch)
	}
}

func (sc *SaramaClient) consumeMessagesLoop(topic Topic) {
	log.Debugw("starting-consuming-messages", log.Fields{"topic": topic.Name})
	var consumerCh *consumerChannels
	if consumerCh = sc.getConsumerChannel(topic); consumerCh == nil {
		log.Errorw("consumer-not-exist", log.Fields{"topic": topic.Name})
		return
	}
startloop:
	for {
		select {
		case err := <-consumerCh.consumer.Errors():
			log.Warnw("consumer-error", log.Fields{"error": err})
		case msg := <-consumerCh.consumer.Messages():
			//log.Debugw("message-received", log.Fields{"msg": msg, "receivedTopic": msg.Topic})
			// Since the only expected message is a proto intercontainermessage then extract it right away
			// instead of dispatching it to the consumers
			msgBody := msg.Value
			icm := &ca.InterContainerMessage{}
			if err := proto.Unmarshal(msgBody, icm); err != nil {
				log.Warnw("invalid-message", log.Fields{"error": err})
				continue
			}
			go sc.dispatchToConsumers(consumerCh, icm)

			//consumerCh.consumer.MarkOffset(msg, "")
			//// TODO:  Dispatch requests and responses separately
		case <-sc.doneCh:
			log.Infow("received-exit-signal", log.Fields{"topic": topic.Name})
			break startloop
		}
	}
	log.Infow("received-exit-signal-out-of-for-loop", log.Fields{"topic": topic.Name})
}

// setupConsumerChannel creates a consumerChannels object for that topic and add it to the consumerChannels map
// for that topic.  It also starts the routine that listens for messages on that topic.
func (sc *SaramaClient) setupConsumerChannel(topic *Topic) (chan *ca.InterContainerMessage, error) {
	// TODO:  Replace this development partition consumer with a group consumer
	var pConsumer *sarama.PartitionConsumer
	var err error
	if pConsumer, err = sc.CreatePartionConsumer(topic, DefaultMaxRetries); err != nil {
		log.Errorw("creating-partition-consumer-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	// Create the consumer/channel structure and set the consumer and create a channel on that topic - for now
	// unbuffered to verify race conditions.
	consumerListeningChannel := make(chan *ca.InterContainerMessage)
	cc := &consumerChannels{
		consumer: *pConsumer,
		channels: []chan *ca.InterContainerMessage{consumerListeningChannel},
	}

	// Add the consumer channel to the map
	sc.addTopicToConsumerChannelMap(topic.Name, cc)

	//Start a consumer to listen on that specific topic
	go sc.consumeMessagesLoop(*topic)

	return consumerListeningChannel, nil
}

func (sc *SaramaClient) CreatePartionConsumer(topic *Topic, retries int) (*sarama.PartitionConsumer, error) {
	log.Debugw("creating-partition-consumer", log.Fields{"topic": topic.Name})
	partitionList, err := sc.consumer.Partitions(topic.Name)
	if err != nil {
		log.Warnw("get-partition-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}

	log.Debugw("partitions", log.Fields{"topic": topic.Name, "partitionList": partitionList, "first": partitionList[0]})
	// Create a partition consumer for that topic - for now just use one partition
	var pConsumer sarama.PartitionConsumer
	if pConsumer, err = sc.consumer.ConsumePartition(topic.Name, partitionList[0], sarama.OffsetNewest); err != nil {
		log.Warnw("consumer-partition-failure", log.Fields{"error": err, "topic": topic.Name})
		return nil, err
	}
	log.Debugw("partition-consumer-created", log.Fields{"topic": topic.Name})
	return &pConsumer, nil
}

func (sc *SaramaClient) UnSubscribe(topic *Topic, ch <-chan *ca.InterContainerMessage) error {
	log.Debugw("unsubscribing-channel-from-topic", log.Fields{"topic": topic.Name})
	err := sc.removeChannelFromConsumerChannelMap(*topic, ch)
	return err
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

func (sc *SaramaClient) DeleteTopic(topic *Topic) error {
	if err := sc.clearTopicFromConsumerChannelMap(*topic); err != nil {
		log.Errorw("failure-clearing-channels", log.Fields{"topic": topic, "error": err})
		return err
	}
	return nil
}
