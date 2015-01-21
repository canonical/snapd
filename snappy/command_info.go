package snappy

import (
	"fmt"
	"strings"
)

func CmdInfo() (err error) {

	m := NewMetaRepository()
	installed, err := m.GetInstalled()
	if err != nil {
		return err
	}

	var frameworks []string
	var apps []string
	var release string
	for _, part := range installed {
		if !part.IsActive() {
			continue
		}
		switch(part.Type()) {
		case "framework":
			frameworks = append(frameworks, part.Name())
		case "app":
			apps = append(apps, part.Name())
		case "core":
			release = part.(*SystemImagePart).Channel()
		}
	}
	fmt.Printf("release: %s\n", release)
	fmt.Printf("frameworks: %s\n", strings.Join(frameworks, ", "))
	fmt.Printf("apps: %s\n", strings.Join(apps, ", "))

	return err
}
