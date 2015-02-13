package snappy

import (
	"fmt"
	"strings"
)

func setActive(pkg, ver string) (err error) {
	m := NewMetaRepository()
	installed, err := m.Installed()
	if err != nil {
		return err
	}

	part := FindSnapByNameAndVersion(pkg, ver, installed)
	if part == nil {
		return fmt.Errorf("Can not find %s with version %s", pkg, ver)
	}
	fmt.Printf("Setting %s to active version %s\n", pkg, ver)
	return part.SetActive()
}

// map from
var setFuncs = map[string]func(k, v string) error{
	"active": setActive,
}

// SetProperty sets a system property from the args list
func SetProperty(args []string) (err error) {
	if len(args) < 1 {
		return fmt.Errorf("Need at least one argument for set")
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
