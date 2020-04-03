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

package common

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
)

func pmNamesInit() []string {
	pmNames := make([]string, 5)

	for i := 0; i < len(pmNames); i++ {
		pmNames[i] = fmt.Sprintf("pmName%d", i)
	}
	return pmNames
}
func TestNewPmMetrics(t *testing.T) {
	//pm := &PmMetrics{deviceId: "deviceId"}
	pm := NewPmMetrics("device1", Frequency(380000), Grouped(false))
	//t.Logf(" freq --> %d" , pm.frequency)
	assert.NotNil(t, pm)
	assert.Equal(t, "device1", pm.deviceId, "device error")
	assert.Equal(t, fmt.Sprint(380000), fmt.Sprint(pm.frequency), "frequency error")

	pmNames := pmNamesInit()

	pm2 := NewPmMetrics("device2", Frequency(380000), Grouped(false), FrequencyOverride(false), Metrics(pmNames))
	assert.NotNil(t, pm2)
}

func TestPmConfig(t *testing.T) {
	pm := NewPmMetrics("device3", Frequency(380000), Grouped(false), FrequencyOverride(false))
	assert.NotNil(t, pm)
	assert.Equal(t, "device3", pm.deviceId)
	assert.EqualValues(t, 380000, pm.frequency)
	assert.Equal(t, false, pm.frequencyOverride)
}
