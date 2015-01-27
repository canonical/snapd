package main

import (
	"fmt"
	"strings"

	"launchpad.net/snappy/snappy"
)

type CmdInfo struct {
}

var cmdInfo CmdInfo

func init() {
	_, _ = Parser.AddCommand("info",
		"Information about your snappy system",
		"Information about your snappy system",
		&cmdInfo)
}

func (x *CmdInfo) Execute(args []string) (err error) {
	return info()
}

func info() error {
	release := "unknown"
	parts, err := snappy.InstalledSnappsByType("core")
	if len(parts) == 1 && err == nil {
		release = parts[0].(*snappy.SystemImagePart).Channel()
	}

	frameworks, _ := snappy.InstalledSnappNamesByType("framework")
	apps, _ := snappy.InstalledSnappNamesByType("app")

	fmt.Printf("release: %s\n", release)
	fmt.Printf("architecture: %s\n", snappy.Architecture())
	fmt.Printf("frameworks: %s\n", strings.Join(frameworks, ", "))
	fmt.Printf("apps: %s\n", strings.Join(apps, ", "))

	return err
}
