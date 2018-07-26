package model

import (
	"encoding/json"
	"fmt"
	"github.com/opencord/voltha/protos/go/voltha"
	"testing"
	"time"
)

var (
	backend *Backend
	//rootPrefix = "service/voltha/data/core/0001"

	basePrefix  = "service/voltha/service/vcores/data/devices"
	deviceId    = "00016f13befaedcc"
	rootPrefix  = basePrefix + "/" + deviceId
	deviceProxy = "/devices/" + deviceId
)

func Test_NewRoot(t *testing.T) {
	backend = NewBackend(ETCD_KV, etcd_host, etcd_port, timeout, rootPrefix)

	//var msgClass *voltha.VolthaInstance
	var msgClass *voltha.DeviceInstance
	root := NewRoot(msgClass, backend, nil)

	start := time.Now()

	r := root.Load(msgClass)
	afterLoad := time.Now()
	fmt.Printf(">>>>>>>>>>>>> Time to load : %f\n", afterLoad.Sub(start).Seconds())

	d := r.Node.Get(deviceProxy, "", 0, false, "")
	afterGet := time.Now()
	fmt.Printf(">>>>>>>>>>>>> Time to load and get: %f\n", afterGet.Sub(start).Seconds())

	jr, _ := json.Marshal(r)
	fmt.Printf("Content of ROOT --> \n%s\n", jr)

	jd, _ := json.Marshal(d)
	fmt.Printf("Content of GET --> \n%s\n", jd)

}
