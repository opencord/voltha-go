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
// gRPC affinity router with active/active backends

package afrouter

import (
	"fmt"
	"sync"
	"google.golang.org/grpc"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
)

func Codec() grpc.Codec {
	return CodecWithParent(&protoCodec{})
}

func CodecWithParent(fallback grpc.Codec) grpc.Codec {
	return &rawCodec{fallback}
}

type rawCodec struct {
	parentCodec grpc.Codec
}

type sbFrame struct {
	payload []byte
	router Router
	method string
	be *backend
	lck sync.Mutex
	metaKey string
	metaVal string
}

type nbFrame struct {
	payload []byte
	router Router
	be *backend
	err error
	mthdSlice []string
	serNo chan uint64
	metaKey string
	metaVal string
}

func (c *rawCodec) Marshal(v interface{}) ([]byte, error) {
	switch t := v.(type) {
		case *sbFrame:
			return t.payload, nil
		case *nbFrame:
			return t.payload, nil
		default:
			return c.parentCodec.Marshal(v)
	}

}

func (c *rawCodec) Unmarshal(data []byte, v interface{}) error {
	switch t := v.(type) {
		case *sbFrame:
			t.payload = data
			// This is where the affinity is established on a northbound response
			t.router.ReplyHandler(v)
			return nil
		case *nbFrame:
			t.payload = data
			// This is were the afinity value is pulled from the payload
			// and the backend selected.
			t.be = t.router.Route(v)
			log.Debugf("Routing returned %v for method %s", t.be, t.mthdSlice[REQ_METHOD])
			return nil
		default:
			return c.parentCodec.Unmarshal(data,v)
	}
}

func (c *rawCodec) String() string {
	return fmt.Sprintf("proxy>%s", c.parentCodec.String())
}

// protoCodec is a Codec implementation with protobuf. It is the default rawCodec for gRPC.
type protoCodec struct{}

func (protoCodec) Marshal(v interface{}) ([]byte, error) {
	return proto.Marshal(v.(proto.Message))
}

func (protoCodec) Unmarshal(data []byte, v interface{}) error {
	return proto.Unmarshal(data, v.(proto.Message))
}

func (protoCodec) String() string {
	return "proto"
}
