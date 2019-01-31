// The template for the tester.
// This template is filled in by the
// test driver based on the configuration.

package main

import (
	"os"
	//"time"
	"os/exec"
	"github.com/opencord/voltha-go/common/log"
)

func main() {
	var cmd *exec.Cmd
	var cmdStr string
	// Setup logging
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}

	defer log.CleanUp()

	log.Info("Running tests")
	{{range .}}
	if err:= os.Chdir(os.Args[1]); err != nil {
		log.Error("Could not change directory to %s: %v", os.Args[1], err)
	}
	cmdStr =  "./"+"{{.}}"[:len("{{.}}")-5]
	log.Infof("Running test %s",cmdStr)
	cmd = exec.Command(cmdStr)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Errorf("Test '%s' failed", cmdStr)
	}
	{{end}}
	log.Info("Tests complete")
}
