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
	"reflect"
	"testing"

	"github.com/opencord/voltha-lib-go/v2/pkg/log"
	"github.com/stretchr/testify/assert"
)

func TestProfiling(t *testing.T) {
	want := &profiling{}
	result := getProfiling()
	if reflect.TypeOf(result) != reflect.TypeOf(want) {
		t.Errorf("GetProfiling() = result: %v, want: %v", result, want)
	}

	/*
	 * GetProfiling() returns a singleton instance of the Profiling structure.
	 * Verifying this by interchangably calling other methods through above
	 * returned "result" instance and by again calling GetProfiling() method,
	 * and comparing the results, i.e. all are getting executed on the single
	 * same profiling instance.
	 */

	log.Info("/***** Unit Test Begin: Profiling Report: *****/")
	result.Report()

	getProfiling().AddToDatabaseRetrieveTime(2.0)
	assert.Equal(t, 2.0, result.DatabaseRetrieveTime)
	assert.Equal(t, 1, result.DatabaseRetrieveCount)
	result.AddToDatabaseRetrieveTime(3.0)
	assert.Equal(t, 5.0, getProfiling().DatabaseRetrieveTime)
	assert.Equal(t, 2, getProfiling().DatabaseRetrieveCount)

	getProfiling().AddToInMemoryModelTime(2.0)
	assert.Equal(t, 2.0, result.InMemoryModelTime)
	assert.Equal(t, 1, result.InMemoryModelCount)
	result.AddToInMemoryModelTime(3.0)
	assert.Equal(t, 5.0, getProfiling().InMemoryModelTime)
	assert.Equal(t, 2, getProfiling().InMemoryModelCount)

	getProfiling().AddToInMemoryProcessTime(2.0)
	assert.Equal(t, 2.0, result.InMemoryProcessTime)
	result.AddToInMemoryProcessTime(3.0)
	assert.Equal(t, 5.0, getProfiling().InMemoryProcessTime)

	getProfiling().AddToDatabaseStoreTime(2.0)
	assert.Equal(t, 2.0, result.DatabaseStoreTime)
	result.AddToDatabaseStoreTime(3.0)
	assert.Equal(t, 5.0, getProfiling().DatabaseStoreTime)

	getProfiling().AddToInMemoryLockTime(2.0)
	assert.Equal(t, 2.0, result.InMemoryLockTime)
	assert.Equal(t, 1, result.InMemoryLockCount)
	result.AddToInMemoryLockTime(3.0)
	assert.Equal(t, 5.0, getProfiling().InMemoryLockTime)
	assert.Equal(t, 2, getProfiling().InMemoryLockCount)

	log.Info("/***** Unit Test End: Profiling Report: *****/")
	getProfiling().Report()

	result.Reset()
}
