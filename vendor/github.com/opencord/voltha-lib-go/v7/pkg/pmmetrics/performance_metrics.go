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

package pmmetrics

import (
	"github.com/opencord/voltha-protos/v5/go/voltha"
)

// PmMetrics structure holds metric and device info
type PmMetrics struct {
	deviceID          string
	frequency         uint32
	grouped           bool
	frequencyOverride bool
	metrics           map[string]*voltha.PmConfig
}

type PmMetricsOption func(*PmMetrics)

// Frequency is to poll stats at this interval
func Frequency(frequency uint32) PmMetricsOption {
	return func(args *PmMetrics) {
		args.frequency = frequency
	}
}

// GetSubscriberMetrics will return the metrics subscribed for the device.
func (pm *PmMetrics) GetSubscriberMetrics() map[string]*voltha.PmConfig {
	if pm == nil {
		return nil
	}
	return pm.metrics
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

// UpdateFrequency will update the frequency.
func (pm *PmMetrics) UpdateFrequency(frequency uint32) {
	pm.frequency = frequency
}

// Metrics will store the PMMetric params
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

// NewPmMetrics will return the pmmetric object
func NewPmMetrics(deviceID string, opts ...PmMetricsOption) *PmMetrics {
	pm := &PmMetrics{}
	pm.deviceID = deviceID
	for _, option := range opts {
		option(pm)
	}
	return pm
}

// ToPmConfigs will enable the defined pmmetric
func (pm *PmMetrics) ToPmConfigs() *voltha.PmConfigs {
	pmConfigs := &voltha.PmConfigs{
		Id:           pm.deviceID,
		DefaultFreq:  pm.frequency,
		Grouped:      pm.grouped,
		FreqOverride: pm.frequencyOverride,
	}
	for _, v := range pm.metrics {
		pmConfigs.Metrics = append(pmConfigs.Metrics, &voltha.PmConfig{Name: v.Name, Type: v.Type, Enabled: v.Enabled})
	}
	return pmConfigs
}
