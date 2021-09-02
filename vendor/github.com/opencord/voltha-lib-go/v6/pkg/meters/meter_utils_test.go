/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package meters

import (
	"context"
	"github.com/opencord/voltha-protos/v4/go/openflow_13"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMeters_TestTcontType1(t *testing.T) {
	//tcont-type-1
	meterConfig := &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 0,
			},
		},
	}
	shapingInfo, _ := GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Equal(t, uint32(10000), shapingInfo.Gir)

	//tcont-type-1
	meterConfig = &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 0,
			},
			{
				Rate:      10000,
				BurstSize: 0,
			},
		},
	}
	shapingInfo, _ = GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Equal(t, uint32(10000), shapingInfo.Pir)
	assert.Equal(t, uint32(10000), shapingInfo.Gir)
}

func TestMeters_TestTcontType2and3(t *testing.T) {
	meterConfig := &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 2000,
			},
			{
				Rate:      30000,
				BurstSize: 3000,
			},
		},
	}
	shapingInfo, _ := GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Equal(t, uint32(30000), shapingInfo.Pir)
	assert.Equal(t, uint32(10000), shapingInfo.Cir)
}

func TestMeters_TestTcontType4(t *testing.T) {
	meterConfig := &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 1000,
			},
		},
	}
	shapingInfo, _ := GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Equal(t, uint32(10000), shapingInfo.Pir)
}

func TestMeters_TestTcontType5(t *testing.T) {
	meterConfig := &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 0,
			},
			{
				Rate:      20000,
				BurstSize: 4000,
			},
			{
				Rate:      30000,
				BurstSize: 5000,
			},
		},
	}
	shapingInfo, _ := GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Equal(t, uint32(30000), shapingInfo.Pir)
	assert.Equal(t, uint32(10000), shapingInfo.Gir)
	assert.Equal(t, uint32(20000), shapingInfo.Cir)
	assert.Equal(t, uint32(5000), shapingInfo.Pbs)
}

func TestMeters_TestInvalidValues(t *testing.T) {
	// cir not found
	meterConfig := &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 0,
			},
			{
				Rate:      20000,
				BurstSize: 4000,
			},
		},
	}
	shapingInfo, _ := GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Nil(t, shapingInfo)

	// gir not found
	meterConfig = &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 3000,
			},
			{
				Rate:      20000,
				BurstSize: 4000,
			},
			{
				Rate:      30000,
				BurstSize: 5000,
			},
		},
	}
	shapingInfo, _ = GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Nil(t, shapingInfo)

	//gir not found
	meterConfig = &openflow_13.OfpMeterConfig{
		MeterId: 1,
		Bands: []*openflow_13.OfpMeterBandHeader{
			{
				Rate:      10000,
				BurstSize: 0,
			},
			{
				Rate:      20000,
				BurstSize: 0,
			},
			{
				Rate:      30000,
				BurstSize: 5000,
			},
		},
	}
	shapingInfo, _ = GetTrafficShapingInfo(context.Background(), meterConfig)
	assert.Nil(t, shapingInfo)
}
