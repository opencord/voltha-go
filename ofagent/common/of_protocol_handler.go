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
	"github.com/opencord/voltha-protos/go/common"
	//        "github.com/opencord/voltha-go/protos/voltha"
	"errors"
)

const (
	ofp_version int = 4 // OFAgent supported versions
)

type OpenFlowProtocolHandler struct {
	cxn         *OpenFlowConnection
	rpc         *GrpcClient
	agent       Agent
	device_id   string
	datapath_id uint64
	role        int
	ofp_version int
	support     bool
}

func NewOpenFlowProtocolHandler(datapathId uint64, deviceId string, agent Agent, cxn *OpenFlowConnection, rpc *GrpcClient) *OpenFlowProtocolHandler {
	var openFlowProtHandler OpenFlowProtocolHandler
	openFlowProtHandler.datapath_id = datapathId
	openFlowProtHandler.device_id = deviceId
	openFlowProtHandler.agent = agent
	openFlowProtHandler.cxn = cxn
	openFlowProtHandler.rpc = rpc
	//openFlowProtHandler.role = None
	log.Debugln("NewOpenFlowProtocolHandler", openFlowProtHandler)
	return &openFlowProtHandler
}

func (e OpenFlowProtocolHandler) Start() OpenFlowProtocolHandler {
	log.Debugln("Start()", e)

	var support bool
	support = false
	/*
	   e.cxn.send(ofp.message.hello(elements=[ofp.common.hello_elem_versionbitmap( bitmaps = [ofp.common.hello_elem_bitmap(e.ofp_version)])]))
	   msg = yield e.cxn.recv_class(ofp.message.hello)
	   // expect to receive a hello message
	   // supports only ofp_versions till 31 and single bitmap.
	*/
	//if (msg) {
	//support = ofp.util.verify_version_support(msg,self.ofp_version)
	//if (false == support) {
	//e.cxn.send(ofp.message.hello_failed_error_msg(xid=msg.xid, code=ofp.OFPHFC_INCOMPATIBLE, data='i support only 1.3'))
	//  log.Debugln("peer-do-not-support-OpenFlow-version",e.ofp_version)
	//   }
	for true == support {
		//req = yield e.cxn.recv_any()
		//req := e.cxn.recv_any()
		//handler = e.main_handlers.get(req.type, None)
		//if (handler != nil) {
		//   handler(e, req)
		//} else {
		//   log.Debugln("cannot-handle",req)
		//}
	}
	// }

	log.Debugln("Started", e)
	return e
}

func (e OpenFlowProtocolHandler) Stop() {
	log.Debugln("Stop()", e)
	// pass  - nothing to do yet
	log.Debugln("Stopped", e)
}

func (e OpenFlowProtocolHandler) handle_echo_request(req int) {
	log.Debugln("handle_echo_request()", e)
	//e.cxn.send(ofp.message.echo_reply(xid=req.xid))
}

func (e OpenFlowProtocolHandler) handle_feature_request(req int) {
	log.Debugln("handle_feature_request()", e)

	//        device_info = yield e.rpc.get_device_info(self.device_id)
	//        kw = pb2dict(device_info.switch_features)
	//          e.cxn.send(ofp.message.features_reply(
	//            xid=req.xid,
	//            datapath_id=self.datapath_id,
	//            **kw))
}

func (e OpenFlowProtocolHandler) handle_stats_request(req int) {

	log.Debugln("handle_stats_request()", e)
	//  handler = e.stats_handlers.get(req.stats_type, None)
	// if (handler != nil) {
	//    handler(e, req)
	//  } else {
	//     raise OpenFlowProtocolError(
	//        'Cannot handle stats request type "{}"'.format(req.stats_type))
	// }
}

func (e OpenFlowProtocolHandler) handle_barrier_request(req int) {
	log.Debugln("handle_barrier_request()", e)
	// not really doing barrier yet, but we respond
	// see https://jira.opencord.org/browse/CORD-823
	// e.cxn.send(ofp.message.barrier_reply(xid=req.xid))
}

func (e OpenFlowProtocolHandler) handle_experimenter_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_flow_mod_request(req int) {
	log.Debugln("handle_flow_mod_request()", e)

	//if (e.role == ofp.OFPCR_ROLE_MASTER || e.role == ofp.OFPCR_ROLE_EQUAL) {
	// grpc_req = to_grpc(req)
	//}
	// except Exception, e:
	//    log.exception('failed-to-convert', e=e)
	// else:
	//    return e.rpc.update_flow_table(e.device_id, grpc_req)

	//elif e.role == ofp.OFPCR_ROLE_SLAVE:
	//  e.cxn.send(ofp.message.bad_request_error_msg(code=ofp.OFPBRC_IS_SLAVE))
}

func (e OpenFlowProtocolHandler) handle_get_async_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_get_config_request(req int) {
	log.Debugln("handle_get_config_request()", e)
	//e.cxn.send(ofp.message.get_config_reply(xid=req.xid, miss_send_len=ofp.OFPCML_NO_BUFFER))
}

func (e OpenFlowProtocolHandler) handle_group_mod_request(req int) {
	log.Debugln("handle_group_mod_request()", e)
	//if e.role == ofp.OFPCR_ROLE_MASTER or e.role == ofp.OFPCR_ROLE_EQUAL:
	//   yield e.rpc.update_group_table(e.device_id, to_grpc(req))
	//elif e.role == ofp.OFPCR_ROLE_SLAVE:
	//   e.cxn.send(ofp.message.bad_request_error_msg(code=ofp.OFPBRC_IS_SLAVE))
}

func (e OpenFlowProtocolHandler) handle_meter_mod_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_role_request(req int) {
	log.Debugln("handle_role_request()", e)

	/*
	   if req.role == ofp.OFPCR_ROLE_MASTER or req.role == ofp.OFPCR_ROLE_SLAVE:
	       if e.agent.generation_is_defined and (
	               ((req.generation_id - e.agent.cached_generation_id) & 0xffffffffffffffff) if abs(
	               req.generation_id - e.agent.cached_generation_id) > 0x7fffffffffffffff else (
	               req.generation_id - e.agent.cached_generation_id)) < 0:
	           e.cxn.send(ofp.message.bad_request_error_msg(code=ofp.OFPRRFC_STALE))
	       else:
	           e.agent.generation_is_defined = True
	           e.agent.cached_generation_id = req.generation_id
	           e.role = req.role
	           e.cxn.send(ofp.message.role_reply(
	            xid=req.xid, role=req.role, generation_id=req.generation_id))
	   elif req.role == ofp.OFPCR_ROLE_EQUAL:
	       e.role = req.role
	       e.cxn.send(ofp.message.role_reply(
	        xid=req.xid, role=req.role))
	*/
}

func (e OpenFlowProtocolHandler) handle_packet_out_request(req int) {
	log.Debugln("handle_packet_out_request()", e)
	/*
	   if e.role == ofp.OFPCR_ROLE_MASTER or e.role == ofp.OFPCR_ROLE_EQUAL:
	      e.rpc.send_packet_out(e.device_id, to_grpc(req))

	   elif e.role == ofp.OFPCR_ROLE_SLAVE:
	      e.cxn.send(ofp.message.bad_request_error_msg(code=ofp.OFPBRC_IS_SLAVE))
	*/
}

func (e OpenFlowProtocolHandler) handle_set_config_request(req int) {
	log.Debugln("handle_set_config_request()", e)
	// Handle set config appropriately
	// https://jira.opencord.org/browse/CORD-826
	//pass
}

func (e OpenFlowProtocolHandler) handle_port_mod_request(req int) {
	log.Debugln("handle_set_config_request()", e)
	/*
	   if e.role == ofp.OFPCR_ROLE_MASTER or e.role == ofp.OFPCR_ROLE_EQUAL:
	       port = yield e.rpc.get_port(e.device_id, str(req.port_no))

	       if port.ofp_port.config & ofp.OFPPC_PORT_DOWN != \
	               req.config & ofp.OFPPC_PORT_DOWN:
	           if req.config & ofp.OFPPC_PORT_DOWN:
	               e.rpc.disable_port(e.device_id, port.id)
	           else:
	               e.rpc.enable_port(e.device_id, port.id)

	   elif e.role == ofp.OFPCR_ROLE_SLAVE:
	       e.cxn.send(ofp.message.bad_request_error_msg(code=ofp.OFPBRC_IS_SLAVE))
	*/
}

func (e OpenFlowProtocolHandler) handle_table_mod_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_queue_get_config_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_set_async_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_aggregate_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

//@inlineCallbacks
func (e OpenFlowProtocolHandler) handle_device_description_request(req int) {
	log.Debugln("handle_device_description_request()", e)

	//device_info = yield e.rpc.get_device_info(e.device_id)
	//kw = pb2dict(device_info.desc)
	//e.cxn.send(ofp.message.desc_stats_reply(xid=req.xid, **kw))
}

func (e OpenFlowProtocolHandler) handle_experimenter_stats_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

//@inlineCallbacks
func (e OpenFlowProtocolHandler) handle_flow_stats_request(req int) {
	log.Debugln("handle_flow_stats_request()", e)

	//flow_stats = yield e.rpc.list_flows(e.device_id)
	//e.cxn.send(ofp.message.flow_stats_reply(xid=req.xid, entries=[to_loxi(f) for f in flow_stats]))
}

//@inlineCallbacks
func (e OpenFlowProtocolHandler) handle_group_stats_request(req int) {
	log.Debugln("handle_group_stats_request()", e)

	//    group_stats = yield e.rpc.list_groups(e.device_id)
	//    e.cxn.send(ofp.message.group_stats_reply(
	//        xid=req.xid, entries=[to_loxi(g.stats) for g  in group_stats]))
}

//@inlineCallbacks
func (e OpenFlowProtocolHandler) handle_group_descriptor_request(req int) {
	log.Debugln("handle_group_descriptor_request()", e)
	//group_stats = yield e.rpc.list_groups(e.device_id)
	//e.cxn.send(ofp.message.group_desc_stats_reply(
	//    xid=req.xid, entries=[to_loxi(g.desc) for g  in group_stats]))
}

func (e OpenFlowProtocolHandler) handle_group_features_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_meter_stats_request(req int) {
	log.Debugln("handle_meter_stats_request()", e)
	//meter_stats = []  // see https://jira.opencord.org/browse/CORD-825
	//e.cxn.send(ofp.message.meter_stats_reply(
	//    xid=req.xid, entries=meter_stats))
}

func (e OpenFlowProtocolHandler) handle_meter_config_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_meter_features_request(req int) {
	log.Debugln("handle_meter_features_request()", e)

	//e.cxn.send(ofp.message.bad_request_error_msg())
}

//@inlineCallbacks
func (e OpenFlowProtocolHandler) handle_port_stats_request(req int) {
	log.Debugln("handle_port_stats_request()", e)

	//ports = yield e.rpc.list_ports(e.device_id)
	//port_stats = [to_loxi(p.ofp_port_stats) for p in ports]
	//of_message = ofp.message.port_stats_reply(xid=req.xid,entries=port_stats)
	//e.cxn.send(of_message)
}

//@inlineCallbacks
func (e OpenFlowProtocolHandler) handle_port_desc_request(req int) {
	log.Debugln("handle_port_desc_request()", e)
	//port_list = yield e.rpc.get_port_list(e.device_id)
	//e.cxn.send(ofp.message.port_desc_stats_reply( xid=req.xid, // flags=None,
	//            entries=[to_loxi(port.ofp_port) for port in port_list]))
}

func (e OpenFlowProtocolHandler) handle_queue_stats_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

func (e OpenFlowProtocolHandler) handle_table_stats_request(req int) {
	log.Debugln("handle_table_stats_request()", e)

	//table_stats = []  //# see https://jira.opencord.org/browse/CORD-825
	//e.cxn.send(ofp.message.table_stats_reply(xid=req.xid, entries=table_stats))
}

func (e OpenFlowProtocolHandler) handle_table_features_request(req int) (*common.OperationResp, error) {
	return nil, errors.New("UnImplemented")
}

/* python

import loxi.of13 as ofp
from converter import to_loxi, pb2dict, to_grpc


   stats_handlers = {
        ofp.OFPST_AGGREGATE: handle_aggregate_request,
        ofp.OFPST_DESC: handle_device_description_request,
        ofp.OFPST_EXPERIMENTER: handle_experimenter_stats_request,
        ofp.OFPST_FLOW: handle_flow_stats_request,
        ofp.OFPST_GROUP: handle_group_stats_request,
        ofp.OFPST_GROUP_DESC: handle_group_descriptor_request,
        ofp.OFPST_GROUP_FEATURES: handle_group_features_request,
        ofp.OFPST_METER: handle_meter_stats_request,
        ofp.OFPST_METER_CONFIG: handle_meter_config_request,
        ofp.OFPST_METER_FEATURES: handle_meter_features_request,
        ofp.OFPST_PORT: handle_port_stats_request,
        ofp.OFPST_PORT_DESC: handle_port_desc_request,
        ofp.OFPST_QUEUE: handle_queue_stats_request,
        ofp.OFPST_TABLE: handle_table_stats_request,
        ofp.OFPST_TABLE_FEATURES: handle_table_features_request
    }

    main_handlers = {
        ofp.OFPT_BARRIER_REQUEST: handle_barrier_request,
        ofp.OFPT_ECHO_REQUEST: handle_echo_request,
        ofp.OFPT_FEATURES_REQUEST: handle_feature_request,
        ofp.OFPT_EXPERIMENTER: handle_experimenter_request,
        ofp.OFPT_FLOW_MOD: handle_flow_mod_request,
        ofp.OFPT_GET_ASYNC_REQUEST: handle_get_async_request,
        ofp.OFPT_GET_CONFIG_REQUEST: handle_get_config_request,
        ofp.OFPT_GROUP_MOD: handle_group_mod_request,
        ofp.OFPT_METER_MOD: handle_meter_mod_request,
        ofp.OFPT_PACKET_OUT: handle_packet_out_request,
        ofp.OFPT_PORT_MOD: handle_port_mod_request,
        ofp.OFPT_QUEUE_GET_CONFIG_REQUEST: handle_queue_get_config_request,
        ofp.OFPT_ROLE_REQUEST: handle_role_request,
        ofp.OFPT_SET_ASYNC: handle_set_async_request,
        ofp.OFPT_SET_CONFIG: handle_set_config_request,
        ofp.OFPT_STATS_REQUEST: handle_stats_request,
        ofp.OFPT_TABLE_MOD: handle_table_mod_request,
    }

*/

func (e OpenFlowProtocolHandler) forward_packet_in(ofp_packet_in int) {
	log.Debugln("forward_packet_in()", e)
	//if e.role == ofp.OFPCR_ROLE_MASTER or e.role == ofp.OFPCR_ROLE_EQUAL:
	//     log.info('sending-packet-in', ofp_packet_in=ofp_packet_in)
	//    e.cxn.send(to_loxi(ofp_packet_in))
}

func (e OpenFlowProtocolHandler) forward_port_status(ofp_port_status int) {
	log.Debugln("forward_port_status()", e)
	//e.cxn.send(to_loxi(ofp_port_status))
}
