// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package main

import (
	"fmt"
	"strings"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

// A pair of key-value strings
type AttributePair struct {
	// The key
	Key string
	// The value
	Value string
}

// UnmarshalFlag parses a string into an AttributePair
func (ap *AttributePair) UnmarshalFlag(value string) error {
	parts := strings.SplitN(value, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("expected attribute in key=value format")
	}
	ap.Key, ap.Value = parts[0], parts[1]
	return nil
}

// MarshalFlag converts a AttributePair into a string
func (ap *AttributePair) MarshalFlag() (string, error) {
	return fmt.Sprintf("%s=%q", ap.Key, ap.Value), nil
}

// AttributePairSliceToMap converts a slice of AttributePair into a map
func AttributePairSliceToMap(attrs []AttributePair) map[string]string {
	result := make(map[string]string)
	for _, attr := range attrs {
		result[attr.Key] = attr.Value
	}
	return result
}

type addCapOptions struct {
	Name  string          `positional-arg-name:"name" description:"unique capability name"`
	Label string          `positional-arg-name:"label" description:"a descriptive label"`
	Type  string          `positional-arg-name:"type" description:"type of capability to add"`
	Attrs []AttributePair `positional-arg-name:"attrs" description:"key=value attributes"`
}

type cmdAddCap struct {
	addCapOptions `positional-args:"true" required:"true"`
}

var (
	shortAddCapHelp = i18n.G("Add a capability to the system")
	longAddCapHelp  = i18n.G("This command adds a capability to the system")
)

func init() {
	_, err := parser.AddCommand("add-cap", shortAddCapHelp, longAddCapHelp, &cmdAddCap{
		addCapOptions{
			Attrs: nil,
		},
	})
	if err != nil {
		logger.Panicf("unable to add add-caps command: %v", err)
	}
}

func (x *cmdAddCap) Execute(args []string) error {
	cap := &client.Capability{
		Name:  x.Name,
		Label: x.Label,
		Type:  x.Type,
		Attrs: AttributePairSliceToMap(x.Attrs),
	}
	return client.New().AddCapability(cap)
}
