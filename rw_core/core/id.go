package core

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	m "math/rand"
)

func randomHex(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func CreateDeviceId() string {
	// deviceId is 16 hex long - lower 12 hex is the device id.  For now just generate the 12 hex
	val, _ := randomHex(12)
	fmt.Println("!!!!!!!!!!!!!!!!!!!!", val)
	return val
}

func CreateLogicalDeviceId() string {
	// Not used - logical device ID represents the MAC address of the OLT
	// logical device id is 16 hex long - lower 12 hex is the logical device id.  For now just generate the 12 hex
	val, _ := randomHex(12)
	return val
}

func createLogicalPortId() uint32 {
	//	A logical port is a uint32
	return m.Uint32()
}
