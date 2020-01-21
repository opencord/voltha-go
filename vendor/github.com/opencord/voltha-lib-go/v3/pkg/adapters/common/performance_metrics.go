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
	"github.com/opencord/voltha-protos/v3/go/voltha"
)

type PmMetrics struct {
	deviceId          string
	frequency         uint32
	grouped           bool
	frequencyOverride bool
	metrics           map[string]*voltha.PmConfig
}

type PmMetricsOption func(*PmMetrics)

func Frequency(frequency uint32) PmMetricsOption {
	return func(args *PmMetrics) {
		args.frequency = frequency
	}
}

func Grouped(grouped bool) PmMetricsOption {
	return func(args *PmMetrics) {
		args.grouped = grouped
	}
}

func FrequencyOverride(frequencyOverride bool) PmMetricsOption {
	return func(args *PmMetrics) {
		args.frequencyOverride = frequencyOverride
	}
}

func Metrics(pmNames []string) PmMetricsOption {
	return func(args *PmMetrics) {
		args.metrics = make(map[string]*voltha.PmConfig)
		for _, name := range pmNames {
			args.metrics[name] = &voltha.PmConfig{
				Name:    name,
				Type:    voltha.PmConfig_COUNTER,
				Enabled: true,
			}
		}
	}
}

func NewPmMetrics(deviceId string, opts ...PmMetricsOption) *PmMetrics {
	pm := &PmMetrics{deviceId: deviceId}
	for _, option := range opts {
		option(pm)
	}
	return pm
}

func (pm *PmMetrics) ToPmConfigs() *voltha.PmConfigs {
	pmConfigs := &voltha.PmConfigs{
		Id:           pm.deviceId,
		DefaultFreq:  pm.frequency,
		Grouped:      pm.grouped,
		FreqOverride: pm.frequencyOverride,
	}
	for _, v := range pm.metrics {
		pmConfigs.Metrics = append(pmConfigs.Metrics, &voltha.PmConfig{Name: v.Name, Type: v.Type, Enabled: v.Enabled})
	}
	return pmConfigs
}
