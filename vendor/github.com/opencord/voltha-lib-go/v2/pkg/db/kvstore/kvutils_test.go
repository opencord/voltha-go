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
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestDurationWithNegativeTimeout(t *testing.T) {
	actualResult := GetDuration(-1)
	var expectedResult = defaultKVGetTimeout * time.Second

	assert.Equal(t, expectedResult, actualResult)
}

func TestDurationWithZeroTimeout(t *testing.T) {
	actualResult := GetDuration(0)
	var expectedResult = defaultKVGetTimeout * time.Second

	assert.Equal(t, expectedResult, actualResult)
}

func TestDurationWithTimeout(t *testing.T) {
	actualResult := GetDuration(10)
	var expectedResult = time.Duration(10) * time.Second

	assert.Equal(t, expectedResult, actualResult)
}

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
