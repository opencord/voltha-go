package model

type CallbackType uint8

const (
	GET CallbackType = iota
	PRE_UPDATE
	POST_UPDATE
	PRE_ADD
	POST_ADD
	PRE_REMOVE
	POST_REMOVE
	POST_LISTCHANGE
)

var enumCallbackTypes = []string{
	"GET",
	"PRE_UPDATE",
	"POST_UPDATE",
	"PRE_ADD",
	"POST_ADD",
	"PRE_REMOVE",
	"POST_REMOVE",
	"POST_LISTCHANGE",
}

func (t CallbackType) String() string {
	return enumCallbackTypes[t]
}
