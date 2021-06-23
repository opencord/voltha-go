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
package kvstore

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"time"
)

const (
	minRetryInterval  = 100
	maxRetryInterval  = 5000
	incrementalFactor = 1.2
	jitter            = 0.2
)

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

// backoff waits an amount of time that is proportional to the attempt value.  The wait time in a range of
// minRetryInterval and maxRetryInterval.
func backoff(ctx context.Context, attempt int) error {
	if attempt == 0 {
		return nil
	}
	backoff := int(minRetryInterval + incrementalFactor*math.Exp(float64(attempt)))
	backoff *= 1 + int(jitter*(rand.Float64()*2-1))
	if backoff > maxRetryInterval {
		backoff = maxRetryInterval
	}
	ticker := time.NewTicker(time.Duration(backoff) * time.Millisecond)
	defer ticker.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-ticker.C:
	}
	return nil
}
