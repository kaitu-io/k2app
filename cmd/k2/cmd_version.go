package main

import "fmt"

func cmdVersion() {
	fmt.Printf("k2 %s (%s)\n", version, commit)
}
