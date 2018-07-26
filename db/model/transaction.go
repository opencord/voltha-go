package model

import "fmt"

type Transaction struct {
	proxy *Proxy
	txid  string
}

func NewTransaction(proxy *Proxy, txid string) *Transaction {
	tx := &Transaction{
		proxy: proxy,
		txid:  txid,
	}
	return tx
}
func (t *Transaction) Get(path string, depth int, deep bool) *Revision {
	if t.txid == "" {
		fmt.Errorf("closed transaction")
		return nil
	}
	// TODO: need to review the return values at the different layers!!!!!
	return t.proxy.Get(path, depth, deep, t.txid).(*Revision)
}
func (t *Transaction) Update(path string, data interface{}, strict bool) *Revision {
	if t.txid == "" {
		fmt.Errorf("closed transaction")
		return nil
	}
	return t.proxy.Update(path, data, strict, t.txid).(*Revision)
}
func (t *Transaction) Add(path string, data interface{}) *Revision {
	if t.txid == "" {
		fmt.Errorf("closed transaction")
		return nil
	}
	return t.proxy.Add(path, data, t.txid).(*Revision)
}
func (t *Transaction) Remove(path string) *Revision {
	if t.txid == "" {
		fmt.Errorf("closed transaction")
		return nil
	}
	return t.proxy.Remove(path, t.txid).(*Revision)
}
func (t *Transaction) Cancel() {
	t.proxy.cancelTransaction(t.txid)
	t.txid = ""
}
func (t *Transaction) Commit() {
	t.proxy.commitTransaction(t.txid)
	t.txid = ""
}
