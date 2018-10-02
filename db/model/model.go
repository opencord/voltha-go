package model

import (
	"github.com/opencord/voltha-go/common/log"
)

func init() {
	log.AddPackage(log.JSON, log.WarnLevel, log.Fields{"instanceId": "DB_MODEL"})
	defer log.CleanUp()
}
