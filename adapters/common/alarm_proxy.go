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
package common

import (
	"errors"
	"github.com/opencord/voltha-go/common/log"
	ab "github.com/opencord/voltha-go/extentions/alarms"
	oa "github.com/opencord/voltha-go/extentions/alarms/onu_alarms"
	"github.com/opencord/voltha-go/kafka"
	oop "github.com/opencord/voltha-protos/go/openolt"
)

const (
	OnuAlarmType_OnuLossOfSignal = 0
	OnuAlarmType_OnuLossOfBurst  = 1
	OnuAlarmType_OnuLOPCMiss     = 2
	OnuAlarmType_OnuLOPCMicError = 3
)

type AlarmProxy struct {
	kafkaClient kafka.Client
	alarmTopic  kafka.Topic
}

func NewAlarmProxy(opts ...AdapterProxyOption) *AlarmProxy {
	var proxy AlarmProxy
	for _, option := range opts {
		option(&proxy)
	}
	return &proxy
}

type AdapterProxyOption func(*AlarmProxy)

func MsgClient(client kafka.Client) AdapterProxyOption {
	return func(args *AlarmProxy) {
		args.kafkaClient = client
	}
}

func MsgTopic(topic kafka.Topic) AdapterProxyOption {
	return func(args *AlarmProxy) {
		args.alarmTopic = topic
	}
}

func (alp *AlarmProxy) ProcessAlarms(alarmInd *oop.AlarmIndication, deviceId string) error {

	switch alarmInd.Data.(type) {
	case *oop.AlarmIndication_DyingGaspInd:
		log.Infow("Received dying gasp indication", log.Fields{"alarm_ind": alarmInd})
		if err := alp.dyingGaspIndication(alarmInd.GetDyingGaspInd(), deviceId); err != nil {
			log.Errorw("Failed to handle dying gasp alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_LosInd:
		log.Infow("Received LOS indication", log.Fields{"alarm_ind": alarmInd})
		if err := alp.OltLosIndication(alarmInd.GetLosInd(), deviceId); err != nil {
			log.Errorw("Failed to handle olt los alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_OnuAlarmInd:
		log.Infow("Received onu alarm indication ", log.Fields{"alarm_ind": alarmInd})
		if err := alp.OnuAlarmIndication(alarmInd.GetOnuAlarmInd(), deviceId); err != nil {
			log.Errorw("Failed to handle onu alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_OnuActivationFailInd:
		log.Infow("Received onu activation fail indication ", log.Fields{"alarm_ind": alarmInd})
		if err := alp.onuActivationFailIndication(alarmInd.GetOnuActivationFailInd(), deviceId); err != nil {
			log.Errorw("Failed to handle onu activation failure alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_OnuLossOmciInd:
		log.Infow("Received onu loss omci indication ", log.Fields{"alarm_ind": alarmInd})
		if err := alp.onuLossOmciIndication(alarmInd.GetOnuLossOmciInd(), deviceId); err != nil {
			log.Errorw("Failed to handle onu loss of omci channel alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_OnuDriftOfWindowInd:
		log.Infow("Received onu drift of window indication ", log.Fields{"alarm_ind": alarmInd})
		if err := alp.onuDriftWindowIndication(alarmInd.GetOnuDriftOfWindowInd(), deviceId); err != nil {
			log.Errorw("Failed to handle onu drift of window alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_OnuSignalDegradeInd:
		log.Infow("Received onu signal degrade indication ", log.Fields{"alarm_ind": alarmInd})
		if err := alp.onuSignalDegradeIndication(alarmInd.GetOnuSignalDegradeInd(), deviceId); err != nil {
			log.Errorw("Failed to handle onu signal degrade alarm", log.Fields{"alarm_ind": alarmInd})
		}
		log.Infow("Received onu signal degrade indication ", log.Fields{"alarm_ind": alarmInd})
	case *oop.AlarmIndication_OnuSignalsFailInd:
		log.Infow("Received onu signal fail indication ", log.Fields{"alarm_ind": alarmInd})
		if err := alp.onuSignalFailIndication(alarmInd.GetOnuSignalsFailInd(), deviceId); err != nil {
			log.Errorw("Failed to handle onu signal fail alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_OnuStartupFailInd:
		log.Infow("Received onu startup fail indication ", log.Fields{"alarm_ind": alarmInd})
		if err := alp.onuSignalFailIndication(alarmInd.GetOnuSignalsFailInd(), deviceId); err != nil {
			log.Errorw("Failed to handle onu startup fail alarm", log.Fields{"alarm_ind": alarmInd})
		}
	case *oop.AlarmIndication_OnuProcessingErrorInd:
		log.Infow("Received onu startup fail indication ", log.Fields{"alarm_ind": alarmInd})
		log.Infow("Not implemented yet", log.Fields{"alarm_ind": alarmInd})
	case *oop.AlarmIndication_OnuTiwiInd:
		log.Infow("Received onu transmission warning indication ", log.Fields{"alarm_ind": alarmInd})
		log.Infow("Not implemented yet", log.Fields{"alarm_ind": alarmInd})

	default:
		log.Errorw("Received unknown indication type", log.Fields{"alarm_ind": alarmInd})
		return errors.New("unknown indication type")
	}
	return nil
}

func (alp *AlarmProxy) RaiseAlarm(alarm ab.AlarmBase) error {
	if err := alarm.RaiseAlarm(alp.kafkaClient, alp.alarmTopic); err != nil {
		log.Errorw("Failed to raise alarm", log.Fields{"Error": err})
		return err
	}
	return nil
}

func (alp *AlarmProxy) ClearAlarm(alarm ab.AlarmBase) error {
	if err := alarm.ClearAlarm(alp.kafkaClient, alp.alarmTopic); err != nil {
		log.Errorw("Failed to clear alarm", log.Fields{"Error": err})
		return err
	}
	return nil
}

func (alp *AlarmProxy) OnuDiscoveryIndication(onudisc *oop.OnuDiscIndication, deviceId string) {
	discAlarm := oa.GetNewOnuDiscoveryAlarm()
	/*Raise Alarm*/
	discAlarm.AlarmData = discAlarm.GetAlarmData(true, deviceId)
	discAlarm.AlarmContext = discAlarm.GetContextData(onudisc.IntfId, onudisc.SerialNumber.String())
	if err := alp.RaiseAlarm(discAlarm); err != nil {
		log.Errorw("Failed to raise onu disc alarm", log.Fields{"serial_number": onudisc.SerialNumber.String()})
	}
	log.Infow("Successfully raised onu disc alarm", log.Fields{"serial_number": onudisc.SerialNumber.String()})
}

func (alp *AlarmProxy) dyingGaspIndication(dgi *oop.DyingGaspIndication, deviceId string) error {
	dgiAlarm := oa.GetNewDyingGaspAlarm()
	if dgi.Status == "on" {
		/*Raise Alarm*/
		dgiAlarm.AlarmData = dgiAlarm.GetAlarmData(true, deviceId)
		dgiAlarm.AlarmContext = dgiAlarm.GetContextData(dgi.OnuId, dgi.IntfId, deviceId)
		if err := alp.RaiseAlarm(dgiAlarm); err != nil {
			log.Errorw("Failed to raise onu dying gasp alarm", log.Fields{"serial_number": dgi.OnuId})
			return err
		}
		log.Infow("Successfully raised onu dying gasp alarm", log.Fields{"serial_number": dgi.OnuId})
	} else {
		/*Clear Alarm*/
		dgiAlarm.AlarmData = dgiAlarm.GetAlarmData(false, deviceId)
		dgiAlarm.AlarmContext = dgiAlarm.GetContextData(dgi.OnuId, dgi.IntfId, deviceId)
		if err := alp.ClearAlarm(dgiAlarm); err != nil {
			log.Errorw("Failed to clear onu disc alarm", log.Fields{"serial_number": dgi.OnuId})
			return err
		}
		log.Infow("Successfully cleared onu dying gasp alarm", log.Fields{"serial_number": dgi.OnuId})
	}
	return nil
}

func (alp *AlarmProxy) OltLosIndication(olos *oop.LosIndication, deviceId string) error {
	oltAlarm := oa.GetNewOltLosAlarm()
	if olos.Status == "on" {
		/*Raise Alarm*/
		oltAlarm.AlarmData = oltAlarm.GetAlarmData(true, deviceId)
		oltAlarm.AlarmContext = oltAlarm.GetContextData(olos.IntfId)
		if err := alp.RaiseAlarm(oltAlarm); err != nil {
			log.Errorw("Failed to raise olt los alarm", log.Fields{"intf_id": olos.IntfId})
			return err
		}
		log.Infow("Successfully raised olt los alarm", log.Fields{"intf_id": olos.IntfId})
	} else {
		/*Clear Alarm*/
		oltAlarm.AlarmData = oltAlarm.GetAlarmData(false, deviceId)
		oltAlarm.AlarmContext = oltAlarm.GetContextData(olos.IntfId)
		if err := alp.ClearAlarm(oltAlarm); err != nil {
			log.Errorw("Failed to clear olt los alarm", log.Fields{"intf_id": olos.IntfId})
			return err
		}
		log.Infow("Successfully cleared olt los alarm", log.Fields{"intf_id": olos.IntfId})
	}
	return nil

}
func (alp *AlarmProxy) OnuAlarmIndication(ona *oop.OnuAlarmIndication, deviceId string) error {
	onuAlarm := oa.GetNewOnuAlarm()

	/*For ONU Loss of signal indication*/
	if ona.LosStatus == "on" {
		/*Raise Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId, OnuAlarmType_OnuLossOfSignal)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu los alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully raised onu los  alarm", log.Fields{"intf_id": ona.IntfId})
	} else {
		/*Clear Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(false, deviceId, OnuAlarmType_OnuLossOfSignal)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.ClearAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu los alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully cleared onu los alarm", log.Fields{"intf_id": ona.IntfId})
	}

	/*For ONU Loss of Burst Indication*/
	if ona.LobStatus == "on" {
		/*Raise Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId, OnuAlarmType_OnuLossOfBurst)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu lob alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully raised onu lob alarm", log.Fields{"intf_id": ona.IntfId})
	} else {
		/*Clear Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(false, deviceId, OnuAlarmType_OnuLossOfBurst)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.ClearAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu lob alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully cleared onu lob alarm", log.Fields{"intf_id": ona.IntfId})
	}
	/*For ONU Loss of PLOAM channel Indication*/
	if ona.LopcMissStatus == "on" {
		/*Raise Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId, OnuAlarmType_OnuLOPCMiss)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu lopc miss alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully raised onu lopc miss alarm", log.Fields{"intf_id": ona.IntfId})
	} else {
		/*Clear Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(false, deviceId, OnuAlarmType_OnuLOPCMiss)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.ClearAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu lopc miss alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully cleared onu lopc miss alarm", log.Fields{"intf_id": ona.IntfId})
	}
	/*For ONU Loss of PLOAM Mic error*/
	if ona.LopcMicErrorStatus == "on" {
		/*Raise Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId, OnuAlarmType_OnuLOPCMicError)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu lopc mic error alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully raised onu lopc mic error alarm", log.Fields{"intf_id": ona.IntfId})
	} else {
		/*Clear Alarm*/
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(false, deviceId, OnuAlarmType_OnuLOPCMicError)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(ona.OnuId, string(ona.IntfId), deviceId)
		if err := alp.ClearAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu lopc mic error alarm", log.Fields{"intf_id": ona.IntfId})
			return err
		}
		log.Infow("Successfully raised onu lopc mic error alarm", log.Fields{"intf_id": ona.IntfId})
	}
	return nil

}

func (alp *AlarmProxy) onuActivationFailIndication(oaf *oop.OnuActivationFailureIndication, deviceId string) error {
	onuAlarm := oa.GetNewOnuActivationFailAlarm()
	onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId)
	onuAlarm.AlarmContext = onuAlarm.GetContextData(oaf.OnuId, string(oaf.IntfId))
	if err := alp.RaiseAlarm(onuAlarm); err != nil {
		log.Errorw("Failed to raise onu activation fail alarm", log.Fields{"intf_id": oaf.IntfId})
		return err
	}
	log.Infow("Successfully raised onu activation fail alarm", log.Fields{"intf_id": oaf.IntfId})
	return nil
}
func (alp *AlarmProxy) onuLossOmciIndication(oloc *oop.OnuLossOfOmciChannelIndication, deviceId string) error {
	onuAlarm := oa.GetNewOnuLossOmciAlarm()
	if oloc.Status == "on" {
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(oloc.OnuId, string(oloc.IntfId))
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu loss of omci channel alarm", log.Fields{"intf_id": oloc.IntfId})
			return err
		}
		log.Infow("Successfully raised onu loss of omci channel alarm", log.Fields{"intf_id": oloc.IntfId})
	} else {
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(false, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(oloc.OnuId, string(oloc.IntfId))
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu loss of omci channel alarm", log.Fields{"intf_id": oloc.IntfId})
			return err
		}
		log.Infow("Successfully raised onu loss of omci channel alarm", log.Fields{"intf_id": oloc.IntfId})
	}
	return nil
}

func (alp *AlarmProxy) onuDriftWindowIndication(oaf *oop.OnuDriftOfWindowIndication, deviceId string) error {
	onuAlarm := oa.GetNewOnuDriftWindowAlarm()
	if oaf.Status == "on" {
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(oaf.OnuId, string(oaf.IntfId), oaf.Drift, oaf.NewEqd)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu drift window alarm", log.Fields{"intf_id": oaf.IntfId})
			return err
		}
	} else {
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(oaf.OnuId, string(oaf.IntfId), oaf.Drift, oaf.NewEqd)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu drift window alarm", log.Fields{"intf_id": oaf.IntfId})
			return err
		}
	}
	return nil
}

func (alp *AlarmProxy) onuSignalDegradeIndication(osd *oop.OnuSignalDegradeIndication, deviceId string) error {
	if osd.Status == "on" {
		onuAlarm := oa.GetNewOnuSignalDegradeAlarm()
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(osd.OnuId, string(osd.IntfId), osd.InverseBitErrorRate)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
			return err
		}
		log.Infow("Successfully raised onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
	} else {
		onuAlarm := oa.GetNewOnuSignalDegradeAlarm()
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(false, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(osd.OnuId, string(osd.IntfId), osd.InverseBitErrorRate)
		if err := alp.ClearAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
			return err
		}
		log.Infow("Successfully cleared onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
	}
	return nil
}

func (alp *AlarmProxy) onuSignalFailIndication(osd *oop.OnuSignalsFailureIndication, deviceId string) error {
	if osd.Status == "on" {
		onuAlarm := oa.GetNewOnuSignalFailAlarm()
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(true, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(osd.OnuId, string(osd.IntfId), osd.InverseBitErrorRate)
		if err := alp.RaiseAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to raise onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
			return err
		}
		log.Infow("Successfully raised onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
	} else {
		onuAlarm := oa.GetNewOnuSignalDegradeAlarm()
		onuAlarm.AlarmData = onuAlarm.GetAlarmData(false, deviceId)
		onuAlarm.AlarmContext = onuAlarm.GetContextData(osd.OnuId, string(osd.IntfId), osd.InverseBitErrorRate)
		if err := alp.ClearAlarm(onuAlarm); err != nil {
			log.Errorw("Failed to clear onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
			return err
		}
		log.Infow("Successfully cleared onu signal degrade alarm", log.Fields{"intf_id": osd.IntfId})
	}
	return nil
}
