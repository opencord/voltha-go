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
	"fmt"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	ic "github.com/opencord/voltha-protos/v3/go/inter_container"
	"google.golang.org/grpc/codes"
	"math/rand"
	"time"
)

//GetRandomSerialNumber returns a serial number formatted as "HOST:PORT"
func GetRandomSerialNumber() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%d.%d.%d.%d:%d",
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(255),
		rand.Intn(9000)+1000,
	)
}

//GetRandomMacAddress returns a random mac address
func GetRandomMacAddress() string {
	rand.Seed(time.Now().UnixNano())
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
		rand.Intn(128),
	)
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

var src = rand.NewSource(time.Now().UnixNano())

func GetRandomString(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}
	return string(b)
}

func ICProxyErrorCodeToGrpcErrorCode(ctx context.Context, icErr ic.ErrorCodeCodes) codes.Code {
	switch icErr {
	case ic.ErrorCode_INVALID_PARAMETERS:
		return codes.InvalidArgument
	case ic.ErrorCode_UNSUPPORTED_REQUEST:
		return codes.Unavailable
	case ic.ErrorCode_DEADLINE_EXCEEDED:
		return codes.DeadlineExceeded
	default:
		logger.Warnw(ctx, "cannnot-map-ic-error-code-to-grpc-error-code", log.Fields{"err": icErr})
		return codes.Internal
	}
}
