package model

import (
	"testing"
	"github.com/opencord/voltha-go/protos/voltha"
	"reflect"
)

func Test_Utils_Clone(t *testing.T) {
	a := &voltha.Device{
		Id: "abcde",
		FirmwareVersion: "someversion",
	}
	b:= &voltha.Device{}
	Clone(reflect.ValueOf(a).Interface(), b)
	t.Logf("A: %+v, B: %+v", a, b)
	b.Id = "12345"
	t.Logf("A: %+v, B: %+v", a, b)

	var c *voltha.Device
	c = Clone2(a).(*voltha.Device)
	t.Logf("A: %+v, C: %+v", a, c)
	c.Id = "12345"
	t.Logf("A: %+v, C: %+v", a, c)
}
