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
