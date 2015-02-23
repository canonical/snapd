package main

import (
	"fmt"
	"strings"

	"launchpad.net/snappy/snappy"
)

type cmdHWInfo struct {
	Positional struct {
		PackageName string `positional-arg-name:"package name" description:"List assigned hardware for a specific installed package"`
	} `positional-args:"yes"`
}

const shortHWInfoHelp = `List assigned hardware device for a package`

const longHWInfoHelp = `This command list what hardware a installed package can access`

func init() {
	var cmdHWInfoData cmdHWInfo
	_, _ = parser.AddCommand("hw-info",
		shortHWInfoHelp,
		longHWInfoHelp,
		&cmdHWInfoData)
}

func (x *cmdHWInfo) Execute(args []string) (err error) {
	if !isRoot() {
		return ErrRequiresRoot
	}

	writePaths, err := snappy.ListHWAccess(x.Positional.PackageName)
	if err != nil {
		return err
	}

	fmt.Printf("'%s:' '%s'\n", x.Positional.PackageName, strings.Join(writePaths, ", "))
	return nil
}
