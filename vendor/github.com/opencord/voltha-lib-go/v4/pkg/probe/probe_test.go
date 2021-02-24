/*
 * Copyright 2019-present Open Networking Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package probe

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestServiceStatusString(t *testing.T) {
	assert.Equal(t, "Unknown", ServiceStatusUnknown.String(), "ServiceStatusUnknown")
	assert.Equal(t, "Preparing", ServiceStatusPreparing.String(), "ServiceStatusPreparing")
	assert.Equal(t, "Prepared", ServiceStatusPrepared.String(), "ServiceStatusPrepared")
	assert.Equal(t, "Running", ServiceStatusRunning.String(), "ServiceStatusRunning")
	assert.Equal(t, "Stopped", ServiceStatusStopped.String(), "ServiceStatusStopped")
	assert.Equal(t, "Failed", ServiceStatusFailed.String(), "ServiceStatusFailed")
	assert.Equal(t, "NotReady", ServiceStatusNotReady.String(), "ServiceStatusNotReady")
}

func AlwaysTrue(map[string]ServiceStatus) bool {
	return true
}

func AlwaysFalse(map[string]ServiceStatus) bool {
	return false
}

func TestWithFuncs(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue).WithHealthFunc(AlwaysFalse)

	assert.NotNil(t, p.readyFunc, "ready func not set")
	assert.True(t, p.readyFunc(nil), "ready func not set correctly")
	assert.NotNil(t, p.healthFunc, "health func not set")
	assert.False(t, p.healthFunc(nil), "health func not set correctly")
}

func TestWithReadyFuncOnly(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue)

	assert.NotNil(t, p.readyFunc, "ready func not set")
	assert.True(t, p.readyFunc(nil), "ready func not set correctly")
	assert.Nil(t, p.healthFunc, "health func set")
}

func TestWithHealthFuncOnly(t *testing.T) {
	p := (&Probe{}).WithHealthFunc(AlwaysTrue)

	assert.Nil(t, p.readyFunc, "ready func set")
	assert.NotNil(t, p.healthFunc, "health func not set")
	assert.True(t, p.healthFunc(nil), "health func not set correctly")
}

func TestRegisterOneService(t *testing.T) {
	p := &Probe{}

	p.RegisterService(context.Background(), "one")

	assert.Equal(t, 1, len(p.status), "wrong number of services")

	_, ok := p.status["one"]
	assert.True(t, ok, "service not found")
}

func TestRegisterMultipleServices(t *testing.T) {
	p := &Probe{}

	p.RegisterService(context.Background(), "one", "two", "three", "four")

	assert.Equal(t, 4, len(p.status), "wrong number of services")

	_, ok := p.status["one"]
	assert.True(t, ok, "service one not found")
	_, ok = p.status["two"]
	assert.True(t, ok, "service two not found")
	_, ok = p.status["three"]
	assert.True(t, ok, "service three not found")
	_, ok = p.status["four"]
	assert.True(t, ok, "service four not found")
}

func TestRegisterMultipleServicesIncremental(t *testing.T) {
	p := &Probe{}
	ctx := context.Background()
	p.RegisterService(ctx, "one")
	p.RegisterService(ctx, "two")
	p.RegisterService(ctx, "three", "four")

	assert.Equal(t, 4, len(p.status), "wrong number of services")

	_, ok := p.status["one"]
	assert.True(t, ok, "service one not found")
	_, ok = p.status["two"]
	assert.True(t, ok, "service two not found")
	_, ok = p.status["three"]
	assert.True(t, ok, "service three not found")
	_, ok = p.status["four"]
	assert.True(t, ok, "service four not found")
}

func TestRegisterMultipleServicesDuplicates(t *testing.T) {
	p := &Probe{}

	p.RegisterService(context.Background(), "one", "one", "one", "two")

	assert.Equal(t, 2, len(p.status), "wrong number of services")

	_, ok := p.status["one"]
	assert.True(t, ok, "service one not found")
	_, ok = p.status["two"]
	assert.True(t, ok, "service two not found")
}

func TestRegisterMultipleServicesDuplicatesIncremental(t *testing.T) {
	p := &Probe{}
	ctx := context.Background()
	p.RegisterService(ctx, "one")
	p.RegisterService(ctx, "one")
	p.RegisterService(ctx, "one", "two")

	assert.Equal(t, 2, len(p.status), "wrong number of services")

	_, ok := p.status["one"]
	assert.True(t, ok, "service one not found")
	_, ok = p.status["two"]
	assert.True(t, ok, "service two not found")
}

func TestUpdateStatus(t *testing.T) {
	p := &Probe{}
	ctx := context.Background()
	p.RegisterService(ctx, "one", "two")
	p.UpdateStatus(ctx, "one", ServiceStatusRunning)

	assert.Equal(t, ServiceStatusRunning, p.status["one"], "status not set")
	assert.Equal(t, ServiceStatusUnknown, p.status["two"], "status set")
}

func TestRegisterOverwriteStatus(t *testing.T) {
	p := &Probe{}
	ctx := context.Background()
	p.RegisterService(ctx, "one", "two")
	p.UpdateStatus(ctx, "one", ServiceStatusRunning)

	assert.Equal(t, ServiceStatusRunning, p.status["one"], "status not set")
	assert.Equal(t, ServiceStatusUnknown, p.status["two"], "status set")

	p.RegisterService(ctx, "one", "three")
	assert.Equal(t, 3, len(p.status), "wrong number of services")
	assert.Equal(t, ServiceStatusRunning, p.status["one"], "status overridden")
	assert.Equal(t, ServiceStatusUnknown, p.status["two"], "status set")
	assert.Equal(t, ServiceStatusUnknown, p.status["three"], "status set")
}

func TestDetailzWithServies(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue).WithHealthFunc(AlwaysTrue)
	p.RegisterService(context.Background(), "one", "two")

	req := httptest.NewRequest("GET", "http://example.com/detailz", nil)
	w := httptest.NewRecorder()
	p.detailzFunc(w, req)
	resp := w.Result()
	body, _ := ioutil.ReadAll(resp.Body)

	assert.Equal(t, http.StatusOK, resp.StatusCode, "invalid status code for no services")
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"), "wrong content type")
	var vals map[string]string
	err := json.Unmarshal(body, &vals)
	assert.Nil(t, err, "unable to unmarshal values")
	assert.Equal(t, "Unknown", vals["one"], "wrong value")
	assert.Equal(t, "Unknown", vals["two"], "wrong value")
}

func TestReadzNoServices(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue)
	req := httptest.NewRequest("GET", "http://example.com/readz", nil)
	w := httptest.NewRecorder()
	p.readzFunc(w, req)
	resp := w.Result()

	assert.Equal(t, http.StatusTeapot, resp.StatusCode, "invalid status code for no services")
}

func TestReadzWithServicesWithTrue(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue).WithHealthFunc(AlwaysTrue)
	p.RegisterService(context.Background(), "one", "two")

	req := httptest.NewRequest("GET", "http://example.com/readz", nil)
	w := httptest.NewRecorder()
	p.readzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "invalid status code for registered only services")
}

func TestReadzWithServicesWithDefault(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one", "two")

	req := httptest.NewRequest("GET", "http://example.com/readz", nil)
	w := httptest.NewRecorder()
	p.readzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusTeapot, resp.StatusCode, "invalid status code for registered only services")
}

func TestReadzNpServicesDefault(t *testing.T) {
	p := &Probe{}

	req := httptest.NewRequest("GET", "http://example.com/readz", nil)
	w := httptest.NewRecorder()
	p.readzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusTeapot, resp.StatusCode, "invalid status code")
}

func TestReadzWithServicesDefault(t *testing.T) {
	p := &Probe{}
	ctx := context.Background()
	p.RegisterService(ctx, "one", "two")
	p.UpdateStatus(ctx, "one", ServiceStatusRunning)
	p.UpdateStatus(ctx, "two", ServiceStatusRunning)

	req := httptest.NewRequest("GET", "http://example.com/readz", nil)
	w := httptest.NewRecorder()
	p.readzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "invalid status code")
}

func TestReadzWithServicesDefaultOne(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one", "two")
	p.UpdateStatus(context.Background(), "one", ServiceStatusRunning)

	req := httptest.NewRequest("GET", "http://example.com/readz", nil)
	w := httptest.NewRecorder()
	p.readzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusTeapot, resp.StatusCode, "invalid status code")
}

func TestHealthzNoServices(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue)
	req := httptest.NewRequest("GET", "http://example.com/healthz", nil)
	w := httptest.NewRecorder()
	p.healthzFunc(w, req)
	resp := w.Result()

	assert.Equal(t, http.StatusTeapot, resp.StatusCode, "invalid status code for no services")
}

func TestHealthzWithServicesWithTrue(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue).WithHealthFunc(AlwaysTrue)
	p.RegisterService(context.Background(), "one", "two")

	req := httptest.NewRequest("GET", "http://example.com/healthz", nil)
	w := httptest.NewRecorder()
	p.healthzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "invalid status code for registered only services")
}

func TestHealthzWithServicesWithDefault(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one", "two")

	req := httptest.NewRequest("GET", "http://example.com/healthz", nil)
	w := httptest.NewRecorder()
	p.healthzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "invalid status code for registered only services")
}

func TestHealthzNoServicesDefault(t *testing.T) {
	p := &Probe{}

	req := httptest.NewRequest("GET", "http://example.com/healthz", nil)
	w := httptest.NewRecorder()
	p.healthzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusTeapot, resp.StatusCode, "invalid status code")
}

func TestHealthzWithServicesDefault(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one", "two")
	p.UpdateStatus(context.Background(), "one", ServiceStatusRunning)
	p.UpdateStatus(context.Background(), "two", ServiceStatusRunning)

	req := httptest.NewRequest("GET", "http://example.com/healthz", nil)
	w := httptest.NewRecorder()
	p.healthzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusOK, resp.StatusCode, "invalid status code")
}

func TestHealthzWithServicesDefaultFailed(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one", "two")
	p.UpdateStatus(context.Background(), "one", ServiceStatusFailed)

	req := httptest.NewRequest("GET", "http://example.com/healthz", nil)
	w := httptest.NewRecorder()
	p.healthzFunc(w, req)
	resp := w.Result()
	assert.Equal(t, http.StatusTeapot, resp.StatusCode, "invalid status code")
}

func TestSetFuncsToNil(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue).WithHealthFunc(AlwaysFalse)
	p.WithReadyFunc(nil).WithHealthFunc(nil)
	assert.Nil(t, p.readyFunc, "ready func not reset to nil")
	assert.Nil(t, p.healthFunc, "health func not reset to nil")
}

func TestGetProbeFromContext(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one")
	ctx := context.WithValue(context.Background(), ProbeContextKey, p)
	pc := GetProbeFromContext(ctx)
	assert.Equal(t, p, pc, "Probe from context was not identical to original probe")
}

func TestGetProbeFromContextMssing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	pc := GetProbeFromContext(ctx)
	assert.Nil(t, pc, "Context had a non-nil probe when it should have been nil")
}

func TestUpdateStatusFromContext(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one")
	ctx := context.WithValue(context.Background(), ProbeContextKey, p)
	UpdateStatusFromContext(ctx, "one", ServiceStatusRunning)

	assert.Equal(t, 1, len(p.status), "wrong number of services")
	_, ok := p.status["one"]
	assert.True(t, ok, "unable to find registered service")
	assert.Equal(t, ServiceStatusRunning, p.status["one"], "status not set correctly from context")
}

func TestUpdateStatusFromNilContext(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one")
	// nolint: staticcheck
	UpdateStatusFromContext(nil, "one", ServiceStatusRunning)

	assert.Equal(t, 1, len(p.status), "wrong number of services")
	_, ok := p.status["one"]
	assert.True(t, ok, "unable to find registered service")
	assert.Equal(t, ServiceStatusUnknown, p.status["one"], "status not set correctly from context")

}

func TestUpdateStatusFromContextWithoutProbe(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	UpdateStatusFromContext(ctx, "one", ServiceStatusRunning)

	assert.Equal(t, 1, len(p.status), "wrong number of services")
	_, ok := p.status["one"]
	assert.True(t, ok, "unable to find registered service")
	assert.Equal(t, ServiceStatusUnknown, p.status["one"], "status not set correctly from context")

}

func TestUpdateStatusFromContextWrongType(t *testing.T) {
	p := &Probe{}
	p.RegisterService(context.Background(), "one")
	ctx := context.WithValue(context.Background(), ProbeContextKey, "Teapot")
	UpdateStatusFromContext(ctx, "one", ServiceStatusRunning)

	assert.Equal(t, 1, len(p.status), "wrong number of services")
	_, ok := p.status["one"]
	assert.True(t, ok, "unable to find registered service")
	assert.Equal(t, ServiceStatusUnknown, p.status["one"], "status not set correctly from context")
}

func TestUpdateStatusNoRegistered(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue).WithHealthFunc(AlwaysFalse)

	p.UpdateStatus(context.Background(), "one", ServiceStatusRunning)
	assert.Equal(t, 1, len(p.status), "wrong number of services")
	_, ok := p.status["one"]
	assert.True(t, ok, "unable to find registered service")
	assert.Equal(t, ServiceStatusRunning, p.status["one"], "status not set correctly from context")
}

func TestIsReadyTrue(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysTrue).WithHealthFunc(AlwaysFalse)

	p.RegisterService(context.Background(), "SomeService")

	assert.True(t, p.IsReady(), "IsReady should have been true")
}

func TestIsReadyFalse(t *testing.T) {
	p := (&Probe{}).WithReadyFunc(AlwaysFalse).WithHealthFunc(AlwaysFalse)

	p.RegisterService(context.Background(), "SomeService")

	assert.False(t, p.IsReady(), "IsReady should have been false")
}

func TestGetStatus(t *testing.T) {
	p := &Probe{}

	p.RegisterService(context.Background(), "one", "two")
	p.UpdateStatus(context.Background(), "one", ServiceStatusRunning)

	ss := p.GetStatus("one")
	assert.Equal(t, ServiceStatusRunning, ss, "Service status should have been ServiceStatusRunning")
}

func TestGetStatusMissingService(t *testing.T) {
	p := &Probe{}

	p.RegisterService(context.Background(), "one", "two")

	ss := p.GetStatus("three")
	assert.Equal(t, ServiceStatusUnknown, ss, "Service status should have been ServiceStatusUnknown")
}
