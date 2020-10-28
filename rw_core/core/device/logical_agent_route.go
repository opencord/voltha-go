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

package device

import (
	"context"
	"fmt"

	"github.com/opencord/voltha-go/rw_core/route"
	"github.com/opencord/voltha-lib-go/v4/pkg/log"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/opencord/voltha-protos/v4/go/voltha"
)

// GetRoute returns a route
func (agent *LogicalAgent) GetRoute(ctx context.Context, ingressPortNo uint32, egressPortNo uint32) ([]route.Hop, error) {
	logger.Debugw(ctx, "getting-route", log.Fields{"ingress-port": ingressPortNo, "egress-port": egressPortNo})
	routes := make([]route.Hop, 0)

	// Note: A port value of 0 is equivalent to a nil port

	// Controller-bound flow
	if egressPortNo != 0 && ((egressPortNo & 0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)) {
		logger.Debugw(ctx, "controller-flow", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo})
		if agent.isNNIPort(ingressPortNo) {
			//This is a trap on the NNI Port
			if agent.deviceRoutes.IsRoutesEmpty() {
				// If there are no routes set (usually when the logical device has only NNI port(s), then just return an
				// route with same IngressHop and EgressHop
				hop := route.Hop{DeviceID: agent.rootDeviceID, Ingress: ingressPortNo, Egress: ingressPortNo}
				routes = append(routes, hop)
				routes = append(routes, hop)
				return routes, nil
			}

			// Return a 'half' route to make the flow decomposer logic happy
			routes, err := agent.deviceRoutes.GetHalfRoute(true, 0, 0)
			if err != nil {
				return nil, fmt.Errorf("no upstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
			}
			return routes, nil
		}
		// Treat it as if the output port is the first NNI of the OLT
		var err error
		if egressPortNo, err = agent.getAnyNNIPort(); err != nil {
			logger.Warnw(ctx, "no-nni-port", log.Fields{"error": err})
			return nil, err
		}
	}

	//If ingress port is not specified (nil), it may be a wildcarded route if egress port is OFPP_CONTROLLER or a nni
	// logical port, in which case we need to create a half-route where only the egress hop is filled, the first hop is nil
	if ingressPortNo == 0 && agent.isNNIPort(egressPortNo) {
		routes, err := agent.deviceRoutes.GetHalfRoute(true, ingressPortNo, egressPortNo)
		if err != nil {
			return nil, fmt.Errorf("no upstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
		}
		return routes, nil
	}

	//If egress port is not specified (nil), we can also can return a "half" route
	if egressPortNo == 0 {
		routes, err := agent.deviceRoutes.GetHalfRoute(false, ingressPortNo, egressPortNo)
		if err != nil {
			return nil, fmt.Errorf("no downstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
		}
		return routes, nil
	}

	//	Return the pre-calculated route
	return agent.deviceRoutes.GetRoute(ctx, ingressPortNo, egressPortNo)
}

// GetDeviceRoutes returns device graph
func (agent *LogicalAgent) GetDeviceRoutes() *route.DeviceRoutes {
	return agent.deviceRoutes
}

//rebuildRoutes rebuilds the device routes
func (agent *LogicalAgent) buildRoutes(ctx context.Context) error {
	logger.Debugf(ctx, "building-routes", log.Fields{"logical-device-id": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if err := agent.deviceRoutes.ComputeRoutes(ctx, agent.listLogicalDevicePorts(ctx)); err != nil {
		return err
	}
	if err := agent.deviceRoutes.Print(ctx); err != nil {
		return err
	}
	return nil
}

//updateRoutes updates the device routes
func (agent *LogicalAgent) updateRoutes(ctx context.Context, deviceID string, devicePorts map[uint32]*voltha.Port, lp *voltha.LogicalPort, lps map[uint32]*voltha.LogicalPort) error {
	logger.Debugw(ctx, "updateRoutes", log.Fields{"logical-device-id": agent.logicalDeviceID, "device-id": deviceID, "port:": lp})

	if err := agent.deviceRoutes.AddPort(ctx, lp, deviceID, devicePorts, lps); err != nil {
		return err
	}
	if err := agent.deviceRoutes.Print(ctx); err != nil {
		return err
	}
	return nil
}

//updateAllRoutes updates the device routes using all the logical ports on that device
func (agent *LogicalAgent) updateAllRoutes(ctx context.Context, deviceID string, devicePorts map[uint32]*voltha.Port) error {
	logger.Debugw(ctx, "updateAllRoutes", log.Fields{"logical-device-id": agent.logicalDeviceID, "device-id": deviceID, "ports-count": len(devicePorts)})

	if err := agent.deviceRoutes.AddAllPorts(ctx, deviceID, devicePorts, agent.listLogicalDevicePorts(ctx)); err != nil {
		return err
	}
	if err := agent.deviceRoutes.Print(ctx); err != nil {
		return err
	}
	return nil
}
