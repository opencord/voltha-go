package kafka

import (
	"context"
)

type callback func(bool, interface{})

// A Topic definition - may be augmented with additional attributes eventually
type Topic struct {
	// The name of the topic. It must start with a letter,
	// and contain only letters (`[A-Za-z]`), numbers (`[0-9]`), dashes (`-`),
	// underscores (`_`), periods (`.`), tildes (`~`), plus (`+`) or percent
	// signs (`%`).
	Name string
}

type KVArg struct {
	Key   string
	Value interface{}
}

// Client represents the set of APIs a Messaging Client must implement - In progress
type Client interface {
	Start()
	Stop()
	Subscribe(ctx context.Context, topic *Topic, cb callback, targetInterfaces ...interface{})
	Publish(ctx context.Context, rpc string, cb callback, topic *Topic, waitForResponse bool, kvArgs ...*KVArg)
	Unsubscribe(ctx context.Context, topic *Topic)
}
