package kvstore

import (
	"fmt"
	"time"
)

// GetDuration converts a timeout value from int to duration.  If the timeout value is
// either not set of -ve then we default KV timeout (configurable) is used.
func GetDuration(timeout int) time.Duration {
	if timeout <= 0 {
		return defaultKVGetTimeout * time.Second
	}
	return time.Duration(timeout) * time.Second
}

// ToString converts an interface value to a string.  The interface should either be of
// a string type or []byte.  Otherwise, an error is returned.
func ToString(value interface{}) (string, error) {
	switch t := value.(type) {
	case []byte:
		return string(value.([]byte)), nil
	case string:
		return value.(string), nil
	default:
		return "", fmt.Errorf("unexpected-type-%T", t)
	}
}

// ToByte converts an interface value to a []byte.  The interface should either be of
// a string type or []byte.  Otherwise, an error is returned.
func ToByte(value interface{}) ([]byte, error) {
	switch t := value.(type) {
	case []byte:
		return value.([]byte), nil
	case string:
		return []byte(value.(string)), nil
	default:
		return nil, fmt.Errorf("unexpected-type-%T", t)
	}
}
