package model

import (
	"github.com/opencord/voltha/protos/go/voltha"
	"testing"
)

func Test_Revision_01_New(t *testing.T) {
	branch := &Branch{}
	data := &voltha.Device{}
	children := make(map[string][]*Revision)
	rev := NewRevision(branch, data, children)

	t.Logf("New revision created: %+v\n", rev)
}
