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
	"fmt"
	"sort"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/polkit/validate"
)

type validateSuite struct{}

var _ = Suite(&validateSuite{})

func Test(t *testing.T) {
	TestingT(t)
}

func validateString(xml string) ([]string, error) {
	return validate.ValidatePolicy(bytes.NewBufferString(xml))
}

func (s *validateSuite) TestRootElement(c *C) {
	// Extra elements after root
	_ := mylog.Check2(validateString("<policyconfig/><policyconfig/>"))
	c.Check(err, ErrorMatches, `invalid XML: additional data after root element`)

	// Extra incomplete elements after root element
	_ = mylog.Check2(validateString("<policyconfig/><incomplete>"))
	c.Check(err, ErrorMatches, `invalid XML: additional data after root element`)

	// Wrong root element
	_ = mylog.Check2(validateString("<xyz/>"))
	c.Check(err, ErrorMatches, `expected element type <policyconfig> but have <xyz>`)

	// Wrong namespace for root element
	_ = mylog.Check2(validateString(`<policyconfig xmlns="http://example.org/ns"/>`))
	c.Check(err, ErrorMatches, `root element must be <policyconfig>`)

	// Invalid XML
	_ = mylog.Check2(validateString("<policyconfig>incomplete"))
	c.Check(err, ErrorMatches, `XML syntax error on line .*`)
}

func (s *validateSuite) TestPolicyConfigElement(c *C) {
	_ := mylog.Check2(validateString("<policyconfig/>"))
	c.Check(err, IsNil)

	// Extra attributes are not allowed
	_ = mylog.Check2(validateString(`<policyconfig foo="bar"/>`))
	c.Check(err, ErrorMatches, `<policyconfig> element contains unexpected attributes`)

	// Unexpected child elements
	_ = mylog.Check2(validateString("<policyconfig><xyz/></policyconfig>"))
	c.Check(err, ErrorMatches, `<policyconfig> element contains unexpected children`)

	// Unexpected character data
	_ = mylog.Check2(validateString("<policyconfig>xyz</policyconfig>"))
	c.Check(err, ErrorMatches, `<policyconfig> element contains unexpected character data`)

	// Supports <vendor>, <vendor_url>, and <icon_name> parameters
	_ = mylog.Check2(validateString(`<policyconfig>
  <vendor>vendor</vendor>
  <vendor_url>url</vendor_url>
  <icon_name>icon</icon_name>
</policyconfig>`))
	c.Check(err, IsNil)

	// Duplicates of those elements are not allowed
	_ = mylog.Check2(validateString(`<policyconfig>
  <vendor>vendor</vendor>
  <vendor>vendor</vendor>
</policyconfig>`))
	c.Check(err, ErrorMatches, `multiple <vendor> elements found under <policyconfig>`)

	_ = mylog.Check2(validateString(`<policyconfig>
  <vendor_url>url</vendor_url>
  <vendor_url>url</vendor_url>
</policyconfig>`))
	c.Check(err, ErrorMatches, `multiple <vendor_url> elements found under <policyconfig>`)

	_ = mylog.Check2(validateString(`<policyconfig>
  <icon_name>icon</icon_name>
  <icon_name>icon</icon_name>
</policyconfig>`))
	c.Check(err, ErrorMatches, `multiple <icon_name> elements found under <policyconfig>`)
}

func validateAction(xml string) ([]string, error) {
	return validateString("<policyconfig>" + xml + "</policyconfig>")
}

func (s *validateSuite) TestActionElement(c *C) {
	// The ID of an action is extracted on successful validation
	actionIDs := mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description>
  <message>msg</message>
</action>`))
	c.Check(err, IsNil)
	c.Check(actionIDs, DeepEquals, []string{"foo"})

	// Actions must have an ID
	_ = mylog.Check2(validateAction("<action/>"))
	c.Check(err, ErrorMatches, `<action> elements must have an ID`)

	// Other attributes are not allowed
	_ = mylog.Check2(validateAction(`<action bar="foo"/>`))
	c.Check(err, ErrorMatches, `<action> element contains unexpected attributes`)

	// Unexpected child elements are not allowed
	_ = mylog.Check2(validateAction(`<action id="foo"><xyz/></action>`))
	c.Check(err, ErrorMatches, `<action> element contains unexpected children`)

	// Character data not allowed inside element
	_ = mylog.Check2(validateAction(`<action id="foo">xyz</action>`))
	c.Check(err, ErrorMatches, `<action> element contains unexpected character data`)

	// Action elements can also contain <vendor>, <vendor_url>,
	// and <icon_name> elements.
	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description><message>msg</message>
  <vendor>vendor</vendor>
  <vendor_url>url</vendor_url>
  <icon_name>icon</icon_name>
</action>`))
	c.Check(err, IsNil)

	// Empty versions of those elements are not allowed
	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description><message>msg</message>
  <vendor/>
</action>`))
	c.Check(err, ErrorMatches, `<vendor> element has no character data`)

	// Duplicates of those elements are not allowed
	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description><message>msg</message>
  <vendor>vendor</vendor>
  <vendor>vendor</vendor>
</action>`))
	c.Check(err, ErrorMatches, `multiple <vendor> elements found under <action>`)

	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description><message>msg</message>
  <vendor_url>url</vendor_url>
  <vendor_url>url</vendor_url>
</action>`))
	c.Check(err, ErrorMatches, `multiple <vendor_url> elements found under <action>`)

	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description><message>msg</message>
  <icon_name>icon</icon_name>
  <icon_name>icon</icon_name>
</action>`))
	c.Check(err, ErrorMatches, `multiple <icon_name> elements found under <action>`)

	// The <description> and <message> elements accept
	// gettext-domain and xml:lang attributes
	_ = mylog.Check2(validateAction(`<action id="foo">
  <description gettext-domain="bar" xml:lang="en-GB">desc</description>
  <message gettext-domain="bar" xml:lang="en-GB">desc</message>
</action>`))
	c.Check(err, IsNil)

	// Other attributes or child elements on <description> and
	// <message> are forbidden
	_ = mylog.Check2(validateAction(`<action id="foo">
  <description bar="foo">desc</description>
  <message>msg</message>
</action>`))
	c.Check(err, ErrorMatches, `<description> element contains unexpected attributes`)

	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc<xyz/></description>
  <message>msg</message>
</action>`))
	c.Check(err, ErrorMatches, `<description> element contains unexpected children`)

	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description>
  <message bar="foo">msg</message>
</action>`))
	c.Check(err, ErrorMatches, `<message> element contains unexpected attributes`)

	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description>
  <message>msg<xyz/></message>
</action>`))
	c.Check(err, ErrorMatches, `<message> element contains unexpected children`)

	// Multiple <description> and <message> children are allowed
	// children
	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description>
  <description>desc</description>
  <description>desc</description>
  <message>msg</message>
</action>`))
	c.Check(err, IsNil)

	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description>
  <message>msg</message>
  <message>msg</message>
  <message>msg</message>
</action>`))
	c.Check(err, IsNil)

	// But at least one is required
	_ = mylog.Check2(validateAction(`<action id="foo">
  <message>msg</message>
</action>`))
	c.Check(err, ErrorMatches, `<action> element missing <description> child`)

	_ = mylog.Check2(validateAction(`<action id="foo">
  <description>desc</description>
</action>`))
	c.Check(err, ErrorMatches, `<action> element missing <message> child`)
}

func validateActionDefaults(xml string) error {
	_ := mylog.Check2(validateAction(fmt.Sprintf(`<action id="foo">
  <description>desc</description><message>msg</message>
  %s
</action>`, xml)))
	return err
}

func (s *validateSuite) TestDefaultsElement(c *C) {
	mylog.
		// Actions can have a single <defaults> element
		Check(validateActionDefaults(`<defaults/>`))
	c.Check(err, IsNil)
	mylog.Check(validateActionDefaults(`<defaults/><defaults/>`))
	c.Check(err, ErrorMatches, `<action> element has multiple <defaults> children`)
	mylog.

		// The <defaults> element does not accept attributes, unknown children or character data
		Check(validateActionDefaults(`<defaults foo="bar"/>`))
	c.Check(err, ErrorMatches, `<defaults> element contains unexpected attributes`)
	mylog.Check(validateActionDefaults(`<defaults>xyz</defaults>`))
	c.Check(err, ErrorMatches, `<defaults> element contains unexpected character data`)
	mylog.Check(validateActionDefaults(`<defaults><xyz/></defaults>`))
	c.Check(err, ErrorMatches, `<defaults> element contains unexpected children`)
	mylog.

		// The defaults section contains default access rules for the action
		Check(validateActionDefaults(`<defaults>
  <allow_any>yes</allow_any>
  <allow_inactive>yes</allow_inactive>
  <allow_active>yes</allow_active>
</defaults>`))
	c.Check(err, IsNil)

	for _, mode := range []string{"allow_any", "allow_inactive", "allow_active"} {
		mylog.
			// Only one instance of the element is allowed
			Check(validateActionDefaults(fmt.Sprintf(`<defaults>
  <%s>yes</%s>
  <%s>yes</%s>
</defaults>`, mode, mode, mode, mode)))
		c.Check(err, ErrorMatches, fmt.Sprintf("multiple <%s> elements found under <defaults>", mode))
		mylog.Check(

			// No attributes or child elements allowed
			validateActionDefaults(fmt.Sprintf(`<defaults>
  <%s foo="bar">yes</%s>
</defaults>`, mode, mode)))
		c.Check(err, ErrorMatches, fmt.Sprintf("<%s> element contains unexpected attributes", mode))
		mylog.Check(validateActionDefaults(fmt.Sprintf(`<defaults>
  <%s>yes<xyz/></%s>
</defaults>`, mode, mode)))
		c.Check(err, ErrorMatches, fmt.Sprintf("<%s> element contains unexpected children", mode))
		mylog.Check(

			// Unknown or missing values are rejected
			validateActionDefaults(fmt.Sprintf(`<defaults>
  <%s>unknown</%s>
</defaults>`, mode, mode)))
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid value for <%s>: "unknown"`, mode))
		mylog.Check(validateActionDefaults(fmt.Sprintf(`<defaults>
  <%s/>
</defaults>`, mode)))
		c.Check(err, ErrorMatches, fmt.Sprintf(`invalid value for <%s>: ""`, mode))

		// Known values are accepted:
		for _, value := range []string{"no", "yes", "auth_self", "auth_admin", "auth_self_keep", "auth_admin_keep"} {
			mylog.Check(validateActionDefaults(fmt.Sprintf(`<defaults>
  <%s>%s</%s>
</defaults>`, mode, value, mode)))
			c.Check(err, IsNil)
		}
	}
}

func validateAnnotation(xml string) ([]string, error) {
	return validateAction(fmt.Sprintf(`<action id="action_id">
  <description>desc</description><message>msg</message>
  %s
</action>`, xml))
}

func (s *validateSuite) TestAnnotateElement(c *C) {
	actionIDs := mylog.Check2(validateAnnotation(`<annotate key="org.freedesktop.policykit.imply">implied_id</annotate>`))
	c.Check(err, IsNil)
	sort.Strings(actionIDs)
	c.Check(actionIDs, DeepEquals, []string{"action_id", "implied_id"})

	// <annotate> elements do not accept unknown attributes or
	// child elements
	_ = mylog.Check2(validateAnnotation(`<annotate foo="bar"/>`))
	c.Check(err, ErrorMatches, `<annotate> element contains unexpected attributes`)
	_ = mylog.Check2(validateAnnotation(`<annotate><xyz/></annotate>`))
	c.Check(err, ErrorMatches, `<annotate> element contains unexpected children`)

	// The key parameter is required
	_ = mylog.Check2(validateAnnotation(`<annotate/>`))
	c.Check(err, ErrorMatches, `<annotate> elements must have a key attribute`)

	// At present, only "imply" annotations are accepted
	_ = mylog.Check2(validateAnnotation(`<annotate key="xyz">foo</annotate>`))
	c.Check(err, ErrorMatches, `unsupported annotation "xyz"`)

	// "imply" annotations take a whitespace separated list of
	// action IDs that are returned by the validation function
	actionIDs = mylog.Check2(validateAnnotation(`<annotate key="org.freedesktop.policykit.imply">id1 id2 id3 id3</annotate>`))
	c.Check(err, IsNil)
	sort.Strings(actionIDs)
	c.Check(actionIDs, DeepEquals, []string{"action_id", "id1", "id2", "id3"})

	// Annotation elements must not be empty
	_ = mylog.Check2(validateAnnotation(`<annotate key="org.freedesktop.policykit.imply"/>`))
	c.Check(err, ErrorMatches, `<annotate> elements must contain character data`)

	// Multiple <annotate> elements are accepted
	actionIDs = mylog.Check2(validateAnnotation(`
<annotate key="org.freedesktop.policykit.imply">id1</annotate>
<annotate key="org.freedesktop.policykit.imply">id2</annotate>`))
	c.Check(err, IsNil)
	sort.Strings(actionIDs)
	c.Check(actionIDs, DeepEquals, []string{"action_id", "id1", "id2"})
}

func (s *validateSuite) TestActionIDExtraction(c *C) {
	actionIDs := mylog.Check2(validateString(`<policyconfig>
  <!-- a comment -->
  <action id="action1">
    <description>desc1</description>
    <message>msg1</message>
  </action>
  <action id="action2">
    <description>desc1</description>
    <message>msg1</message>
    <annotate key="org.freedesktop.policykit.imply">action3</annotate>
  </action>
  <action id="action3">
    <description>desc1</description>
    <message>msg1</message>
    <annotate key="org.freedesktop.policykit.imply">action2 action4</annotate>
  </action>
</policyconfig>`))
	c.Check(err, IsNil)
	sort.Strings(actionIDs)
	c.Check(actionIDs, DeepEquals, []string{"action1", "action2", "action3", "action4"})
}
