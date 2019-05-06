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

type OnuSignalFailAlarm struct {
	AlarmData    ab.AlarmData
	AlarmContext map[string]string
}

func GetNewOnuSignalFailAlarm() OnuSignalFailAlarm {
	var sda OnuSignalFailAlarm
	return sda
}

func (sfa OnuSignalFailAlarm) GetAlarmData(status bool, deviceId string) ab.AlarmData {
	var alarmData ab.AlarmData
	alarmData.Ts = float32(time.Now().UnixNano())
	alarmData.Description = FormatDescription("ONU_SIGNAL_FAIL_ALARM", true)
	alarmData.Id = FormatId("ONU_SIGNAL_FAIL_ALARM")
	alarmData.Category = voltha.AlarmEventCategory_ONU
	alarmData.Severity = voltha.AlarmEventSeverity_MAJOR
	alarmData.Type = voltha.AlarmEventType_COMMUNICATION
	alarmData.LogicalDeviceId = deviceId
	if status {
		alarmData.State = voltha.AlarmEventState_RAISED
	} else {
		alarmData.State = voltha.AlarmEventState_CLEARED
	}
	alarmData.Name = "ONU_SIGNAL_FAIL_ALARM"

	return alarmData
}

func (sfa OnuSignalFailAlarm) GetContextData(onuId uint32, intfId string, inverseBitErrorRate uint32) map[string]string {
	alarmContext := make(map[string]string)
	alarmContext["onu-intf-id"] = intfId
	alarmContext["onu-id"] = string(onuId)
	alarmContext["inverse-bit-error-rate"] = string(inverseBitErrorRate)
	return alarmContext
}

func (sfa OnuSignalFailAlarm) FormatDescription(alarmName string, status bool) string {
	if status {
		return fmt.Sprintf("Alarm - %s - RAISED", alarmName)
	}
	return fmt.Sprintf("Alarm - %s - CLEARED", alarmName)
}

func (sfa OnuSignalFailAlarm) FormatId(alarmName string) string {
	return fmt.Sprintf("Voltha.openolt.%s.%s", alarmName, string(time.Now().Nanosecond()))
}

func (sfa OnuSignalFailAlarm) RaiseAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&sfa.AlarmData, sfa.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}

func (sfa OnuSignalFailAlarm) ClearAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&sfa.AlarmData, sfa.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}
