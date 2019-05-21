package afrouter

import (
	"github.com/opencord/voltha-go/common/log"
	"regexp"
)

type methodDetails struct {
	all     string
	pkg     string
	service string
	method  string
}

// The compiled regex to extract the package/service/method
var mthdSlicer = regexp.MustCompile(`^/([a-zA-Z][a-zA-Z0-9]+)\.([a-zA-Z][a-zA-Z0-9]+)/([a-zA-Z][a-zA-Z0-9]+)`)

func newMethodDetails(fullMethodName string) methodDetails {
	// The full method name is structured as follows:
	// <package name>.<service>/<method>
	mthdSlice := mthdSlicer.FindStringSubmatch(fullMethodName)
	if mthdSlice == nil {
		log.Errorf("Faled to slice full method %s, result: %v", fullMethodName, mthdSlice)
	} else {
		log.Debugf("Sliced full method %s: %v", fullMethodName, mthdSlice)
	}
	return methodDetails{
		all:     mthdSlice[0],
		pkg:     mthdSlice[1],
		service: mthdSlice[2],
		method:  mthdSlice[3],
	}
}
