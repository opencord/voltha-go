// +build integration

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
	"os"
	//"fmt"
	//"flag"
	//"path"
	//"bufio"
	//"errors"
	//"os/exec"
	//"strconv"
	//"io/ioutil"
	//"encoding/json"
	"text/template"
	//"github.com/golang/protobuf/proto"
	"github.com/opencord/voltha-lib-go/v3/pkg/log"
	//pb "github.com/golang/protobuf/protoc-gen-go/descriptor"
)

type test struct {
	Core  int
	SerNo int
}

type suite struct {
	CrTests  []test
	GetTests []test
}

const SUITE_LEN = 55000

//const SUITE_LEN = 100

func main() {

	var ary suite

	// Setup logging
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	for i := 0; i < SUITE_LEN; i++ {
		ary.CrTests = append(ary.CrTests, test{Core: (i % 3) + 1, SerNo: i})
		ary.GetTests = append(ary.GetTests, test{Core: (i % 3) + 1, SerNo: i + SUITE_LEN})
	}

	// Load the template to execute
	t := template.Must(template.New("").ParseFiles("./test2.tmpl.json"))
	if f, err := os.Create("test2.json"); err == nil {
		defer f.Close()
		if err := t.ExecuteTemplate(f, "test2.tmpl.json", ary); err != nil {
			log.Errorf("Unable to execute template for test2.tmpl.json: %v", err)
		}
	} else {
		log.Errorf("Couldn't create file test2.json: %v", err)
	}
	return
}
