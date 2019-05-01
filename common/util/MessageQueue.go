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

package util

import (
	"fmt"
	"github.com/golang-collections/go-datastructures/queue"
	"github.com/opencord/voltha-go/common/log"
)

type MessageQueue struct {
	queue   *queue.Queue
	waiting *queue.Queue
}

func NewMessageQueue() *MessageQueue {
	var msgQ MessageQueue

	msgQ.queue = queue.New(64)
	msgQ.waiting = queue.New(64)

	log.Debugln("NewMessageQueue", msgQ)

	return &msgQ
}

func (e MessageQueue) reset() {
	log.Debugln("reset", e)
	//for d, _ in self.waiting:
	//   d.errback(Exception('message queue reset() was called'))
	e.waiting.Empty()
	e.queue.Empty()
}

func (e MessageQueue) _cancelGet(d int) {
	log.Debugln("_cancelGet", e)
	/*
	       for (i = 0;i < len(e.waiting); i++) {
	           //if (e.waiting.Get[i][0] == d) {
	            //       e.waiting.Pop(i)
	   	}
	        }
	*/
}

// should be and object, not a string - TODO:
func (e MessageQueue) put(tmpObj string) {

	// if someone is waiting for this, return right away
	/*
	   for i in range(len(self.waiting)):
	       d, predicate = self.waiting[i]
	       if predicate is None or predicate(obj):
	           self.waiting.pop(i)
	           d.callback(obj)
	           return
	*/
	// otherwise...
	e.queue.Put(tmpObj)
}

// find out what type predicate is
func (e MessageQueue) Get(predicate int64) []interface{} {

	var lenQ = e.queue.Len()
	if lenQ > 0 {
		msg, _ := e.queue.Get(predicate)
		fmt.Printf("Len: %d", len(msg))
		return msg
	}
	return nil
	//	e.queue

}

/* python

   def get(self, predicate=None):
       """
       Attempt to retrieve and remove an object from the queue that
       matches the optional predicate.
       :return: Deferred which fires with the next object available.
       If predicate was provided, only objects for which
       predicate(obj) is True will be considered.
       """
       for i in range(len(self.queue)):
           msg = self.queue[i]
           if predicate is None or predicate(msg):
               self.queue.pop(i)
               return succeed(msg)

       # there were no matching entries if we got here, so we wait
       d = Deferred(canceller=self._cancelGet)
       self.waiting.append((d, predicate))
       return d
*/
