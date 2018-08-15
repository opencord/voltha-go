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
package kafka

import (
	"context"
)

type callback func(bool, interface{})

// A Topic definition - may be augmented with additional attributes eventually
type Topic struct {
	// The name of the topic. It must start with a letter,
	// and contain only letters (`[A-Za-z]`), numbers (`[0-9]`), dashes (`-`),
	// underscores (`_`), periods (`.`), tildes (`~`), plus (`+`) or percent
	// signs (`%`).
	Name string
}

type KVArg struct {
	Key   string
	Value interface{}
}

// Client represents the set of APIs a Messaging Client must implement - In progress
type Client interface {
	Start()
	Stop()
	Subscribe(ctx context.Context, topic *Topic, cb callback, targetInterfaces ...interface{})
	Publish(ctx context.Context, rpc string, cb callback, topic *Topic, waitForResponse bool, kvArgs ...*KVArg)
	Unsubscribe(ctx context.Context, topic *Topic)
}
