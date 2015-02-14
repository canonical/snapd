package main

import (
	"errors"
	"fmt"
	"strings"

	"launchpad.net/snappy/snappy"
)

type cmdInfo struct {
	Verbose    bool `short:"v" long:"verbose" description:"Provides more detailed information"`
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"Provide information about a specific installed package"`
	} `positional-args:"yes"`
}

const shortHelp = `A concise summary of key attributes of the snappy system, such as the release and channel.`

const longHelp = `A concise summary of key attributes of the snappy system, such as the release and channel.

The verbose output includes the specific version information for the factory image, the running image and the image that will be run on reboot, together with a list of the available channels for this image.

Providing a package name will display information about a specific installed package.

The verbose version of the info command for a package will also tell you the available channels for that package, when it was installed for the first time, disk space utilization, and in the case of frameworks, which apps are able to use the framework.
`

func init() {
	var cmdInfoData cmdInfo
	if _, err := parser.AddCommand("info", shortHelp, longHelp, &cmdInfoData); err != nil {
		// panic here as something must be terribly wrong if there is an
		// error here
		panic(err)
	}
}

func (x *cmdInfo) Execute(args []string) (err error) {
	// TODO implement per package info
	if x.Positional.PackageName != "" {
		return errors.New("Information request for specific packages not implemented")
	}

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
