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

func setActive(pkg, ver string) (err error) {
	m := snappy.NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	part := snappy.FindPartByNameAndVersion(pkg, ver, installed)
	if part == nil {
		return fmt.Errorf("Can not find %s with version %s", pkg, ver)
	}
	fmt.Printf("Setting %s to active version %s\n", pkg, ver)
	return part.SetActive()
}

func set(args []string) (err error) {
	// map from
	setFuncs := map[string]func(k, v string) error{
		"active": setActive,
	}

	// check if the first argument is of the form property=value,
	// if so, the spec says we need to put "ubuntu-core" here
	if strings.Contains(args[0], "=") {
		// go version of prepend()
		args = append([]string{"ubuntu-core"}, args...)
	}

	pkg := args[0]
	for _, propVal := range args[1:] {
		s := strings.Split(propVal, "=")
		prop := s[0]
		f, ok := setFuncs[prop]
		if !ok {
			return fmt.Errorf("Unknown property %s", prop)
		}
		err := f(pkg, s[1])
		if err != nil {
			return err
		}
	}

	return err
}
