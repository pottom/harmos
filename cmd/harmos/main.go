// Command harmos is a terminal password client for Pleasant Password
// Server and local .kdbx files.
package main

import "os"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
