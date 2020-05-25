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
	"errors"
	"fmt"

	"github.com/opencord/voltha-go/rw_core/route"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetRoute returns route
func (agent *LogicalAgent) GetRoute(ctx context.Context, ingressPortNo uint32, egressPortNo uint32) ([]route.Hop, error) {
	logger.Debugw("getting-route", log.Fields{"ingress-port": ingressPortNo, "egress-port": egressPortNo})
	routes := make([]route.Hop, 0)

	// Note: A port value of 0 is equivalent to a nil port

	//	Consider different possibilities
	if egressPortNo != 0 && ((egressPortNo & 0x7fffffff) == uint32(ofp.OfpPortNo_OFPP_CONTROLLER)) {
		logger.Debugw("controller-flow", log.Fields{"ingressPortNo": ingressPortNo, "egressPortNo": egressPortNo, "logicalPortsNo": agent.logicalPortsNo})
		if agent.isNNIPort(ingressPortNo) {
			//This is a trap on the NNI Port
			if len(agent.deviceRoutes.Routes) == 0 {
				// If there are no routes set (usually when the logical device has only NNI port(s), then just return an
				// route with same IngressHop and EgressHop
				hop := route.Hop{DeviceID: agent.rootDeviceID, Ingress: ingressPortNo, Egress: ingressPortNo}
				routes = append(routes, hop)
				routes = append(routes, hop)
				return routes, nil
			}
			//Return a 'half' route to make the flow decomposer logic happy
			for routeLink, path := range agent.deviceRoutes.Routes {
				if agent.isNNIPort(routeLink.Egress) {
					routes = append(routes, route.Hop{}) // first hop is set to empty
					routes = append(routes, path[1])
					return routes, nil
				}
			}
			return nil, fmt.Errorf("no upstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
		}
		//treat it as if the output port is the first NNI of the OLT
		var err error
		if egressPortNo, err = agent.getFirstNNIPort(); err != nil {
			logger.Warnw("no-nni-port", log.Fields{"error": err})
			return nil, err
		}
	}
	//If ingress port is not specified (nil), it may be a wildcarded
	//route if egress port is OFPP_CONTROLLER or a nni logical port,
	//in which case we need to create a half-route where only the egress
	//hop is filled, the first hop is nil
	if ingressPortNo == 0 && agent.isNNIPort(egressPortNo) {
		// We can use the 2nd hop of any upstream route, so just find the first upstream:
		for routeLink, path := range agent.deviceRoutes.Routes {
			if agent.isNNIPort(routeLink.Egress) {
				routes = append(routes, route.Hop{}) // first hop is set to empty
				routes = append(routes, path[1])
				return routes, nil
			}
		}
		return nil, fmt.Errorf("no upstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
	}
	//If egress port is not specified (nil), we can also can return a "half" route
	if egressPortNo == 0 {
		for routeLink, path := range agent.deviceRoutes.Routes {
			if routeLink.Ingress == ingressPortNo {
				routes = append(routes, path[0])
				routes = append(routes, route.Hop{})
				return routes, nil
			}
		}
		return nil, fmt.Errorf("no downstream route from:%d to:%d :%w", ingressPortNo, egressPortNo, route.ErrNoRoute)
	}
	//	Return the pre-calculated route
	return agent.getPreCalculatedRoute(ingressPortNo, egressPortNo)
}

func (agent *LogicalAgent) getPreCalculatedRoute(ingress, egress uint32) ([]route.Hop, error) {
	logger.Debugw("ROUTE", log.Fields{"len": len(agent.deviceRoutes.Routes)})
	for routeLink, route := range agent.deviceRoutes.Routes {
		logger.Debugw("ROUTELINKS", log.Fields{"ingress": ingress, "egress": egress, "routelink": routeLink})
		if ingress == routeLink.Ingress && egress == routeLink.Egress {
			return route, nil
		}
	}
	return nil, status.Errorf(codes.FailedPrecondition, "no route from:%d to:%d", ingress, egress)
}

// GetDeviceRoutes returns device graph
func (agent *LogicalAgent) GetDeviceRoutes() *route.DeviceRoutes {
	return agent.deviceRoutes
}

//generateDeviceRoutesIfNeeded generates the device routes if the logical device has been updated since the last time
//that device graph was generated.
func (agent *LogicalAgent) generateDeviceRoutesIfNeeded(ctx context.Context) error {
	agent.lockDeviceRoutes.Lock()
	defer agent.lockDeviceRoutes.Unlock()

	if agent.deviceRoutes != nil && agent.deviceRoutes.IsUpToDate(agent.listLogicalDevicePorts()) {
		return nil
	}
	logger.Debug("Generation of device route required")
	if err := agent.buildRoutes(ctx); err != nil {
		// No Route is not an error
		if !errors.Is(err, route.ErrNoRoute) {
			return err
		}
	}
	return nil
}

//rebuildRoutes rebuilds the device routes
func (agent *LogicalAgent) buildRoutes(ctx context.Context) error {
	logger.Debugf("building-routes", log.Fields{"logical-device-id": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if agent.deviceRoutes == nil {
		agent.deviceRoutes = route.NewDeviceRoutes(agent.logicalDeviceID, agent.deviceMgr.getDevice)
	}
	// Get all the logical ports on that logical device
	ports := agent.listLogicalDevicePorts()

	if err := agent.deviceRoutes.ComputeRoutes(ctx, ports); err != nil {
		return err
	}
	if err := agent.deviceRoutes.Print(); err != nil {
		return err
	}
	return nil
}

//updateRoutes updates the device routes
func (agent *LogicalAgent) updateRoutes(ctx context.Context, lp *voltha.LogicalPort) error {
	logger.Debugw("updateRoutes", log.Fields{"logicalDeviceId": agent.logicalDeviceID})
	if err := agent.requestQueue.WaitForGreenLight(ctx); err != nil {
		return err
	}
	defer agent.requestQueue.RequestComplete()

	if agent.deviceRoutes == nil {
		agent.deviceRoutes = route.NewDeviceRoutes(agent.logicalDeviceID, agent.deviceMgr.getDevice)
	}
	if err := agent.deviceRoutes.AddPort(ctx, lp, agent.listLogicalDevicePorts()); err != nil {
		return err
	}
	if err := agent.deviceRoutes.Print(); err != nil {
		return err
	}
	return nil
}
