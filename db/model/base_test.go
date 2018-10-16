package model

import (
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/protos/common"
	"github.com/opencord/voltha-go/protos/openflow_13"
	"github.com/opencord/voltha-go/protos/voltha"
)

type ModelTestConfig struct {
	Root      *root
	Backend   *Backend
	RootProxy *Proxy
	DbPrefix  string
	DbType    string
	DbHost    string
	DbPort    int
	DbTimeout int
}

var (
	modelTestConfig = &ModelTestConfig{
		DbPrefix:  "service/voltha/data/core/0001",
		DbType:    "etcd",
		DbHost:    "localhost",
		//DbHost:    "10.106.153.44",
		DbPort:    2379,
		DbTimeout: 5,
	}

	ports = []*voltha.Port{
		{
			PortNo:     123,
			Label:      "test-port-0",
			Type:       voltha.Port_PON_OLT,
			AdminState: common.AdminState_ENABLED,
			OperStatus: common.OperStatus_ACTIVE,
			DeviceId:   "etcd_port-0-device-id",
			Peers:      []*voltha.Port_PeerPort{},
		},
	}

	stats = &openflow_13.OfpFlowStats{
		Id: 1111,
	}
	flows = &openflow_13.Flows{
		Items: []*openflow_13.OfpFlowStats{stats},
	}
	device = &voltha.Device{
		Id:         devId,
		Type:       "simulated_olt",
		Address:    &voltha.Device_HostAndPort{HostAndPort: "1.2.3.4:5555"},
		AdminState: voltha.AdminState_PREPROVISIONED,
		Flows:      flows,
		Ports:      ports,
	}
	devId          string
	targetDeviceId string
)

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, nil)
	log.UpdateAllLoggers(log.Fields{"instanceId": "MODEL_TEST"})

	defer log.CleanUp()

	modelTestConfig.Backend = NewBackend(
		modelTestConfig.DbType,
		modelTestConfig.DbHost,
		modelTestConfig.DbPort,
		modelTestConfig.DbTimeout,
		modelTestConfig.DbPrefix,
	)

	msgClass := &voltha.Voltha{}
	root := NewRoot(msgClass, modelTestConfig.Backend)

	if modelTestConfig.Backend != nil {
		modelTestConfig.Root = root.Load(msgClass)
	} else {
		modelTestConfig.Root = root
	}

	GetProfiling().Report()

	modelTestConfig.RootProxy = modelTestConfig.Root.GetProxy("/", false)
}

func commonCallback(args ...interface{}) interface{} {
	log.Infof("Running common callback - arg count: %s", len(args))

	for i := 0; i < len(args); i++ {
		log.Infof("ARG %d : %+v", i, args[i])
	}
	execStatus := args[1].(*bool)

	// Inform the caller that the callback was executed
	*execStatus = true

	return nil
}

func firstCallback(args ...interface{}) interface{} {
	name := args[0]
	id := args[1]
	log.Infof("Running first callback - name: %s, id: %s\n", name, id)
	return nil
}
func secondCallback(args ...interface{}) interface{} {
	name := args[0].(map[string]string)
	id := args[1]
	log.Infof("Running second callback - name: %s, id: %f\n", name["name"], id)
	// FIXME: the panic call seem to interfere with the logging mechanism
	//panic("Generating a panic in second callback")
	return nil
}
func thirdCallback(args ...interface{}) interface{} {
	name := args[0]
	id := args[1].(*voltha.Device)
	log.Infof("Running third callback - name: %+v, id: %s\n", name, id.Id)
	return nil
}
