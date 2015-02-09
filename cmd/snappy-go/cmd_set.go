package main

import (
	"fmt"
	"strings"

	"launchpad.net/snappy/snappy"
)

type CmdSet struct {
}

var cmdSet CmdSet

func init() {
	_, _ = Parser.AddCommand("set",
		"Set properties of system or package",
		"Set properties of system or package",
		&cmdSet)
}

func (x *CmdSet) Execute(args []string) (err error) {
	return set(args)
}

func set(args []string) (err error) {
	m := snappy.NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	pkg := args[0]
	for _, propVal := range args[1:] {
		s := strings.Split(propVal, "=")
		if s[0] == "active" {
			ver := s[1]
			part := snappy.FindPartByNameAndVersion(pkg, ver, installed)
			if part != nil {
				fmt.Printf("Setting %s to active version %s\n", pkg, ver)
				return part.SetActive()
			} else {
				return fmt.Errorf("Can not find %s with version %s", pkg, ver)
			}
		}
	}

	return err
}
