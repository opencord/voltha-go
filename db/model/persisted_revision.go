package model

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/golang/protobuf/proto"
	"io/ioutil"
	"reflect"
)

type PersistedRevision struct {
	*Revision
	Compress bool
	kvStore  *Backend
}

func NewPersistedRevision(branch *Branch, data interface{}, children map[string][]*Revision) *PersistedRevision {
	pr := &PersistedRevision{}
	pr.kvStore = branch.node.root.KvStore
	pr.Revision = NewRevision(branch, data, children)
	return pr
}

func (pr *PersistedRevision) finalize() {
	pr.Revision.finalize()
	pr.store()
}

type revData struct {
	Children map[string][]string
	Config   string
}

func (pr *PersistedRevision) store() {
	if ok, _ := pr.kvStore.Get(pr.Revision.Hash); ok != nil {
		return
	}

	pr.storeConfig()

	childrenHashes := make(map[string][]string)
	for fieldName, children := range pr.Children {
		hashes := []string{}
		for _, rev := range children {
			hashes = append(hashes, rev.Hash)
		}
		childrenHashes[fieldName] = hashes
	}
	data := &revData{
		Children: childrenHashes,
		Config:   pr.Config.Hash,
	}
	if blob, err := json.Marshal(data); err != nil {
		// TODO report error
	} else {
		if pr.Compress {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write(blob)
			w.Close()
			blob = b.Bytes()
		}
		pr.kvStore.Put(pr.Hash, blob)
	}
}

func (pr *PersistedRevision) load(branch *Branch, kvStore *Backend, msgClass interface{}, hash string) *PersistedRevision {
	blob, _ := kvStore.Get(hash)
	output := blob.Value.([]byte)
	var data revData
	if pr.Compress {
		b := bytes.NewBuffer(blob.Value.([]byte))
		if r, err := gzip.NewReader(b); err != nil {
			// TODO : report error
		} else {
			if output, err = ioutil.ReadAll(r); err != nil {
				// TODO report error
			}
		}
	}
	if err := json.Unmarshal(output, &data); err != nil {
		fmt.Errorf("problem to unmarshal data - %s", err.Error())
	}

	configHash := data.Config
	configData := pr.loadConfig(kvStore, msgClass, configHash)
	assembledChildren := make(map[string][]*Revision)

	childrenHashes := data.Children
	node := branch.node
	for fieldName, child := range ChildrenFields(msgClass) {
		var children []*Revision
		for _, childHash := range childrenHashes[fieldName] {
			//fmt.Printf("child class type: %+v", reflect.New(n).Elem().Interface())
			childNode := node.makeNode(reflect.New(child.ClassType).Elem().Interface(), "")
			childNode.LoadLatest(kvStore, childHash)
			childRev := childNode.Latest()
			children = append(children, childRev)
		}
		assembledChildren[fieldName] = children
	}
	rev := NewPersistedRevision(branch, configData, assembledChildren)
	return rev
}

func (pr *PersistedRevision) storeConfig() {
	if ok, _ := pr.kvStore.Get(pr.Config.Hash); ok != nil {
		return
	}
	if blob, err := proto.Marshal(pr.Config.Data.(proto.Message)); err != nil {
		// TODO report error
	} else {
		if pr.Compress {
			var b bytes.Buffer
			w := gzip.NewWriter(&b)
			w.Write(blob)
			w.Close()
			blob = b.Bytes()
		}
		pr.kvStore.Put(pr.Config.Hash, blob)
	}
}

func (pr *PersistedRevision) loadConfig(kvStore *Backend, msgClass interface{}, hash string) interface{} {
	blob, _ := kvStore.Get(hash)
	output := blob.Value.([]byte)

	if pr.Compress {
		b := bytes.NewBuffer(blob.Value.([]byte))
		if r, err := gzip.NewReader(b); err != nil {
			// TODO : report error
		} else {
			if output, err = ioutil.ReadAll(r); err != nil {
				// TODO report error
			}
		}
	}

	var data reflect.Value
	if msgClass != nil {
		data = reflect.New(reflect.TypeOf(msgClass).Elem())
		if err := proto.Unmarshal(output, data.Interface().(proto.Message)); err != nil {
			// TODO report error
		}
	}

	return data.Interface()
}
