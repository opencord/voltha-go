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

const (
	OnuAlarmType_OnuLossOfSignal = 0
	OnuAlarmType_OnuLossOfBurst  = 1
	OnuAlarmType_OnuLOPCMiss     = 2
	OnuAlarmType_OnuLOPCMicError = 3
)

type OnuAlarm struct {
	AlarmData    ab.AlarmData
	AlarmContext map[string]string
}

func GetNewOnuAlarm() OnuAlarm {
	var onulos OnuAlarm
	return onulos
}

func (losAlarm OnuAlarm) GetAlarmData(status bool, deviceId string, aType uint32) ab.AlarmData {
	var alarmData ab.AlarmData

	alarmData.Ts = float32(time.Now().UnixNano())
	alarmData.Description = FormatDescription("OLT_LOS_ALARM", true)
	switch aType {
	case OnuAlarmType_OnuLossOfSignal:
		alarmData.Id = FormatId("ONU_LOS_ALARM")
		alarmData.Name = "ONU_LOS_ALARM"
	case OnuAlarmType_OnuLossOfBurst:
		alarmData.Id = FormatId("ONU_LOB_ALARM")
		alarmData.Name = "ONU_LOB_ALARM"
	case OnuAlarmType_OnuLOPCMiss:
		alarmData.Id = FormatId("ONU_LOPC_MISS_ALARM")
		alarmData.Name = "ONU_LOPC_MISS_ALARM"
	case OnuAlarmType_OnuLOPCMicError:
		alarmData.Id = FormatId("ONU_LOPC_MIC_ERROR_ALARM")
		alarmData.Name = "ONU_LOPC_MIC_ERROR_ALARM"
	}
	alarmData.Category = voltha.AlarmEventCategory_ONU
	alarmData.Severity = voltha.AlarmEventSeverity_MAJOR
	alarmData.Type = voltha.AlarmEventType_COMMUNICATION
	alarmData.LogicalDeviceId = deviceId
	if status {
		alarmData.State = voltha.AlarmEventState_RAISED
	} else {
		alarmData.State = voltha.AlarmEventState_CLEARED
	}

	return alarmData
}

func (losAlarm OnuAlarm) GetContextData(onuId uint32, intfId string, deviceId string) map[string]string {
	alarmContext := make(map[string]string)
	alarmContext["onu-id"] = string(onuId)
	alarmContext["onu-intf-id"] = intfId

	return alarmContext
}

func (losAlarm OnuAlarm) FormatDescription(alarmName string, status bool) string {
	if status {
		return fmt.Sprintf("Alarm - %s - RAISED", alarmName)
	}
	return fmt.Sprintf("Alarm - %s - CLEARED", alarmName)
}

func (losAlarm OnuAlarm) FormatId(alarmName string) string {
	return fmt.Sprintf("Voltha.openolt.%s.%s", alarmName, string(time.Now().Nanosecond()))
}

func (losAlarm OnuAlarm) RaiseAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&losAlarm.AlarmData, losAlarm.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}

func (losAlarm OnuAlarm) ClearAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&losAlarm.AlarmData, losAlarm.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}
