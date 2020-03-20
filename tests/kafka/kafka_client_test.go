// +build integration

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
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"
	"github.com/google/uuid"
	kk "github.com/opencord/voltha-lib-go/v3/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"github.com/stretchr/testify/assert"
)

/*
Prerequite:  Start the kafka/zookeeper containers.
*/

var partionClient kk.Client
var groupClient kk.Client
var totalTime int64
var numMessageToSend int
var totalMessageReceived int

type sendToKafka func(interface{}, *kk.Topic, ...string) error

func init() {
	hostIP := os.Getenv("DOCKER_HOST_IP")
	partionClient = kk.NewSaramaClient(
		kk.ConsumerType(kk.PartitionConsumer),
		kk.Host(hostIP),
		kk.Port(9092),
		kk.AutoCreateTopic(true),
		kk.ProducerFlushFrequency(5))
	partionClient.Start()
	groupClient = kk.NewSaramaClient(
		kk.ConsumerType(kk.GroupCustomer),
		kk.Host(hostIP),
		kk.Port(9092),
		kk.AutoCreateTopic(false),
		kk.ProducerFlushFrequency(5))
	groupClient.Start()
	numMessageToSend = 1
}

func waitForMessage(ch <-chan *ic.InterContainerMessage, doneCh chan string, maxMessages int) {
	totalTime = 0
	totalMessageReceived = 0
	mytime := time.Now()
startloop:
	for {
		select {
		case msg := <-ch:
			if totalMessageReceived == 0 {
				mytime = time.Now()
			}
			totalTime = totalTime + (time.Now().UnixNano()-msg.Header.Timestamp)/int64(time.Millisecond)
			//logger.Debugw("msg-received", log.Fields{"msg":msg})
			totalMessageReceived = totalMessageReceived + 1
			if totalMessageReceived == maxMessages {
				doneCh <- "All received"
				break startloop
			}
			if totalMessageReceived%10000 == 0 {
				fmt.Println("received-so-far", totalMessageReceived, totalTime, totalTime/int64(totalMessageReceived))
			}
		}
	}
	logger.Infow("Received all messages", log.Fields{"total": time.Since(mytime)})
}

func sendMessages(topic *kk.Topic, numMessages int, fn sendToKafka) error {
	// Loop for numMessages
	for i := 0; i < numMessages; i++ {
		msg := &ic.InterContainerMessage{}
		msg.Header = &ic.Header{
			Id:        uuid.New().String(),
			Type:      ic.MessageType_REQUEST,
			FromTopic: topic.Name,
			ToTopic:   topic.Name,
			Timestamp: time.Now().UnixNano(),
		}
		var marshalledArg *any.Any
		var err error
		body := &ic.InterContainerRequestBody{Rpc: "testRPC", Args: []*ic.Argument{}}
		if marshalledArg, err = ptypes.MarshalAny(body); err != nil {
			logger.Warnw("cannot-marshal-request", log.Fields{"error": err})
			return err
		}
		msg.Body = marshalledArg
		msg.Header.Timestamp = time.Now().UnixNano()
		go fn(msg, topic)
		//go partionClient.Send(msg, topic)
	}
	return nil
}

func runWithPartionConsumer(topic *kk.Topic, numMessages int, doneCh chan string) error {
	var ch <-chan *ic.InterContainerMessage
	var err error
	if ch, err = partionClient.Subscribe(topic); err != nil {
		return nil
	}
	go waitForMessage(ch, doneCh, numMessages)

	//Now create a routine to send messages
	go sendMessages(topic, numMessages, partionClient.Send)

	return nil
}

func runWithGroupConsumer(topic *kk.Topic, numMessages int, doneCh chan string) error {
	var ch <-chan *ic.InterContainerMessage
	var err error
	if ch, err = groupClient.Subscribe(topic); err != nil {
		return nil
	}
	go waitForMessage(ch, doneCh, numMessages)

	//Now create a routine to send messages
	go sendMessages(topic, numMessages, groupClient.Send)

	return nil
}

func TestPartitionConsumer(t *testing.T) {
	done := make(chan string)
	topic := &kk.Topic{Name: "CoreTest1"}
	runWithPartionConsumer(topic, numMessageToSend, done)
	start := time.Now()
	// Wait for done
	val := <-done
	err := partionClient.DeleteTopic(topic)
	assert.Nil(t, err)
	partionClient.Stop()
	assert.Equal(t, numMessageToSend, totalMessageReceived)
	logger.Infow("Partition consumer completed", log.Fields{"TotalMesages": totalMessageReceived, "TotalTime": totalTime, "val": val, "AverageTime": totalTime / int64(totalMessageReceived), "execTime": time.Since(start)})
}

func TestGroupConsumer(t *testing.T) {
	done := make(chan string)
	topic := &kk.Topic{Name: "CoreTest2"}
	runWithGroupConsumer(topic, numMessageToSend, done)
	start := time.Now()
	// Wait for done
	val := <-done
	err := groupClient.DeleteTopic(topic)
	assert.Nil(t, err)
	groupClient.Stop()
	assert.Equal(t, numMessageToSend, totalMessageReceived)
	logger.Infow("Group consumer completed", log.Fields{"TotalMesages": totalMessageReceived, "TotalTime": totalTime, "val": val, "AverageTime": totalTime / int64(totalMessageReceived), "execTime": time.Since(start)})

}

func TestCreateDeleteTopic(t *testing.T) {
	hostIP := os.Getenv("DOCKER_HOST_IP")
	client := kk.NewSaramaClient(
		kk.ConsumerType(kk.PartitionConsumer),
		kk.Host(hostIP),
		kk.Port(9092),
		kk.AutoCreateTopic(true),
		kk.ProducerFlushFrequency(5))
	client.Start()
	topic := &kk.Topic{Name: "CoreTest20"}
	err := client.CreateTopic(topic, 3, 1)
	assert.Nil(t, err)
	err = client.DeleteTopic(topic)
	assert.Nil(t, err)
}
