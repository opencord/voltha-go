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

// TODO: implement weak references or something equivalent
// TODO: missing proper logging

type Branch struct {
	node      *Node
	Txid      string
	origin    *Revision
	revisions map[string]*Revision
	Latest    *Revision
}

func NewBranch(node *Node, txid string, origin *Revision, autoPrune bool) *Branch {
	cb := &Branch{}
	cb.node = node
	cb.Txid = txid
	cb.origin = origin
	cb.revisions = make(map[string]*Revision)
	cb.Latest = origin

	return cb
}

// TODO: Check if the following are required
func (cb *Branch) get(hash string) *Revision {
	return cb.revisions[hash]
}
func (cb *Branch) GetLatest() *Revision {
	return cb.Latest
}
func (cb *Branch) GetOrigin() *Revision {
	return cb.origin
}
