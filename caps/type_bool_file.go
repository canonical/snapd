// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2016 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package caps

import (
	"encoding/json"
	"fmt"
)

const (
	// Name of the capability type.
	boolFileName = "bool-file"
	// Name of the attribute referring to the boolean file on the filesystem.
	boolFilePath = "path"
)

// boolFile is a capability for accessing a "boolean file".
// A "boolean file" is any device that reacts when either '0' or '1' is written to it.
// Typical examples include LEDs and GPIOs.
type boolFile struct {
	name     string
	label    string
	path     string
	realPath string
	attrs    map[string]string
}

func (c *boolFile) Name() string {
	return c.name
}

func (c *boolFile) Label() string {
	return c.label
}

func (c *boolFile) TypeName() string {
	return boolFileName
}

func (c *boolFile) AttrMap() map[string]string {
	a := make(map[string]string)
	for k, v := range c.attrs {
		a[k] = v
	}
	a[boolFilePath] = c.path
	return a
}

func (c *boolFile) Validate() error {
	// TODO: validate path against allowed regexp
	if c.path == "" {
		return fmt.Errorf("%s must have the %s attribute", boolFileName, boolFilePath)
	}
	return nil
}

func (c *boolFile) String() string {
	return c.Name()
}

func (c *boolFile) MarshalJSON() ([]byte, error) {
	return json.Marshal(Info(c))
}

type boolFileType struct{}

func (t *boolFileType) String() string {
	return boolFileName
}

func (t *boolFileType) Name() string {
	return boolFileName
}

func (t *boolFileType) Make(name, label string, attrs map[string]string) (Capability, error) {
	a := make(map[string]string)
	for k, v := range attrs {
		a[k] = v
	}
	delete(a, boolFilePath)
	path := attrs[boolFilePath]
	// TODO: resolve symlinks
	realPath := path
	c := &boolFile{
		name:     name,
		label:    label,
		path:     path,
		realPath: realPath,
		attrs:    a,
	}
	return c, nil
}
