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
	"fmt"
	"github.com/opencord/voltha-lib-go/v6/pkg/log"
	ofp "github.com/opencord/voltha-protos/v4/go/openflow_13"
	tp_pb "github.com/opencord/voltha-protos/v4/go/tech_profile"
)

// GetTrafficShapingInfo returns CIR,PIR and GIR values
func GetTrafficShapingInfo(ctx context.Context, meterConfig *ofp.OfpMeterConfig) (*tp_pb.TrafficShapingInfo, error) {
	switch meterBandSize := len(meterConfig.Bands); {
	case meterBandSize == 1:
		band := meterConfig.Bands[0]
		if band.BurstSize == 0 { // GIR, tcont type 1
			return &tp_pb.TrafficShapingInfo{Gir: band.Rate}, nil
		}
		return &tp_pb.TrafficShapingInfo{Pir: band.Rate, Pbs: band.BurstSize}, nil // PIR, tcont type 4
	case meterBandSize == 2:
		firstBand, secondBand := meterConfig.Bands[0], meterConfig.Bands[1]
		if firstBand.BurstSize == 0 && secondBand.BurstSize == 0 &&
			firstBand.Rate == secondBand.Rate { // PIR == GIR, tcont type 1
			return &tp_pb.TrafficShapingInfo{Pir: firstBand.Rate, Gir: secondBand.Rate}, nil
		}
		if firstBand.BurstSize > 0 && secondBand.BurstSize > 0 { // PIR, CIR, tcont type 2 or 3
			if firstBand.Rate > secondBand.Rate { // always PIR >= CIR
				return &tp_pb.TrafficShapingInfo{Pir: firstBand.Rate, Pbs: firstBand.BurstSize, Cir: secondBand.Rate, Cbs: secondBand.BurstSize}, nil
			}
			return &tp_pb.TrafficShapingInfo{Pir: secondBand.Rate, Pbs: secondBand.BurstSize, Cir: firstBand.Rate, Cbs: firstBand.BurstSize}, nil
		}
	case meterBandSize == 3: // PIR,CIR,GIR, tcont type 5
		var count, girIndex int
		for i, band := range meterConfig.Bands {
			if band.BurstSize == 0 { // find GIR
				count = count + 1
				girIndex = i
			}
		}
		if count == 1 {
			bands := make([]*ofp.OfpMeterBandHeader, len(meterConfig.Bands))
			copy(bands, meterConfig.Bands)
			pirCirBands := append(bands[:girIndex], bands[girIndex+1:]...)
			firstBand, secondBand := pirCirBands[0], pirCirBands[1]
			if firstBand.Rate > secondBand.Rate {
				return &tp_pb.TrafficShapingInfo{Pir: firstBand.Rate, Pbs: firstBand.BurstSize, Cir: secondBand.Rate, Cbs: secondBand.BurstSize, Gir: meterConfig.Bands[girIndex].Rate}, nil
			}
			return &tp_pb.TrafficShapingInfo{Pir: secondBand.Rate, Pbs: secondBand.BurstSize, Cir: firstBand.Rate, Cbs: firstBand.BurstSize, Gir: meterConfig.Bands[girIndex].Rate}, nil
		}
	default:
		logger.Errorw(ctx, "invalid-meter-config", log.Fields{"meter-config": meterConfig})
		return nil, fmt.Errorf("invalid-meter-config: %v", meterConfig)
	}
	return nil, fmt.Errorf("invalid-meter-config: %v", meterConfig)
}
