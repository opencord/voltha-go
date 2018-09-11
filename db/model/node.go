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

// TODO: proper error handling
// TODO: proper logging

import (
	"fmt"
	"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-go/common/log"
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
	Tags      map[string]Revision
	Proxy     *Proxy
	EventBus  *EventBus
	AutoPrune bool
}

type ChangeTuple struct {
	Type CallbackType
	Data interface{}
}

func NewNode(root *Root, initialData interface{}, autoPrune bool, txid string) *Node {
	n := &Node{}

	n.root = root
	n.Branches = make(map[string]*Branch)
	n.Tags = make(map[string]Revision)
	n.Proxy = nil
	n.EventBus = nil
	n.AutoPrune = autoPrune

	if IsProtoMessage(initialData) {
		n.Type = reflect.ValueOf(initialData).Interface()
		dataCopy := proto.Clone(initialData.(proto.Message))
		n.initialize(dataCopy, txid)
	} else if reflect.ValueOf(initialData).IsValid() {
		n.Type = reflect.ValueOf(initialData).Interface()
	} else {
		// not implemented error
		fmt.Errorf("cannot process initial data - %+v", initialData)
	}

	return n
}

func (n *Node) MakeNode(data interface{}, txid string) *Node {
	return NewNode(n.root, data, true, txid)
}

func (n *Node) MakeRevision(branch *Branch, data interface{}, children map[string][]Revision) Revision {
	return n.root.MakeRevision(branch, data, children)
}

func (n *Node) MakeLatest(branch *Branch, revision Revision, changeAnnouncement []ChangeTuple) {
	if _, ok := branch.Revisions[revision.GetHash()]; !ok {
		branch.Revisions[revision.GetHash()] = revision
	}

	if branch.Latest == nil || revision.GetHash() != branch.Latest.GetHash() {
		branch.Latest = revision
	}

	if changeAnnouncement != nil && branch.Txid == "" {
		if n.Proxy != nil {
			for _, change := range changeAnnouncement {
				// TODO: Invoke callback
				fmt.Printf("invoking callback - changeType: %+v, data:%+v\n", change.Type, change.Data)
				n.root.addCallback(n.Proxy.InvokeCallbacks, change.Type, change.Data, true)
			}
		}

		for _, change := range changeAnnouncement {
			// TODO: send notifications
			fmt.Printf("sending notification - changeType: %+v, data:%+v\n", change.Type, change.Data)
			n.root.addNotificationCallback(n.makeEventBus().Advertise, change.Type, change.Data, revision.GetHash())
		}
	}
}

func (n *Node) Latest() Revision {
	if branch, exists := n.Branches[NONE]; exists {
		return branch.Latest
	}
	return nil
}

func (n *Node) GetHash(hash string) Revision {
	return n.Branches[NONE].Revisions[hash]
}

func (n *Node) initialize(data interface{}, txid string) {
	var children map[string][]Revision
	children = make(map[string][]Revision)
	for fieldName, field := range ChildrenFields(n.Type) {
		_, fieldValue := GetAttributeValue(data, fieldName, 0)

		if fieldValue.IsValid() {
			if field.IsContainer {
				if field.Key != "" {
					var keysSeen []string

					for i := 0; i < fieldValue.Len(); i++ {
						v := fieldValue.Index(i)
						rev := n.MakeNode(v.Interface(), txid).Latest()
						_, key := GetAttributeValue(v.Interface(), field.Key, 0)
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
						children[fieldName] = append(children[fieldName], n.MakeNode(v.Interface(), txid).Latest())
					}
				}
			} else {
				children[fieldName] = append(children[fieldName], n.MakeNode(fieldValue.Interface(), txid).Latest())
			}
		} else {
			fmt.Errorf("field is invalid - %+v", fieldValue)
		}
	}
	// FIXME: ClearField???  No such method in go protos.  Reset?
	//data.ClearField(field_name)
	branch := NewBranch(n, "", nil, n.AutoPrune)
	rev := n.MakeRevision(branch, data, children)
	n.MakeLatest(branch, rev, nil)

	if txid == "" {
		n.Branches[NONE] = branch
	} else {
		n.Branches[txid] = branch
	}
}

//
// Get operation
//
func (n *Node) Get(path string, hash string, depth int, deep bool, txid string) interface{} {
	if deep {
		depth = -1
	}

	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var rev Revision

	// FIXME: should empty txid be cleaned up?
	if branch = n.Branches[txid]; txid == "" || branch == nil {
		branch = n.Branches[NONE]
	}

	if hash != "" {
		rev = branch.Revisions[hash]
	} else {
		rev = branch.Latest
	}

	return n.get(rev, path, depth)
}

func (n *Node) findRevByKey(revs []Revision, keyName string, value string) (int, Revision) {
	for i, rev := range revs {
		dataValue := reflect.ValueOf(rev.GetData())
		dataStruct := GetAttributeStructure(rev.GetData(), keyName, 0)

		fieldValue := dataValue.Elem().FieldByName(dataStruct.Name)

		if fieldValue.Interface().(string) == value {
			return i, rev
		}
	}

	fmt.Errorf("key %s=%s not found", keyName, value)

	return -1, nil
}

func (n *Node) get(rev Revision, path string, depth int) interface{} {
	if path == "" {
		return n.doGet(rev, depth)
	}

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]

	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	names := ChildrenFields(n.Type)
	field := names[name]

	if field != nil && field.IsContainer {
		if field.Key != "" {
			children := rev.GetChildren()[name]
			if path != "" {
				partition = strings.SplitN(path, "/", 2)
				key := partition[0]
				path = ""
				key = field.KeyFromStr(key).(string)
				if _, childRev := n.findRevByKey(children, field.Key, key); childRev == nil {
					return nil
				} else {
					childNode := childRev.GetNode()
					return childNode.get(childRev, path, depth)
				}
			} else {
				var response []interface{}
				for _, childRev := range children {
					childNode := childRev.GetNode()
					value := childNode.doGet(childRev, depth)
					response = append(response, value)
				}
				return response
			}
		} else {
			var response []interface{}
			if path != "" {
				// TODO: raise error
				return response
			}
			for _, childRev := range rev.GetChildren()[name] {
				childNode := childRev.GetNode()
				value := childNode.doGet(childRev, depth)
				response = append(response, value)
			}
			return response
		}
	} else {
		c1 := rev.GetChildren()[name]
		childRev := c1[0]
		childNode := childRev.GetNode()
		return childNode.get(childRev, path, depth)
	}
	return nil
}

func (n *Node) doGet(rev Revision, depth int) interface{} {
	msg := rev.Get(depth)

	if n.Proxy != nil {
		log.Debug("invoking proxy GET Callbacks")
		msg = n.Proxy.InvokeCallbacks(GET, msg, false)

	}
	return msg
}

//
// Update operation
//
func (n *Node) Update(path string, data interface{}, strict bool, txid string, makeBranch t_makeBranch) Revision {
	// FIXME: is this required ... a bit overkill to take out a "/"
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}

	var branch *Branch
	var ok bool
	if txid == "" {
		branch = n.Branches[NONE]
	} else if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	log.Debugf("Branch data : %+v, Passed data: %+v", branch.Latest.GetData(), data)

	if path == "" {
		return n.doUpdate(branch, data, strict)
	}

	// TODO missing some code here...
	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]

	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	var children []Revision

	if field.IsContainer {
		if path == "" {
			fmt.Errorf("cannot update a list\n")
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			key = field.KeyFromStr(key).(string)
			// TODO. Est-ce que le copy ne fonctionne pas? dois-je plutôt faire un clone de chaque item?
			for _, v := range rev.GetChildren()[name] {
				revCopy := reflect.ValueOf(v).Interface().(Revision)
				children = append(children, revCopy)
			}
			idx, childRev := n.findRevByKey(children, field.Key, key)
			childNode := childRev.GetNode()
			newChildRev := childNode.Update(path, data, strict, txid, makeBranch)
			if newChildRev.GetHash() == childRev.GetHash() {
				if newChildRev != childRev {
					log.Debug("clear-hash - %s %+v", newChildRev.GetHash(), newChildRev)
					newChildRev.ClearHash()
				}
				return branch.Latest
			}
			if _, newKey := GetAttributeValue(newChildRev.GetData(), field.Key, 0); newKey.Interface().(string) != key {
				fmt.Errorf("cannot change key field\n")
			}
			children[idx] = newChildRev
			rev = rev.UpdateChildren(name, children, branch)
			n.root.MakeLatest(branch, rev, nil)
			return rev
		} else {
			fmt.Errorf("cannot index into container with no keys\n")
		}
	} else {
		childRev := rev.GetChildren()[name][0]
		childNode := childRev.GetNode()
		newChildRev := childNode.Update(path, data, strict, txid, makeBranch)
		rev = rev.UpdateChildren(name, []Revision{newChildRev}, branch)
		n.root.MakeLatest(branch, rev, nil)
		return rev
	}
	return nil
}

func (n *Node) doUpdate(branch *Branch, data interface{}, strict bool) Revision {
	log.Debugf("Comparing types - expected: %+v, actual: %+v", reflect.ValueOf(n.Type).Type(), reflect.TypeOf(data))

	if reflect.TypeOf(data) != reflect.ValueOf(n.Type).Type() {
		// TODO raise error
		fmt.Errorf("data does not match type: %+v", n.Type)
		return nil
	}

	// TODO: validate that this actually works
	//if n.hasChildren(data) {
	//	return nil
	//}

	if n.Proxy != nil {
		log.Debug("invoking proxy PRE_UPDATE Callbacks")
		n.Proxy.InvokeCallbacks(PRE_UPDATE, data, false)
	}
	if !reflect.DeepEqual(branch.Latest.GetData(), data) {
		if strict {
			// TODO: checkAccessViolations(data, Branch.GetLatest.data)
			fmt.Println("checking access violations")
		}
		rev := branch.Latest.UpdateData(data, branch)
		n.root.MakeLatest(branch, rev, nil) // TODO -> changeAnnouncement needs to be a tuple (CallbackType.
		// POST_UPDATE, rev.data)
		return rev
	} else {
		return branch.Latest
	}
}

//
// Add operation
//
func (n *Node) Add(path string, data interface{}, txid string, makeBranch t_makeBranch) Revision {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		fmt.Errorf("cannot add for non-container mode\n")
	}

	var branch *Branch
	var ok bool
	if txid == "" {
		branch = n.Branches[NONE]
	} else if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]

	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	var children []Revision

	if field.IsContainer {
		if path == "" {
			if field.Key != "" {
				if n.Proxy != nil {
					log.Debug("invoking proxy PRE_ADD Callbacks")
					n.Proxy.InvokeCallbacks(PRE_ADD, data, false)
				}

				for _, v := range rev.GetChildren()[name] {
					revCopy := reflect.ValueOf(v).Interface().(Revision)
					children = append(children, revCopy)
				}
				_, key := GetAttributeValue(data, field.Key, 0)
				if _, rev := n.findRevByKey(children, field.Key, key.String()); rev != nil {
					// TODO raise error
					fmt.Errorf("duplicate key found: %s", key.String())
				}

				childRev := n.MakeNode(data, "").Latest()
				children = append(children, childRev)
				rev := rev.UpdateChildren(name, children, branch)
				n.root.MakeLatest(branch, rev, nil) // TODO -> changeAnnouncement needs to be a tuple (CallbackType.
				// POST_ADD, rev.data)
				return rev
			} else {
				fmt.Errorf("cannot add to non-keyed container\n")
			}
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			key = field.KeyFromStr(key).(string)
			copy(children, rev.GetChildren()[name])
			idx, childRev := n.findRevByKey(children, field.Key, key)
			childNode := childRev.GetNode()
			newChildRev := childNode.Add(path, data, txid, makeBranch)
			children[idx] = newChildRev
			rev := rev.UpdateChildren(name, children, branch)
			n.root.MakeLatest(branch, rev, nil)
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
func (n *Node) Remove(path string, txid string, makeBranch t_makeBranch) Revision {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		// TODO raise error
		fmt.Errorf("cannot remove for non-container mode\n")
	}
	var branch *Branch
	var ok bool
	if txid == "" {
		branch = n.Branches[NONE]
	} else if branch, ok = n.Branches[txid]; !ok {
		branch = makeBranch(n)
	}

	rev := branch.Latest

	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	if len(partition) < 2 {
		path = ""
	} else {
		path = partition[1]
	}

	field := ChildrenFields(n.Type)[name]
	var children []Revision
	postAnnouncement := []ChangeTuple{}

	if field.IsContainer {
		if path == "" {
			fmt.Errorf("cannot remove without a key\n")
		} else if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			if len(partition) < 2 {
				path = ""
			} else {
				path = partition[1]
			}
			key = field.KeyFromStr(key).(string)
			if path != "" {
				for _, v := range rev.GetChildren()[name] {
					newV := reflect.ValueOf(v).Interface().(Revision)
					children = append(children, newV)
				}
				idx, childRev := n.findRevByKey(children, field.Key, key)
				childNode := childRev.GetNode()
				newChildRev := childNode.Remove(path, txid, makeBranch)
				children[idx] = newChildRev
				rev := rev.UpdateChildren(name, children, branch)
				n.root.MakeLatest(branch, rev, nil)
				return rev
			} else {
				for _, v := range rev.GetChildren()[name] {
					newV := reflect.ValueOf(v).Interface().(Revision)
					children = append(children, newV)
				}
				idx, childRev := n.findRevByKey(children, field.Key, key)
				if n.Proxy != nil {
					data := childRev.GetData()
					n.Proxy.InvokeCallbacks(PRE_REMOVE, data, false)
					postAnnouncement = append(postAnnouncement, ChangeTuple{POST_REMOVE, data})
				} else {
					postAnnouncement = append(postAnnouncement, ChangeTuple{POST_REMOVE, childRev.GetData()})
				}
				children = append(children[:idx], children[idx+1:]...)
				rev := rev.UpdateChildren(name, children, branch)
				n.root.MakeLatest(branch, rev, postAnnouncement)
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

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Branching ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

type t_makeBranch func(*Node) *Branch

func (n *Node) makeTxBranch(txid string) *Branch {
	branchPoint := n.Branches[NONE].Latest
	branch := NewBranch(n, txid, branchPoint, true)
	n.Branches[txid] = branch
	return branch
}

func (n *Node) deleteTxBranch(txid string) {
	delete(n.Branches, txid)
}

func (n *Node) mergeChild(txid string, dryRun bool) func(Revision) Revision {
	f := func(rev Revision) Revision {
		childBranch := rev.GetBranch()

		if childBranch.Txid == txid {
			rev, _ = childBranch.Node.mergeTxBranch(txid, dryRun)
		}

		return rev
	}
	return f
}

func (n *Node) mergeTxBranch(txid string, dryRun bool) (Revision, error) {
	srcBranch := n.Branches[txid]
	dstBranch := n.Branches[NONE]

	forkRev := srcBranch.Origin
	srcRev := srcBranch.Latest
	dstRev := dstBranch.Latest

	rev, changes := Merge3Way(forkRev, srcRev, dstRev, n.mergeChild(txid, dryRun), dryRun)

	if !dryRun {
		n.root.MakeLatest(dstBranch, rev, changes)
		delete(n.Branches, txid)
	}

	// TODO: return proper error when one occurs
	return rev, nil
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Diff utility ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

//func (n *Node) diff(hash1, hash2, txid string) {
//	branch := n.Branches[txid]
//	rev1 := branch.get(hash1)
//	rev2 := branch.get(hash2)
//
//	if rev1.GetHash() == rev2.GetHash() {
//		// empty patch
//	} else {
//		// translate data to json and generate patch
//		patch, err := jsonpatch.MakePatch(rev1.GetData(), rev2.GetData())
//		patch.
//	}
//}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Tag utility ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

// TODO: is tag mgmt used in the python implementation? Need to validate

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Internals ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

func (n *Node) hasChildren(data interface{}) bool {
	for fieldName, field := range ChildrenFields(n.Type) {
		_, fieldValue := GetAttributeValue(data, fieldName, 0)

		if (field.IsContainer && fieldValue.Len() > 0) || !fieldValue.IsNil() {
			log.Error("cannot update external children")
			return true
		}
	}

	return false
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Node Proxy ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

func (n *Node) GetProxy(path string, exclusive bool) *Proxy {
	return n.getProxy(path, n.root, path, exclusive)
}
func (n *Node) getProxy(path string, root *Root, fullPath string, exclusive bool) *Proxy {
	for strings.HasPrefix(path, "/") {
		path = path[1:]
	}
	if path == "" {
		return n.makeProxy(n.root, path, exclusive)
	}

	rev := n.Branches[NONE].Latest
	partition := strings.SplitN(path, "/", 2)
	name := partition[0]
	path = partition[1]

	field := ChildrenFields(n.Type)[name]
	if field.IsContainer {
		if path == "" {
			log.Error("cannot proxy a container field")
		}
		if field.Key != "" {
			partition := strings.SplitN(path, "/", 2)
			key := partition[0]
			path = partition[1]
			key = field.KeyFromStr(key).(string)
			children := rev.GetChildren()[name]
			_, childRev := n.findRevByKey(children, field.Key, key)
			childNode := childRev.GetNode()
			return childNode.getProxy(path, root, fullPath, exclusive)
		}
		log.Error("cannot index into container with no keys")
	} else {
		childRev := rev.GetChildren()[name][0]
		childNode := childRev.GetNode()
		return childNode.getProxy(path, root, fullPath, exclusive)
	}

	return nil
}

func (n *Node) makeProxy(root *Root, fullPath string, exclusive bool) *Proxy {
	if n.Proxy == nil {
		n.Proxy = NewProxy(root, n, fullPath, exclusive)
	} else {
		if n.Proxy.Exclusive {
			log.Error("node is already owned exclusively")
		}
	}
	return n.Proxy
}

func (n *Node) makeEventBus() *EventBus {
	if n.EventBus == nil {
		n.EventBus = NewEventBus()
	}
	return n.EventBus
}

// ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~ Persistence Loading ~~~~~~~~~~~~~~~~~~~~~~~~~~~~~~

func (n *Node) LoadLatest(kvStore *Backend, hash string) {
	branch := NewBranch(n, "", nil, n.AutoPrune)
	pr := &PersistedRevision{}
	rev := pr.Load(branch, kvStore, n.Type, hash)
	n.MakeLatest(branch, rev, nil)
	n.Branches[NONE] = branch
}
