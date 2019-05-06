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
package onu_alarms

import (
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	ab "github.com/opencord/voltha-go/extentions/alarms"
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-protos/go/voltha"
	"time"
)

type OnuDiscoveryAlarm struct {
	AlarmData    ab.AlarmData
	AlarmContext map[string]string
}

func GetNewOnuDiscoveryAlarm() OnuDiscoveryAlarm {
	var onuDisc OnuDiscoveryAlarm
	return onuDisc
}

func (onuDiscAlarm OnuDiscoveryAlarm) GetAlarmData(status bool, deviceId string) ab.AlarmData {
	var alarmData ab.AlarmData
	alarmData.Ts = float32(time.Now().UnixNano())
	alarmData.Description = FormatDescription("ONU_DISCOVERED", true)
	alarmData.Id = FormatId("ONU_DISCOVERED")
	alarmData.Category = voltha.AlarmEventCategory_PON
	alarmData.Severity = voltha.AlarmEventSeverity_MAJOR
	alarmData.Type = voltha.AlarmEventType_COMMUNICATION
	alarmData.LogicalDeviceId = deviceId
	if status {
		alarmData.State = voltha.AlarmEventState_RAISED
	} else {
		alarmData.State = voltha.AlarmEventState_CLEARED
	}
	alarmData.Name = "ONU_DISCOVERED"

	return alarmData
}

func (onuDiscAlarm OnuDiscoveryAlarm) GetContextData(intfId uint32, serialNumber string) map[string]string {
	alarmContext := make(map[string]string)
	alarmContext["pon_id"] = string(intfId)
	alarmContext["serial_number"] = string(serialNumber)

	return alarmContext
}

func (onuDiscAlarm OnuDiscoveryAlarm) FormatDescription(alarmName string, status bool) string {
	if status {
		return fmt.Sprintf("Alarm - %s - RAISED", alarmName)
	}
	return fmt.Sprintf("Alarm - %s - CLEARED", alarmName)
}

func (onuDiscAlarm OnuDiscoveryAlarm) FormatId(alarmName string) string {
	return fmt.Sprintf("Voltha.openolt.%s.%s", alarmName, string(time.Now().Nanosecond()))
}

func (onuDiscAlarm OnuDiscoveryAlarm) RaiseAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&onuDiscAlarm.AlarmData, onuDiscAlarm.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}

func (onuDiscAlarm OnuDiscoveryAlarm) ClearAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&onuDiscAlarm.AlarmData, onuDiscAlarm.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}
