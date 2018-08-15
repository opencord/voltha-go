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
package model

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"reflect"
	"strings"
)

const (
	NONE string = "none"
)

type Node struct {
	root      *Root
	Type      interface{}
	Branches  map[string]*Branch
	Tags      map[string]*Revision
	Proxy     *Proxy
	EventBus  *EventBus
	AutoPrune bool
}

func NewNode(root *Root, initialData interface{}, autoPrune bool, txid string) *Node {
	cn := &Node{}

	cn.root = root
	cn.Branches = make(map[string]*Branch)
	cn.Tags = make(map[string]*Revision)
	cn.Proxy = nil
	cn.EventBus = nil
	cn.AutoPrune = autoPrune

	if IsProtoMessage(initialData) {
		cn.Type = reflect.ValueOf(initialData).Interface()
		dataCopy := proto.Clone(initialData.(proto.Message))
		cn.initialize(dataCopy, txid)
	} else if reflect.ValueOf(initialData).IsValid() {
		cn.Type = reflect.ValueOf(initialData).Interface()
	} else {
		// not implemented error
		fmt.Errorf("cannot process initial data - %+v", initialData)
	}

	return cn
}

func (cn *Node) makeNode(data interface{}, txid string) *Node {
	return NewNode(cn.root, data, true, txid)
}

func (cn *Node) makeRevision(branch *Branch, data interface{}, children map[string][]*Revision) *Revision {
	return cn.root.makeRevision(branch, data, children)
}

func (cn *Node) makeLatest(branch *Branch, revision *Revision, changeAnnouncement map[string]interface{}) {
	if _, ok := branch.revisions[revision.Hash]; !ok {
		branch.revisions[revision.Hash] = revision
	}

	if branch.Latest == nil || revision.Hash != branch.Latest.Hash {
		branch.Latest = revision
	}

	if changeAnnouncement != nil && branch.Txid == "" {
		if cn.Proxy != nil {
			for changeType, data := range changeAnnouncement {
				// TODO: Invoke callback
				fmt.Printf("invoking callback - changeType: %+v, data:%+v\n", changeType, data)
			}
		}

		for changeType, data := range changeAnnouncement {
			// TODO: send notifications
			fmt.Printf("sending notification - changeType: %+v, data:%+v\n", changeType, data)
		}
	}
}

func (cn *Node) Latest() *Revision {
	if branch, exists := cn.Branches[NONE]; exists {
		return branch.Latest
	}
	return nil
}

func (cn *Node) GetHash(hash string) *Revision {
	return cn.Branches[NONE].revisions[hash]
}

func (cn *Node) initialize(data interface{}, txid string) {
	var children map[string][]*Revision
	children = make(map[string][]*Revision)
	for fieldName, field := range ChildrenFields(cn.Type) {
		fieldValue := GetAttributeValue(data, fieldName, 0)

		if fieldValue.IsValid() {
			if field.IsContainer {
				if field.Key != "" {
					var keysSeen []string

					for i := 0; i < fieldValue.Len(); i++ {
						v := fieldValue.Index(i)
						rev := cn.makeNode(v.Interface(), txid).Latest()
						key := GetAttributeValue(v.Interface(), field.Key, 0)
						for _, k := range keysSeen {
							if k == key.String() {
								fmt.Errorf("duplicate key - %s", k)
							}
						}
						children[fieldName] = append(children[fieldName], rev)
						keysSeen = append(keysSeen, key.String())
					}

				} else {
					for i := 0; i < fieldValue.Len(); i++ {
						v := fieldValue.Index(i)
						children[fieldName] = append(children[fieldName], cn.makeNode(v.Interface(), txid).Latest())
					}
				}
			} else {
				children[fieldName] = append(children[fieldName], cn.makeNode(fieldValue.Interface(), txid).Latest())
			}
		} else {
			fmt.Errorf("field is invalid - %+v", fieldValue)
		}
	}
	// FIXME: ClearField???  No such method in go protos.  Reset?
	//data.ClearField(field_name)
	branch := NewBranch(cn, "", nil, cn.AutoPrune)
	rev := cn.makeRevision(branch, data, children)
	cn.makeLatest(branch, rev, nil)
	cn.Branches[txid] = branch
}

func (cn *Node) makeTxBranch(txid string) *Branch {
	branchPoint := cn.Branches[NONE].Latest
	branch := NewBranch(cn, txid, branchPoint, true)
	cn.Branches[txid] = branch
	return branch
}

func (cn *Node) deleteTxBranch(txid string) {
	delete(cn.Branches, txid)
}

type t_makeBranch func(*Node) *Branch

//
// Get operation
//
func (cn *Node) Get(path string, hash string, depth int, deep bool, txid string) interface{} {
	if deep {
		depth = -1
	}

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var rev *Revision

	// FIXME: should empty txid be cleaned up?
	if branch = cn.Branches[txid]; txid == "" || branch == nil {
		branch = cn.Branches[NONE]
	}

	if hash != "" {
		rev = branch.revisions[hash]
	} else {
		rev = branch.Latest
	}

	return cn.get(rev, path, depth)
}

func (cn *Node) findRevByKey(revs []*Revision, keyName string, value string) (int, *Revision) {
	for i, rev := range revs {
		dataValue := reflect.ValueOf(rev.Config.Data)
		dataStruct := GetAttributeStructure(rev.Config.Data, keyName, 0)

		fieldValue := dataValue.Elem().FieldByName(dataStruct.Name)

		if fieldValue.Interface().(string) == value {
			return i, rev
		}
	}

	fmt.Errorf("key %s=%s not found", keyName, value)

	return -1, nil
}

func (cn *Node) get(rev *Revision, path string, depth int) interface{} {
	if path == "" {
		return cn.doGet(rev, depth)
	}

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	path = partition[1]

	field := ChildrenFields(cn.Type)[name]

	if field.IsContainer {
		if field.Key != "" {
			children := rev.Children[name]
			if path != "" {
				partition = strings.SplitN(path, "/", 2)
				key := partition[0]
				path = ""
				key = field.KeyFromStr(key).(string)
				_, childRev := cn.findRevByKey(children, field.Key, key)
				childNode := childRev.getNode()
				return childNode.get(childRev, path, depth)
			} else {
				var response []interface{}
				for _, childRev := range children {
					childNode := childRev.getNode()
					value := childNode.doGet(childRev, depth)
					response = append(response, value)
				}
				return response
			}
		} else {
			childRev := rev.Children[name][0]
			childNode := childRev.getNode()
			return childNode.get(childRev, path, depth)
		}
	}
	return nil
}

func (cn *Node) doGet(rev *Revision, depth int) interface{} {
	msg := rev.Get(depth)

	if cn.Proxy != nil {
		// TODO: invoke GET callback
		fmt.Println("invoking proxy GET Callbacks")
	}
	return msg
}

//
// Update operation
//
func (n *Node) Update(path string, data interface{}, strict bool, txid string, makeBranch t_makeBranch) *Revision {
	// FIXME: is this required ... a bit overkill to take out a "/"
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var ok bool
	if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	if path == "" {
		return n.doUpdate(branch, data, strict)
	}
	return &Revision{}
}

func (n *Node) doUpdate(branch *Branch, data interface{}, strict bool) *Revision {
	if reflect.TypeOf(data) != n.Type {
		// TODO raise error
		fmt.Errorf("data does not match type: %+v", n.Type)
	}

	// TODO: noChildren?

	if n.Proxy != nil {
		// TODO: n.proxy.InvokeCallbacks(CallbackType.PRE_UPDATE, data)
		fmt.Println("invoking proxy PRE_UPDATE Callbacks")
	}
	if branch.Latest.getData() != data {
		if strict {
			// TODO: checkAccessViolations(data, branch.GetLatest.data)
			fmt.Println("checking access violations")
		}
		rev := branch.Latest.UpdateData(data, branch)
		n.makeLatest(branch, rev, nil) // TODO -> changeAnnouncement needs to be a tuple (CallbackType.POST_UPDATE, rev.data)
		return rev
	} else {
		return branch.Latest
	}
}

// TODO: the python implementation has a method to check if the data has no children
//func (n *SomeNode) noChildren(data interface{}) bool {
//	for fieldName, field := range ChildrenFields(n.Type) {
//		fieldValue := GetAttributeValue(data, fieldName)
//
//		if fieldValue.IsValid() {
//			if field.IsContainer {
//				if len(fieldValue) > 0 {
//
//				}
//			} else {
//
//			}
//
//		}
//	}
//}

//
// Add operation
//
func (n *Node) Add(path string, data interface{}, txid string, makeBranch t_makeBranch) *Revision {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		fmt.Errorf("cannot add for non-container mode\n")
	}

	var branch *Branch
	var ok bool
	if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	path = partition[1]

	field := ChildrenFields(n.Type)[name]
	var children []*Revision

	if field.IsContainer {
		if path == "" {
			if field.Key != "" {
				if n.Proxy != nil {
					// TODO -> n.proxy.InvokeCallbacks(PRE_ADD, data)
					fmt.Println("invoking proxy PRE_ADD Callbacks")
				}

				copy(children, rev.Children[name])
				key := GetAttributeValue(data, field.Key, 0)
				if _, rev := n.findRevByKey(children, field.Key, key.String()); rev != nil {
					// TODO raise error
					fmt.Errorf("duplicate key found: %s", key.String())
				}

				childRev := n.makeNode(data, "").Latest()
				children = append(children, childRev)
				rev := rev.UpdateChildren(name, children, branch)
				n.makeLatest(branch, rev, nil) // TODO -> changeAnnouncement needs to be a tuple (CallbackType.POST_ADD, rev.data)
				return rev
			} else {
				fmt.Errorf("cannot add to non-keyed container\n")
			}
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			path = partition[1]
			key = field.KeyFromStr(key).(string)
			copy(children, rev.Children[name])
			idx, childRev := n.findRevByKey(children, field.Key, key)
			childNode := childRev.getNode()
			newChildRev := childNode.Add(path, data, txid, makeBranch)
			children[idx] = newChildRev
			rev := rev.UpdateChildren(name, children, branch)
			n.makeLatest(branch, rev, nil)
			return rev
		} else {
			fmt.Errorf("cannot add to non-keyed container\n")
		}
	} else {
		fmt.Errorf("cannot add to non-container field\n")
	}
	return nil
}

//
// Remove operation
//
func (n *Node) Remove(path string, txid string, makeBranch t_makeBranch) *Revision {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		fmt.Errorf("cannot remove for non-container mode\n")
	}
	var branch *Branch
	var ok bool
	if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	path = partition[1]

	field := ChildrenFields(n.Type)[name]
	var children []*Revision
	post_anno := make(map[string]interface{})

	if field.IsContainer {
		if path == "" {
			fmt.Errorf("cannot remove without a key\n")
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			path = partition[1]
			key = field.KeyFromStr(key).(string)
			if path != "" {
				copy(children, rev.Children[name])
				idx, childRev := n.findRevByKey(children, field.Key, key)
				childNode := childRev.getNode()
				newChildRev := childNode.Remove(path, txid, makeBranch)
				children[idx] = newChildRev
				rev := rev.UpdateChildren(name, children, branch)
				n.makeLatest(branch, rev, nil)
				return rev
			} else {
				copy(children, rev.Children[name])
				idx, childRev := n.findRevByKey(children, field.Key, key)
				if n.Proxy != nil {
					data := childRev.getData()
					fmt.Println("invoking proxy PRE_REMOVE Callbacks")
					fmt.Printf("setting POST_REMOVE Callbacks : %+v\n", data)
				} else {
					fmt.Println("setting POST_REMOVE Callbacks")
				}
				children = append(children[:idx], children[idx+1:]...)
				rev := rev.UpdateChildren(name, children, branch)
				n.makeLatest(branch, rev, post_anno)
				return rev
			}
		} else {
			fmt.Errorf("cannot add to non-keyed container\n")
		}
	} else {
		fmt.Errorf("cannot add to non-container field\n")
	}

	return nil
}

func (n *Node) LoadLatest(kvStore *Backend, hash string) {
	branch := NewBranch(n, "", nil, n.AutoPrune)
	pr := &PersistedRevision{}
	rev := pr.load(branch, kvStore, n.Type, hash)
	n.makeLatest(branch, rev.Revision, nil)
	n.Branches[NONE] = branch
}
