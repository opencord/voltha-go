// +build integration

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

package main

import (
	"fmt"
	"os"
	//"flag"
	//"path"
	//"bufio"
	"errors"
	//"os/exec"
	//"strconv"
	"io/ioutil"
	//"encoding/json"
	"text/template"
	//"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	//pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

// This test suite validates that the different method types get routed
// properly. That is, rw methods to the rw cores and ro methods to the
// ro cores
type test struct {
	Name   string
	Core   int
	SerNo  int
	Method string
	Param  string
	Expect string
}

type tests struct {
	RoTests  []test
	RwTests  []test
	CtlTests []test
}

var roMethods []test = []test{
	{
		Name:   "Test GetVoltha",
		Method: "GetVoltha", // rpc GetVoltha(google.protobuf.Empty) returns(Voltha)
		Param:  "{}",
		Expect: `{Version:\"abcdef\"}`,
	},
	/*
		    {
				Name: "Test ListCoreInstances",
				Method: "ListCoreInstances", // rpc ListCoreInstances(google.protobuf.Empty) returns(CoreInstances)
				Param: "{}",
				Expect:  `{Items:[]*voltha.CoreInstance{&voltha.CoreInstance{InstanceId:\"ABC\",Health:&voltha.HealthStatus{State:0}}}}`,
			},
			{
				Name: "Test GetCoreInstance",
				Method: "GetCoreInstance", // rpc GetCoreInstance(ID) returns(CoreInstance)
				Param: `{Id:\"arou8390\"}`,
				Expect: `{InstanceId:\"arou8390\", Health:&voltha.HealthStatus{State:0}}`,
			},*/
	{
		Name:   "Test ListAdapters",
		Method: "ListAdapters", // rpc ListAdapters(google.protobuf.Empty) returns(Adapters)
		Param:  "{}",
		Expect: `{Items:[]*voltha.Adapter{&voltha.Adapter{Id:\"ABC\", Vendor:\"afrouterTest\", Version:\"Version 1.0\"}}}`,
	},
	{
		Name:   "Test ListLogicalDevices",
		Method: "ListLogicalDevices", // rpc ListLogicalDevices(google.protobuf.Empty) returns(LogicalDevices)
		Param:  "{}",
		Expect: `{Items:[]*voltha.LogicalDevice{&voltha.LogicalDevice{Id:\"LDevId\", DatapathId:64, RootDeviceId:\"Root\"}}}`,
	},
	{
		Name:   "Test GetLogicalDevice",
		Method: "GetLogicalDevice", // rpc GetLogicalDevice(ID) returns(LogicalDevice)
		Param:  `{Id:\"ABC123XYZ\"}`,
		Expect: `{Id:\"ABC123XYZ\", DatapathId:64, RootDeviceId:\"Root-of:ABC123XYZ\"}`,
	},
	{
		Name:   "Test ListLogicalDevicePorts",
		Method: "ListLogicalDevicePorts", // rpc ListLogicalDevicePorts(ID) returns(LogicalPorts)
		Param:  `{Id:\"ABC123XYZ\"}`,
		Expect: `{Items:[]*voltha.LogicalPort{&voltha.LogicalPort{Id:\"PortId-1\", DeviceId:\"ABC12XYZ\", DevicePortNo:64, RootPort:true},&voltha.LogicalPort{Id:\"PortId-2\", DeviceId:\"ABC12XYZ\", DevicePortNo:64, RootPort:false}}}`,
	},
	{
		Name:   "Test GetLogicalDevicePort",
		Method: "GetLogicalDevicePort", // rpc GetLogicalDevicePort(LogicalPortId) returns(LogicalPort)
		Param:  `{Id:\"PortId-1\"}`,
		Expect: `{Id:\"PortId-1\", DeviceId:\"ABC12XYZ\", DevicePortNo:64, RootPort:true}`,
	},
	{
		Name:   "Test ListLogicalDeviceFlows",
		Method: "ListLogicalDeviceFlows", // rpc ListLogicalDeviceFlows(ID) returns(openflow_13.Flows)
		Param:  `{Id:\"Flow-ABC123XYZ\"}`,
		Expect: `{Items:[]*openflow_13.OfpFlowStats{&openflow_13.OfpFlowStats{Id:1, TableId:2, DurationSec:12, Priority:0},&openflow_13.OfpFlowStats{Id:1, TableId:2, DurationSec:12, Priority:0}}}`,
	},
	{
		Name:   "Test ListLogicalDeviceFlowGroups",
		Method: "ListLogicalDeviceFlowGroups", // rpc ListLogicalDeviceFlowGroups(ID) returns(openflow_13.FlowGroups)
		Param:  `{Id:\"Flow-ABC123XYZ\"}`,
		Expect: `{Items:[]*openflow_13.OfpGroupEntry{&openflow_13.OfpGroupEntry{Desc:&openflow_13.OfpGroupDesc{GroupId:12}, Stats:&openflow_13.OfpGroupStats{GroupId:1, RefCount:1, PacketCount:3}}}}`,
	},
	{
		Name:   "Test ListDevices",
		Method: "ListDevices", // rpc ListDevices(google.protobuf.Empty) returns(Devices)
		Param:  `{}`,
		Expect: `{Items:[]*voltha.Device{&voltha.Device{Id:\"ABC123XYZ\", Type:\"SomeDeviceType\", Root:false, ParentId:\"ZYX321CBA\"},&voltha.Device{Id:\"ZYX321CBA\", Type:\"SomeDeviceType\", Root:true, ParentId:\"ROOT\"}}}`,
	},
	{
		Name:   "Test ListDeviceIds",
		Method: "ListDeviceIds", // rpc ListDeviceIds(google.protobuf.Empty) returns(IDs)
		Param:  `{}`,
		Expect: `{Items:[]*voltha.ID{&voltha.ID{Id:\"ABC123XYZ\"},&voltha.ID{Id:\"ZYX321CBA\"}}}`,
	},
	{
		Name:   "Test GetDevice",
		Method: "GetDevice", // rpc GetDevice(ID) returns(Device)
		Param:  `{Id:\"ZYX321CBA\"}`,
		Expect: `{Id:\"ABC123XYZ\", Type:\"SomeDeviceType\", Root:false, ParentId:\"ZYX321CBA\"}`,
	},
	{
		Name:   "Test GetImageDownloadStatus",
		Method: "GetImageDownloadStatus", // rpc GetImageDownloadStatus(ImageDownload) returns(ImageDownload)
		Param:  `{Id:\"Image-ZYX321CBA\"}`,
		Expect: `{Id:\"Image-ABC123XYZ\", Name:\"SomeName\", Url:\"Some URL\", Crc:123456}`,
	},
	{
		Name:   "Test GetImageDownload",
		Method: "GetImageDownload", // rpc GetImageDownload(ImageDownload) returns(ImageDownload)
		Param:  `{Id:\"Image-ZYX321CBA\"}`,
		Expect: `{Id:\"Image-ABC123XYZ\", Name:\"SomeName\", Url:\"Some URL\", Crc:123456}`,
	},
	{
		Name:   "Test ListImageDownloads",
		Method: "ListImageDownloads", // rpc ListImageDownloads(ID) returns(ImageDownloads)
		Param:  `{Id:\"ZYX321CBA\"}`,
		Expect: `{Items:[]*voltha.ImageDownload{&voltha.ImageDownload{Id:\"Image1-ABC123XYZ\", Name:\"SomeName\", Url:\"Some URL\", Crc:123456}, &voltha.ImageDownload{Id:\"Image2-ABC123XYZ\", Name:\"SomeName\", Url:\"Some other URL\", Crc:654321}}}`,
	},
	{
		Name:   "Test ListDevicePorts",
		Method: "ListDevicePorts", // rpc ListDevicePorts(ID) returns(Ports)
		Param:  `{}`,
		Expect: `{Items:[]*voltha.Port{&voltha.Port{PortNo:100000, Label:\"Port one hundred thousand\", DeviceId:\"ZYX321CBA\"},&voltha.Port{PortNo:100001, Label:\"Port one hundred thousand and one\", DeviceId:\"ZYX321CBA\"}}}`,
	},
	{
		Name:   "Test ListDevicePmConfigs",
		Method: "ListDevicePmConfigs", // rpc ListDevicePmConfigs(ID) returns(PmConfigs)
		Param:  `{}`,
		Expect: `{Id:\"ABC123XYZ\", DefaultFreq: 10000, Grouped:false, FreqOverride:false, Metrics:[]*voltha.PmConfig{&voltha.PmConfig{Name:\"Speed\", Enabled: true, SampleFreq:10000}, &voltha.PmConfig{Name:\"Errors\", Enabled: true, SampleFreq:10000}}}`,
	},
	{
		Name:   "Test ListDeviceFlows",
		Method: "ListDeviceFlows", // rpc ListDeviceFlows(ID) returns(openflow_13.Flows)
		Param:  `{Id:\"Flow-ABC123XYZ\"}`,
		Expect: `{Items:[]*openflow_13.OfpFlowStats{&openflow_13.OfpFlowStats{Id:1, TableId:2, DurationSec:12, Priority:0},&openflow_13.OfpFlowStats{Id:1, TableId:2, DurationSec:12, Priority:0}}}`,
	},
	{
		Name:   "Test ListDeviceFlowGroups",
		Method: "ListDeviceFlowGroups", // rpc ListDeviceFlowGroups(ID) returns(openflow_13.FlowGroups)
		Param:  `{Id:\"Flow-ABC123XYZ\"}`,
		Expect: `{Items:[]*openflow_13.OfpGroupEntry{&openflow_13.OfpGroupEntry{Desc:&openflow_13.OfpGroupDesc{GroupId:12}, Stats:&openflow_13.OfpGroupStats{GroupId:1, RefCount:1, PacketCount:3}}}}`,
	},
	{
		Name:   "Test ListDeviceTypes",
		Method: "ListDeviceTypes", // rpc ListDeviceTypes(google.protobuf.Empty) returns(DeviceTypes)
		Param:  `{}`,
		Expect: `{Items:[]*voltha.DeviceType{&voltha.DeviceType{Id:\"ABC123XYZ\", VendorId:\"Ciena\", Adapter:\"SAOS\"},&voltha.DeviceType{Id:\"ZYX321CBA\", VendorId:\"Ciena\", Adapter:\"SAOS-Tibit\"}}}`,
	},
	{
		Name:   "Test GetDeviceType",
		Method: "GetDeviceType", // rpc GetDeviceType(ID) returns(DeviceType)
		Param:  `{Id:\"ZYX321CBA\"}`,
		Expect: `{Id:\"ZYX321CBA\", VendorId:\"Ciena\", Adapter:\"SAOS-Tibit\"}`,
	},
	{
		Name:   "Test ListDeviceGroups",
		Method: "ListDeviceGroups", // rpc ListDeviceGroups(google.protobuf.Empty) returns(DeviceGroups)
		Param:  `{}`,
		Expect: `{Items:[]*voltha.DeviceGroup{&voltha.DeviceGroup{Id:\"group-ABC123XYZ\", LogicalDevices: []*voltha.LogicalDevice{&voltha.LogicalDevice{Id:\"LDevId\", DatapathId:64, RootDeviceId:\"Root\"}}, Devices: []*voltha.Device{&voltha.Device{Id:\"ABC123XYZ\", Type:\"SomeDeviceType\", Root:false, ParentId:\"ZYX321CBA\"},&voltha.Device{Id:\"ZYX321CBA\", Type:\"SomeDeviceType\", Root:true, ParentId:\"ROOT\"}}}}}`,
	},
	{
		Name:   "Test GetDeviceGroup",
		Method: "GetDeviceGroup", // rpc GetDeviceGroup(ID) returns(DeviceGroup)
		Param:  `{Id:\"ZYX321CBA\"}`,
		Expect: `{Id:\"group-ABC123XYZ\", LogicalDevices: []*voltha.LogicalDevice{&voltha.LogicalDevice{Id:\"LDevId\", DatapathId:64, RootDeviceId:\"Root\"}}, Devices: []*voltha.Device{&voltha.Device{Id:\"ABC123XYZ\", Type:\"SomeDeviceType\", Root:false, ParentId:\"ZYX321CBA\"},&voltha.Device{Id:\"ZYX321CBA\", Type:\"SomeDeviceType\", Root:true, ParentId:\"ROOT\"}}}`,
	},
	{
		Name:   "Test ListEventFilters",
		Method: "ListEventFilters", // rpc ListEventFilters(google.protobuf.Empty) returns(EventFilters)
		Param:  `{}`,
		Expect: `{Filters:[]*voltha.EventFilter{&voltha.EventFilter{Id:\"ABC123XYZ\", DeviceId: \"Device123\", Enable: \"True\", EventType: \"DeviceEvent\", Rules: []*voltha.EventFilterRule{&voltha.EventFilterRule{Value:\"Rule Value\"}}}}}`,
	},
	{
		Name:   "Test GetEventFilter",
		Method: "GetEventFilter", // rpc GetEventFilter(ID) returns(EventFilters)
		Param:  `{Id:\"ABC123XYZ\"}`,
		Expect: `{Filters:[]*voltha.EventFilter{&voltha.EventFilter{Id:\"ABC123XYZ\", DeviceId: \"Device123\", Enable: \"True\", EventType: \"DeviceEvent\", Rules: []*voltha.EventFilterRule{&voltha.EventFilterRule{Value:\"Rule Value\"}}}}}`,
	},
	{
		Name:   "Test GetImages",
		Method: "GetImages", // rpc GetImages(ID) returns(Images)
		Param:  `{Id:\"ZYX321CBA\"}`,
		Expect: `{Image: []*voltha.Image{&voltha.Image{Name:\"Image1\", Version: \"0.1\", Hash:\"123@\", InstallDatetime:\"Yesterday\", IsActive:true, IsCommitted:true, IsValid:false},&voltha.Image{Name:\"Image2\", Version: \"0.2\", Hash:\"ABC@\", InstallDatetime:\"Today\", IsActive:false, IsCommitted:false, IsValid:false}}}`,
	},
}

var ctlMethods []test = []test{
	{
		Name:   "Test UpdateLogLevel",
		Method: "UpdateLogLevel", // rpc UpdateLogLevel(Logging) returns(google.protobuf.Empty)
		Param:  `{Level: 0, PackageName:\"abc123\"}`,
		Expect: "{}",
	},
}

var rwMethods []test = []test{
	{
		Name:   "Test EnableLogicalDevicePort",
		Method: "EnableLogicalDevicePort", // rpc EnableLogicalDevicePort(LogicalPortId) returns(google.protobuf.Empty)
		Param:  `{Id:\"ZYX321CBA\", PortId:\"100,000\"}`,
		Expect: `{}`,
	},
	{
		Name:   "Test DisableLogicalDevicePort",
		Method: "DisableLogicalDevicePort", // rpc DisableLogicalDevicePort(LogicalPortId) returns(google.protobuf.Empty)
		Param:  `{Id:\"ABC123XYZ\", PortId:\"100,000\"}`,
		Expect: `{}`,
	},
	{
		Name:   "Test UpdateLogicalDeviceFlowTable",
		Method: "UpdateLogicalDeviceFlowTable", // rpc UpdateLogicalDeviceFlowTable(openflow_13.FlowTableUpdate)
		Param:  `{Id:\"XYZ123ABC\", FlowMod:&openflow_13.OfpFlowMod{Cookie:10, CookieMask:255, TableId:10000, Command:openflow_13.OfpFlowModCommand_OFPFC_ADD}}`,
		Expect: `{}`,
	},
	{
		Name:   "Test UpdateLogicalDeviceFlowGroupTable",
		Method: "UpdateLogicalDeviceFlowGroupTable", // rpc UpdateLogicalDeviceFlowGroupTable(openflow_13.FlowGroupTableUpdate)
		Param:  `{Id:\"ZYX123ABC\", GroupMod:&openflow_13.OfpGroupMod{Command:openflow_13.OfpGroupModCommand_OFPGC_ADD, Type:openflow_13.OfpGroupType_OFPGT_INDIRECT, GroupId:255}}`,
		Expect: `{}`,
	},
	//"ReconcileDevices", // rpc ReconcileDevices(IDs) returns(google.protobuf.Empty)
	{
		Name:   "Test CreateDevice",
		Method: "CreateDevice", // rpc CreateDevice(Device) returns(Device)
		Param:  `{Type:\"simulated_OLT\"}`,
		Expect: `{Id:\"ZYX321ABC\", Type:\"\"}`,
	},
	{
		Name:   "Test EnableDevice",
		Method: "EnableDevice", // rpc EnableDevice(ID) returns(google.protobuf.Empty)
		Param:  `{Id:\"XYZ321ABC\"}`,
		Expect: `{}`,
	},
	{
		Name:   "Test DisableDevice",
		Method: "DisableDevice", // rpc DisableDevice(ID) returns(google.protobuf.Empty)
		Param:  `{Id:\"XYZ123CBA\"}`,
		Expect: `{}`,
	},
	{
		Name:   "Test RebootDevice",
		Method: "RebootDevice", // rpc RebootDevice(ID) returns(google.protobuf.Empty)
		Param:  `{Id:\"ZYX123CBA\"}`,
		Expect: `{}`,
	},
	{
		Name:   "Test DeleteDevice",
		Method: "DeleteDevice", // rpc DeleteDevice(ID) returns(google.protobuf.Empty)
		Param:  `{Id:\"CBA123XYZ\"}`,
		Expect: `{}`,
	},
	{
		Name:   "Test DownloadImage",
		Method: "DownloadImage", // rpc DownloadImage(ImageDownload) returns(OperationResp)
		Param:  `{Id:\"CBA321XYZ\", Name:\"Image-1\", Crc: 54321}`,
		Expect: `{Code:voltha.OperationResp_OPERATION_SUCCESS, AdditionalInfo:\"It worked!\"}`,
	},
	{
		Name:   "Test CancelImageDownload",
		Method: "CancelImageDownload", // rpc CancelImageDownload(ImageDownload) returns(OperationResp)
		Param:  `{Id:\"CBA321ZYX\", Name:\"Image-1\", Crc: 54321}`,
		Expect: `{Code:voltha.OperationResp_OPERATION_SUCCESS, AdditionalInfo:\"It worked!\"}`,
	},
	{
		Name:   "Test ActivateImageUpdate",
		Method: "ActivateImageUpdate", // rpc ActivateImageUpdate(ImageDownload) returns(OperationResp)
		Param:  `{Id:\"ABC321ZYX\", Name:\"Image-1\", Crc: 54321}`,
		Expect: `{Code:voltha.OperationResp_OPERATION_FAILURE, AdditionalInfo:\"It bombed!\"}`,
	},
	{
		Name:   "Test RevertImageUpdate",
		Method: "RevertImageUpdate", // rpc RevertImageUpdate(ImageDownload) returns(OperationResp)
		Param:  `{Id:\"ABC123ZYX\", Name:\"Image-1\", Crc: 54321}`,
		Expect: `{Code:voltha.OperationResp_OPERATION_FAILURE, AdditionalInfo:\"It bombed!\"}`,
	},
	{
		Name:   "Test UpdateDevicePmConfigs",
		Method: "UpdateDevicePmConfigs", // rpc UpdateDevicePmConfigs(voltha.PmConfigs) returns(google.protobuf.Empty)
		Param:  `{Id:\"CBA123ZYX\", DefaultFreq:1000000, Grouped: false, FreqOverride: false}`,
		Expect: `{}`,
	},
	{
		Name:   "Test CreateEventFilter",
		Method: "CreateEventFilter", // rpc CreateEventFilter(EventFilter) returns(EventFilter)
		//		Param:  `{Id:\"abc123xyz\", Rules:[]*voltha.EventFilterRule{&voltha.AlarmFilterRule{Key:voltha.AlarmFilterRuleKey_type, Value:\"Type man, it's the type!\"}, &voltha.AlarmFilterRule{Key:voltha.AlarmFilterRuleKey_category, Value:\"Category yeah!\"}}}`,
		Param:  `{Id:\"abc123xyz\", DeviceId: \"Device123\", Enable: True, EventType: \"device_event\", Rules:[]*voltha.EventFilterRule{&voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_category, Value:\"Equipment\"}, &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_sub_category, Value:\"ONU\"}, &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_device_event_type, Value:\"dying_gasp_event\"}}}`,
		Expect: `{Id:\"abc123xyz\", DeviceId: \"Device123\", Enable: True, EventType: \"device_event\", Rules:[]*voltha.EventFilterRule{&voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_category, Value:\"Equipment\"},     &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_sub_category, Value:\"ONU\"}, &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_device_event_type, Value:\"dying_gasp_event\"}}}`,
	},
	{
		Name:   "Test UpdateEventFilter",
		Method: "UpdateEventFilter", // rpc UpdateEventFilter(EventFilter) returns(EventFilter)
		Param:  `{Id:\"abc123xyz\", DeviceId: \"Device123\", Enable: True, EventType: \"device_event\", Rules:[]*voltha.EventFilterRule{&voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_category, Value:\"Equipment\"}, &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_sub_category, Value:\"ONU\"}, &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_device_event_type, Value:\"dying_gasp_event\"}}}`,
		Expect: `{Id:\"abc123xyz\", DeviceId: \"Device123\", Enable: True, EventType: \"device_event\", Rules:[]*voltha.EventFilterRule{&voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_category, Value:\"Equipment\"}, &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_sub_category, Value:\"ONU\"}, &voltha.EventFilterRule{Key:voltha.EventFilterRuleKey_device_event_type, Value:\"dying_gasp_event\"}}}`,
	},
	{
		Name:   "Test DeleteEventFilter",
		Method: "DeleteEventFilter", // rpc DeleteEventFilter(EventFilter) returns(google.protobuf.Empty)
		Param:  `{Id:\"acb123xyz\"}`,
		Expect: `{}`,
	},
	{
		Name:   "Test SelfTest",
		Method: "SelfTest", // rpc SelfTest(ID) returns(SelfTestResponse)
		Param:  `{Id:\"bac123xyz\"}`,
		Expect: `{Result:voltha.SelfTestResponse_UNKNOWN_ERROR}`,
	},
} /*
//    "StreamPacketsOut", // rpc StreamPacketsOut(stream openflow_13.PacketOut)
//    "ReceivePacketsIn", // rpc ReceivePacketsIn(google.protobuf.Empty)
//    "ReceiveChangeEvents", // rpc ReceiveChangeEvents(google.protobuf.Empty)
    "Subscribe ", // rpc Subscribe (OfAgentSubscriber) returns (OfAgentSubscriber)

}
*/

func dumpFile(fileNm string) error {
	// Dump the generated file for debugging purposes
	if c, err := ioutil.ReadFile(fileNm); err == nil {
		fmt.Print(string(c))
	} else {
		e := errors.New(fmt.Sprintf("Could not read the file '%s', %v", fileNm, err))
		return e
	}
	return nil
}

func main() {

	//var rwAry []test
	//var roAry []test
	var serNo int = 0

	// Setup logging
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	core := 0
	for k, _ := range roMethods {
		roMethods[k].SerNo = serNo
		roMethods[k].Core = (core % 3) + 1
		log.Infof("Processing method %s, serNo: %d", roMethods[k].Method, roMethods[k].SerNo)
		serNo++
		core++
	}

	// New round robin starts here because of the different route
	core = 0
	for k, _ := range rwMethods {
		rwMethods[k].SerNo = serNo
		rwMethods[k].Core = (core % 3) + 1
		log.Infof("Processing method %s, serNo: %d", rwMethods[k].Method, rwMethods[k].SerNo)
		serNo++
		core++
	}

	// New round robin starts here because of the different route
	core = 0
	for k, _ := range ctlMethods {
		ctlMethods[k].SerNo = serNo
		ctlMethods[k].Core = (core % 3) + 1
		log.Infof("Processing method %s, serNo: %d", ctlMethods[k].Method, ctlMethods[k].SerNo)
		serNo++
		core++
	}

	tsts := tests{RoTests: roMethods, RwTests: rwMethods, CtlTests: ctlMethods}

	t := template.Must(template.New("").ParseFiles("./test3.tmpl.json"))
	if f, err := os.Create("test3.json"); err == nil {
		_ = f
		defer f.Close()
		if err := t.ExecuteTemplate(f, "test3.tmpl.json", tsts); err != nil {
			log.Errorf("Unable to execute template for test3.tmpl.json: %v", err)
		}
	} else {
		log.Errorf("Couldn't create file test3.json: %v", err)
	}

	// Dump the generated file for debugging purposes
	//if err := dumpFile("test3.json"); err != nil {
	//	log.Error(err)
	//}

}
