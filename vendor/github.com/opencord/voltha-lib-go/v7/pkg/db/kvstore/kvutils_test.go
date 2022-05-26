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
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestToStringWithString(t *testing.T) {
	actualResult, _ := ToString("myString")
	var expectedResult = "myString"

	assert.Equal(t, expectedResult, actualResult)
}

func TestToStringWithEmpty(t *testing.T) {
	actualResult, _ := ToString("")
	var expectedResult = ""

	assert.Equal(t, expectedResult, actualResult)
}

func TestToStringWithByte(t *testing.T) {
	mByte := []byte("Hello")
	actualResult, _ := ToString(mByte)
	var expectedResult = "Hello"

	assert.Equal(t, expectedResult, actualResult)
}

func TestToStringWithEmptyByte(t *testing.T) {
	mByte := []byte("")
	actualResult, _ := ToString(mByte)
	var expectedResult = ""

	assert.Equal(t, expectedResult, actualResult)
}

func TestToStringForErrorCase(t *testing.T) {
	mInt := 200
	actualResult, error := ToString(mInt)
	var expectedResult = ""

	assert.Equal(t, expectedResult, actualResult)
	assert.NotEqual(t, error, nil)
}

func TestBackoffNoWait(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
	defer cancel()
	err := backoff(ctx, 0)
	assert.Nil(t, err)
}

func TestBackoffSuccess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	defer cancel()
	previous := time.Duration(0)
	for i := 1; i < 5; i++ {
		start := time.Now()
		err := backoff(ctx, i)
		assert.Nil(t, err)
		current := time.Since(start)
		assert.Greater(t, current.Milliseconds(), previous.Milliseconds())
		previous = current
	}
}

func TestBackoffContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	defer cancel()
	err := backoff(ctx, 10)
	assert.NotNil(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}
