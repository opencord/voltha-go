// The template for the tester.
// This template is filled in by the
// test driver based on the configuration.

package main

import (
	"os"
	"time"
	"os/exec"
	"strings"
	"context"
	"github.com/opencord/voltha-go/common/log"
)

func startSut(cmdStr string) (context.CancelFunc, error) {
	var err error = nil

	cmdAry := strings.Fields(cmdStr)
	log.Infof("Running the affinity router: %s",cmdStr)
	//ctx, cncl := context.WithCancel(context.Background())
	ctx, cncl := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cmdAry[0], cmdAry[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		log.Errorf("Failed to run the affinity router: %s %v", cmdStr,err)
	}
	time.Sleep(1 * time.Second) // Give the command time to get started
	return cncl, err
}

func cleanUp(cncl context.CancelFunc) {
	cncl()
	// Give the child processes time to terminate
	time.Sleep(1 * time.Second)
}

func main() {
	// Setup logging
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	defer log.CleanUp()

	// Initialize the servers
	if err := serverInit(); err != nil {
		log.Errorf("Error initializing server: %v", err)
		return
	}

	// Start the sofware under test
	cnclFunc, err := startSut("./{{.Command}}");
	defer cleanUp(cnclFunc)
	if  err != nil {
		return
	}

	// Initialize the clients
	if err := clientInit(); err != nil {
		log.Errorf("Error initializing client: %v", err)
		return
	}

	log.Infof("The servers are: %v",servers)

	// Run all the test cases now
	log.Infof("Executing tests")
	runTests()
}
