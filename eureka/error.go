package eureka

import (
	"fmt"
)

// Errors introduced by handling requests
var (
	ErrRequestCancelled    error = fmt.Errorf("sending request is cancelled")
	ErrInstanceNotFound    error = fmt.Errorf("instance not found")
	ErrApplicationNotFound error = fmt.Errorf("application not found")
	ErrEurekaNotReachable  error = fmt.Errorf("all the given peers are not reachable: tried to connect to each peer twice and failed")
)
