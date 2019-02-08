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
	"context"
	"encoding/json"
	//"golang.org/x/net/context"
	"google.golang.org/grpc/metadata"
	"github.com/opencord/voltha-go/common/log"
	{{range .Imports}}
	_ "{{.}}"
	{{end}}
	{{range .ProtoImports}}
	{{.Short}} "{{.Package}}"
	{{end}}
)

var glCtx context.Context

func resetChannels() {
	// Drain all channels of data
	for _,v := range servers {
		done := false
		for {
			select {
			case _ = <-v.incmg:
			case _ = <-v.replyData:
			default:
				done = true
			}
			if done == true {
				break
			}
		}
	}
}

func runTests() {
	{{range $k,$v := .Tests}}
	if err := test{{$k}}(); err == nil {
		resFile.testLog("\tTest Successful\n")
	} else {
		resFile.testLog("\tTest Failed\n")
	}
	//resetChannels()
	{{end}}
}

{{range $k,$v := .Tests}}
func test{{$k}}() error {
	var rtrn error = nil
	var cancel context.CancelFunc

	glCtx, cancel = context.WithTimeout(context.Background(), 900*time.Millisecond)
	defer cancel()
	// Announce the test being run
	resFile.testLog("******************** Running test case ({{$k}}): {{$v.Name}}\n")
	// Acquire the client used to run the test
	cl := clients["{{$v.Send.Client}}"]
	// Create the server's reply data structure
	repl := &reply{repl:&{{$v.Send.ExpectType}}{{$v.Send.Expect}}}
	// Send the reply data structure to each of the servers
	{{range $s := .Srvr}}
	if servers["{{$s.Name}}"] == nil {
		err := errors.New("Server '{{$s.Name}}' is nil")
		log.Error(err)
		return err
	}
	// Start a go routine to send the the reply data to the
	// server. The go routine blocks until the server picks
	// up the data or the timeout is exceeded.
	go func (ctx context.Context) {
		select {
		case servers["{{$s.Name}}"].replyData <- repl:
		case <-ctx.Done():
			rtrn := errors.New("Could not provide server {{$s.Name}} with reply data")
			log.Error(rtrn)
			resFile.testLog("%s\n", rtrn.Error())
		}
	}(glCtx)
	{{end}}

	// Now call the RPC with the data provided
	if expct,err := json.Marshal(repl.repl); err != nil {
		log.Errorf("Marshaling the reply for test {{$v.Name}}: %v",err)
	} else {
		// Create the context for the call
		ctx := context.Background()
		{{range $m := $v.Send.MetaData}}
		ctx = metadata.AppendToOutgoingContext(ctx, "{{$m.Key}}", "{{$m.Val}}")
		{{end}}
		var md map[string]string = make(map[string]string)
		{{range $m := $v.Send.ExpectMeta}}
			md["{{$m.Key}}"] = "{{$m.Val}}"
		{{end}}
		expectMd := metadata.New(md)
		if err := cl.send("{{$v.Send.Method}}", ctx,
							&{{$v.Send.ParamType}}{{$v.Send.Param}},
							string(expct), expectMd); err != nil {
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
	// Oddly sometimes the data isn't in the channel yet when we come to read it.
	select {
	case i = <-s.incmg:
		if i.payload != payload {
			rtrn = errors.New(fmt.Sprintf("Mismatched payload expected '%s', got '%s'", payload, i.payload))
			log.Error(rtrn.Error())
			resFile.testLog("%s\n", rtrn.Error())
		}
		{{range $m := $s.Meta}}
		if mv,ok := i.meta["{{$m.Key}}"]; ok == true {
			if "{{$m.Val}}" != mv[0] {
				rtrn=errors.New(fmt.Sprintf("Mismatched metadata on server '%s' expected '%s', got '%s'", "{{$s.Name}}", "{{$m.Val}}", mv[0]))
				log.Error(rtrn.Error())
				resFile.testLog("%s\n", rtrn.Error())
			}
		}
		{{end}}
	case <-glCtx.Done():
		rtrn = errors.New("Timeout: no response data available for server {{$s.Name}}")
		resFile.testLog("%s\n", rtrn.Error())
		log.Error(rtrn)
	}
	{{end}}

	return rtrn
}
{{end}}


