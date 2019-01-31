
package main

import (
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
	test{{$k}}()
	{{end}}
	time.Sleep(5 * time.Second)
}

{{range $k,$v := .Tests}}
func test{{$k}}() error {
	// Announce the test being run
	log.Info("******************** Running test case: {{$v.Name}}")
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

	// Now call the RPC witht the data provided
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
	}
	{{range $m := $s.Meta}}
	if mv,ok := i.meta["{{$m.Key}}"]; ok == true {
		if "{{$m.Val}}" != mv[0] {
			log.Errorf("Mismatched metadata for test {{$v.Name}}, %s:%s", mv, "{{$m.Val}}")
		}
	}
	{{end}}
	{{end}}

	return nil
}
{{end}}


