package kafka

import (
	"github.com/stretchr/testify/assert"
	"testing"
)


func TestDefaultKafkaProxy(t *testing.T) {
	actualResult, error := NewKafkaMessagingProxy()
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.KafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.KafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.TargetInterface, interface{}(nil))
	assert.Equal(t, actualResult.DefaultTopic.Name, "Core")
}

func TestKafkaProxyOptionHost(t *testing.T) {
	actualResult, error := NewKafkaMessagingProxy(KafkaHost("10.20.30.40"))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.KafkaHost, "10.20.30.40")
	assert.Equal(t, actualResult.KafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.TargetInterface, interface{}(nil))
	assert.Equal(t, actualResult.DefaultTopic.Name, "Core")
}

func TestKafkaProxyOptionPort(t *testing.T) {
	actualResult, error := NewKafkaMessagingProxy(KafkaPort(1020))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.KafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.KafkaPort, 1020)
	assert.Equal(t, actualResult.TargetInterface, interface{}(nil))
	assert.Equal(t, actualResult.DefaultTopic.Name, "Core")
}

func TestKafkaProxyOptionTopic(t *testing.T) {
	actualResult, error := NewKafkaMessagingProxy(DefaultTopic(&Topic{Name: "Adapter"}))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.KafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.KafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.TargetInterface, interface{}(nil))
	assert.Equal(t, actualResult.DefaultTopic.Name, "Adapter")
}

type myInterface struct {
}

func (m *myInterface) doSomething() {
}

func TestKafkaProxyOptionTargetInterface(t *testing.T) {
	var m *myInterface
	actualResult, error := NewKafkaMessagingProxy(TargetInterface(m))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.KafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.KafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.TargetInterface, m)
	assert.Equal(t, actualResult.DefaultTopic.Name, "Core")
}

func TestKafkaProxyChangeAllOptions(t *testing.T) {
	var m *myInterface
	actualResult, error := NewKafkaMessagingProxy(
		KafkaHost("10.20.30.40"),
		KafkaPort(1020),
		DefaultTopic(&Topic{Name: "Adapter"}),
		TargetInterface(m))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.KafkaHost, "10.20.30.40")
	assert.Equal(t, actualResult.KafkaPort, 1020)
	assert.Equal(t, actualResult.TargetInterface, m)
	assert.Equal(t, actualResult.DefaultTopic.Name, "Adapter")
}

