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
package model

var (
	backend    *Backend
	rootPrefix = "service/voltha/data/core/0001"

	//basePrefix  = "service/voltha/service/vcores/data/devices"
	deviceId = "00016f13befaedcc"
	//rootPrefix  = basePrefix + "/" + deviceId
	deviceProxy = "/devices/" + deviceId
)

//func Test_NewRoot(t *testing.T) {
//	backend = NewBackend(ETCD_KV, etcd_host, etcd_port, timeout, rootPrefix)
//
//	var msgClass *voltha.Voltha
//	//var msgClass *voltha.DeviceInstance
//	root := NewRoot(msgClass, backend)
//
//	start := time.Now()
//
//	//r := root.Load(msgClass)
//	afterLoad := time.Now()
//	log.Infof(">>>>>>>>>>>>> Time to Load : %f\n", afterLoad.Sub(start).Seconds())
//
//	d := r.node.Get(deviceProxy, "", 0, false, "")
//	afterGet := time.Now()
//	log.Infof(">>>>>>>>>>>>> Time to Load and get: %f\n", afterGet.Sub(start).Seconds())
//
//	jr, _ := json.Marshal(r)
//	log.Infof("Content of ROOT --> \n%s\n", jr)
//
//	jd, _ := json.Marshal(d)
//	log.Infof("Content of GET --> \n%s\n", jd)
//
//}
