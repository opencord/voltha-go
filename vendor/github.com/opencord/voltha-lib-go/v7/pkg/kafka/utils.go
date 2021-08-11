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
	"strings"
	"time"

	"github.com/golang/protobuf/ptypes/any"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	"github.com/opencord/voltha-lib-go/v7/pkg/probe"
)

const (
	TopicSeparator = "_"
	DeviceIdLength = 24
)

// A Topic definition - may be augmented with additional attributes eventually
type Topic struct {
	// The name of the topic. It must start with a letter,
	// and contain only letters (`[A-Za-z]`), numbers (`[0-9]`), dashes (`-`),
	// underscores (`_`), periods (`.`), tildes (`~`), plus (`+`) or percent
	// signs (`%`).
	Name string
}

type KVArg struct {
	Key   string
	Value interface{}
}

type RpcMType int

const (
	RpcFormattingError RpcMType = iota
	RpcSent
	RpcReply
	RpcTimeout
	RpcTransportError
	RpcSystemClosing
)

type RpcResponse struct {
	MType RpcMType
	Err   error
	Reply *any.Any
}

func NewResponse(messageType RpcMType, err error, body *any.Any) *RpcResponse {
	return &RpcResponse{
		MType: messageType,
		Err:   err,
		Reply: body,
	}
}

// TODO:  Remove and provide better may to get the device id
// GetDeviceIdFromTopic extract the deviceId from the topic name.  The topic name is formatted either as:
//			<any string> or <any string>_<deviceId>.  The device Id is 24 characters long.
func GetDeviceIdFromTopic(topic Topic) string {
	pos := strings.LastIndex(topic.Name, TopicSeparator)
	if pos == -1 {
		return ""
	}
	adjustedPos := pos + len(TopicSeparator)
	if adjustedPos >= len(topic.Name) {
		return ""
	}
	deviceId := topic.Name[adjustedPos:len(topic.Name)]
	if len(deviceId) != DeviceIdLength {
		return ""
	}
	return deviceId
}

// WaitUntilKafkaConnectionIsUp waits until the kafka client can establish a connection to the kafka broker or until the
// context times out.
func StartAndWaitUntilKafkaConnectionIsUp(ctx context.Context, kClient Client, connectionRetryInterval time.Duration, serviceName string) error {
	if kClient == nil {
		return errors.New("kafka-client-is-nil")
	}
	for {
		if err := kClient.Start(ctx); err != nil {
			probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusNotReady)
			logger.Warnw(ctx, "kafka-connection-down", log.Fields{"error": err})
			select {
			case <-time.After(connectionRetryInterval):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusRunning)
		logger.Info(ctx, "kafka-connection-up")
		break
	}
	return nil
}

/**
MonitorKafkaReadiness checks the liveliness and readiness of the kafka service
and update the status in the probe.
*/
func MonitorKafkaReadiness(ctx context.Context,
	kClient Client,
	liveProbeInterval, notLiveProbeInterval time.Duration,
	serviceName string) {

	if kClient == nil {
		logger.Fatal(ctx, "kafka-client-is-nil")
	}

	logger.Infow(ctx, "monitor-kafka-readiness", log.Fields{"service": serviceName})

	livelinessChannel := kClient.EnableLivenessChannel(ctx, true)
	healthinessChannel := kClient.EnableHealthinessChannel(ctx, true)
	timeout := liveProbeInterval
	failed := false
	for {
		timeoutTimer := time.NewTimer(timeout)

		select {
		case healthiness := <-healthinessChannel:
			if !healthiness {
				// This will eventually cause K8s to restart the container, and will do
				// so in a way that allows cleanup to continue, rather than an immediate
				// panic and exit here.
				probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusFailed)
				logger.Infow(ctx, "kafka-not-healthy", log.Fields{"service": serviceName})
				failed = true
			}
			// Check if the timer has expired or not
			if !timeoutTimer.Stop() {
				<-timeoutTimer.C
			}
		case liveliness := <-livelinessChannel:
			if failed {
				// Failures of the message bus are permanent and can't ever be recovered from,
				// so make sure we never inadvertently reset a failed state back to unready.
			} else if !liveliness {
				// kafka not reachable or down, updating the status to not ready state
				probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusNotReady)
				logger.Infow(ctx, "kafka-not-live", log.Fields{"service": serviceName})
				timeout = notLiveProbeInterval
			} else {
				// kafka is reachable , updating the status to running state
				probe.UpdateStatusFromContext(ctx, serviceName, probe.ServiceStatusRunning)
				timeout = liveProbeInterval
			}
			// Check if the timer has expired or not
			if !timeoutTimer.Stop() {
				<-timeoutTimer.C
			}
		case <-timeoutTimer.C:
			logger.Infow(ctx, "kafka-proxy-liveness-recheck", log.Fields{"service": serviceName})
			// send the liveness probe in a goroutine; we don't want to deadlock ourselves as
			// the liveness probe may wait (and block) writing to our channel.
			go func() {
				err := kClient.SendLiveness(ctx)
				if err != nil {
					// Catch possible error case if sending liveness after Sarama has been stopped.
					logger.Warnw(ctx, "error-kafka-send-liveness", log.Fields{"error": err, "service": serviceName})
				}
			}()
		case <-ctx.Done():
			return // just exit
		}
	}
}
