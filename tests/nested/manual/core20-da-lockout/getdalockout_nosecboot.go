//go:build nosecboot

// Note: This file is needed to ensure that debian does not pick it up when building,
// otherwise it produces the error: cannot find package "github.com/canonical/go-tpm2"

package main

func main() {
}
