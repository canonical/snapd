package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/snapcore/snapd/tests/lib/fakestore/refresh"
)

type cmdNewSnapResourceRevision struct {
	Positional struct {
		Component               string `description:"Path to a component blob file"`
		SnapResourceRevJsonPath string `description:"Path to a json encoded snap resource revision subset"`
	} `positional-args:"yes" required:"yes"`

	TopDir string `long:"dir" description:"Directory to be used by the store to keep and serve snaps, <dir>/asserts is used for assertions"`
}

func (x *cmdNewSnapResourceRevision) Execute(args []string) error {
	content, err := os.ReadFile(x.Positional.SnapResourceRevJsonPath)
	if err != nil {
		return err
	}

	headers := make(map[string]any)
	if err := json.Unmarshal(content, &headers); err != nil {
		return err
	}

	p, err := refresh.NewSnapResourceRevision(x.TopDir, x.Positional.Component, headers)
	if err != nil {
		return err
	}
	fmt.Println(p)
	return nil
}

var shortNewSnapResourceRevisionHelp = "Make a new snap resource revision"

var longNewSnapResourceRevisionHelp = `
Generate a new snap resource revision signed with test keys. Snap ID and
revision must be provided in the given JSON file. All other headers are either
derived from the component file or optional, but can be overridden via the given
JSON file.
`

func init() {
	parser.AddCommand("new-snap-resource-revision", shortNewSnapResourceRevisionHelp, longNewSnapResourceRevisionHelp,
		&cmdNewSnapResourceRevision{})
}
