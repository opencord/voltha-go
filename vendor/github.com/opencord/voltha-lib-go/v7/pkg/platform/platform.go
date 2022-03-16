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

package platform

import (
	"context"

	"github.com/opencord/voltha-lib-go/v7/pkg/flows"
	"github.com/opencord/voltha-lib-go/v7/pkg/log"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

/*=====================================================================
Logical (OF) UNI port number

    OpenFlow port number corresponding to PON UNI

     24        16          8          0
    +--+--------+----------+----------+
    |0 | pon id |  onu id  |  uni id  |
    +--+--------+----------+----------+

    pon id = 8 bits = 256 PON ports
    onu id = 8 bits = 256 ONUs per PON port
    uni id = 8 bits = 256 UNIs per ONU

Logical (OF) NNI port number

    OpenFlow port number corresponding to PON NNI

     24                             0
    +--+----------------------------+
    |1 |                    intf_id |
    +--+----------------------------+

    No overlap with UNI port number space


PON OLT (OF) port number

    OpenFlow port number corresponding to PON OLT ports

     31     28                                 0
    +--------+------------------------~~~------+
    |  0x2   |          pon intf id            |
    +--------+------------------------~~~------+
*/

const (
	// Number of bits for the physical UNI of the ONUs
	bitsForUniID = 8
	// Number of bits for the ONU ID
	bitsForONUID = 8
	// Number of bits for PON ID
	bitsForPONID = 8
	// MaxOnusPerPon is Max number of ONUs on any PON port
	MaxOnusPerPon = (1 << bitsForONUID)
	// MaxPonsPerOlt is Max number of PON ports on any OLT
	MaxPonsPerOlt = (1 << bitsForPONID)
	// MaxUnisPerOnu is the Max number of UNI ports on any ONU
	MaxUnisPerOnu = (1 << bitsForUniID)
	// Bit position where the differentiation bit is located
	nniUniDiffPos = (bitsForUniID + bitsForONUID + bitsForPONID)
	// Bit position where the marker for PON port type of OF port is present
	ponIntfMarkerPos = 28
	// Value of marker used to distinguish PON port type of OF port
	ponIntfMarkerValue = 0x2
	// minNniPortNum is used to store start range of nni port number (1 << 24) 16777216
	minNniPortNum = (1 << nniUniDiffPos)
	// maxNniPortNum is used to store the maximum range of nni port number ((1 << 25)-1) 33554431
	maxNniPortNum = ((1 << (nniUniDiffPos + 1)) - 1)
	// minPonIntfPortNum stores the minimum pon port number (536870912)
	minPonIntfPortNum = ponIntfMarkerValue << ponIntfMarkerPos
	// maxPonIntfPortNum stores the maximum pon port number (536871167)
	maxPonIntfPortNum = ((ponIntfMarkerValue << ponIntfMarkerPos) | (1 << bitsForPONID)) - 1
	upstream          = "upstream"
	downstream        = "downstream"
	//Technology Profiles ID start value
	TpIDStart = 64
	//Technology Profiles ID end value
	TpIDEnd = 256
	//Number of Technology Profiles can be defined.
	TpRange = TpIDEnd - TpIDStart
)

//MkUniPortNum returns new UNIportNum based on intfID, inuID and uniID
func MkUniPortNum(ctx context.Context, intfID, onuID, uniID uint32) uint32 {
	var limit = int(intfID)
	if limit > MaxPonsPerOlt {
		logger.Warn(ctx, "Warning: exceeded the MAX pons per OLT")
	}
	limit = int(onuID)
	if limit > MaxOnusPerPon {
		logger.Warn(ctx, "Warning: exceeded the MAX ONUS per PON")
	}
	limit = int(uniID)
	if limit > MaxUnisPerOnu {
		logger.Warn(ctx, "Warning: exceeded the MAX UNIS per ONU")
	}
	return (intfID << (bitsForUniID + bitsForONUID)) | (onuID << bitsForUniID) | uniID
}

//OnuIDFromPortNum returns ONUID derived from portNumber
func OnuIDFromPortNum(portNum uint32) uint32 {
	return (portNum >> bitsForUniID) & (MaxOnusPerPon - 1)
}

//IntfIDFromUniPortNum returns IntfID derived from portNum
func IntfIDFromUniPortNum(portNum uint32) uint32 {
	return (portNum >> (bitsForUniID + bitsForONUID)) & (MaxPonsPerOlt - 1)
}

//UniIDFromPortNum return UniID derived from portNum
func UniIDFromPortNum(portNum uint32) uint32 {
	return (portNum) & (MaxUnisPerOnu - 1)
}

//IntfIDToPortNo returns portId derived from intftype, intfId and portType
func IntfIDToPortNo(intfID uint32, intfType voltha.Port_PortType) uint32 {
	if (intfType) == voltha.Port_ETHERNET_NNI {
		return (1 << nniUniDiffPos) | intfID
	}
	if (intfType) == voltha.Port_PON_OLT {
		return (ponIntfMarkerValue << ponIntfMarkerPos) | intfID
	}
	return 0
}

//PortNoToIntfID returns portnumber derived from interfaceID
func PortNoToIntfID(portno uint32, intfType voltha.Port_PortType) uint32 {
	if (intfType) == voltha.Port_ETHERNET_NNI {
		return (1 << nniUniDiffPos) ^ portno
	}
	if (intfType) == voltha.Port_PON_OLT {
		return (ponIntfMarkerValue << ponIntfMarkerPos) ^ portno
	}
	return 0
}

//IntfIDFromNniPortNum returns Intf ID derived from portNum
func IntfIDFromNniPortNum(ctx context.Context, portNum uint32) (uint32, error) {
	if portNum < minNniPortNum || portNum > maxNniPortNum {
		logger.Errorw(ctx, "nniportnumber-is-not-in-valid-range", log.Fields{"portnum": portNum})
		return uint32(0), status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", portNum)
	}
	return (portNum & (minNniPortNum - 1)), nil
}

//IntfIDFromPonPortNum returns Intf ID derived from portNum
func IntfIDFromPonPortNum(ctx context.Context, portNum uint32) (uint32, error) {
	if portNum < minPonIntfPortNum || portNum > maxPonIntfPortNum {
		logger.Errorw(ctx, "ponportnumber-is-not-in-valid-range", log.Fields{"portnum": portNum})
		return uint32(0), status.Errorf(codes.InvalidArgument, "invalid-pon-port-number:%d", portNum)
	}
	return (portNum & ((1 << ponIntfMarkerPos) - 1)), nil
}

//IntfIDToPortTypeName returns port type derived from the intfId
func IntfIDToPortTypeName(intfID uint32) voltha.Port_PortType {
	if ((ponIntfMarkerValue << ponIntfMarkerPos) ^ intfID) < MaxPonsPerOlt {
		return voltha.Port_PON_OLT
	}
	if (intfID & (1 << nniUniDiffPos)) == (1 << nniUniDiffPos) {
		return voltha.Port_ETHERNET_NNI
	}
	return voltha.Port_ETHERNET_UNI
}

//ExtractAccessFromFlow returns AccessDevice information
func ExtractAccessFromFlow(inPort, outPort uint32) (uint32, uint32, uint32, uint32) {
	if IsUpstream(outPort) {
		return inPort, IntfIDFromUniPortNum(inPort), OnuIDFromPortNum(inPort), UniIDFromPortNum(inPort)
	}
	return outPort, IntfIDFromUniPortNum(outPort), OnuIDFromPortNum(outPort), UniIDFromPortNum(outPort)
}

//IsUpstream returns true for Upstream and false for downstream
func IsUpstream(outPort uint32) bool {
	if IsControllerBoundFlow(outPort) {
		return true
	}
	return (outPort & (1 << nniUniDiffPos)) == (1 << nniUniDiffPos)
}

//IsControllerBoundFlow returns true/false
func IsControllerBoundFlow(outPort uint32) bool {
	return outPort == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)
}

//OnuIDFromUniPortNum returns onuId from give portNum information.
func OnuIDFromUniPortNum(portNum uint32) uint32 {
	return (portNum >> bitsForUniID) & (MaxOnusPerPon - 1)
}

//FlowExtractInfo fetches uniport from the flow, based on which it gets and returns ponInf, onuID, uniID, inPort and ethType
func FlowExtractInfo(ctx context.Context, flow *ofp.OfpFlowStats, flowDirection string) (uint32, uint32, uint32, uint32, uint32, uint32, error) {
	var uniPortNo uint32
	var ponIntf uint32
	var onuID uint32
	var uniID uint32
	var inPort uint32
	var ethType uint32

	if flowDirection == upstream {
		if uniPortNo = flows.GetChildPortFromTunnelId(flow); uniPortNo == 0 {
			for _, field := range flows.GetOfbFields(flow) {
				if field.GetType() == flows.IN_PORT {
					uniPortNo = field.GetPort()
					break
				}
			}
		}
	} else if flowDirection == downstream {
		if uniPortNo = flows.GetChildPortFromTunnelId(flow); uniPortNo == 0 {
			for _, field := range flows.GetOfbFields(flow) {
				if field.GetType() == flows.METADATA {
					for _, action := range flows.GetActions(flow) {
						if action.Type == flows.OUTPUT {
							if out := action.GetOutput(); out != nil {
								uniPortNo = out.GetPort()
							}
							break
						}
					}
				} else if field.GetType() == flows.IN_PORT {
					inPort = field.GetPort()
				} else if field.GetType() == flows.ETH_TYPE {
					ethType = field.GetEthType()
				}
			}
		}
	}

	if uniPortNo == 0 {
		return 0, 0, 0, 0, 0, 0, status.Errorf(codes.NotFound, "uni-not-found-flow-diraction:%s", flowDirection)
	}

	ponIntf = IntfIDFromUniPortNum(uniPortNo)
	onuID = OnuIDFromUniPortNum(uniPortNo)
	uniID = UniIDFromPortNum(uniPortNo)

	logger.Debugw(ctx, "flow-extract-info-result",
		log.Fields{
			"uniportno": uniPortNo,
			"pon-intf":  ponIntf,
			"onu-id":    onuID,
			"uni-id":    uniID,
			"inport":    inPort,
			"ethtype":   ethType})

	return uniPortNo, ponIntf, onuID, uniID, inPort, ethType, nil
}
