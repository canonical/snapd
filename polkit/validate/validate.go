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

package validate

import (
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
)

type Element struct {
	CharData        string     `xml:",chardata"`
	UnknownAttrs    []xml.Attr `xml:",any,attr"`
	UnknownChildren []xml.Name `xml:",any"`
}

type policyConfig struct {
	XMLName xml.Name `xml:"policyconfig"`
	Element

	Vendor    []Element `xml:"vendor"`
	VendorURL []Element `xml:"vendor_url"`
	IconName  []Element `xml:"icon_name"`

	Actions []action `xml:"action"`
}

type action struct {
	Element

	ID string `xml:"id,attr"`

	Vendor    []Element `xml:"vendor"`
	VendorURL []Element `xml:"vendor_url"`
	IconName  []Element `xml:"icon_name"`

	Description []message  `xml:"description"`
	Message     []message  `xml:"message"`
	Defaults    []defaults `xml:"defaults"`
	Annotate    []annotate `xml:"annotate"`
}

type message struct {
	Element

	GettextDomain string `xml:"gettext-domain,attr"`
	Language      string `xml:"lang,attr"` // to match xml:lang
}

type defaults struct {
	Element

	AllowAny      []Element `xml:"allow_any"`
	AllowInactive []Element `xml:"allow_inactive"`
	AllowActive   []Element `xml:"allow_active"`
}

type annotate struct {
	Element

	Key string `xml:"key,attr"`
}

func ValidatePolicy(r io.Reader) (actionIDs []string, err error) {
	decoder := xml.NewDecoder(r)
	var config policyConfig
	mylog.Check(decoder.Decode(&config))

	// check for additional data after the root element
	if err := decoder.Decode(new(interface{})); err != io.EOF {
		return nil, fmt.Errorf("invalid XML: additional data after root element")
	}

	return validateConfig(config)
}

func validateConfig(config policyConfig) ([]string, error) {
	if config.XMLName != (xml.Name{Local: "policyconfig"}) {
		return nil, fmt.Errorf("root element must be <policyconfig>")
	}
	mylog.Check(validateElement(config.Element, "<policyconfig>", 0))
	mylog.Check(validateOptionalProperty(config.Vendor, "<vendor>", "<policyconfig>"))
	mylog.Check(validateOptionalProperty(config.VendorURL, "<vendor_url>", "<policyconfig>"))
	mylog.Check(validateOptionalProperty(config.IconName, "<icon_name>", "<policyconfig>"))

	seenIDs := make(map[string]struct{})
	for _, a := range config.Actions {
		mylog.Check(validateAction(a, seenIDs))
	}

	actionIDs := make([]string, 0, len(seenIDs))
	for id := range seenIDs {
		actionIDs = append(actionIDs, id)
	}
	return actionIDs, nil
}

type validateFlags int

const (
	allowCharData validateFlags = 1 << 1
)

func validateElement(element Element, name string, flags validateFlags) error {
	if len(element.UnknownAttrs) != 0 {
		return fmt.Errorf("%s element contains unexpected attributes", name)
	}
	if len(element.UnknownChildren) != 0 {
		return fmt.Errorf("%s element contains unexpected children", name)
	}
	if flags&allowCharData == 0 && len(strings.TrimSpace(element.CharData)) != 0 {
		return fmt.Errorf("%s element contains unexpected character data", name)
	}
	return nil
}

func validateOptionalProperty(prop []Element, name, parent string) error {
	switch len(prop) {
	case 0:
		// nothing
	case 1:
		mylog.Check(validateElement(prop[0], name, allowCharData))

		if len(strings.TrimSpace(prop[0].CharData)) == 0 {
			return fmt.Errorf("%s element has no character data", name)
		}
	default:
		return fmt.Errorf("multiple %s elements found under %s", name, parent)
	}
	return nil
}

func validateAction(action action, seenIDs map[string]struct{}) error {
	mylog.Check(validateElement(action.Element, "<action>", 0))

	if action.ID == "" {
		return fmt.Errorf("<action> elements must have an ID")
	}
	seenIDs[action.ID] = struct{}{}
	mylog.Check(validateOptionalProperty(action.Vendor, "<vendor>", "<action>"))
	mylog.Check(validateOptionalProperty(action.VendorURL, "<vendor_url>", "<action>"))
	mylog.Check(validateOptionalProperty(action.IconName, "<icon_name>", "<action>"))

	// There must be at least one description
	if len(action.Description) == 0 {
		return fmt.Errorf("<action> element missing <description> child")
	}
	for _, d := range action.Description {
		mylog.Check(validateElement(d.Element, "<description>", allowCharData))
	}

	// There must be at least one message
	if len(action.Message) == 0 {
		return fmt.Errorf("<action> element missing <message> child")
	}
	for _, m := range action.Message {
		mylog.Check(validateElement(m.Element, "<message>", allowCharData))
	}
	mylog.Check(

		// Check defaults
		validateActionDefaults(action.Defaults))

	// Check annotations
	for _, annotation := range action.Annotate {
		mylog.Check(validateElement(annotation.Element, "<annotate>", allowCharData))

		if len(annotation.Key) == 0 {
			return fmt.Errorf("<annotate> elements must have a key attribute")
		}
		value := strings.TrimSpace(annotation.CharData)
		if len(value) == 0 {
			return fmt.Errorf("<annotate> elements must contain character data")
		}

		// Is this a known annotation?
		switch annotation.Key {
		case "org.freedesktop.policykit.imply":
			// Value contains a space separated list of action IDs
			for _, id := range strings.Fields(value) {
				seenIDs[id] = struct{}{}
			}

		default:
			return fmt.Errorf("unsupported annotation %q", annotation.Key)
		}
	}

	return nil
}

func validateActionDefaults(defaults []defaults) error {
	switch len(defaults) {
	case 0:
		return nil
	case 1:
		// nothing
	default:
		return fmt.Errorf("<action> element has multiple <defaults> children")
	}

	d := defaults[0]
	mylog.Check(validateElement(d.Element, "<defaults>", 0))
	mylog.Check(validateDefaultAuth(d.AllowAny, "<allow_any>"))
	mylog.Check(validateDefaultAuth(d.AllowInactive, "<allow_inactive>"))
	mylog.Check(validateDefaultAuth(d.AllowActive, "<allow_active>"))

	return nil
}

func validateDefaultAuth(auth []Element, name string) error {
	switch len(auth) {
	case 0:
		// nothing
	case 1:
		mylog.Check(validateElement(auth[0], name, allowCharData))

		value := strings.TrimSpace(auth[0].CharData)
		switch value {
		case "no", "yes", "auth_self", "auth_admin", "auth_self_keep", "auth_admin_keep":
			// nothing
		default:
			return fmt.Errorf("invalid value for %s: %q", name, value)
		}
	default:
		return fmt.Errorf("multiple %s elements found under <defaults>", name)
	}
	return nil
}
