#
# Copyright 2018 the original author or authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#

"""
Openolt adapter.
"""
import arrow
import grpc
import structlog
from google.protobuf.empty_pb2 import Empty
from google.protobuf.json_format import MessageToDict
from scapy.layers.inet import Raw
import json
from google.protobuf.message import Message
from grpc._channel import _Rendezvous
from scapy.layers.l2 import Ether, Dot1Q
from simplejson import dumps
from twisted.internet import reactor
from twisted.internet.defer import inlineCallbacks, returnValue
from twisted.internet.task import LoopingCall

from python.adapters.common.frameio.frameio import BpfProgramFilter, hexify
from python.common.utils.asleep import asleep
from python.common.utils.registry import registry
from python.adapters.iadapter import OltAdapter
from python.adapters.kafka.kafka_proxy import get_kafka_proxy
from python.protos import openolt_pb2
from python.protos import third_party
from python.protos.common_pb2 import OperStatus, ConnectStatus
from python.protos.common_pb2 import LogLevel
from python.protos.common_pb2 import OperationResp
from python.protos.inter_container_pb2 import SwitchCapability, PortCapability, \
    InterAdapterMessageType, InterAdapterResponseBody
from python.protos.device_pb2 import Port, PmConfig, PmConfigs. \
    DeviceType, DeviceTypes
from python.protos.adapter_pb2 import Adapter
from python.protos.adapter_pb2 import AdapterConfig

 
from python.protos.events_pb2 import KpiEvent, KpiEventType, MetricValuePairs
from python.protos.logical_device_pb2 import LogicalPort
from python.protos.openflow_13_pb2 import OFPPS_LIVE, OFPPF_FIBER, \
    OFPPF_1GB_FD, \
    OFPC_GROUP_STATS, OFPC_PORT_STATS, OFPC_TABLE_STATS, OFPC_FLOW_STATS, \
    ofp_switch_features, ofp_desc
from python.protos.openflow_13_pb2 import ofp_port
from python.protos.ponsim_pb2 import FlowTable, PonSimFrame, PonSimMetricsRequest

_ = third_party
log = structlog.get_logger()
#OpenOltDefaults = {
#    'support_classes': {
#        'platform': OpenOltPlatform,
#        'resource_mgr': OpenOltResourceMgr,
#        'flow_mgr': OpenOltFlowMgr,
#        'alarm_mgr': OpenOltAlarmMgr,
#        'stats_mgr': OpenOltStatisticsMgr,
#        'bw_mgr': OpenOltBW
#    }
#}


class OpenoltAdapter(object):
    name = 'openolt'

    supported_device_types = [
        DeviceType(
            id=name,
            adapter=name,
            accepts_bulk_flow_update=True,
            accepts_direct_logical_flows_update=True
        )
    ]

    def __init__(self, core_proxy, adapter_proxy, config):
        self.adapter_agent = adapter_agent
        self.config = config
        self.descriptor = Adapter(
            id=self.name,
            vendor='OLT white box vendor',
            version='0.1',
            config=config
        )
        log.debug('openolt.__init__', adapter_agent=adapter_agent)
        self.devices = dict()  # device_id -> OpenoltDevice()
        self.interface = registry('main').get_args().interface
        self.logical_device_id_to_root_device_id = dict()
        self.num_devices = 0

