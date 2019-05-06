package alarms

import (
	"github.com/opencord/voltha-go/common/log"
	"github.com/opencord/voltha-go/kafka"
	"github.com/opencord/voltha-protos/go/voltha"
)

type AlarmEvent struct {
	Id              string
	Type            voltha.AlarmEventType_AlarmEventType
	Category        voltha.AlarmEventCategory_AlarmEventCategory
	State           voltha.AlarmEventState_AlarmEventState
	Severity        voltha.AlarmEventSeverity_AlarmEventSeverity
	RaisedTs        float32
	ReportedTs      float32
	ChangedTs       float32
	ResourceId      uint32
	Description     string
	Context         map[string]string
	LogicalDeviceId string
	AlarmTypeName   string
}

type AlarmData struct {
	Ts              float32
	Description     string
	Id              string
	Type            voltha.AlarmEventType_AlarmEventType
	Category        voltha.AlarmEventCategory_AlarmEventCategory
	Severity        voltha.AlarmEventSeverity_AlarmEventSeverity
	State           voltha.AlarmEventState_AlarmEventState
	AlarmType       string
	LogicalDeviceId string
	Name            string
	ResourceId      uint32
}

type AlarmBase interface {
	RaiseAlarm(kp kafka.Client, topic kafka.Topic) error
	ClearAlarm(kp kafka.Client, topic kafka.Topic) error
}

func CreateAlarmEvent(alarmData *AlarmData, contextData map[string]string) AlarmEvent {
	var ae AlarmEvent

	ae.Id = alarmData.Id
	ae.Type = alarmData.Type
	ae.Category = alarmData.Category
	ae.State = alarmData.State
	ae.Severity = alarmData.Severity
	ae.RaisedTs = alarmData.Ts
	ae.ResourceId = alarmData.ResourceId
	ae.Description = alarmData.Description
	ae.LogicalDeviceId = alarmData.LogicalDeviceId
	ae.AlarmTypeName = alarmData.Name
	ae.Context = contextData

	log.Debugw("Created alarm event", log.Fields{"id": ae.Id, "type": ae.Type, "category": ae.Category, "state": ae.State,
		"severity": ae.Severity, "raised_ts": ae.RaisedTs, "resource_id": ae.ResourceId,
		"description": ae.Description, "logical_device_id": ae.LogicalDeviceId,
		"alarm_type_name": ae.AlarmTypeName, "context": ae.Context})
	return ae
}

func SendAlarm(kc kafka.Client, topic kafka.Topic, ae AlarmEvent) error {
	msg := voltha.AlarmEvent{
		Id:              ae.Id,
		Type:            ae.Type,
		Category:        ae.Category,
		State:           ae.State,
		Severity:        ae.Severity,
		RaisedTs:        ae.RaisedTs,
		ResourceId:      string(ae.ResourceId),
		Description:     ae.Description,
		Context:         ae.Context,
		LogicalDeviceId: ae.LogicalDeviceId,
		AlarmTypeName:   ae.AlarmTypeName,
	}
	if err := kc.Send(&msg, &topic); err != nil {
		return err
	}
	log.Debugw("Sent alarm event to kafka", log.Fields{"id": ae.Id, "type": ae.Type, "category": ae.Category, "state": ae.State,
		"severity": ae.Severity, "raised_ts": ae.RaisedTs, "resource_id": ae.ResourceId,
		"description": ae.Description, "logical_device_id": ae.LogicalDeviceId,
		"alarm_type_name": ae.AlarmTypeName, "context": ae.Context})
	return nil
}
