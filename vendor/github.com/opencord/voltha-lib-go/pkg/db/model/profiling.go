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
	"github.com/opencord/voltha-lib-go/pkg/log"
	"sync"
)

// Profiling is used to store performance details collected at runtime
type profiling struct {
	sync.RWMutex
	DatabaseRetrieveTime  float64
	DatabaseRetrieveCount int
	InMemoryModelTime     float64
	InMemoryModelCount    int
	InMemoryProcessTime   float64
	DatabaseStoreTime     float64
	InMemoryLockTime      float64
	InMemoryLockCount     int
}

var profilingInstance *profiling
var profilingOnce sync.Once

// GetProfiling returns a singleton instance of the Profiling structure
func GetProfiling() *profiling {
	profilingOnce.Do(func() {
		profilingInstance = &profiling{}
	})
	return profilingInstance
}

// AddToDatabaseRetrieveTime appends a time period to retrieve data from the database
func (p *profiling) AddToDatabaseRetrieveTime(period float64) {
	p.Lock()
	defer p.Unlock()

	p.DatabaseRetrieveTime += period
	p.DatabaseRetrieveCount++
}

// AddToInMemoryModelTime appends a time period to construct/deconstruct data in memory
func (p *profiling) AddToInMemoryModelTime(period float64) {
	p.Lock()
	defer p.Unlock()

	p.InMemoryModelTime += period
	p.InMemoryModelCount++
}

// AddToInMemoryProcessTime appends a time period to process data
func (p *profiling) AddToInMemoryProcessTime(period float64) {
	p.Lock()
	defer p.Unlock()

	p.InMemoryProcessTime += period
}

// AddToDatabaseStoreTime appends a time period to store data in the database
func (p *profiling) AddToDatabaseStoreTime(period float64) {
	p.Lock()
	defer p.Unlock()

	p.DatabaseStoreTime += period
}

// AddToInMemoryLockTime appends a time period when a code block was locked
func (p *profiling) AddToInMemoryLockTime(period float64) {
	p.Lock()
	defer p.Unlock()

	p.InMemoryLockTime += period
	p.InMemoryLockCount++
}

// Reset initializes the profile counters
func (p *profiling) Reset() {
	p.Lock()
	defer p.Unlock()

	p.DatabaseRetrieveTime = 0
	p.DatabaseRetrieveCount = 0
	p.InMemoryModelTime = 0
	p.InMemoryModelCount = 0
	p.InMemoryProcessTime = 0
	p.DatabaseStoreTime = 0
	p.InMemoryLockTime = 0
	p.InMemoryLockCount = 0
}

// Report will provide the current profile counter status
func (p *profiling) Report() {
	p.Lock()
	defer p.Unlock()

	log.Infof("[ Profiling Report ]")
	log.Infof("Database Retrieval : %f", p.DatabaseRetrieveTime)
	log.Infof("Database Retrieval Count : %d", p.DatabaseRetrieveCount)
	log.Infof("Avg Database Retrieval : %f", p.DatabaseRetrieveTime/float64(p.DatabaseRetrieveCount))
	log.Infof("In-Memory Modeling : %f", p.InMemoryModelTime)
	log.Infof("In-Memory Modeling Count: %d", p.InMemoryModelCount)
	log.Infof("Avg In-Memory Modeling : %f", p.InMemoryModelTime/float64(p.InMemoryModelCount))
	log.Infof("In-Memory Locking : %f", p.InMemoryLockTime)
	log.Infof("In-Memory Locking Count: %d", p.InMemoryLockCount)
	log.Infof("Avg In-Memory Locking : %f", p.InMemoryLockTime/float64(p.InMemoryLockCount))

}
