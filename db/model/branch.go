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
