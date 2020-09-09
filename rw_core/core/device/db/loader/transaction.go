package loader

import (
	"context"
	"fmt"

	"github.com/gogo/protobuf/proto"
	"github.com/opencord/voltha-go/db/model"
)

// a transaction is used for each request to guarantee tht the
type Txn interface {
	Close()
	Commit(ctx context.Context) error
	Remove(proxy *model.Proxy, key string, callback func(success bool))
	Set(proxy *model.Proxy, key string, message proto.Message, callback func(success bool))
	OnSuccess(callback func())
	CheckSaneLockOrder(id LockID)
}

type txn struct {
	proxy            *model.Proxy
	ops              map[string]proto.Message
	callbacks        []func(bool)
	successCallbacks []func()
	lastLocked       LockID
	done             bool
}

type Unlocker interface {
	Unlock()
}

func NewTxn() Txn {
	return &txn{
		ops: make(map[string]proto.Message),
	}
}

func (t *txn) Close() {
	t.runCallbacks(false)
}

func (t *txn) runCallbacks(success bool) {
	if t.done {
		return
	}
	t.done = true

	for _, callback := range t.callbacks {
		callback(success)
	}
	if success {
		for _, callback := range t.successCallbacks {
			callback()
		}
	}
	t.callbacks, t.successCallbacks = nil, nil
}

func (t *txn) Commit(ctx context.Context) error {
	if t.done {
		panic("commit on closed transaction")
	}
	defer t.Close()

	if err := t.proxy.BulkUpdate(ctx, t.ops); err != nil {
		return err
	}
	t.runCallbacks(true)
	return nil
}

func (t *txn) Remove(proxy *model.Proxy, key string, callback func(success bool)) {
	if t.done {
		panic("delete on closed transaction")
	}
	t.Set(proxy, key, nil, callback)
}

func (t *txn) Set(proxy *model.Proxy, key string, message proto.Message, callback func(success bool)) {
	if t.done {
		panic("update on closed transaction")
	}
	if _, have := t.ops[key]; have {
		panic(fmt.Sprintf("tried to update the key '%s' multiple times in a single transaction", key))
	}

	t.proxy = proxy
	t.callbacks = append(t.callbacks, callback)
	t.ops[key] = message
}

func (t *txn) OnSuccess(callback func()) {
	t.successCallbacks = append(t.successCallbacks, callback)
}
