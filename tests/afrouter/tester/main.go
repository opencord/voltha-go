// The template for the tester.
// This template is filled in by the
// test driver based on the configuration.

package main

import (
	"github.com/opencord/voltha-go/common/log"
)

func main() {
	// Setup logging
	/*
	if _, err := log.SetDefaultLogger(log.JSON, 0, nil); err != nil {
		log.With(log.Fields{"error": err}).Fatal("Cannot setup logging")
	}
	*/

	defer log.CleanUp()

	log.Info("Template runs!")
}
