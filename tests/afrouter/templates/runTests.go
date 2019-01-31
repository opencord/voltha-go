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


package main

import (
	"fmt"
	"time"
	"errors"
	"encoding/json"
	"github.com/opencord/voltha-go/common/log"
	{{range .Imports}}
	_ "{{.}}"
	{{end}}
	{{range .ProtoImports}}
	{{.Short}} "{{.Package}}"
	{{end}}
)

func runTests() {
	{{range $k,$v := .Tests}}
	if err := test{{$k}}(); err == nil {
		resFile.Write([]byte("\tTest Successful\n"))
	} else {
		resFile.Write([]byte("\tTest Failed\n"))
	}
	{{end}}
	time.Sleep(5 * time.Second)
}

{{range $k,$v := .Tests}}
func test{{$k}}() error {
	var rtrn error = nil
	// Announce the test being run
	resFile.Write([]byte("******************** Running test case: {{$v.Name}}\n"))
	// Acquire the client used to run the test
	cl := clients["{{$v.Send.Client}}"]
	// Create the server's reply data structure
	repl := &reply{repl:&{{$v.Send.ExpectType}}{{$v.Send.Expect}}}
	// Send the reply data structure to each of the servers
	{{range $s := .Srvr}}
	if servers["{{$s.Name}}"] == nil {
		log.Error("Server '{{$s.Name}}' is nil")
		return errors.New("GAAK")
	}
	servers["{{$s.Name}}"].replyData <- repl
	{{end}}

	// Now call the RPC with the data provided
	if expct,err := json.Marshal(repl.repl); err != nil {
		log.Errorf("Marshaling the reply for test {{$v.Name}}: %v",err)
	} else {
		if err := cl.send("{{$v.Send.Method}}",
							&{{$v.Send.ParamType}}{{$v.Send.Param}},
							string(expct)); err != nil {
			log.Errorf("Test case {{$v.Name}} failed!: %v", err)

		}
	}

	// Now read the servers' information to validate it
	var s *serverCtl
	var payload string
	var i *incoming
	if pld, err := json.Marshal(&{{$v.Send.ParamType}}{{$v.Send.Param}}); err != nil {
		log.Errorf("Marshaling paramter for test {{$v.Name}}: %v", err)
	} else {
		payload = string(pld)
	}
	{{range $s := .Srvr}}
	s = servers["{{$s.Name}}"]
	i = <-s.incmg
	if i.payload != payload {
		log.Errorf("Mismatched payload for test {{$v.Name}}, %s:%s", i.payload, payload)
		resFile.Write([]byte(fmt.Sprintf("Mismatched payload expected %s, got %s\n", payload, i.payload)))
		rtrn = errors.New("Failed")
	}
	{{range $m := $s.Meta}}
	if mv,ok := i.meta["{{$m.Key}}"]; ok == true {
		if "{{$m.Val}}" != mv[0] {
			log.Errorf("Mismatched metadata for test {{$v.Name}}, %s:%s", mv[0], "{{$m.Val}}")
			resFile.Write([]byte(fmt.Sprintf("Mismatched metadata on server %s expected %s, got %s\n", "{{$s.Name}}", "{{$m.Val}}", mv[0])))
			rtrn = errors.New("Failed")
		}
	}
	{{end}}
	{{end}}

	return rtrn
}
{{end}}


