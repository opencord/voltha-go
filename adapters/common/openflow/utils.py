#
# Copyright 2017 the original author or authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#


from adapters.protos import openflow_13_pb2 as ofp

OUTPUT = ofp.OFPAT_OUTPUT
ETH_TYPE = ofp.OFPXMT_OFB_ETH_TYPE
IP_PROTO = ofp.OFPXMT_OFB_IP_PROTO

def get_ofb_fields(flow):
    assert isinstance(flow, ofp.ofp_flow_stats)
    assert flow.match.type == ofp.OFPMT_OXM
    ofb_fields = []
    for field in flow.match.oxm_fields:
        assert field.oxm_class == ofp.OFPXMC_OPENFLOW_BASIC
        ofb_fields.append(field.ofb_field)
    return ofb_fields

def get_actions(flow):
    """Extract list of ofp_action objects from flow spec object"""
    assert isinstance(flow, ofp.ofp_flow_stats)
    # we have the following hard assumptions for now
    for instruction in flow.instructions:
        if instruction.type == ofp.OFPIT_APPLY_ACTIONS:
            return instruction.actions.actions

def get_out_port(flow):
    for action in get_actions(flow):
        if action.type == OUTPUT:
            return action.output.port
    return None
