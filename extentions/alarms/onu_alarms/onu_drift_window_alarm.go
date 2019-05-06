package onu_alarms

import (
	"fmt"
	"github.com/opencord/voltha-go/common/log"
	ab "github.com/opencord/voltha-go/extentions/alarms"
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-protos/go/voltha"
	"time"
)

type OnuDriftWindowAlarm struct {
	AlarmData    ab.AlarmData
	AlarmContext map[string]string
}

func GetNewOnuDriftWindowAlarm() OnuDriftWindowAlarm {
	var dwa OnuDriftWindowAlarm
	return dwa
}

func (dwa OnuDriftWindowAlarm) GetAlarmData(status bool, deviceId string) ab.AlarmData {
	var alarmData ab.AlarmData
	alarmData.Ts = float32(time.Now().UnixNano())
	alarmData.Description = FormatDescription("ONU_DRIFT_OF_WINDOW_ALARM", true)
	alarmData.Id = FormatId("ONU_DRIFT_OF_WINDOW_ALARM")
	alarmData.Category = voltha.AlarmEventCategory_ONU
	alarmData.Severity = voltha.AlarmEventSeverity_MAJOR
	alarmData.Type = voltha.AlarmEventType_COMMUNICATION
	alarmData.LogicalDeviceId = deviceId
	if status {
		alarmData.State = voltha.AlarmEventState_RAISED
	} else {
		alarmData.State = voltha.AlarmEventState_CLEARED
	}
	alarmData.Name = "OLT_DRIFT_OF_WINDOW_ALARM"

	return alarmData
}

func (dwa OnuDriftWindowAlarm) GetContextData(onuId uint32, intfId string, drift uint32, newEqd uint32) map[string]string {
	alarmContext := make(map[string]string)
	alarmContext["pon-id"] = intfId
	alarmContext["onu-id"] = string(onuId)
	alarmContext["drift"] = string(drift)
	alarmContext["NewEqd"] = string(newEqd)
	return alarmContext
}

func (dwa OnuDriftWindowAlarm) FormatDescription(alarmName string, status bool) string {
	if status {
		return fmt.Sprintf("Alarm - %s - RAISED", alarmName)
	}
	return fmt.Sprintf("Alarm - %s - CLEARED", alarmName)
}

func (dwa OnuDriftWindowAlarm) FormatId(alarmName string) string {
	return fmt.Sprintf("Voltha.openolt.%s.%s", alarmName, string(time.Now().Nanosecond()))
}

func (dwa OnuDriftWindowAlarm) RaiseAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&dwa.AlarmData, dwa.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}

func (dwa OnuDriftWindowAlarm) ClearAlarm(kc kafka.Client, topic kafka.Topic) error {
	ae := ab.CreateAlarmEvent(&dwa.AlarmData, dwa.AlarmContext)
	if err := ab.SendAlarm(kc, topic, ae); err != nil {
		log.Errorw("Failed to send alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name, "error": err})
		return err
	}
	log.Infow("Successfully sent alarm to kafka", log.Fields{"alarm_event": ae, "topic": topic.Name})
	return nil
}
