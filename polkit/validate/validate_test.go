// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package validate_test

import (
	"bytes"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/polkit/validate"
)

type ValidateSuite struct{}

var _ = Suite(&ValidateSuite{})

func Test(t *testing.T) {
	TestingT(t)
}

func validateString(xml string) ([]string, error) {
	return validate.ValidatePolicy(bytes.NewBufferString(xml))
}

func (s *ValidateSuite) TestRootElement(c *C) {
	// Extra elements after root
	_, err := validateString("<policyconfig/><policyconfig/>")
	c.Check(err, ErrorMatches, `invalid XML: additional data after root element`)

	// Wrong root element
	_, err = validateString("<xyz/>")
	c.Check(err, ErrorMatches, `root element must be <policyconfig>`)

	// Invalid XML
	_, err = validateString("<xyz>incomplete")
	c.Check(err, ErrorMatches, `XML syntax error on line .*`)
}

func (s *ValidateSuite) TestPolicyConfigElement(c *C) {
	_, err := validateString("<policyconfig/>")
	c.Check(err, IsNil)

	// Extra attributes are not allowed
	_, err = validateString(`<policyconfig foo="bar"/>`)
	c.Check(err, ErrorMatches, `<policyconfig> element contains unexpected attributes`)

	// Unexpected child elements
	_, err = validateString("<policyconfig><xyz/></policyconfig>")
	c.Check(err, ErrorMatches, `<policyconfig> element contains unexpected children`)

	// Unexpected character data
	_, err = validateString("<policyconfig>xyz</policyconfig>")
	c.Check(err, ErrorMatches, `<policyconfig> element contains unexpected character data`)

	// Supports <vendor>, <vendor_url>, and <icon_name> parameters
	_, err = validateString(`<policyconfig>
  <vendor>vendor</vendor>
  <vendor_url>url</vendor_url>
  <icon_name>icon</icon_name>
</policyconfig>`)
	c.Check(err, IsNil)

	// Duplicates of those elements are not allowed
	_, err = validateString(`<policyconfig>
  <vendor>vendor</vendor>
  <vendor>vendor</vendor>
</policyconfig>`)
	c.Check(err, ErrorMatches, `multiple <vendor> elements found under <policyconfig>`)

	_, err = validateString(`<policyconfig>
  <vendor_url>url</vendor_url>
  <vendor_url>url</vendor_url>
</policyconfig>`)
	c.Check(err, ErrorMatches, `multiple <vendor_url> elements found under <policyconfig>`)

	_, err = validateString(`<policyconfig>
  <icon_name>icon</icon_name>
  <icon_name>icon</icon_name>
</policyconfig>`)
	c.Check(err, ErrorMatches, `multiple <icon_name> elements found under <policyconfig>`)
}
