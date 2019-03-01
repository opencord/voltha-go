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
from python.protos.device_pb2 import Port, PmConfig, PmConfigs, \
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

    # System Init Methods #
    def __init__(self, core_proxy, adapter_proxy, config):
        self.adapter_proxy = adapter_proxy
        self.core_proxy = core_proxy
        self.config = config
        self.descriptor = Adapter(
            id=self.name,
            vendor='OLT white box vendor',
            version='0.1',
            config=config
        )
        log.debug('openolt.__init__', adapter_proxy=adapter_proxy)
        self.devices = dict()  # device_id -> OpenoltDevice()
        self.interface = registry('main').get_args().interface
        self.logical_device_id_to_root_device_id = dict()
        self.num_devices = 0

    def start(self):
        log.info('started', interface=self.interface)

    def stop(self):
        log.info('stopped', interface=self.interface)


    # Info Methods #
    def adapter_descriptor(self):
        log.debug('get descriptor', interface=self.interface)
        return self.descriptor

    def device_types(self):
        log.debug('get device_types', interface=self.interface,
                  items=self.supported_device_types)
        return DeviceTypes(items=self.supported_device_types)

    def health(self):
        log.debug('get health', interface=self.interface)
        raise NotImplementedError()

    def get_device_details(self, device):
        log.debug('get_device_details', device=device)
        raise NotImplementedError()


    # Device Operation Methods #
    def change_master_state(self, master):
        log.debug('change_master_state', interface=self.interface,
                  master=master)
        raise NotImplementedError()

    def abandon_device(self, device):
        log.info('abandon-device', device=device)
        raise NotImplementedError()


    # Configuration Methods #
    def update_flows_incrementally(self, device, flow_changes, group_changes):
        log.debug('update_flows_incrementally', device=device,
                  flow_changes=flow_changes, group_changes=group_changes)
        log.info('This device does not allow this, therefore it is Not '
                 'implemented')
        raise NotImplementedError()

    def update_pm_config(self, device, pm_configs):
        log.info('update_pm_config - Not implemented yet', device=device,
                 pm_configs=pm_configs)
        raise NotImplementedError()

    def receive_proxied_message(self, proxy_address, msg):
        log.debug('receive_proxied_message - Not implemented',
                  proxy_address=proxy_address,
                  proxied_msg=msg)
        raise NotImplementedError()

    def receive_inter_adapter_message(self, msg):
        log.info('rx_inter_adapter_msg - Not implemented')
        raise NotImplementedError()


    # Image Operations Methods #
    def download_image(self, device, request):
        log.info('image_download - Not implemented yet', device=device,
                 request=request)
        raise NotImplementedError()

    def get_image_download_status(self, device, request):
        log.info('get_image_download - Not implemented yet', device=device,
                 request=request)
        raise NotImplementedError()

    def cancel_image_download(self, device, request):
        log.info('cancel_image_download - Not implemented yet', device=device)
        raise NotImplementedError()

    def activate_image_update(self, device, request):
        log.info('activate_image_update - Not implemented yet',
                 device=device, request=request)
        raise NotImplementedError()

    def revert_image_update(self, device, request):
        log.info('revert_image_update - Not implemented yet',
                 device=device, request=request)
        raise NotImplementedError()

    def self_test_device(self, device):
        # from voltha.protos.voltha_pb2 import SelfTestResponse
        log.info('Not implemented yet')
        raise NotImplementedError()


    # PON Operations Methods #
    def create_interface(self, device, data):
        log.debug('create-interface - Not implemented - We do not use this',
                  data=data)
        raise NotImplementedError()

    def update_interface(self, device, data):
        log.debug('update-interface - Not implemented - We do not use this',
                  data=data)
        raise NotImplementedError()

    def remove_interface(self, device, data):
        log.debug('remove-interface - Not implemented - We do not use this',
                  data=data)
        raise NotImplementedError()

    def receive_onu_detect_state(self, proxy_address, state):
        log.debug('receive-onu-detect-state - Not implemented - We do not '
                  'use this', proxy_address=proxy_address,
                  state=state)
        raise NotImplementedError()

    def create_tcont(self, device, tcont_data, traffic_descriptor_data):
        log.info('create-tcont - Not implemented - We do not use this',
                 tcont_data=tcont_data,
                 traffic_descriptor_data=traffic_descriptor_data)
        raise NotImplementedError()

    def update_tcont(self, device, tcont_data, traffic_descriptor_data):
        log.info('update-tcont - Not implemented - We do not use this',
                 tcont_data=tcont_data,
                 traffic_descriptor_data=traffic_descriptor_data)
        raise NotImplementedError()

    def remove_tcont(self, device, tcont_data, traffic_descriptor_data):
        log.info('remove-tcont - Not implemented - We do not use this',
                 tcont_data=tcont_data,
                 traffic_descriptor_data=traffic_descriptor_data)
        raise NotImplementedError()

    def create_gemport(self, device, data):
        log.info('create-gemport - Not implemented - We do not use this',
                 data=data)
        raise NotImplementedError()

    def update_gemport(self, device, data):
        log.info('update-gemport - Not implemented - We do not use this',
                 data=data)
        raise NotImplementedError()

    def remove_gemport(self, device, data):
        log.info('remove-gemport - Not implemented - We do not use this',
                 data=data)
        raise NotImplementedError()

    def create_multicast_gemport(self, device, data):
        log.info('create-mcast-gemport  - Not implemented - We do not use '
                 'this', data=data)
        raise NotImplementedError()

    def update_multicast_gemport(self, device, data):
        log.info('update-mcast-gemport - Not implemented - We do not use '
                 'this', data=data)
        raise NotImplementedError()

    def remove_multicast_gemport(self, device, data):
        log.info('remove-mcast-gemport - Not implemented - We do not use '
                 'this', data=data)
        raise NotImplementedError()

    def create_multicast_distribution_set(self, device, data):
        log.info('create-mcast-distribution-set - Not implemented - We do '
                 'not use this', data=data)
        raise NotImplementedError()

    def update_multicast_distribution_set(self, device, data):
        log.info('update-mcast-distribution-set - Not implemented - We do '
                 'not use this', data=data)
        raise NotImplementedError()

    def remove_multicast_distribution_set(self, device, data):
        log.info('remove-mcast-distribution-set - Not implemented - We do '
                 'not use this', data=data)
        raise NotImplementedError()


    # Alarm Methods #
    def suppress_alarm(self, filter):
        log.info('suppress_alarm - Not implemented yet', filter=filter)
        raise NotImplementedError()

    def unsuppress_alarm(self, filter):
        log.info('unsuppress_alarm - Not implemented yet', filter=filter)
        raise NotImplementedError()