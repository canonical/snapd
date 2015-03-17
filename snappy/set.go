package snappy

import (
	"fmt"
	"strings"

	"launchpad.net/snappy/logger"
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

// SetProperty sets a property for the given pkgname from the args list
func SetProperty(pkgname string, args ...string) (err error) {
	if len(args) < 1 {
		return fmt.Errorf("Need at least one argument for set")
	}

	for _, propVal := range args {
		s := strings.SplitN(propVal, "=", 2)
		if len(s) != 2 {
			return fmt.Errorf("Can not parse property %s", propVal)
		}
		prop := s[0]
		f, ok := setFuncs[prop]
		if !ok {
			return fmt.Errorf("Unknown property %s", prop)
		}
		err := f(pkgname, s[1])
		if err != nil {
			return logger.LogError(err)
		}
	}

	return err
}
