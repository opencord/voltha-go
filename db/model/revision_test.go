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
	"github.com/opencord/voltha-go/protos/voltha"
	"testing"
)

func Test_Revision_01_New(t *testing.T) {
	branch := &Branch{}
	data := &voltha.Device{}
	children := make(map[string][]Revision)
	rev := NewNonPersistedRevision(nil, branch, data, children)

	t.Logf("New revision created: %+v\n", rev)
}
