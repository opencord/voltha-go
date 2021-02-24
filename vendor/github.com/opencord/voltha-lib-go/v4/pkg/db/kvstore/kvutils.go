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
package kvstore

import (
	"bytes"
	"fmt"
)

// ToString converts an interface value to a string.  The interface should either be of
// a string type or []byte.  Otherwise, an error is returned.
func ToString(value interface{}) (string, error) {
	switch t := value.(type) {
	case []byte:
		return string(value.([]byte)), nil
	case string:
		return value.(string), nil
	default:
		return "", fmt.Errorf("unexpected-type-%T", t)
	}
}

// ToByte converts an interface value to a []byte.  The interface should either be of
// a string type or []byte.  Otherwise, an error is returned.
func ToByte(value interface{}) ([]byte, error) {
	switch t := value.(type) {
	case []byte:
		return value.([]byte), nil
	case string:
		return []byte(value.(string)), nil
	default:
		return nil, fmt.Errorf("unexpected-type-%T", t)
	}
}

// Helper function to verify mostly whether the content of two interface types are the same.  Focus is []byte and
// string types
func isEqual(val1 interface{}, val2 interface{}) bool {
	b1, err := ToByte(val1)
	b2, er := ToByte(val2)
	if err == nil && er == nil {
		return bytes.Equal(b1, b2)
	}
	return val1 == val2
}
