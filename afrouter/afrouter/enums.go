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
	"encoding/json"
	"fmt"
)

type backendType int

const (
	BackendUndefined backendType = iota
	BackendActiveActive
	BackendSingleServer
)

var stringToBeType = map[string]backendType{"active_active": BackendActiveActive, "server": BackendSingleServer}
var beTypeToString = map[backendType]string{BackendActiveActive: "active_active", BackendSingleServer: "server"}

func (t backendType) MarshalJSON() ([]byte, error) {
	if t == BackendUndefined {
		return json.Marshal(nil)
	}
	if str, have := beTypeToString[t]; have {
		return json.Marshal(str)
	}
	return nil, fmt.Errorf("unknown %T '%d'", t, t)
}

func (t *backendType) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	var have bool
	if *t, have = stringToBeType[str]; !have {
		return fmt.Errorf("invalid %T %s", *t, str)
	}
	return nil
}

type associationLocation int

const (
	AssociationLocationUndefined associationLocation = iota
	AssociationLocationHeader
	AssociationLocationProtobuf
)

var stringToAlType = map[string]associationLocation{"header": AssociationLocationHeader, "protobuf": AssociationLocationProtobuf}
var alTypeToString = map[associationLocation]string{AssociationLocationHeader: "header", AssociationLocationProtobuf: "protobuf"}

func (t associationLocation) MarshalJSON() ([]byte, error) {
	if t == AssociationLocationUndefined {
		return json.Marshal(nil)
	}
	if str, have := alTypeToString[t]; have {
		return json.Marshal(str)
	}
	return nil, fmt.Errorf("unknown %T '%d'", t, t)
}

func (t *associationLocation) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	var have bool
	if *t, have = stringToAlType[str]; !have {
		return fmt.Errorf("invalid %T %s", *t, str)
	}
	return nil
}

type associationStrategy int

const (
	AssociationStrategyUndefined associationStrategy = iota
	AssociationStrategySerialNo
)

var stringToAsType = map[string]associationStrategy{"serial_number": AssociationStrategySerialNo}
var asTypeToString = map[associationStrategy]string{AssociationStrategySerialNo: "serial_number"}

func (t associationStrategy) MarshalJSON() ([]byte, error) {
	if t == AssociationStrategyUndefined {
		return json.Marshal(nil)
	}
	if str, have := asTypeToString[t]; have {
		return json.Marshal(str)
	}
	return nil, fmt.Errorf("unknown %T '%d'", t, t)
}

func (t *associationStrategy) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	var have bool
	if *t, have = stringToAsType[str]; !have {
		return fmt.Errorf("invalid %T %s", *t, str)
	}
	return nil
}

type backendSequence int

const (
	BackendSequenceRoundRobin backendSequence = iota
)

type routeType int

const (
	RouteTypeUndefined routeType = iota
	RouteTypeRpcAffinityMessage
	RouteTypeRpcAffinityHeader
	RouteTypeBinding
	RouteTypeRoundRobin
)

// String names for display in error messages.
var stringToRouteType = map[string]routeType{"rpc_affinity_message": RouteTypeRpcAffinityMessage, "rpc_affinity_header": RouteTypeRpcAffinityHeader, "binding": RouteTypeBinding, "round_robin": RouteTypeRoundRobin}
var routeTypeToString = map[routeType]string{RouteTypeRpcAffinityMessage: "rpc_affinity_message", RouteTypeRpcAffinityHeader: "rpc_affinity_header", RouteTypeBinding: "binding", RouteTypeRoundRobin: "round_robin"}

func (t routeType) String() string {
	if str, have := routeTypeToString[t]; have {
		return str
	}
	return fmt.Sprintf("%T(%d)", t, t)
}

func (t routeType) MarshalJSON() ([]byte, error) {
	if t == RouteTypeUndefined {
		return json.Marshal(nil)
	}
	if str, have := routeTypeToString[t]; have {
		return json.Marshal(str)
	}
	return nil, fmt.Errorf("unknown %T '%d'", t, t)
}

func (t *routeType) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	var have bool
	if *t, have = stringToRouteType[str]; !have {
		return fmt.Errorf("invalid %T %s", *t, str)
	}
	return nil
}

type associationType int

const (
	AssociationUndefined associationType = iota
	AssociationRoundRobin
)

var stringToAssociationType = map[string]associationType{"round_robin": AssociationRoundRobin}
var associationTypeToString = map[associationType]string{AssociationRoundRobin: "round_robin"}

func (t associationType) MarshalJSON() ([]byte, error) {
	if t == AssociationUndefined {
		return json.Marshal(nil)
	}
	if str, have := associationTypeToString[t]; have {
		return json.Marshal(str)
	}
	return nil, fmt.Errorf("unknown %T '%d'", t, t)
}

func (t *associationType) UnmarshalJSON(b []byte) error {
	var str string
	if err := json.Unmarshal(b, &str); err != nil {
		return err
	}
	var have bool
	if *t, have = stringToAssociationType[str]; !have {
		return fmt.Errorf("invalid %T %s", *t, str)
	}
	return nil
}
