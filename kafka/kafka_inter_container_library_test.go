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
package kafka

import (
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestDefaultKafkaProxy(t *testing.T) {
	actualResult, error := NewInterContainerProxy()
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.kafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.kafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.defaultRequestHandlerInterface, interface{}(nil))
}

func TestKafkaProxyOptionHost(t *testing.T) {
	actualResult, error := NewInterContainerProxy(InterContainerHost("10.20.30.40"))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.kafkaHost, "10.20.30.40")
	assert.Equal(t, actualResult.kafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.defaultRequestHandlerInterface, interface{}(nil))
}

func TestKafkaProxyOptionPort(t *testing.T) {
	actualResult, error := NewInterContainerProxy(InterContainerPort(1020))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.kafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.kafkaPort, 1020)
	assert.Equal(t, actualResult.defaultRequestHandlerInterface, interface{}(nil))
}

func TestKafkaProxyOptionTopic(t *testing.T) {
	actualResult, error := NewInterContainerProxy(DefaultTopic(&Topic{Name: "Adapter"}))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.kafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.kafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.defaultRequestHandlerInterface, interface{}(nil))
	assert.Equal(t, actualResult.DefaultTopic.Name, "Adapter")
}

type myInterface struct {
}

func (m *myInterface) doSomething() {
}

func TestKafkaProxyOptionTargetInterface(t *testing.T) {
	var m *myInterface
	actualResult, error := NewInterContainerProxy(RequestHandlerInterface(m))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.kafkaHost, DefaultKafkaHost)
	assert.Equal(t, actualResult.kafkaPort, DefaultKafkaPort)
	assert.Equal(t, actualResult.defaultRequestHandlerInterface, m)
}

func TestKafkaProxyChangeAllOptions(t *testing.T) {
	var m *myInterface
	actualResult, error := NewInterContainerProxy(
		InterContainerHost("10.20.30.40"),
		InterContainerPort(1020),
		DefaultTopic(&Topic{Name: "Adapter"}),
		RequestHandlerInterface(m))
	assert.Equal(t, error, nil)
	assert.Equal(t, actualResult.kafkaHost, "10.20.30.40")
	assert.Equal(t, actualResult.kafkaPort, 1020)
	assert.Equal(t, actualResult.defaultRequestHandlerInterface, m)
	assert.Equal(t, actualResult.DefaultTopic.Name, "Adapter")
}
