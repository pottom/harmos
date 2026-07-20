// Command harmos is a read-only terminal password client for Pleasant Password
// Server and local .kdbx files.
//
// Pre-alpha scaffold: no functionality yet — this only establishes the module,
// the build, and the cgo boundary. Features land in later milestones.
package main

import "fmt"

// version is overridden at release time by the build.
var version = "0.0.0-dev"

func main() {
	fmt.Printf("harmos %s\n", version)
}
