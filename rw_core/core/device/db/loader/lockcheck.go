package loader

import (
	"fmt"
)

type LockID struct {
	Type LockableType
	ID   interface{}
}

type LockableType uint8

// these constants determine the order in which locks must be acquired
const (
	None LockableType = iota
	Device
	LogicalDevice
	Port
	LogicalPort
	Flow
	Group
	Meter
)

func (l LockableType) String() string {
	return map[LockableType]string{
		None:          "None",
		Device:        "Device",
		LogicalDevice: "LogicalDevice",
		Port:          "Port",
		LogicalPort:   "LogicalPort",
		Flow:          "Flow",
		Group:         "Group",
		Meter:         "Meter",
	}[l]
}

// CheckSaneLockOrder verifies that locks are acquired in the same (predefined) order in every request.
// This function panics if an invalid ordering is detected.
func (t *txn) CheckSaneLockOrder(id LockID) {
	if id.Type < t.lastLocked.Type || (id.Type == t.lastLocked.Type && !ensureGreaterID(id.ID, t.lastLocked.ID)) {
		panic(fmt.Sprintf("attempted to acquire %v after %v; locks must be acquired in the same order every time", id, t.lastLocked.Type))
	}
	t.lastLocked = id
}

func ensureGreaterID(a, b interface{}) bool {
	switch a := a.(type) {
	case uint32:
		return a > b.(uint32)
	case uint64:
		return a > b.(uint64)
	case string:
		return a > b.(string)
	default:
		panic(fmt.Sprintf("id of unknown type %T: %v", a, a))
	}
}
