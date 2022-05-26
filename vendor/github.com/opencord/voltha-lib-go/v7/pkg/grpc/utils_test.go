/*
 * Copyright 2021-present Open Networking Foundation

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
package grpc_test

import (
	"context"
	"os"
	"testing"
	"time"

	vgrpc "github.com/opencord/voltha-lib-go/v7/pkg/grpc"
	"github.com/stretchr/testify/assert"
)

func TestBackoffNoWait(t *testing.T) {
	initTime := 1 * time.Millisecond
	maxTime := 500 * time.Millisecond
	maxElapsedTime := 0 * time.Millisecond
	backoff := vgrpc.NewBackoff(initTime, maxTime, maxElapsedTime)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := backoff.Backoff(ctx)
	assert.Nil(t, err)
}

func TestBackoffElapsedTime(t *testing.T) {
	initTime := 10 * time.Millisecond
	maxTime := 50 * time.Millisecond
	maxElapsedTime := 500 * time.Millisecond
	b := vgrpc.NewBackoff(initTime, maxTime, maxElapsedTime)
	start := time.Now()
loop:
	for {
		err := b.Backoff(context.Background())
		if err != nil {
			break loop
		}
	}
	assert.GreaterOrEqual(t, time.Since(start).Milliseconds(), maxTime.Milliseconds())
}

func TestBackoffSuccess(t *testing.T) {
	initTime := 100 * time.Millisecond
	maxTime := 500 * time.Millisecond
	maxElapsedTime := 0 * time.Millisecond
	backoff := vgrpc.NewBackoff(initTime, maxTime, maxElapsedTime)
	ctx, cancel := context.WithTimeout(context.Background(), 2000*time.Millisecond)
	defer cancel()
	previous := time.Duration(0)
	for i := 1; i < 5; i++ {
		start := time.Now()
		err := backoff.Backoff(ctx)
		assert.Nil(t, err)
		current := time.Since(start)
		if current < maxTime {
			assert.GreaterOrEqual(t, current.Milliseconds(), previous.Milliseconds())
		}
		previous = current
	}
}

func TestBackoffContextTimeout(t *testing.T) {
	initTime := 1000 * time.Millisecond
	maxTime := 1100 * time.Millisecond
	maxElapsedTime := 0 * time.Millisecond
	backoff := vgrpc.NewBackoff(initTime, maxTime, maxElapsedTime)
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	err := backoff.Backoff(ctx)
	assert.NotNil(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func TestSetFromEnvVariable(t *testing.T) {
	// 1. Test with unsupported type
	var valInt int
	err := vgrpc.SetFromEnvVariable("MY-KEY", valInt)
	assert.NotNil(t, err)

	//2. Test with supported type but no env variable present
	var valDuration time.Duration
	err = vgrpc.SetFromEnvVariable("MY-KEY", &valDuration)
	assert.Nil(t, err)

	//3. Test with supported type and env variable present
	err = os.Setenv("MY-KEY", "10s")
	assert.Nil(t, err)
	err = vgrpc.SetFromEnvVariable("MY-KEY", &valDuration)
	assert.Nil(t, err)
	assert.Equal(t, 10*time.Second, valDuration)
}
