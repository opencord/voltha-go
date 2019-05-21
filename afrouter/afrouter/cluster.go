/*
 * Copyright 2019-present Open Networking Foundation

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

package afrouter

import (
	"errors"
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	"sync/atomic"
)

var clusters = make(map[string]*cluster)

// cluster a collection of HA backends
type cluster struct {
	name string
	//backends map[string]*backend
	backends        []*backend
	backendIDMap    map[*backend]int
	serialNoCounter uint64
}

//TODO: Move the backend type (active/active etc) to the cluster
// level. All backends should really be of the same type.
// Create a new backend cluster
func newBackendCluster(conf *BackendClusterConfig) (*cluster, error) {
	var err error = nil
	var rtrn_err = false
	var be *backend
	log.Debugf("Creating a backend cluster with %v", conf)
	// Validate the configuration
	if conf.Name == "" {
		log.Error("A backend cluster must have a name")
		rtrn_err = true
	}
	//bc :=  &cluster{name:conf.Name,backends:make(map[string]*backend)}
	bc := &cluster{name: conf.Name, backendIDMap: make(map[*backend]int)}
	clusters[bc.name] = bc
	idx := 0
	for _, bec := range conf.Backends {
		if bec.Name == "" {
			log.Errorf("A backend must have a name in cluster %s\n", conf.Name)
			rtrn_err = true
		}
		if be, err = newBackend(&bec, conf.Name); err != nil {
			log.Errorf("Error creating backend %s", bec.Name)
			rtrn_err = true
		}
		bc.backends = append(bc.backends, be)
		bc.backendIDMap[bc.backends[idx]] = idx
		idx++
	}
	if rtrn_err {
		return nil, errors.New("Error creating backend(s)")
	}
	return bc, nil
}

func (c *cluster) getBackend(name string) *backend {
	for _, v := range c.backends {
		if v.name == name {
			return v
		}
	}
	return nil
}

func (c *cluster) allocateSerialNumber() uint64 {
	return atomic.AddUint64(&c.serialNoCounter, 1) - 1
}

func (c *cluster) nextBackend(be *backend, seq backendSequence) (*backend, error) {
	switch seq {
	case BackendSequenceRoundRobin: // Round robin
		in := be
		// If no backend is found having a connection
		// then return nil.
		if be == nil {
			log.Debug("Previous backend is nil")
			be = c.backends[0]
			in = be
			if be.openConns != 0 {
				return be, nil
			}
		}
		for {
			log.Debugf("Requesting a new backend starting from %s", be.name)
			cur := c.backendIDMap[be]
			cur++
			if cur >= len(c.backends) {
				cur = 0
			}
			log.Debugf("Next backend is %d:%s", cur, c.backends[cur].name)
			if c.backends[cur].openConns > 0 {
				return c.backends[cur], nil
			}
			if c.backends[cur] == in {
				err := fmt.Errorf("No backend with open connections found")
				log.Debug(err)
				return nil, err
			}
			be = c.backends[cur]
			log.Debugf("Backend '%s' has no open connections, trying next", c.backends[cur].name)
		}
	default: // Invalid, default to round robin
		log.Errorf("Invalid backend sequence %d. Defaulting to round robin", seq)
		return c.nextBackend(be, BackendSequenceRoundRobin)
	}
}

func (c *cluster) handler(srv interface{}, serverStream grpc.ServerStream, r Router, methodInfo methodDetails,
	mk string, mv string) error {
	//func (c *cluster) handler(nbR * nbRequest) error {

	// The final backend cluster needs to be determined here. With non-affinity routed backends it could
	// just be determined here and for affinity routed backends the first message must be received
	// before the backend is determined. In order to keep things simple, the same approach is taken for
	// now.

	// Get the backend to use.
	// Allocate the nbFrame here since it holds the "context" of this communication
	nf := &nbFrame{router: r, methodInfo: methodInfo, serialNo: c.allocateSerialNumber(), metaKey: mk, metaVal: mv}
	log.Debugf("Nb frame allocate with method %s", nf.methodInfo.method)

	if be, err := c.assignBackend(serverStream, nf); err != nil {
		// At this point, no backend streams have been initiated
		// so just return the error.
		return err
	} else {
		log.Debugf("Backend '%s' selected", be.name)
		// Allocate a sbFrame here because it might be needed for return value intercept
		sf := &sbFrame{router: r, backend: be, method: nf.methodInfo.method, metaKey: mk, metaVal: mv}
		log.Debugf("Sb frame allocated with router %s", r.Name())
		return be.handler(srv, serverStream, nf, sf)
	}
}

func (c *cluster) assignBackend(src grpc.ServerStream, f *nbFrame) (*backend, error) {
	// Receive the first message from the server. This calls the assigned codec in which
	// Unmarshal gets executed. That will use the assigned router to select a backend
	// and add it to the frame
	if err := src.RecvMsg(f); err != nil {
		return nil, err
	}
	// Check that the backend was routable and actually has connections open.
	// If it doesn't then return a nil backend to indicate this
	if f.backend == nil {
		err := fmt.Errorf("Unable to route method '%s'", f.methodInfo.method)
		log.Error(err)
		return nil, err
	} else if f.backend.openConns == 0 {
		err := fmt.Errorf("No open connections on backend '%s'", f.backend.name)
		log.Error(err)
		return f.backend, err
	}
	return f.backend, nil
}
