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

package core

import (
	"context"
	"time"

	"github.com/opencord/voltha-go/rw_core/core/adapter"
	"github.com/opencord/voltha-go/rw_core/core/api"
	"github.com/opencord/voltha-go/rw_core/core/device"
	"github.com/opencord/voltha-lib-go/v4/pkg/kafka"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	"github.com/opencord/voltha-lib-go/v4/pkg/probe"
)

// startKafkInterContainerProxy is responsible for starting the Kafka Interadapter Proxy
func startKafkInterContainerProxy(ctx context.Context, kafkaClient kafka.Client, address string, coreTopic string, connectionRetryInterval time.Duration) (kafka.InterContainerProxy, error) {
	logger.Infow(ctx, "initialize-kafka-manager", log.Fields{"address": address, "topic": coreTopic})

	probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusPreparing)

	// create the kafka RPC proxy
	kmp := kafka.NewInterContainerProxy(
		kafka.InterContainerAddress(address),
		kafka.MsgClient(kafkaClient),
		kafka.DefaultTopic(&kafka.Topic{Name: coreTopic}))

	probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusPrepared)

	// wait for connectivity
	logger.Infow(ctx, "starting-kafka-manager", log.Fields{"address": address, "topic": coreTopic})

	for {
		// If we haven't started yet, then try to start
		logger.Infow(ctx, "starting-kafka-proxy", log.Fields{})
		if err := kmp.Start(ctx); err != nil {
			// We failed to start. Delay and then try again later.
			// Don't worry about liveness, as we can't be live until we've started.
			probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusNotReady)
			logger.Infow(ctx, "error-starting-kafka-messaging-proxy", log.Fields{"error": err})
			select {
			case <-time.After(connectionRetryInterval):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		// We started. We only need to do this once.
		// Next we'll fall through and start checking liveness.
		logger.Infow(ctx, "started-kafka-proxy", log.Fields{})
		break
	}
	return kmp, nil
}

/*
 * monitorKafkaLiveness is responsible for monitoring the Kafka Interadapter Proxy connectivity state
 *
 * Any producer that fails to send will cause KafkaInterContainerProxy to
 * post a false event on its liveness channel. Any producer that succeeds in sending
 * will cause KafkaInterContainerProxy to post a true event on its liveness
 * channel. Group receivers also update liveness state, and a receiver will typically
 * indicate a loss of liveness within 3-5 seconds of Kafka going down. Receivers
 * only indicate restoration of liveness if a message is received. During normal
 * operation, messages will be routinely produced and received, automatically
 * indicating liveness state. These routine liveness indications are rate-limited
 * inside sarama_client.
 *
 * This thread monitors the status of KafkaInterContainerProxy's liveness and pushes
 * that state to the core's readiness probes. If no liveness event has been seen
 * within a timeout, then the thread will make an attempt to produce a "liveness"
 * message, which will in turn trigger a liveness event on the liveness channel, true
 * or false depending on whether the attempt succeeded.
 *
 * The gRPC server in turn monitors the state of the readiness probe and will
 * start issuing UNAVAILABLE response while the probe is not ready.
 *
 * startupRetryInterval -- interval between attempts to start
 * liveProbeInterval -- interval between liveness checks when in a live state
 * notLiveProbeInterval -- interval between liveness checks when in a notLive state
 *
 * liveProbeInterval and notLiveProbeInterval can be configured separately,
 * though the current default is that both are set to 60 seconds.
 */
func monitorKafkaLiveness(ctx context.Context, kmp kafka.InterContainerProxy, liveProbeInterval time.Duration, notLiveProbeInterval time.Duration) {
	logger.Info(ctx, "started-kafka-message-proxy")

	livenessChannel := kmp.EnableLivenessChannel(ctx, true)

	logger.Info(ctx, "enabled-kafka-liveness-channel")

	timeout := liveProbeInterval
	for {
		timeoutTimer := time.NewTimer(timeout)
		select {
		case liveness := <-livenessChannel:
			logger.Infow(ctx, "kafka-manager-thread-liveness-event", log.Fields{"liveness": liveness})
			// there was a state change in Kafka liveness
			if !liveness {
				probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusNotReady)
				logger.Info(ctx, "kafka-manager-thread-set-server-notready")

				// retry frequently while life is bad
				timeout = notLiveProbeInterval
			} else {
				probe.UpdateStatusFromContext(ctx, "message-bus", probe.ServiceStatusRunning)
				logger.Info(ctx, "kafka-manager-thread-set-server-ready")

				// retry infrequently while life is good
				timeout = liveProbeInterval
			}
			if !timeoutTimer.Stop() {
				<-timeoutTimer.C
			}
		case <-timeoutTimer.C:
			logger.Info(ctx, "kafka-proxy-liveness-recheck")
			// send the liveness probe in a goroutine; we don't want to deadlock ourselves as
			// the liveness probe may wait (and block) writing to our channel.
			go func() {
				err := kmp.SendLiveness(ctx)
				if err != nil {
					// Catch possible error case if sending liveness after Sarama has been stopped.
					logger.Warnw(ctx, "error-kafka-send-liveness", log.Fields{"error": err})
				}
			}()
		case <-ctx.Done():
			return // just exit
		}
	}
}

func registerAdapterRequestHandlers(ctx context.Context, kmp kafka.InterContainerProxy, dMgr *device.Manager, aMgr *adapter.Manager, coreTopic string) {
	requestProxy := api.NewAdapterRequestHandlerProxy(dMgr, aMgr)

	// Register the broadcast topic to handle any core-bound broadcast requests
	if err := kmp.SubscribeWithRequestHandlerInterface(ctx, kafka.Topic{Name: coreTopic}, requestProxy); err != nil {
		logger.Fatalw(ctx, "Failed-registering-broadcast-handler", log.Fields{"topic": coreTopic})
	}

	logger.Info(ctx, "request-handler-registered")
}
