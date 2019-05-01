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

package ofagent

import (
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-protos/go/voltha"
	"google.golang.org/grpc"
	"strconv"
	"strings"
)

type ConnectionManager struct {
	grpc_client *GrpcClient // single, shared gRPC client to vcore
	client_conn *grpc.ClientConn

	agent_map                    map[string]Agent
	device_id_to_datapath_id_map map[string]int

	controller_endpoints []string
	consul_endpoint      string
	vcore_endpoint       string
	instance_id          string
	key_file             string
	cert_file            string
	core_binding_key     string

	vcore_retry_interval          float64
	devices_refresh_interval      int
	subscription_refresh_interval int
	subscription                  int
	channel                       int
	grpc_timeout                  int

	running    bool
	enable_tls bool
	is_alive   bool
}

func NewConnectionManager(consul_endpoint string, vcore_endpoint string, vcore_grpc_timeout int, vcore_binding_key string, controller_endpoints []string, instance_id string, enable_tls bool, key_file string, cert_file string, vcore_retry_interval float64, devices_refresh_interval int, subscription_refresh_interval int) *ConnectionManager {
	var connMgr ConnectionManager

	connMgr.controller_endpoints = controller_endpoints
	connMgr.consul_endpoint = consul_endpoint
	connMgr.vcore_endpoint = vcore_endpoint
	connMgr.grpc_timeout = vcore_grpc_timeout
	connMgr.core_binding_key = vcore_binding_key
	connMgr.instance_id = instance_id
	connMgr.enable_tls = enable_tls
	connMgr.key_file = key_file
	connMgr.cert_file = cert_file

	connMgr.channel = None
	connMgr.grpc_client = nil // single, shared gRPC client to vcore

	// these are maps
	connMgr.agent_map = make(map[string]Agent) // (datapath_id, controller_endpoint) -> Agent()
	connMgr.device_id_to_datapath_id_map = make(map[string]int)

	connMgr.vcore_retry_interval = vcore_retry_interval
	connMgr.devices_refresh_interval = devices_refresh_interval
	connMgr.subscription_refresh_interval = subscription_refresh_interval
	connMgr.subscription = None

	connMgr.running = false
	fmt.Println("NewConnectionManager")
	//log.Debugln("NewConnectionManager", connMgr)
	return &connMgr
}

func (e ConnectionManager) Start() ConnectionManager {
	//log.Debugln("start()", e)
	if e.running {
		return e
	}
	e.running = true
	// Get a subscription to vcore
	//reactor.callInThread(self.get_vcore_subscription)
	// Start monitoring logical devices and manage agents accordingly
	//reactor.callLater(0, self.monitor_logical_devices)
	//log.Debugln("started", e)
	fmt.Println("ConnectionManager::Started()")
	return e
}

func (e ConnectionManager) Stop() {
	// clean up all controller connections
	log.Debugln("stop()", e)
	for name, agent := range e.agent_map {
		fmt.Println("name:", name, "stopping")
		agent.stop()
	}
	e.running = false
	e._reset_grpc_attributes()
	log.Debugln("stopped", e)
}

func (e ConnectionManager) resolve_endpoint(endpoint string) (string, int) {
	var ip_port_endpoint = endpoint
	startsWith := strings.HasPrefix(endpoint, "@")
	if true == startsWith {
	}

	/*
	   if endpoint.startswith('@'):
	       try:
	           ip_port_endpoint = get_endpoint_from_consul(
	               e.consul_endpoint, endpoint[1:])
	           log.info(
	               '{}-service-endpoint-found'.format(endpoint), address=ip_port_endpoint)
	       except Exception as e:
	           log.error('{}-service-endpoint-not-found'.format(endpoint), exception=repr(e))
	           log.error('committing-suicide')
	           # Committing suicide in order to let docker restart ofagent
	           os.system("kill -15 {}".format(os.getpid()))
	*/
	if len(ip_port_endpoint) > 0 {
		s := strings.Split(ip_port_endpoint, ":")
		port, err := strconv.Atoi(s[1])
		if err == nil {
			return s[0], port
		}
	}
	return "", 0
}

func (e ConnectionManager) _reset_grpc_attributes() {
	log.Debugln("_reset_grpc_attributes()", e)

	if e.grpc_client != nil {
		e.grpc_client.stop()
	}

	//if e.channel is not None:
	//   del e.channel

	e.is_alive = false
	e.channel = None
	e.subscription = None
	e.grpc_client = nil
	log.Debugln("end _reset_grpc_attributes()", e)
}

func (e ConnectionManager) _assign_grpc_attributes() {
	log.Debugln("_assign_grpc_attributes()", e)

	host, port := e.resolve_endpoint(e.vcore_endpoint)
	log.Debugln("revolved-vcore-endpoint", e.vcore_endpoint, host, port)

	//assert host is not None
	//assert port is not None
	// Establish a connection to the vcore GRPC server

	// grpc client instead of channel?
	//e.channel = grpc.insecure_channel('{}:{}'.format(host, port))

	var theHost string
	var err error
	theHost = fmt.Sprintf("%s:%d", host, port)

	e.client_conn, err = grpc.Dial(theHost, grpc.WithInsecure())
	if err != nil {
		log.Debugln("grpc Dial error: ", err)
	}

	e.is_alive = true
	log.Debugln("end _assign_grpc_attributes()", e)
}

func (e ConnectionManager) get_vcore_subscription() {
	log.Debugln("get_vcore_subscription()", e)

	for true == e.running && None == e.subscription {
		// If a subscription is not yet assigned then establish new GRPC connection
		// ... otherwise keep using existing connection details

		//if self.subscription is None: // duplicate code from loop which already checks
		e._assign_grpc_attributes()

		// Send subscription request to register the current ofagent instance
		// global? and what is it for?
		// container_name = e.instance_id
		if nil == e.grpc_client {
			e.grpc_client = NewGrpcClient(e, e.channel, e.grpc_timeout, e.core_binding_key)

			// need to find the link to OfAgenSubscriber
			//subscription = yield e.grpc_client.subscribe(OfAgentSubscriber(ofagent_id=container_name))

		}
	}
}

func (e ConnectionManager) refresh_agent_connections(devices []*voltha.Device) {
	log.Debugln("refresh_agent_connections()", e)

	//device *voltha.Device

	// Use datapath ids for deciding what's new and what's obsolete
	//desired_datapath_ids = set(d.datapath_id for d in devices)
	//current_datapath_ids = set(datapath_ids[0] for datapath_ids in self.agent_map.iterkeys())

	// if identical, nothing to do
	//if (desired_datapath_ids == current_datapath_ids) {
	//   return
	// }

	// ... otherwise calculate differences

	//to_add = desired_datapath_ids.difference(current_datapath_ids)
	//to_del = current_datapath_ids.difference(desired_datapath_ids)

	// remove what we don't need
	//for datapath_id in to_del:
	//    e.delete_agent(datapath_id)

	// start new agents as needed

	for _, device := range devices {
		//if (device.datapath_id
		e.create_agent(device)
	}
	/*
	   for device in devices:
	       if device.datapath_id in to_add:
	          self.create_agent(device)
	*/
	log.Debugln("updated-agent-list ", len(e.agent_map))
	log.Debugln("updated-device-id-to-datapath-id-map ", len(e.device_id_to_datapath_id_map))
}

//@inlineCallbacks
func (e ConnectionManager) get_list_of_logical_devices_from_voltha() {
	log.Debugln("get_list_of_logical_devices_from_voltha()", e)

	for true == e.running {
		//devices = yield self.grpc_client.list_logical_devices()
		//for device in devices {
		//    log.info("logical-device-entry", id=device.id, datapath_id=device.datapath_id)
	}
	//returnValue(devices)
	/*
	   except _Rendezvous, e:
	       status = e.code()
	       log.error('vcore-communication-failure', exception=e, status=status)
	       if status == StatusCode.UNAVAILABLE or status == StatusCode.DEADLINE_EXCEEDED:
	           os.system("kill -15 {}".format(os.getpid()))
	   except Exception as e:
	       log.exception('logical-devices-retrieval-failure', exception=e)
	   log.info('reconnect', after_delay=self.vcore_retry_interval)
	   yield asleep(self.vcore_retry_interval)
	*/
}

/* python
   @inlineCallbacks
   def get_vcore_subscription(self):
       log.debug('start-get-vcore-subscription')

       while self.running and self.subscription is None:
           try:
               # If a subscription is not yet assigned then establish new GRPC connection
               # ... otherwise keep using existing connection details
               if self.subscription is None:
                   self._assign_grpc_attributes()

               # Send subscription request to register the current ofagent instance
               container_name = self.instance_id
               if self.grpc_client is None:
                   self.grpc_client = GrpcClient(self, self.channel, self.grpc_timeout,
                                                 self.core_binding_key)
               subscription = yield self.grpc_client.subscribe(
                   OfAgentSubscriber(ofagent_id=container_name))

               # If the subscriber id matches the current instance
               # ... then the subscription has succeeded
               if subscription is not None and subscription.ofagent_id == container_name:
                   if self.subscription is None:
                       # Keep details on the current GRPC session and subscription
                       log.debug('subscription-with-vcore-successful', subscription=subscription)
                       self.subscription = subscription
                       self.grpc_client.start()

                   # Sleep a bit in between each subscribe
                   yield asleep(self.subscription_refresh_interval)

                   # Move on to next subscribe request
                   continue

               # The subscription did not succeed, reset and move on
               else:
                   log.info('subscription-with-vcore-unavailable', subscription=subscription)

           except _Rendezvous, e:
               log.error('subscription-with-vcore-terminated',exception=e, status=e.code())

           except Exception as e:
               log.exception('unexpected-subscription-termination-with-vcore', e=e)

          # Reset grpc details
           # The vcore instance is either not available for subscription
           # or a failure occurred with the existing communication.
           self._reset_grpc_attributes()

           # Sleep for a short period and retry
           yield asleep(self.vcore_retry_interval)

       log.debug('stop-get-vcore-subscription')

   @inlineCallbacks
   def get_list_of_logical_devices_from_voltha(self):

       while self.running:
           log.info('retrieve-logical-device-list')
           try:
               devices = yield \
                   self.grpc_client.list_logical_devices()

               for device in devices:
                   log.info("logical-device-entry", id=device.id,
                            datapath_id=device.datapath_id)

               returnValue(devices)

           except _Rendezvous, e:
               status = e.code()
               log.error('vcore-communication-failure', exception=e, status=status)
               if status == StatusCode.UNAVAILABLE or status == StatusCode.DEADLINE_EXCEEDED:
                   os.system("kill -15 {}".format(os.getpid()))

           except Exception as e:
               log.exception('logical-devices-retrieval-failure', exception=e)

           log.info('reconnect', after_delay=self.vcore_retry_interval)
           yield asleep(self.vcore_retry_interval)


   def refresh_agent_connections(self, devices):
       """
       Based on the new device list, update the following state in the class:
       * agent_map
       * datapath_map
       * device_id_map
       :param devices: full device list freshly received from Voltha
       :return: None
       """

       # Use datapath ids for deciding what's new and what's obsolete
       desired_datapath_ids = set(d.datapath_id for d in devices)
       current_datapath_ids = set(datapath_ids[0] for datapath_ids in self.agent_map.iterkeys())

       # if identical, nothing to do
       if desired_datapath_ids == current_datapath_ids:
           return

       # ... otherwise calculate differences
       to_add = desired_datapath_ids.difference(current_datapath_ids)
       to_del = current_datapath_ids.difference(desired_datapath_ids)

       # remove what we don't need
       for datapath_id in to_del:
           self.delete_agent(datapath_id)

       # start new agents as needed
       for device in devices:
           if device.datapath_id in to_add:
               self.create_agent(device)

       log.debug('updated-agent-list', count=len(self.agent_map))
       log.debug('updated-device-id-to-datapath-id-map',
                 map=str(self.device_id_to_datapath_id_map))

   @inlineCallbacks
   def monitor_logical_devices(self):
       log.debug('start-monitor-logical-devices')

       while self.running:
           log.info('monitoring-logical-devices')

           # should change to a gRPC streaming call
           # see https://jira.opencord.org/browse/CORD-821

           try:
               if self.channel is not None and self.grpc_client is not None and \
                               self.subscription is not None:
                   # get current list from Voltha
                   devices = yield \
                       self.get_list_of_logical_devices_from_voltha()

                   # update agent list and mapping tables as needed
                   self.refresh_agent_connections(devices)
               else:
                   log.info('vcore-communication-unavailable')

               # wait before next poll
               yield asleep(self.devices_refresh_interval)

           except _Rendezvous, e:
               log.error('vcore-communication-failure', exception=repr(e), status=e.code())

           except Exception as e:
               log.exception('unexpected-vcore-communication-failure', exception=repr(e))

       log.debug('stop-monitor-logical-devices')

*/

func (e ConnectionManager) monitor_logical_devices() {
	//log.debug('start-monitor-logical-devices')

	for e.running == true {
		// log.info('monitoring-logical-devices')

		// should change to a gRPC streaming call
		// see https://jira.opencord.org/browse/CORD-821

		if e.channel != None && e.grpc_client != nil && e.subscription != None {
			// get current list from Voltha
			//                    devices = e.get_list_of_logical_devices_from_voltha()
			//yield \ TODO

			// update agent list and mapping tables as needed
			//                   e.refresh_agent_connections(devices)
		} else {
			//log.info('vcore-communication-unavailable')
		}
		// wait before next poll
		// yield asleep(self.devices_refresh_interval)
	}

}

func (e ConnectionManager) create_agent(device *voltha.Device) {
	//TODO find datapath_id - can't find in device for voltha-go
	datapath_id := 0 //device.datapath_id
	device_id := device.Id
	for _, controller_endpoint := range e.controller_endpoints {
		agent := NewAgent(controller_endpoint, datapath_id,
			device_id, e.grpc_client, e.enable_tls,
			e.key_file, e.cert_file, 5)
		agent.start()
		var keyString = fmt.Sprintf("%d:%s", datapath_id, controller_endpoint)
		e.agent_map[keyString] = *agent
		e.device_id_to_datapath_id_map[device_id] = datapath_id
	}
}

func (e ConnectionManager) delete_agent(datapath_id int) {

	for index := range e.controller_endpoints {
		var agent Agent
		var keyString = fmt.Sprintf("%d:%s", datapath_id, e.controller_endpoints[index])
		agent = e.agent_map[keyString]
		device_id := agent.get_device_id()
		agent.stop()
		var combined string
		combined = fmt.Sprintf("%d%s", datapath_id, e.controller_endpoints[index])
		delete(e.agent_map, combined)
		delete(e.device_id_to_datapath_id_map, device_id)
	}
}

func (e ConnectionManager) forward_packet_in(device_id string, ofp_packet_in int) {
	var datapath_id int
	datapath_id = e.device_id_to_datapath_id_map[device_id]
	if datapath_id > 0 {
		for _, controller_endpoint := range e.controller_endpoints {
			var combined string
			combined = fmt.Sprintf("%d%s", datapath_id, controller_endpoint)
			agent := e.agent_map[combined]
			agent.forward_packet_in(ofp_packet_in)
		}
	}
}

// event is most likely a message - not string
func (e ConnectionManager) forward_change_event(device_id int, event string) {
	var datapath_id int
	//datapath_id = e.device_id_to_datapath_id_map.get(device_id, None)
	if datapath_id > 0 {
		for _, controller_endpoint := range e.controller_endpoints {
			// element is the element from someSlice for where we are
			var combined string
			combined = fmt.Sprintf("%d%s", datapath_id, controller_endpoint)
			agent := e.agent_map[combined]
			if &agent != nil {
				agent.forward_change_event(event)
			}
		}
		//for controller_endpoint in e.controller_endpoints:
		//agent = e.agent_map[(datapath_id, controller_endpoint)]
		//agent.forward_change_event(event)
	}
}
