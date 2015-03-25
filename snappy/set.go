package snappy

import (
	"fmt"
	"strings"
)

// map from
var setFuncs = map[string]func(k, v string) error{
	"active": makeSnapActiveByNameAndVersion,
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
			return err
		}
	}

	return err
}
