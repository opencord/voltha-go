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

//Package core provides the utility for olt devices, flows and statistics
package platform

import (
	"context"
	fu "github.com/opencord/voltha-lib-go/v7/pkg/flows"
	ofp "github.com/opencord/voltha-protos/v5/go/openflow_13"
	"github.com/opencord/voltha-protos/v5/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"math"
	"reflect"
	"testing"
)

func TestMkUniPortNum(t *testing.T) {
	type args struct {
		intfID uint32
		onuID  uint32
		uniID  uint32
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		// TODO: Add test cases.
		{"MkUniPortNum-1", args{1, 1, 1}, ((1 * 65536) + (1 * 256) + 1)},
		{"MkUniPortNum-2", args{4, 5, 6}, ((4 * 65536) + (5 * 256) + 6)},
		// Negative test cases to cover the log.warn
		{"MkUniPortNum-3", args{4, 130, 6}, ((4 * 65536) + (130 * 256) + 6)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MkUniPortNum(context.Background(), tt.args.intfID, tt.args.onuID, tt.args.uniID); got != tt.want {
				t.Errorf("MkUniPortNum() = %v, want %v", got, tt.want)
			} else {
				t.Logf("Expected %v , Actual %v \n", tt.want, got)
			}
		})
	}
}

func TestOnuIDFromPortNum(t *testing.T) {
	type args struct {
		portNum uint32
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		// TODO: Add test cases.
		{"OnuIDFromPortNum-1", args{portNum: 8096}, ((8096 / 256) & 255)},
		{"OnuIDFromPortNum-2", args{portNum: 9095}, ((9095 / 256) & 255)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := OnuIDFromPortNum(tt.args.portNum); got != tt.want {
				t.Errorf("OnuIDFromPortNum() = %v, want %v", got, tt.want)
			} else {
				t.Logf("Expected %v , Actual %v \n", tt.want, got)
			}
		})
	}
}

func TestIntfIDFromUniPortNum(t *testing.T) {
	type args struct {
		portNum uint32
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		// TODO: Add test cases.
		{"IntfIDFromUniPortNum-1", args{portNum: 8096}, ((8096 / 65536) & 255)},
		{"IntfIDFromUniPortNum-2", args{portNum: 1024}, ((1024 / 65536) & 255)},
		{"IntfIDFromUniPortNum-3", args{portNum: 66560}, ((66560 / 65536) & 255)},
		{"IntfIDFromUniPortNum-4", args{portNum: 16712193}, ((16712193 / 65536) & 255)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IntfIDFromUniPortNum(tt.args.portNum); got != tt.want {
				t.Errorf("IntfIDFromUniPortNum() = %v, want %v", got, tt.want)
			} else {
				t.Logf("Expected %v , Actual %v \n", tt.want, got)
			}
		})
	}
}

func TestUniIDFromPortNum(t *testing.T) {
	type args struct {
		portNum uint32
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{

		// TODO: Add test cases.
		{"UniIDFromPortNum-1", args{portNum: 8096}, (8096 & 255)},
		{"UniIDFromPortNum-2", args{portNum: 1024}, (1024 & 255)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := UniIDFromPortNum(tt.args.portNum); got != tt.want {
				t.Errorf("UniIDFromPortNum() = %v, want %v", got, tt.want)
			} else {
				t.Logf("Expected %v , Actual %v \n", tt.want, got)
			}
		})
	}
}

func TestIntfIDToPortNo(t *testing.T) {
	type args struct {
		intfID   uint32
		intfType voltha.Port_PortType
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		// TODO: Add test cases.
		{"IntfIDToPortNo-1", args{intfID: 120, intfType: voltha.Port_ETHERNET_NNI}, (uint32(math.Pow(2, 24)) + 120)},
		{"IntfIDToPortNo-2", args{intfID: 1024, intfType: voltha.Port_ETHERNET_UNI}, 0},
		{"IntfIDToPortNo-3", args{intfID: 456, intfType: voltha.Port_PON_OLT}, (uint32(2*math.Pow(2, 28)) + 456)},
		{"IntfIDToPortNo-4", args{intfID: 28, intfType: voltha.Port_PON_ONU}, 0},
		{"IntfIDToPortNo-5", args{intfID: 45, intfType: voltha.Port_UNKNOWN}, 0},
		{"IntfIDToPortNo-6", args{intfID: 45, intfType: voltha.Port_VENET_OLT}, 0},
		{"IntfIDToPortNo-7", args{intfID: 45, intfType: voltha.Port_VENET_ONU}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IntfIDToPortNo(tt.args.intfID, tt.args.intfType); got != tt.want {
				t.Errorf("IntfIDToPortNo() = %v, want %v", got, tt.want)
			} else {
				t.Logf("Expected %v , Actual %v \n", tt.want, got)
			}
		})
	}
}

func TestPortNoToIntfID(t *testing.T) {
	type args struct {
		portNo   uint32
		intfType voltha.Port_PortType
	}
	tests := []struct {
		name string
		args args
		want uint32
	}{
		// TODO: Add test cases.
		{"PortNoToIntfID-1", args{portNo: 16777216, intfType: voltha.Port_ETHERNET_NNI}, 0},
		{"PortNoToIntfID-2", args{portNo: 16777217, intfType: voltha.Port_ETHERNET_NNI}, 1},
		{"PortNoToIntfID-3", args{portNo: 16777218, intfType: voltha.Port_ETHERNET_NNI}, 2},
		{"PortNoToIntfID-4", args{portNo: 1024, intfType: voltha.Port_ETHERNET_UNI}, 0},
		{"PortNoToIntfID-5", args{portNo: 536870912, intfType: voltha.Port_PON_OLT}, 0},
		{"PortNoToIntfID-6", args{portNo: 536871167, intfType: voltha.Port_PON_OLT}, 255},
		{"PortNoToIntfID-7", args{portNo: 28, intfType: voltha.Port_PON_ONU}, 0},
		{"PortNoToIntfID-8", args{portNo: 45, intfType: voltha.Port_UNKNOWN}, 0},
		{"PortNoToIntfID-9", args{portNo: 45, intfType: voltha.Port_VENET_OLT}, 0},
		{"PortNoToIntfID-10", args{portNo: 45, intfType: voltha.Port_VENET_ONU}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PortNoToIntfID(tt.args.portNo, tt.args.intfType); got != tt.want {
				t.Errorf("PortNoToIntfID() = %v, want %v", got, tt.want)
			} else {
				t.Logf("Expected %v , Actual %v \n", tt.want, got)
			}
		})
	}
}

func TestIntfIDFromNniPortNum(t *testing.T) {
	type args struct {
		portNum uint32
	}

	tests := []struct {
		name    string
		args    args
		want    uint32
		wantErr error
	}{
		// TODO: Add test cases. min 16777216, max 33554432
		{"IntfIDFromNniPortNum-01", args{portNum: 8081}, 0, status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", 8081)},
		{"IntfIDFromNniPortNum-02", args{portNum: 9090}, 0, status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", 9090)},
		{"IntfIDFromNniPortNum-03", args{portNum: 0}, 0, status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", 0)},
		{"IntfIDFromNniPortNum-04", args{portNum: 65535}, 0, status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", 65535)},
		{"IntfIDFromNniPortNum-05", args{portNum: 16777215}, 0, status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", 16777215)},
		{"IntfIDFromNniPortNum-06", args{portNum: 16777216}, 0, nil},
		{"IntfIDFromNniPortNum-07", args{portNum: 16777217}, 1, nil},
		{"IntfIDFromNniPortNum-08", args{portNum: 16777218}, 2, nil},
		{"IntfIDFromNniPortNum-09", args{portNum: 16777219}, 3, nil},
		{"IntfIDFromNniPortNum-10", args{portNum: 33554430}, 16777214, nil},
		{"IntfIDFromNniPortNum-11", args{portNum: 33554431}, 16777215, nil},
		{"IntfIDFromNniPortNum-12", args{portNum: 33554432}, 0, status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", 33554432)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IntfIDFromNniPortNum(context.Background(), tt.args.portNum)
			if got != tt.want {
				t.Errorf("IntfIDFromNniPortNum(): FOR[%v] WANT[%v and %v] GOT[%v and %v]",
					tt.args.portNum, tt.want, tt.wantErr, got, err)
			}
		})
	}
}

func TestIntfIDFromPonPortNum(t *testing.T) {
	type args struct {
		portNum uint32
	}

	tests := []struct {
		name    string
		args    args
		want    uint32
		wantErr error
	}{
		// TODO: Add test cases. min 16777216, max 33554432
		{"IntfIDFromPonPortNum-02", args{portNum: 9090}, 0, status.Errorf(codes.InvalidArgument, "pon-port-number-out-of-range:%d", 9090)},
		{"IntfIDFromPonPortNum-03", args{portNum: 0}, 0, status.Errorf(codes.InvalidArgument, "pon-port-number-out-of-range:%d", 0)},
		{"IntfIDFromPonPortNum-04", args{portNum: 65535}, 0, status.Errorf(codes.InvalidArgument, "pon-port-number-out-of-range:%d", 65535)},
		{"IntfIDFromPonPortNum-05", args{portNum: 16777215}, 0, status.Errorf(codes.InvalidArgument, "pon-port-number-out-of-range:%d", 16777215)},
		{"IntfIDFromPonPortNum-01", args{portNum: 536870911}, 0, status.Errorf(codes.InvalidArgument, "pon-port-number-out-of-range:%d", 536870911)},
		{"IntfIDFromPonPortNum-06", args{portNum: 536870912}, 0, nil},
		{"IntfIDFromPonPortNum-07", args{portNum: 536870913}, 1, nil},
		{"IntfIDFromPonPortNum-08", args{portNum: 536870914}, 2, nil},
		{"IntfIDFromPonPortNum-09", args{portNum: 536870915}, 3, nil},
		{"IntfIDFromPonPortNum-10", args{portNum: 536871166}, 254, nil},
		{"IntfIDFromPonPortNum-11", args{portNum: 536871167}, 255, nil},
		{"IntfIDFromPonPortNum-12", args{portNum: 536871168}, 0, status.Errorf(codes.InvalidArgument, "nni-port-number-out-of-range:%d", 536871168)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := IntfIDFromPonPortNum(context.Background(), tt.args.portNum)
			if got != tt.want {
				t.Errorf("IntfIDFromPonPortNum(): FOR[%v] WANT[%v and %v] GOT[%v and %v]",
					tt.args.portNum, tt.want, tt.wantErr, got, err)
			}
		})
	}
}
func TestIntfIDToPortTypeName(t *testing.T) {
	type args struct {
		intfID uint32
	}
	input := uint32(2*math.Pow(2, 28)) | 3
	tests := []struct {
		name string
		args args
		want voltha.Port_PortType
	}{
		// TODO: Add test cases.
		{"IntfIDToPortTypeName-1", args{intfID: 16777216}, voltha.Port_ETHERNET_NNI},
		{"IntfIDToPortTypeName-2", args{intfID: 1000}, voltha.Port_ETHERNET_UNI},
		{"IntfIDToPortTypeName-2", args{intfID: input}, voltha.Port_PON_OLT},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IntfIDToPortTypeName(tt.args.intfID); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("IntfIDToPortTypeName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractAccessFromFlow(t *testing.T) {
	type args struct {
		inPort  uint32
		outPort uint32
	}
	tests := []struct {
		name   string
		args   args
		port   uint32
		IntfID uint32
		onuID  uint32
		uniID  uint32
	}{
		// TODO: Add test cases.
		{"ExtractAccessFromFlow-1", args{inPort: 1540, outPort: 16777216}, 1540, 0, 6, 4},
		{"ExtractAccessFromFlow-2", args{inPort: 1048576, outPort: 10}, 10, 0, 0, 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, got2, got3 := ExtractAccessFromFlow(tt.args.inPort, tt.args.outPort)
			if got != tt.port {
				t.Errorf("ExtractAccessFromFlow() got = %v, want %v", got, tt.port)
			}
			if got1 != tt.IntfID {
				t.Errorf("ExtractAccessFromFlow() got1 = %v, want %v", got1, tt.IntfID)
			}
			if got2 != tt.onuID {
				t.Errorf("ExtractAccessFromFlow() got2 = %v, want %v", got2, tt.onuID)
			}
			if got3 != tt.uniID {
				t.Errorf("ExtractAccessFromFlow() got3 = %v, want %v", got3, tt.uniID)
			}
		})
	}
	//t.Error()
}

func TestIsUpstream(t *testing.T) {
	type args struct {
		outPort uint32
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// TODO: Add test cases.
		{"TestIsUpstream-1", args{outPort: 2147483645}, true}, //controller bound
		{"TestIsUpstream-2", args{outPort: 16777215}, false},
		{"TestIsUpstream-3", args{outPort: 16777216}, true},
		{"TestIsUpstream-4", args{outPort: 16777217}, true},
		{"TestIsUpstream-5", args{outPort: 33554431}, true},
		{"TestIsUpstream-6", args{outPort: 33554432}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsUpstream(tt.args.outPort); got != tt.want {
				t.Errorf("IsUpstream() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsControllerBoundFlow(t *testing.T) {
	type args struct {
		outPort uint32
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		// TODO: Add test cases.
		{"IsControllerBoundFlow-1", args{outPort: 2147483645}, true},
		{"IsControllerBoundFlow-2", args{outPort: 2147483646}, false},
		{"IsControllerBoundFlow-3", args{outPort: 4294967293}, false},
		{"IsControllerBoundFlow-4", args{outPort: 4294967294}, false},
		{"IsControllerBoundFlow-5", args{outPort: 65539}, false},
		{"IsControllerBoundFlow-6", args{outPort: 1000}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsControllerBoundFlow(tt.args.outPort); got != tt.want {
				t.Errorf("IsControllerBoundFlow() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFlowExtractInfo(t *testing.T) {
	fa := &fu.FlowArgs{
		MatchFields: []*ofp.OfpOxmOfbField{
			fu.InPort(2),
			fu.Metadata_ofp(uint64(ofp.OfpInstructionType_OFPIT_WRITE_METADATA | 2)),
			fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT)),
			fu.EthType(2048),
		},

		Actions: []*ofp.OfpAction{
			fu.SetField(fu.Metadata_ofp(uint64(ofp.OfpInstructionType_OFPIT_WRITE_METADATA))),
			fu.SetField(fu.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 101)),
			fu.Output(1),
		},
	}
	ofpstats, _ := fu.MkFlowStat(fa)
	type args struct {
		flow          *ofp.OfpFlowStats
		flowDirection string
	}
	tests := []struct {
		name    string
		args    args
		want    uint32
		want1   uint32
		want2   uint32
		want3   uint32
		want4   uint32
		want5   uint32
		wantErr bool
	}{
		// TODO: Add test cases.
		{"FlowExtractInfo-1", args{flow: ofpstats, flowDirection: "upstream"}, 2, 0, 0, 2, 0, 0, false},

		// Negative Testcases
		{"FlowExtractInfo-2", args{flow: ofpstats, flowDirection: "downstream"}, 1, 0, 0, 1, 2, 2048, false},
		{"FlowExtractInfo-3", args{flow: nil, flowDirection: "downstream"}, 0, 0, 0, 0, 0, 0, true},
		{"FlowExtractInfo-4", args{flow: &ofp.OfpFlowStats{}, flowDirection: "downstream"}, 0, 0, 0, 0, 0, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, got2, got3, got4, got5, err := FlowExtractInfo(context.Background(), tt.args.flow, tt.args.flowDirection)
			if (err != nil) != tt.wantErr {
				t.Errorf("FlowExtractInfo() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("FlowExtractInfo() got = %v, want %v", got, tt.want)
				return
			}
			if got1 != tt.want1 {
				t.Errorf("FlowExtractInfo() got1 = %v, want %v", got1, tt.want1)
				return
			}
			if got2 != tt.want2 {
				t.Errorf("FlowExtractInfo() got2 = %v, want %v", got2, tt.want2)
				return
			}
			if got3 != tt.want3 {
				t.Errorf("FlowExtractInfo() got3 = %v, want %v", got3, tt.want3)
				return
			}
			if got4 != tt.want4 {
				t.Errorf("FlowExtractInfo() got4 = %v, want %v", got4, tt.want4)
				return
			}
			if got5 != tt.want5 {
				t.Errorf("FlowExtractInfo() got5 = %v, want %v", got5, tt.want5)
				return
			}
		})
	}
}
