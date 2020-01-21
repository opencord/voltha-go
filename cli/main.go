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
	"flag"
	"fmt"
	"log"
	"os/exec"

	"github.com/opencord/voltha-go/cli/menu/mainmenu"
	l "github.com/opencord/voltha-lib-go/v3/pkg/log"
	"google.golang.org/grpc"
)

func main() {

	// disable input buffering
	err := exec.Command("stty", "-F", "/dev/tty", "cbreak", "min", "1").Run()
	if err != nil {
		l.Errorw("unable-to-configure-terminal-session-parameters-cbreak-and-min", l.Fields{"error": err})
	}
	// do not display entered characters on the screen
	err = exec.Command("stty", "-F", "/dev/tty", "-echo").Run()
	if err != nil {
		l.Errorw("unable-to-configure-terminal-session-parameter-echo", l.Fields{"error": err})
	}
	printHeader()

	volthaAddress := flag.String("voltha_address", "localhost:6161", "IP/Hostname:Port for Voltha Core")
	h := flag.Bool("h", false, "Print help")
	help := flag.Bool("help", false, "Print help")
	flag.Parse()
	if *h || *help {
		fmt.Println("cli -h(elp) print this message")
		fmt.Println("cli -voltha_address=$VOLTHA_ADDRESS:PORT default localhost:6161")
		return
	}
	conn, err := grpc.Dial(*volthaAddress, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("did not connect: %s", err)
	}
	defer conn.Close()
	fmt.Println("Connecting to " + *volthaAddress)

	mainmenu.MainLoop(conn)
}
func printHeader() {
	header :=
		`         _ _   _            ___ _    ___
__ _____| | |_| |_  __ _   / __| |  |_ _|
\ V / _ \ |  _| ' \/ _' | | (__| |__ | |
 \_/\___/_|\__|_||_\__,_|  \___|____|___|
`
	fmt.Println(header)
}
