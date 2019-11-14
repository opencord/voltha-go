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

import (
	"runtime/debug"
	"sync"

	"github.com/opencord/voltha-lib-go/v2/pkg/db"
	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/opencord/voltha-protos/v2/go/voltha"
)

type ModelTestConfig struct {
	Root      *root
	Backend   *db.Backend
	RootProxy *Proxy
	DbPrefix  string
	DbType    string
	DbHost    string
	DbPort    int
	DbTimeout int
}

var callbackMutex sync.Mutex

func commonChanCallback(args ...interface{}) interface{} {
	log.Infof("Running common callback - arg count: %d", len(args))

	//for i := 0; i < len(args); i++ {
	//	log.Infof("ARG %d : %+v", i, args[i])
	//}

	callbackMutex.Lock()
	defer callbackMutex.Unlock()

	execDoneChan := args[1].(*chan struct{})

	// Inform the caller that the callback was executed
	if *execDoneChan != nil {
		log.Infof("Sending completion indication - stack:%s", string(debug.Stack()))
		close(*execDoneChan)
		*execDoneChan = nil
	}

	return nil
}

func commonCallback2(args ...interface{}) interface{} {
	log.Infof("Running common2 callback - arg count: %d %+v", len(args), args)

	return nil
}

func commonCallbackFunc(args ...interface{}) interface{} {
	log.Infof("Running common callback - arg count: %d", len(args))

	for i := 0; i < len(args); i++ {
		log.Infof("ARG %d : %+v", i, args[i])
	}
	execStatusFunc := args[1].(func(bool))

	// Inform the caller that the callback was executed
	execStatusFunc(true)

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
