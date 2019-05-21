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

package afrouter

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
	"google.golang.org/grpc"
	"sync"
)

func Codec() grpc.Codec {
	return CodecWithParent(&protoCodec{})
}

func CodecWithParent(parent grpc.Codec) grpc.Codec {
	return &transparentRoutingCodec{parent}
}

type transparentRoutingCodec struct {
	parentCodec grpc.Codec
}

// sbFrame is a frame being "returned" to whomever established the connection
type sbFrame struct {
	payload []byte
	router  Router
	method  string
	backend *backend
	mutex   sync.Mutex
	metaKey string
	metaVal string
}

// nbFrame is a frame coming in from whomever established the connection
type nbFrame struct {
	payload    []byte
	router     Router
	backend    *backend
	err        error
	methodInfo methodDetails
	serialNo   uint64
	metaKey    string
	metaVal    string
}

func (cdc *transparentRoutingCodec) Marshal(v interface{}) ([]byte, error) {
	switch t := v.(type) {
	case *sbFrame:
		return t.payload, nil
	case *nbFrame:
		return t.payload, nil
	default:
		return cdc.parentCodec.Marshal(v)
	}
}

func (cdc *transparentRoutingCodec) Unmarshal(data []byte, v interface{}) error {
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
		t.backend = t.router.Route(v)
		log.Debugf("Routing returned %v for method %s", t.backend, t.methodInfo.method)
		return nil
	default:
		return cdc.parentCodec.Unmarshal(data, v)
	}
}

func (cdc *transparentRoutingCodec) String() string {
	return fmt.Sprintf("%s", cdc.parentCodec.String())
}

// protoCodec is a Codec implementation with protobuf. It is the default Codec for gRPC.
type protoCodec struct{}

func (protoCodec) Marshal(v interface{}) ([]byte, error) {
	return proto.Marshal(v.(proto.Message))
}

func (protoCodec) Unmarshal(data []byte, v interface{}) error {
	return proto.Unmarshal(data, v.(proto.Message))
}

func (protoCodec) String() string {
	return "protoCodec"
}
