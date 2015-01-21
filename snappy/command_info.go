package snappy

import (
	"fmt"
	"strings"
)

func CmdInfo() (err error) {

	release := "unknown"
	parts, err := GetInstalledSnappsByType("core")
	if len(parts) == 1 && err == nil {
		release = parts[0].(*SystemImagePart).Channel()
	}

	frameworks, _ := GetInstalledSnappNamesByType("framework")
	apps, _ := GetInstalledSnappNamesByType("app")
	
	fmt.Printf("release: %s\n", release)
	fmt.Printf("architecture: %s\n", getArchitecture())
	fmt.Printf("frameworks: %s\n", strings.Join(frameworks, ", "))
	fmt.Printf("apps: %s\n", strings.Join(apps, ", "))

	return err
}
