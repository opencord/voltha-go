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
	"context"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestSaramaClientEnableLivenessChannel(t *testing.T) {
	// Note: This doesn't actually start the client
	client := NewSaramaClient()

	ch := client.EnableLivenessChannel(context.Background(), true)

	// The channel should have one "true" message on it
	assert.NotEmpty(t, ch)

	select {
	case stuff := <-ch:
		assert.True(t, stuff)
	default:
		t.Error("Failed to read from the channel")
	}
}
