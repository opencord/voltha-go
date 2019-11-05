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

// CallbackType is an enumerated value to express when a callback should be executed
type CallbackType uint8

// Enumerated list of callback types
const (
	GET CallbackType = iota
	PRE_UPDATE
	POST_UPDATE
	PRE_ADD
	POST_ADD
	PRE_REMOVE
	POST_REMOVE
	POST_LISTCHANGE
)

var enumCallbackTypes = []string{
	"GET",
	"PRE_UPDATE",
	"POST_UPDATE",
	"PRE_ADD",
	"POST_ADD",
	"PRE_REMOVE",
	"POST_REMOVE",
	"POST_LISTCHANGE",
}

func (t CallbackType) String() string {
	return enumCallbackTypes[t]
}
