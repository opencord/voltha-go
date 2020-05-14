package device

import (
	fu "github.com/opencord/voltha-lib-go/v3/pkg/flows"
	ofp "github.com/opencord/voltha-protos/v3/go/openflow_13"
	"github.com/opencord/voltha-protos/v3/go/voltha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (agent *LogicalAgent) addLogicalPortToMap(portNo uint32, nniPort bool) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	if exist := agent.logicalPortsNo[portNo]; !exist {
		agent.logicalPortsNo[portNo] = nniPort
	}
}

func (agent *LogicalAgent) addLogicalPortsToMap(lps []*voltha.LogicalPort) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, lp := range lps {
		if exist := agent.logicalPortsNo[lp.DevicePortNo]; !exist {
			agent.logicalPortsNo[lp.DevicePortNo] = lp.RootPort
		}
	}
}

func (agent *LogicalAgent) deleteLogicalPortsFromMap(portsNo []uint32) {
	agent.lockLogicalPortsNo.Lock()
	defer agent.lockLogicalPortsNo.Unlock()
	for _, pNo := range portsNo {
		delete(agent.logicalPortsNo, pNo)
	}
}

func (agent *LogicalAgent) isNNIPort(portNo uint32) bool {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	if exist := agent.logicalPortsNo[portNo]; exist {
		return agent.logicalPortsNo[portNo]
	}
	return false
}

func (agent *LogicalAgent) getFirstNNIPort() (uint32, error) {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	for portNo, nni := range agent.logicalPortsNo {
		if nni {
			return portNo, nil
		}
	}
	return 0, status.Error(codes.NotFound, "No NNI port found")
}

//GetNNIPorts returns NNI ports.
func (agent *LogicalAgent) GetNNIPorts() []uint32 {
	agent.lockLogicalPortsNo.RLock()
	defer agent.lockLogicalPortsNo.RUnlock()
	nniPorts := make([]uint32, 0)
	for portNo, nni := range agent.logicalPortsNo {
		if nni {
			nniPorts = append(nniPorts, portNo)
		}
	}
	return nniPorts
}

// getUNILogicalPortNo returns the UNI logical port number specified in the flow
func (agent *LogicalAgent) getUNILogicalPortNo(flow *ofp.OfpFlowStats) (uint32, error) {
	var uniPort uint32
	inPortNo := fu.GetInPort(flow)
	outPortNo := fu.GetOutPort(flow)
	if agent.isNNIPort(inPortNo) {
		uniPort = outPortNo
	} else if agent.isNNIPort(outPortNo) {
		uniPort = inPortNo
	}
	if uniPort != 0 {
		return uniPort, nil
	}
	return 0, status.Errorf(codes.NotFound, "no-uni-port: %v", flow)
}
