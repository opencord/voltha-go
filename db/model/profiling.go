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
	"github.com/opencord/voltha-go/common/log"
	"sync"
)

type profiling struct {
	DatabaseRetrieveTime  float64
	DatabaseRetrieveCount int
	InMemoryModelTime     float64
	InMemoryModelCount    int
	InMemoryProcessTime   float64
	DatabaseStoreTime     float64
	InMemoryLockTime      float64
	InMemoryLockCount     int
}

var profiling_instance *profiling
var profiling_once sync.Once

func GetProfiling() *profiling {
	profiling_once.Do(func() {
		profiling_instance = &profiling{}
	})
	return profiling_instance
}

func (p *profiling) AddToDatabaseRetrieveTime(period float64) {
	p.DatabaseRetrieveTime += period
	p.DatabaseRetrieveCount += 1
}
func (p *profiling) AddToInMemoryModelTime(period float64) {
	p.InMemoryModelTime += period
	p.InMemoryModelCount += 1
}
func (p *profiling) AddToInMemoryProcessTime(period float64) {
	p.InMemoryProcessTime += period
}
func (p *profiling) AddToDatabaseStoreTime(period float64) {
	p.DatabaseStoreTime += period
}

func (p *profiling) AddToInMemoryLockTime(period float64) {
	p.InMemoryLockTime += period
	p.InMemoryLockCount += 1
}

func (p *profiling) Reset() {
	p.DatabaseRetrieveTime = 0
	p.DatabaseRetrieveCount = 0
	p.InMemoryModelTime = 0
	p.InMemoryModelCount = 0
	p.InMemoryProcessTime = 0
	p.DatabaseStoreTime = 0
	p.InMemoryLockTime = 0
	p.InMemoryLockCount = 0
}

func (p *profiling) Report() {
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
