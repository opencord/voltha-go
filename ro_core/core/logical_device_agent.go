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
package core

import (
	"context"
	"fmt"
	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/db/model"
	ic "github.com/opencord/voltha-go/protos/inter_container"
	ofp "github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
	fd "github.com/opencord/voltha-go/ro_core/flow_decomposition"
	"github.com/opencord/voltha-go/ro_core/graph"
	fu "github.com/opencord/voltha-go/ro_core/utils"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"sync"
)

type LogicalDeviceAgent struct {
	logicalDeviceId   string
	lastData          *voltha.LogicalDevice
	rootDeviceId      string
	deviceMgr         *DeviceManager
	ldeviceMgr        *LogicalDeviceManager
	clusterDataProxy  *model.Proxy
	exitChannel       chan int
	deviceGraph       *graph.DeviceGraph
	DefaultFlowRules  *fu.DeviceRules
	flowProxy         *model.Proxy
	groupProxy        *model.Proxy
	lockLogicalDevice sync.RWMutex
}

func newLogicalDeviceAgent(id string, deviceId string, ldeviceMgr *LogicalDeviceManager,
	deviceMgr *DeviceManager,
	cdProxy *model.Proxy) *LogicalDeviceAgent {
	var agent LogicalDeviceAgent
	agent.exitChannel = make(chan int, 1)
	agent.logicalDeviceId = id
	agent.rootDeviceId = deviceId
	agent.deviceMgr = deviceMgr
	agent.clusterDataProxy = cdProxy
	agent.ldeviceMgr = ldeviceMgr
	agent.lockLogicalDevice = sync.RWMutex{}
	return &agent
}

// start creates the logical device and add it to the data model
func (agent *LogicalDeviceAgent) start(ctx context.Context) error {
	log.Infow("starting-logical_device-agent", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	//Build the logical device based on information retrieved from the device adapter
	var switchCap *ic.SwitchCapability
	var err error
	if switchCap, err = agent.deviceMgr.getSwitchCapability(ctx, agent.rootDeviceId); err != nil {
		log.Errorw("error-creating-logical-device", log.Fields{"error": err})
		return err
	}

	ld := &voltha.LogicalDevice{Id: agent.logicalDeviceId, RootDeviceId: agent.rootDeviceId}

	// Create the datapath ID (uint64) using the logical device ID (based on the MAC Address)
	var datapathID uint64
	if datapathID, err = CreateDataPathId(agent.logicalDeviceId); err != nil {
		log.Errorw("error-creating-datapath-id", log.Fields{"error": err})
		return err
	}
	ld.DatapathId = datapathID
	ld.Desc = (proto.Clone(switchCap.Desc)).(*ofp.OfpDesc)
	ld.SwitchFeatures = (proto.Clone(switchCap.SwitchFeatures)).(*ofp.OfpSwitchFeatures)
	ld.Flows = &ofp.Flows{Items: nil}
	ld.FlowGroups = &ofp.FlowGroups{Items: nil}

	//Add logical ports to the logical device based on the number of NNI ports discovered
	//First get the default port capability - TODO:  each NNI port may have different capabilities,
	//hence. may need to extract the port by the NNI port id defined by the adapter during device
	//creation
	var nniPorts *voltha.Ports
	if nniPorts, err = agent.deviceMgr.getPorts(ctx, agent.rootDeviceId, voltha.Port_ETHERNET_NNI); err != nil {
		log.Errorw("error-creating-logical-port", log.Fields{"error": err})
	}
	var portCap *ic.PortCapability
	for _, port := range nniPorts.Items {
		log.Infow("!!!!!!!NNI PORTS", log.Fields{"NNI": port})
		if portCap, err = agent.deviceMgr.getPortCapability(ctx, agent.rootDeviceId, port.PortNo); err != nil {
			log.Errorw("error-creating-logical-device", log.Fields{"error": err})
			return err
		}
		portCap.Port.RootPort = true
		lp := (proto.Clone(portCap.Port)).(*voltha.LogicalPort)
		lp.DeviceId = agent.rootDeviceId
		lp.Id = fmt.Sprintf("nni-%d", port.PortNo)
		lp.OfpPort.PortNo = port.PortNo
		lp.OfpPort.Name = portCap.Port.Id
		lp.DevicePortNo = port.PortNo
		ld.Ports = append(ld.Ports, lp)
	}
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	// Save the logical device
	if added := agent.clusterDataProxy.AddWithID("/logical_devices", ld.Id, ld, ""); added == nil {
		log.Errorw("failed-to-add-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	} else {
		log.Debugw("logicaldevice-created", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	}

	agent.flowProxy = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/flows", agent.logicalDeviceId),
		false)
	agent.groupProxy = agent.clusterDataProxy.Root.CreateProxy(
		fmt.Sprintf("/logical_devices/%s/flow_groups", agent.logicalDeviceId),
		false)

	return nil
}

// stop stops the logical devuce agent.  This removes the logical device from the data model.
func (agent *LogicalDeviceAgent) stop(ctx context.Context) {
	log.Info("stopping-logical_device-agent")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	//Remove the logical device from the model
	if removed := agent.clusterDataProxy.Remove("/logical_devices/"+agent.logicalDeviceId, ""); removed == nil {
		log.Errorw("failed-to-remove-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	} else {
		log.Debugw("logicaldevice-removed", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
	}
	agent.exitChannel <- 1
	log.Info("logical_device-agent-stopped")
}

// GetLogicalDevice locks the logical device model and then retrieves the latest logical device information
func (agent *LogicalDeviceAgent) GetLogicalDevice() (*voltha.LogicalDevice, error) {
	log.Debug("GetLogicalDevice")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func (agent *LogicalDeviceAgent) ListLogicalDevicePorts() (*voltha.LogicalPorts, error) {
	log.Debug("!!!!!ListLogicalDevicePorts")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		lPorts := make([]*voltha.LogicalPort, 0)
		for _, port := range lDevice.Ports {
			lPorts = append(lPorts, port)
		}
		return &voltha.LogicalPorts{Items: lPorts}, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

// listFlows locks the logical device model and then retrieves the latest flow information
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlows() (*voltha.Flows, error) {
	log.Debug("listFlows")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId+"/flows", 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.Flows, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

// listFlowGroups locks the logical device model and then retrieves the latest flow groups information
func (agent *LogicalDeviceAgent) ListLogicalDeviceFlowGroups() (*voltha.FlowGroups, error) {
	log.Debug("listFlowGroups")
	agent.lockLogicalDevice.Lock()
	defer agent.lockLogicalDevice.Unlock()
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice.FlowGroups, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

// getLogicalDeviceWithoutLock retrieves a logical device from the model without locking it.   This is used only by
// functions that have already acquired the logical device lock to the model
func (agent *LogicalDeviceAgent) getLogicalDeviceWithoutLock() (*voltha.LogicalDevice, error) {
	log.Debug("getLogicalDeviceWithoutLock")
	logicalDevice := agent.clusterDataProxy.Get("/logical_devices/"+agent.logicalDeviceId, 1, false, "")
	if lDevice, ok := logicalDevice.(*voltha.LogicalDevice); ok {
		return lDevice, nil
	}
	return nil, status.Errorf(codes.NotFound, "logical_device-%s", agent.logicalDeviceId)
}

func isNNIPort(portNo uint32, nniPortsNo []uint32) bool {
	for _, pNo := range nniPortsNo {
		if pNo == portNo {
			return true
		}
	}
	return false
}

func (agent *LogicalDeviceAgent) getPreCalculatedRoute(ingress, egress uint32) []graph.RouteHop {
	log.Debugw("ROUTE", log.Fields{"len": len(agent.deviceGraph.Routes)})
	for routeLink, route := range agent.deviceGraph.Routes {
		log.Debugw("ROUTELINKS", log.Fields{"ingress": ingress, "egress": egress, "routelink": routeLink})
		if ingress == routeLink.Ingress && egress == routeLink.Egress {
			return route
		}
	}
	log.Warnw("no-route", log.Fields{"logicalDeviceId": agent.logicalDeviceId, "ingress": ingress, "egress": egress})
	return nil
}

func (agent *LogicalDeviceAgent) GetRoute(ingressPortNo uint32, egressPortNo uint32) []graph.RouteHop {
	log.Debugw("getting-route", log.Fields{"ingress-port": ingressPortNo, "egress-port": egressPortNo})
	// Get the updated logical device
	var ld *ic.LogicalDevice
	routes := make([]graph.RouteHop, 0)
	var err error
	if ld, err = agent.getLogicalDeviceWithoutLock(); err != nil {
		return nil
	}
	nniLogicalPortsNo := make([]uint32, 0)
	for _, logicalPort := range ld.Ports {
		if logicalPort.RootPort {
			nniLogicalPortsNo = append(nniLogicalPortsNo, logicalPort.OfpPort.PortNo)
		}
	}
	if len(nniLogicalPortsNo) == 0 {
		log.Errorw("no-nni-ports", log.Fields{"LogicalDeviceId": ld.Id})
		return nil
	}
	// Note: A port value of 0 is equivalent to a nil port

	//	Consider different possibilities
	if egressPortNo != 0 && ((egressPortNo & 0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)) {
		log.Debugw("controller-flow", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
		if isNNIPort(ingressPortNo, nniLogicalPortsNo) {
			log.Debug("returning-half-route")
			//This is a trap on the NNI Port
			//Return a 'half' route to make the flow decomposer logic happy
			for routeLink, route := range agent.deviceGraph.Routes {
				if isNNIPort(routeLink.Egress, nniLogicalPortsNo) {
					routes = append(routes, graph.RouteHop{}) // first hop is set to empty
					routes = append(routes, route[1])
					return routes
				}
			}
			log.Warnw("no-upstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
			return nil
		}
		//treat it as if the output port is the first NNI of the OLT
		egressPortNo = nniLogicalPortsNo[0]
	}
	//If ingress port is not specified (nil), it may be a wildcarded
	//route if egress port is OFPP_CONTROLLER or a nni logical port,
	//in which case we need to create a half-route where only the egress
	//hop is filled, the first hop is nil
	if ingressPortNo == 0 && isNNIPort(egressPortNo, nniLogicalPortsNo) {
		// We can use the 2nd hop of any upstream route, so just find the first upstream:
		for routeLink, route := range agent.deviceGraph.Routes {
			if isNNIPort(routeLink.Egress, nniLogicalPortsNo) {
				routes = append(routes, graph.RouteHop{}) // first hop is set to empty
				routes = append(routes, route[1])
				return routes
			}
		}
		log.Warnw("no-upstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
		return nil
	}
	//If egress port is not specified (nil), we can also can return a "half" route
	if egressPortNo == 0 {
		for routeLink, route := range agent.deviceGraph.Routes {
			if routeLink.Ingress == ingressPortNo {
				routes = append(routes, route[0])
				routes = append(routes, graph.RouteHop{})
				return routes
			}
		}
		log.Warnw("no-downstream-route", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "nniPortsNo": nniLogicalPortsNo})
		return nil
	}

	//	Return the pre-calculated route
	return agent.getPreCalculatedRoute(ingressPortNo, egressPortNo)
}

// updateRoutes updates the device routes whenever there is a device or port changes relevant to this
// logical device.   TODO: Add more heuristics to this process to update the routes where a change has occurred
// instead of rebuilding the entire set of routes
func (agent *LogicalDeviceAgent) updateRoutes() {
	if ld, err := agent.GetLogicalDevice(); err == nil {
		agent.deviceGraph.ComputeRoutes(ld.Ports)
	}
}

func (agent *LogicalDeviceAgent) rootDeviceDefaultRules() *fu.FlowsAndGroups {
	return fu.NewFlowsAndGroups()
}

func (agent *LogicalDeviceAgent) leafDeviceDefaultRules(deviceId string) *fu.FlowsAndGroups {
	fg := fu.NewFlowsAndGroups()
	var device *voltha.Device
	var err error
	if device, err = agent.deviceMgr.GetDevice(deviceId); err != nil {
		return fg
	}
	//set the upstream and downstream ports
	upstreamPorts := make([]*voltha.Port, 0)
	downstreamPorts := make([]*voltha.Port, 0)
	for _, port := range device.Ports {
		if port.Type == voltha.Port_PON_ONU || port.Type == voltha.Port_VENET_ONU {
			upstreamPorts = append(upstreamPorts, port)
		} else if port.Type == voltha.Port_ETHERNET_UNI {
			downstreamPorts = append(downstreamPorts, port)
		}
	}
	//it is possible that the downstream ports are not created, but the flow_decomposition has already
	//kicked in. In such scenarios, cut short the processing and return.
	if len(downstreamPorts) == 0 {
		return fg
	}
	// set up the default flows
	var fa *fu.FlowArgs
	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fd.InPort(downstreamPorts[0].PortNo),
			fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0),
		},
		Actions: []*ofp.OfpAction{
			fd.SetField(fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | device.Vlan)),
			fd.Output(upstreamPorts[0].PortNo),
		},
	}
	fg.AddFlow(fd.MkFlowStat(fa))

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fd.InPort(downstreamPorts[0].PortNo),
			fd.VlanVid(0),
		},
		Actions: []*ofp.OfpAction{
			fd.PushVlan(0x8100),
			fd.SetField(fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | device.Vlan)),
			fd.Output(upstreamPorts[0].PortNo),
		},
	}
	fg.AddFlow(fd.MkFlowStat(fa))

	fa = &fu.FlowArgs{
		KV: fu.OfpFlowModArgs{"priority": 500},
		MatchFields: []*ofp.OfpOxmOfbField{
			fd.InPort(upstreamPorts[0].PortNo),
			fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | device.Vlan),
		},
		Actions: []*ofp.OfpAction{
			fd.SetField(fd.VlanVid(uint32(ofp.OfpVlanId_OFPVID_PRESENT) | 0)),
			fd.Output(downstreamPorts[0].PortNo),
		},
	}
	fg.AddFlow(fd.MkFlowStat(fa))

	return fg
}

func (agent *LogicalDeviceAgent) generateDefaultRules() *fu.DeviceRules {
	rules := fu.NewDeviceRules()
	var ld *voltha.LogicalDevice
	var err error
	if ld, err = agent.GetLogicalDevice(); err != nil {
		log.Warnw("no-logical-device", log.Fields{"logicaldeviceId": agent.logicalDeviceId})
		return rules
	}

	deviceNodeIds := agent.deviceGraph.GetDeviceNodeIds()
	for deviceId := range deviceNodeIds {
		if deviceId == ld.RootDeviceId {
			rules.AddFlowsAndGroup(deviceId, agent.rootDeviceDefaultRules())
		} else {
			rules.AddFlowsAndGroup(deviceId, agent.leafDeviceDefaultRules(deviceId))
		}
	}
	return rules
}

func (agent *LogicalDeviceAgent) GetAllDefaultRules() *fu.DeviceRules {
	// Get latest
	var lDevice *voltha.LogicalDevice
	var err error
	if lDevice, err = agent.GetLogicalDevice(); err != nil {
		return fu.NewDeviceRules()
	}
	if agent.DefaultFlowRules == nil { // Nothing setup yet
		agent.deviceGraph = graph.NewDeviceGraph(agent.deviceMgr.GetDevice)
		agent.deviceGraph.ComputeRoutes(lDevice.Ports)
		agent.DefaultFlowRules = agent.generateDefaultRules()
	}
	return agent.DefaultFlowRules
}

func (agent *LogicalDeviceAgent) GetWildcardInputPorts(excludePort ...uint32) []uint32 {
	lPorts := make([]uint32, 0)
	var exclPort uint32
	if len(excludePort) == 1 {
		exclPort = excludePort[0]
	}
	if lDevice, _ := agent.GetLogicalDevice(); lDevice != nil {
		for _, port := range lDevice.Ports {
			if port.OfpPort.PortNo != exclPort {
				lPorts = append(lPorts, port.OfpPort.PortNo)
			}
		}
	}
	return lPorts
}

func (agent *LogicalDeviceAgent) GetDeviceGraph() *graph.DeviceGraph {
	return agent.deviceGraph
}