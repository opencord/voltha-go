package model

import (
	"bytes"
	"crypto/md5"
	"fmt"
	"reflect"
	"sort"
)

var (
	RevisionCache = make(map[string]interface{})
)

type Revision struct {
	Config   *DataRevision
	Children map[string][]*Revision
	Hash     string
	branch   *Branch
	WeakRef  string
}

func NewRevision(branch *Branch, data interface{}, children map[string][]*Revision) *Revision {
	cr := &Revision{}
	cr.branch = branch
	cr.Config = NewDataRevision(data)
	cr.Children = children
	cr.finalize()

	return cr
}

func (cr *Revision) finalize() {
	cr.Hash = cr.hashContent()

	if _, exists := RevisionCache[cr.Hash]; !exists {
		RevisionCache[cr.Hash] = cr
	}
	if _, exists := RevisionCache[cr.Config.Hash]; !exists {
		RevisionCache[cr.Config.Hash] = cr.Config
	} else {
		cr.Config = RevisionCache[cr.Config.Hash].(*DataRevision)
	}
}

func (cr *Revision) hashContent() string {
	var buffer bytes.Buffer
	var childrenKeys []string

	if cr.Config != nil {
		buffer.WriteString(cr.Config.Hash)
	}

	for key, _ := range cr.Children {
		childrenKeys = append(childrenKeys, key)
	}
	sort.Strings(childrenKeys)

	if cr.Children != nil && len(cr.Children) > 0 {
		// Loop through sorted Children keys
		for _, key := range childrenKeys {
			for _, child := range cr.Children[key] {
				buffer.WriteString(child.Hash)
			}
		}
	}

	return fmt.Sprintf("%x", md5.Sum(buffer.Bytes()))[:12]
}

func (cr *Revision) getData() interface{} {
	if cr.Config == nil {
		return nil
	}
	return cr.Config.Data
}

func (cr *Revision) getNode() *Node {
	return cr.branch.node
}

func (cr *Revision) getType() reflect.Type {
	// TODO: what is this returning really?
	return reflect.TypeOf(cr.getData())
}

func (cr *Revision) clearHash() {
	cr.Hash = ""
}

func (cr *Revision) Get(depth int) interface{} {
	originalData := cr.getData()
	data := Clone(originalData)

	if depth > 0 {
		for fieldName, field := range ChildrenFields(cr.getType()) {
			if field.IsContainer {
				for _, rev := range cr.Children[fieldName] {
					childData := rev.Get(depth - 1)
					childDataHolder := GetAttributeValue(data, fieldName, 0)
					// TODO: merge with childData
					fmt.Printf("data:%+v, dataHolder:%+v", childData, childDataHolder)
				}
			} else {
				rev := cr.Children[fieldName][0]
				childData := rev.Get(depth - 1)
				childDataHolder := GetAttributeValue(data, fieldName, 0)
				// TODO: merge with childData
				fmt.Printf("data:%+v, dataHolder:%+v", childData, childDataHolder)
			}
		}
	}
	return data
}

func (cr *Revision) UpdateData(data interface{}, branch *Branch) *Revision {
	newRev := Clone(cr).(*Revision)
	newRev.branch = branch
	newRev.Config = data.(*DataRevision)
	newRev.finalize()

	return newRev
}

func (cr *Revision) UpdateChildren(name string, children []*Revision, branch *Branch) *Revision {
	newChildren := Clone(cr.Children).(map[string][]*Revision)
	newChildren[name] = children
	newRev := Clone(cr).(*Revision)
	newRev.branch = branch
	newRev.Children = newChildren
	newRev.finalize()

	return newRev
}

func (cr *Revision) UpdateAllChildren(children map[string][]*Revision, branch *Branch) *Revision {
	newRev := Clone(cr).(*Revision)
	newRev.branch = branch
	newRev.Children = children
	newRev.finalize()

	return newRev
}
