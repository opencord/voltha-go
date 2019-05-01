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
        "github.com/opencord/voltha-go/common/log"
        "github.com/opencord/voltha-go/common/util"
	"bytes"
        //"strings"
        //"strconv"
        //"fmt"
        //"os"
        //"crypto/tls"
)

type OpenFlowConnection struct {
	//protocol		protocol.Protocol
        agent 			Agent
	next_xid		int
	read_buffer		[]byte
	rx			util.MessageQueue
				// the protocol will call agent.enter_disconnected()
                            	// and agent.enter_connected() methods to indicate
                            	// when state change is necessary
}

func NewOpenFlowConnection(agent Agent) *OpenFlowConnection {
    var oFlowConnect OpenFlowConnection
    oFlowConnect.agent = agent
    log.Debugln("NewOpenFlowConnection", oFlowConnect)
    return &oFlowConnect
}

func (e OpenFlowConnection) connectionLost(reason string) {
    log.Debugln("connectionLost", e)
    e.agent.enter_disconnected("connection-lost", reason)
}

func (e OpenFlowConnection) connectionMade() {
    log.Debugln("connectionMade", e)
    e.agent.enter_connected()
}

func (e OpenFlowConnection) dataReceived(data []byte) {
    log.Debugln("dataReceived", e)
    //var lenTypes = len(data)  
    var buf []byte = e.read_buffer

    if (len(buf) > 0) {
	//bytes.Join(buf, data)

	var b *bytes.Buffer = bytes.NewBuffer(buf) 
	b.Write(data)

    } else {
	buf = data
    }
    var offset = 0
    for(offset < len(buf)) {
       if (offset + 8 > len(buf)) {  // not enough data for the OpenFlow header
	   break
       }
/*
            # parse the header to get type
            _version, _type, _len, _xid = \
                loxi.of14.message.parse_header(buf[offset:])

            ofp = loxi.protocol(_version)

            if (offset + _len) > len(buf):
                break  # not enough data to cover whole message

            rawmsg = buf[offset : offset + _len]
            offset += _len

            msg = ofp.message.parse_message(rawmsg)
            if not msg:
                log.warn('could-not-parse',
                         data=hexdump(rawmsg, result='return'))
            log.debug('received-msg', module=type(msg).__module__,
                  name=type(msg).__name__, xid=msg.xid, len=len(buf))
            self.rx.put(msg)
*/
    }
}

func (e OpenFlowConnection) send_raw(buf []byte) {
    log.Debugln("send_raw ", len(buf), e)
    // assert e.connected
    //e.transport.write(buf)
}

func (e OpenFlowConnection) send(msg []byte) {
    log.Debugln("send ", e)
    //assert self.connected
    //if msg.xid is None:
    //   msg.xid = self._gen_xid()
    //buf = msg.pack()
    //log.debug('sending', module=type(msg).__module__,
    //              name=type(msg).__name__, xid=msg.xid, len=len(buf))
    //e.transport.write(buf)
    //log.debug('data-sent', sent=hexdump(buf, result='return'))
}

func (e OpenFlowConnection) Recv(predicate int64) []interface {} {
    log.Debugln("recv", e)
    //assert e.connected
    return e.rx.Get(predicate)
}

func (e OpenFlowConnection) recv_any() []interface{} {
    log.Debugln("recv_any", e)
    // python was using lambda _: True 
    // TODO - was blocking here
    var popRow int64
    popRow = 0
    return e.Recv(popRow)
}

func (e OpenFlowConnection) recv_xid(xid int) []interface{} {
    log.Debugln("recv_xid", e)
    // python was using - lambda msg: msg.xid == xid)
   // var blocking bool
   // if (msg.xid == xid) {
   //    blocking = true 
   // }
    var popRow int64
    popRow = 0
    return e.Recv(popRow)
}
