package main

import (
	"fmt"
	"strings"

	"launchpad.net/snappy/snappy"
)

type cmdInfo struct {
}

func init() {
	var cmdInfoData cmdInfo
	_, _ = parser.AddCommand("info",
		"Information about your snappy system",
		"Information about your snappy system",
		&cmdInfoData)
}

func (x *cmdInfo) Execute(args []string) (err error) {
	return info()
}

func info() error {
	release := "unknown"
	parts, err := snappy.InstalledSnapsByType(snappy.SnapTypeCore)
	if len(parts) == 1 && err == nil {
		release = parts[0].(*snappy.SystemImagePart).Channel()
	}

	frameworks, _ := snappy.InstalledSnapNamesByType(snappy.SnapTypeFramework)
	apps, _ := snappy.InstalledSnapNamesByType(snappy.SnapTypeApp)

	fmt.Printf("release: %s\n", release)
	fmt.Printf("architecture: %s\n", snappy.Architecture())
	fmt.Printf("frameworks: %s\n", strings.Join(frameworks, ", "))
	fmt.Printf("apps: %s\n", strings.Join(apps, ", "))

	return err
}
