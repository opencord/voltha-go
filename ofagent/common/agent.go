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
	"crypto/tls"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"os"
	"strconv"
	"strings"
)

type Agent struct {
	// clientFactory	protocol.ClientFactory
	proto_handler       *OpenFlowProtocolHandler
	rpc_stub            *GrpcClient
	controller_endpoint string
	key_file            string
	cert_file           string
	device_id           string
	datapath_id         uint64
	connector           int
	retry_interval      int
	d_disconnected      int
	connected           bool
	exiting             bool
	running             bool
	enable_tls          bool
}

var None = 0

func NewAgent(controllerEndpoint string, datapathId uint64, deviceId string, rpcStub *GrpcClient, enableTls bool, keyFile string, certFile string, connRetryInterval int) *Agent {
	var agent Agent
	agent.controller_endpoint = controllerEndpoint
	agent.datapath_id = datapathId
	agent.device_id = deviceId
	agent.rpc_stub = rpcStub
	agent.enable_tls = enableTls
	agent.key_file = keyFile
	agent.cert_file = certFile
	agent.retry_interval = connRetryInterval
	agent.running = false
	agent.connector = None      //# will be a Connector instance once connected
	agent.d_disconnected = None //# a deferred to signal reconnect loop when
	agent.connected = false
	agent.exiting = false
	agent.proto_handler = nil
	return &agent
}

func (e Agent) get_device_id() string {
	return e.device_id
}

func (e Agent) start() *Agent {
	log.Debugln("starting", e)
	if e.running {
		return nil
	}
	e.running = true
	go e.keep_connected()
	log.Debugln("started", e)
	return &e
}

func (e Agent) stop() {
	log.Debugln("stopping", e)
	e.connected = false
	e.exiting = true
	//e.connector.disconnect()
	log.Debugln("stopped", e)
}

func (e Agent) resolve_endpoint(endpoint string) (string, int) {
	// enable optional resolution via consul;
	// see https://jira.opencord.org/browse/CORD-820
	log.Debugln("resolve_endpoint", e)
	svals := strings.Split(endpoint, ":")
	var port int
	if port, err := strconv.Atoi(svals[1]); err == nil {
		fmt.Printf("%T, %v", port, port)
	}
	return svals[0], port
}

//@inlineCallbacks
func (e Agent) keep_connected() {
	//"""Keep reconnecting to the controller"""

	log.Debugln("keep_connected", e)
	for false == e.exiting {
		host, port := e.resolve_endpoint(e.controller_endpoint)
		if e.enable_tls {
			// get rid of the next 2 lines
			if len(host) > 0 && port > 0 {
			}
			// Check that key_file and cert_file is provided and
			// the files exist
			if len(e.key_file) == 0 || len(e.cert_file) == 0 {
				if _, err := os.Stat(e.key_file); os.IsNotExist(err) {
					log.Debugln("key_file not found", e)
					log.Fatal(err)
				}
				if _, err := os.Stat(e.cert_file); os.IsNotExist(err) {
					log.Debugln("cert_file not found", e)
					log.Fatal(err)
				}
			} else {
				cert, err := tls.LoadX509KeyPair(e.cert_file, e.key_file)
				if err != nil {
					log.Fatal(err)
				}
				config := &tls.Config{Certificates: []tls.Certificate{cert}}
				listener, err := tls.Listen("tcp", ":443", config)
				if err != nil {
					log.Debugln("Error with config or listening", e)
					log.Fatal(err)
					return
				}
				defer listener.Close()
			}
		}
	}
}

func (e Agent) enter_disconnected(event string, reason string) {
	log.Debugln("enter_disconnected", e)
	e.connected = false
	if e.exiting == false {
		log.Debugln("exiting = false", event, reason)
		//e.d_disconnected.callback(None)
	}
}

func (e Agent) enter_connected() {
	log.Debugln("enter_connected", e)
	e.connected = true
	//e.read_buffer = None
	go e.proto_handler.Start()
}

func (e Agent) forward_packet_in(ofp_packet_in int) {
	if &e.proto_handler != nil {
		e.proto_handler.forward_packet_in(ofp_packet_in)
	}
}

func (e Agent) forward_change_event(event string) {
	// assert isinstance(event, ChangeEvent)
	log.Debugln("got-change_event", e, event)
	//        if event.HasField("port_status"):
	//            if e.proto_handler is not None:
	//                e.proto_handler.forward_port_status(event.port_status)
	//        else:
	//            log.error('unknown-change-event', change_event=event)
}

func (e Agent) protocol() *OpenFlowConnection {
	cxn := NewOpenFlowConnection(e)
	e.proto_handler = NewOpenFlowProtocolHandler(e.datapath_id, e.device_id, e, cxn, e.rpc_stub)
	return cxn
}

func (e Agent) clientConnectionFailed(connector int, reason string) {
	log.Debugln("clientConnectionFailed", e)
	e.enter_disconnected("connection-failed", reason)
}

func (e Agent) clientConnectionLost(connector int, reason string) {
	if false == e.exiting {
		log.Debugln("clientConnectionLost ", reason, connector)
	}
}

/*
   def keep_connected(self):
       while not self.exiting:
           log.info('connecting', host=host, port=port)
           if self.enable_tls:
               try:
                   if self.key_file is None or             \
                      self.cert_file is None or            \
                      not os.path.isfile(self.key_file) or \
                      not os.path.isfile(self.cert_file):
                       raise Exception('key_file "{}" or cert_file "{}"'
                                       ' is not found'.
                                        format(self.key_file, self.cert_file))
                   with open(self.key_file) as keyFile:
                       with open(self.cert_file) as certFile:
                           clientCert = ssl.PrivateCertificate.loadPEM(
                               keyFile.read() + certFile.read())

                   ctx = clientCert.options()
                   self.connector = reactor.connectSSL(host, port, self, ctx)
                   log.info('tls-enabled')

               except Exception as e:
                   log.exception('failed-to-connect', reason=e)
           else:
               self.connector = reactor.connectTCP(host, port, self)
               log.info('tls-disabled')

           self.d_disconnected = Deferred()
           yield self.d_disconnected
           log.debug('reconnect', after_delay=self.retry_interval)
           yield asleep(self.retry_interval)
*/

/*  python

class Agent(protocol.ClientFactory):

    generation_is_defined = False
    cached_generation_id = None

    def __init__(self,
                 controller_endpoint,
                 datapath_id,
                 device_id,
                 rpc_stub,
                 enable_tls=False,
                 key_file=None,
                 cert_file=None,
                 conn_retry_interval=1):

        self.controller_endpoint = controller_endpoint
        self.datapath_id = datapath_id
        self.device_id = device_id
        self.rpc_stub = rpc_stub
        self.enable_tls = enable_tls
        self.key_file = key_file
        self.cert_file = cert_file
        self.retry_interval = conn_retry_interval

        self.running = False
        self.connector = None # will be a Connector instance once connected
        self.d_disconnected = None  # a deferred to signal reconnect loop when
                                    # TCP connection is lost
        self.connected = False
        self.exiting = False
        self.proto_handler = None

if __name__ == '__main__':
    """Run this to test the agent for N concurrent sessions:
       python agent [<number-of-desired-instances>]
    """

    n = 1 if len(sys.argv) < 2 else int(sys.argv[1])

    from utils import mac_str_to_tuple

    class MockRpc(object):
        @staticmethod
        def get_port_list(_):
            ports = []
            cap = of13.OFPPF_1GB_FD | of13.OFPPF_FIBER
            for pno, mac, nam, cur, adv, sup, spe in (
                    (1, '00:00:00:00:00:01', 'onu1', cap, cap, cap,
                     of13.OFPPF_1GB_FD),
                    (2, '00:00:00:00:00:02', 'onu2', cap, cap, cap,
                     of13.OFPPF_1GB_FD),
                    (129, '00:00:00:00:00:81', 'olt', cap, cap, cap,
                     of13.OFPPF_1GB_FD)
            ):
                port = of13.common.port_desc(pno, mac_str_to_tuple(mac), nam,
                                             curr=cur, advertised=adv,
                                             supported=sup,
                                             curr_speed=spe, max_speed=spe)
                ports.append(port)
            return ports

    stub = MockRpc()
    agents = [Agent('localhost:6653', 256 + i, stub).start() for i in range(n)]

    def shutdown():
        [a.stop() for a in agents]

    reactor.addSystemEventTrigger('before', 'shutdown', shutdown)
    reactor.run()

*/
