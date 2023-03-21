package http2

import (
	"fmt"
	"log"
	"regexp"
	"runtime"
	"time"
)

func TimeTrack(start time.Time, info string) {
	elapsed := time.Since(start)

	// Skip this function, and fetch the PC and file for its parent.
	pc, _, _, _ := runtime.Caller(1)

	// Retrieve a function object this functions parent.
	funcObj := runtime.FuncForPC(pc)

	// Regex to extract just the function name (and not the module path).
	runtimeFunc := regexp.MustCompile(`^.*\.(.*)$`)
	name := runtimeFunc.ReplaceAllString(funcObj.Name(), "$1")

	log.Println(fmt.Sprintf("\u001b[32;1m[TimeTrack]\u001b[0m %s took %d (ns); (Info: %v)", name, elapsed.Nanoseconds(), info))
}
