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
package common

import (
	"context"
	ic "github.com/opencord/voltha-protos/v4/go/inter_container"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"strconv"
	sp "strings"
	"testing"
)

const sim = "0123456789abcdefABCDEF"
const rstr = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func TestGetSerialNumber(t *testing.T) {

	serial := GetRandomSerialNumber()
	assert.NotNil(t, serial)

	sparsed := sp.Split(serial, ".")
	sparsed2 := sparsed[3]

	for i := 0; i <= 2; i++ {
		ioct, _ := strconv.ParseInt(sparsed[i], 10, 0)

		assert.True(t, ioct <= 255 && ioct >= 0, "Octect %d IP octect wrong!", i)
	}

	sp3 := sp.Split(sparsed2, ":")
	oct4, _ := strconv.ParseInt(sp3[0], 10, 0)

	assert.True(t, oct4 <= 255 && oct4 >= 0, "Fourth IP octect wrong!")

	port, _ := strconv.ParseInt(sp3[1], 10, 0)
	assert.True(t, port <= 65535 && port >= 0)
}
func TestGetString(t *testing.T) {
	str := GetRandomString(20)
	strslide := sp.Split(str, "")
	for i := 0; i < len(strslide); i++ {
		assert.True(t, sp.Contains(rstr, strslide[i]), "Error! The string doesn't appears correct --> %s Expected in --> %s", str, rstr)
	}
	assert.NotNil(t, str)
}
func TestGetMacAddress(t *testing.T) {
	mac := GetRandomMacAddress()
	assert.NotNil(t, mac, "Mac address null")
	smac := sp.Split(mac, ":")
	assert.True(t, len(smac) == 6, "mac address not correctly formatted")

	for i := 0; i < len(smac); i++ {
		oct := sp.Split(smac[i], "")
		assert.True(t, sp.Contains(sim, oct[0]))
		assert.True(t, sp.Contains(sim, oct[1]))
	}

}

func TestICProxyErrorCodeToGrpcErrorCode(t *testing.T) {
	unsupported := ICProxyErrorCodeToGrpcErrorCode(context.Background(), ic.ErrorCode_UNSUPPORTED_REQUEST)
	assert.Equal(t, unsupported, codes.Unavailable)

	invalid := ICProxyErrorCodeToGrpcErrorCode(context.Background(), ic.ErrorCode_INVALID_PARAMETERS)
	assert.Equal(t, invalid, codes.InvalidArgument)

	timeout := ICProxyErrorCodeToGrpcErrorCode(context.Background(), ic.ErrorCode_DEADLINE_EXCEEDED)
	assert.Equal(t, timeout, codes.DeadlineExceeded)
}
