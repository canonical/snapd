// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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

package asserts

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/snapcore/snapd/snap/naming"
)

// AttrMatchContext has contextual helpers for evaluating attribute constraints.
type AttrMatchContext interface {
	PlugAttr(arg string) (interface{}, error)
	SlotAttr(arg string) (interface{}, error)
	PlugPublisherID() string
	SlotPublisherID() string
}

const (
	// feature label for on-store/on-brand/on-model
	deviceScopeConstraintsFeature = "device-scope-constraints"
	// feature label for plug-names/slot-names constraints
	nameConstraintsFeature = "name-constraints"
)

// AttributeConstraints implements a set of constraints on the attributes of a slot or plug.
type AttributeConstraints struct {
	matcher attrMatcher
}

func (ac *AttributeConstraints) feature(flabel string) bool {
	return ac.matcher.feature(flabel)
}

// compileAttributeConstraints checks and compiles a mapping or list
// from the assertion format into AttributeConstraints.
func compileAttributeConstraints(constraints interface{}) (*AttributeConstraints, error) {
	cc := compileContext{
		opts: &compileAttrMatcherOptions{
			allowedOperations: []string{"SLOT", "PLUG"},
			allowedRefs:       []string{"PLUG_PUBLISHER_ID", "SLOT_PUBLISHER_ID"},
		},
	}
	matcher, err := compileAttrMatcher(cc, constraints)
	if err != nil {
		return nil, err
	}
	return &AttributeConstraints{matcher: matcher}, nil
}

type fixedAttrMatcher struct {
	result error
}

func (matcher fixedAttrMatcher) feature(flabel string) bool {
	return false
}

func (matcher fixedAttrMatcher) match(apath string, v interface{}, ctx *attrMatchingContext) error {
	return matcher.result
}

var (
	AlwaysMatchAttributes = &AttributeConstraints{matcher: fixedAttrMatcher{nil}}
	NeverMatchAttributes  = &AttributeConstraints{matcher: fixedAttrMatcher{errors.New("not allowed")}}
)

// Attrer reflects part of the Attrer interface (see interfaces.Attrer).
type Attrer interface {
	Lookup(path string) (interface{}, bool)
}

// Check checks whether attrs don't match the constraints.
func (c *AttributeConstraints) Check(attrer Attrer, helper AttrMatchContext) error {
	return c.matcher.match("", attrer, &attrMatchingContext{
		attrWord: "attribute",
		helper:   helper,
	})
}

// SideArityConstraint specifies a constraint for the overall arity of
// the set of connected slots for a given plug or the set of
// connected plugs for a given slot.
// It is used to express parsed slots-per-plug and plugs-per-slot
// constraints.
// See https://forum.snapcraft.io/t/plug-slot-declaration-rules-greedy-plugs/12438
type SideArityConstraint struct {
	// N can be:
	// =>1
	// 0 means default and is used only internally during rule
	// compilation or on deny- rules where these constraints are
	// not applicable
	// -1 represents *, that means any (number of)
	N int
}

// Any returns whether this represents the * (any number of) constraint.
func (ac SideArityConstraint) Any() bool {
	return ac.N == -1
}

func compileSideArityConstraint(context *subruleContext, which string, v interface{}) (SideArityConstraint, error) {
	var a SideArityConstraint
	if context.installation() || !context.allow() {
		return a, fmt.Errorf("%s cannot specify a %s constraint, they apply only to allow-*connection", context, which)
	}
	x, ok := v.(string)
	if !ok || len(x) == 0 {
		return a, fmt.Errorf("%s in %s must be an integer >=1 or *", which, context)
	}
	if x == "*" {
		return SideArityConstraint{N: -1}, nil
	}
	n, err := atoi(x, "%s in %s", which, context)
	switch _, syntax := err.(intSyntaxError); {
	case err == nil && n < 1:
		fallthrough
	case syntax:
		return a, fmt.Errorf("%s in %s must be an integer >=1 or *", which, context)
	case err != nil:
		return a, err
	}
	return SideArityConstraint{N: n}, nil
}

type sideArityConstraintsHolder interface {
	setSlotsPerPlug(SideArityConstraint)
	setPlugsPerSlot(SideArityConstraint)

	slotsPerPlug() SideArityConstraint
	plugsPerSlot() SideArityConstraint
}

func normalizeSideArityConstraints(context *subruleContext, c sideArityConstraintsHolder) {
	if !context.allow() {
		return
	}
	any := SideArityConstraint{N: -1}
	// normalized plugs-per-slot is always *
	c.setPlugsPerSlot(any)
	slotsPerPlug := c.slotsPerPlug()
	if context.autoConnection() {
		// auto-connection slots-per-plug can be any or 1
		if !slotsPerPlug.Any() {
			c.setSlotsPerPlug(SideArityConstraint{N: 1})
		}
	} else {
		// connection slots-per-plug can be only any
		c.setSlotsPerPlug(any)
	}
}

var (
	sideArityConstraints        = []string{"slots-per-plug", "plugs-per-slot"}
	sideArityConstraintsSetters = map[string]func(sideArityConstraintsHolder, SideArityConstraint){
		"slots-per-plug": sideArityConstraintsHolder.setSlotsPerPlug,
		"plugs-per-slot": sideArityConstraintsHolder.setPlugsPerSlot,
	}
)

// OnClassicConstraint specifies a constraint based whether the system is classic and optional specific distros' sets.
type OnClassicConstraint struct {
	Classic   bool
	SystemIDs []string
}

// OnCoreDesktopConstraint specifies a constraint based whether the system is core desktop.
type OnCoreDesktopConstraint struct {
	CoreDesktop bool
}

type nameMatcher interface {
	match(name string, special map[string]string) error
}

var (
	// validates special name constraints like $INTERFACE
	validSpecialNameConstraint = regexp.MustCompile(`^\$[A-Z][A-Z0-9_]*$`)
)

func compileNameMatcher(whichName string, v interface{}) (nameMatcher, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("%s constraint entry must be a regexp or special $ value", whichName)
	}
	if strings.HasPrefix(s, "$") {
		if !validSpecialNameConstraint.MatchString(s) {
			return nil, fmt.Errorf("%s constraint entry special value %q is invalid", whichName, s)
		}
		return specialNameMatcher{special: s}, nil
	}
	if strings.IndexFunc(s, unicode.IsSpace) != -1 {
		return nil, fmt.Errorf("%s constraint entry regexp contains unexpected spaces", whichName)
	}
	rx, err := regexp.Compile("^(" + s + ")$")
	if err != nil {
		return nil, fmt.Errorf("cannot compile %s constraint entry %q: %v", whichName, s, err)
	}
	return regexpNameMatcher{rx}, nil
}

type regexpNameMatcher struct {
	*regexp.Regexp
}

func (matcher regexpNameMatcher) match(name string, special map[string]string) error {
	if !matcher.Regexp.MatchString(name) {
		return fmt.Errorf("%q does not match %v", name, matcher.Regexp)
	}
	return nil
}

type specialNameMatcher struct {
	special string
}

func (matcher specialNameMatcher) match(name string, special map[string]string) error {
	expected := special[matcher.special]
	if expected == "" || expected != name {
		return fmt.Errorf("%q does not match %v", name, matcher.special)
	}
	return nil
}

// NameConstraints implements a set of constraints on the names of slots or plugs.
// See https://forum.snapcraft.io/t/plug-slot-rules-plug-names-slot-names-constraints/12439
type NameConstraints struct {
	matchers []nameMatcher
}

func compileNameConstraints(whichName string, constraints interface{}) (*NameConstraints, error) {
	l, ok := constraints.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s constraints must be a list of regexps and special $ values", whichName)
	}
	matchers := make([]nameMatcher, 0, len(l))
	for _, nm := range l {
		matcher, err := compileNameMatcher(whichName, nm)
		if err != nil {
			return nil, err
		}
		matchers = append(matchers, matcher)
	}
	return &NameConstraints{matchers: matchers}, nil
}

// Check checks whether name doesn't match the constraints.
func (nc *NameConstraints) Check(whichName, name string, special map[string]string) error {
	for _, m := range nc.matchers {
		if err := m.match(name, special); err == nil {
			return nil
		}
	}
	return fmt.Errorf("%s %q does not match constraints", whichName, name)
}

// rules

var (
	validSnapType  = regexp.MustCompile(`^(?:core|kernel|gadget|app)$`)
	validDistro    = regexp.MustCompile(`^[-0-9a-z._]+$`)
	validPublisher = regexp.MustCompile(`^(?:[a-z0-9A-Z]{32}|[-a-z0-9]{2,28}|\$[A-Z][A-Z0-9_]*)$`) // account ids look like snap-ids or are nice identifiers, support our own special markers $MARKER

	validIDConstraints = map[string]*regexp.Regexp{
		"slot-snap-type":    validSnapType,
		"slot-snap-id":      naming.ValidSnapID,
		"slot-publisher-id": validPublisher,
		"plug-snap-type":    validSnapType,
		"plug-snap-id":      naming.ValidSnapID,
		"plug-publisher-id": validPublisher,
	}
)

func checkMapOrShortcut(v interface{}) (m map[string]interface{}, invert bool, err error) {
	switch x := v.(type) {
	case map[string]interface{}:
		return x, false, nil
	case string:
		switch x {
		case "true":
			return nil, false, nil
		case "false":
			return nil, true, nil
		}
	}
	return nil, false, errors.New("unexpected type")
}

type constraintsHolder interface {
	setNameConstraints(field string, cstrs *NameConstraints)
	setAttributeConstraints(field string, cstrs *AttributeConstraints)
	setIDConstraints(field string, cstrs []string)
	setOnClassicConstraint(onClassic *OnClassicConstraint)
	setOnCoreDesktopConstraint(onCoreDesktop *OnCoreDesktopConstraint)
	setDeviceScopeConstraint(deviceScope *DeviceScopeConstraint)
}

func baseCompileConstraints(context *subruleContext, cDef constraintsDef, target constraintsHolder, nameConstraints, attrConstraints, idConstraints []string) error {
	cMap := cDef.cMap
	if cMap == nil {
		fixed := AlwaysMatchAttributes // "true"
		if cDef.invert {               // "false"
			fixed = NeverMatchAttributes
		}
		for _, field := range attrConstraints {
			target.setAttributeConstraints(field, fixed)
		}
		return nil
	}
	defaultUsed := 0
	for _, field := range nameConstraints {
		v := cMap[field]
		if v != nil {
			nc, err := compileNameConstraints(field, v)
			if err != nil {
				return err
			}
			target.setNameConstraints(field, nc)
		} else {
			defaultUsed++
		}
	}
	for _, field := range attrConstraints {
		cstrs := AlwaysMatchAttributes
		v := cMap[field]
		if v != nil {
			var err error
			cstrs, err = compileAttributeConstraints(cMap[field])
			if err != nil {
				return fmt.Errorf("cannot compile %s in %s: %v", field, context, err)
			}
		} else {
			defaultUsed++
		}
		target.setAttributeConstraints(field, cstrs)
	}
	for _, field := range idConstraints {
		lst, err := checkStringListInMap(cMap, field, fmt.Sprintf("%s in %s", field, context), validIDConstraints[field])
		if err != nil {
			return err
		}
		if lst == nil {
			defaultUsed++
		}
		target.setIDConstraints(field, lst)
	}
	for _, field := range sideArityConstraints {
		v := cMap[field]
		if v != nil {
			c, err := compileSideArityConstraint(context, field, v)
			if err != nil {
				return err
			}
			h, ok := target.(sideArityConstraintsHolder)
			if !ok {
				return fmt.Errorf("internal error: side arity constraint compiled for unexpected subrule %T", target)
			}
			sideArityConstraintsSetters[field](h, c)
		} else {
			defaultUsed++
		}
	}
	onClassic := cMap["on-classic"]
	if onClassic == nil {
		defaultUsed++
	} else {
		var c *OnClassicConstraint
		switch x := onClassic.(type) {
		case string:
			switch x {
			case "true":
				c = &OnClassicConstraint{Classic: true}
			case "false":
				c = &OnClassicConstraint{Classic: false}
			}
		case []interface{}:
			lst, err := checkStringListInMap(cMap, "on-classic", fmt.Sprintf("on-classic in %s", context), validDistro)
			if err != nil {
				return err
			}
			c = &OnClassicConstraint{Classic: true, SystemIDs: lst}
		}
		if c == nil {
			return fmt.Errorf("on-classic in %s must be 'true', 'false' or a list of operating system IDs", context)
		}
		target.setOnClassicConstraint(c)
	}
	onCoreDesktop := cMap["on-core-desktop"]
	if onCoreDesktop == nil {
		defaultUsed++
	} else {
		var c *OnCoreDesktopConstraint
		switch x := onCoreDesktop.(type) {
		case string:
			switch x {
			case "true":
				c = &OnCoreDesktopConstraint{CoreDesktop: true}
			case "false":
				c = &OnCoreDesktopConstraint{CoreDesktop: false}
			}
		}
		if c == nil {
			return fmt.Errorf("on-core-desktop in %s must be 'true' or 'false'", context)
		}
		target.setOnCoreDesktopConstraint(c)
	}
	dsc, err := compileDeviceScopeConstraint(cMap, context.String())
	if err != nil {
		return err
	}
	if dsc == nil {
		defaultUsed++
	} else {
		target.setDeviceScopeConstraint(dsc)
	}
	// checks whether defaults have been used for everything, which is not
	// well-formed
	// +1+1+1 accounts for defaults for missing on-classic, on-core-desktop plus missing
	// on-store/on-brand/on-model
	if defaultUsed == len(nameConstraints)+len(attributeConstraints)+len(idConstraints)+len(sideArityConstraints)+1+1+1 {
		return fmt.Errorf("%s must specify at least one of %s, %s, %s, %s, on-classic, on-core-desktop, on-store, on-brand, on-model", context, strings.Join(nameConstraints, ", "), strings.Join(attrConstraints, ", "), strings.Join(idConstraints, ", "), strings.Join(sideArityConstraints, ", "))
	}
	return nil
}

type rule interface {
	setConstraints(field string, cstrs []constraintsHolder)
}

type constraintsDef struct {
	cMap   map[string]interface{}
	invert bool
}

// subruleContext carries queryable context information about one the
// {allow,deny}-* subrules that end up compiled as
// Plug|Slot*Constraints.  The information includes the parent rule,
// the introductory subrule key ({allow,deny}-*) and which alternative
// it corresponds to if any.
// The information is useful for constraints compilation now that we
// have constraints with different behavior depending on the kind of
// subrule that hosts them (e.g. slots-per-plug, plugs-per-slot).
type subruleContext struct {
	// rule is the parent rule context description
	rule string
	// subrule is the subrule key
	subrule string
	// alt is which alternative this is (if > 0)
	alt int
}

func (c *subruleContext) String() string {
	subctxt := fmt.Sprintf("%s in %s", c.subrule, c.rule)
	if c.alt != 0 {
		subctxt = fmt.Sprintf("alternative %d of %s", c.alt, subctxt)
	}
	return subctxt
}

// allow returns whether the subrule is an allow-* subrule.
func (c *subruleContext) allow() bool {
	return strings.HasPrefix(c.subrule, "allow-")
}

// installation returns whether the subrule is an *-installation subrule.
func (c *subruleContext) installation() bool {
	return strings.HasSuffix(c.subrule, "-installation")
}

// autoConnection returns whether the subrule is an *-auto-connection subrule.
func (c *subruleContext) autoConnection() bool {
	return strings.HasSuffix(c.subrule, "-auto-connection")
}

type subruleCompiler func(context *subruleContext, def constraintsDef) (constraintsHolder, error)

func baseCompileRule(context string, rule interface{}, target rule, subrules []string, compilers map[string]subruleCompiler, defaultOutcome, invertedOutcome map[string]interface{}) error {
	rMap, invert, err := checkMapOrShortcut(rule)
	if err != nil {
		return fmt.Errorf("%s must be a map or one of the shortcuts 'true' or 'false'", context)
	}
	if rMap == nil {
		rMap = defaultOutcome // "true"
		if invert {
			rMap = invertedOutcome // "false"
		}
	}
	defaultUsed := 0
	// compile and set subrules
	for _, subrule := range subrules {
		v := rMap[subrule]
		var lst []interface{}
		alternatives := false
		switch x := v.(type) {
		case nil:
			v = defaultOutcome[subrule]
			defaultUsed++
		case []interface{}:
			alternatives = true
			lst = x
		}
		if lst == nil { // v is map or a string, checked below
			lst = []interface{}{v}
		}
		compiler := compilers[subrule]
		if compiler == nil {
			panic(fmt.Sprintf("no compiler for %s in %s", subrule, context))
		}
		alts := make([]constraintsHolder, len(lst))
		for i, alt := range lst {
			subctxt := &subruleContext{
				rule:    context,
				subrule: subrule,
			}
			if alternatives {
				subctxt.alt = i + 1
			}
			cMap, invert, err := checkMapOrShortcut(alt)
			if err != nil || (cMap == nil && alternatives) {
				efmt := "%s must be a map"
				if !alternatives {
					efmt = "%s must be a map or one of the shortcuts 'true' or 'false'"
				}
				return fmt.Errorf(efmt, subctxt)
			}

			cstrs, err := compiler(subctxt, constraintsDef{
				cMap:   cMap,
				invert: invert,
			})
			if err != nil {
				return err
			}
			alts[i] = cstrs
		}
		target.setConstraints(subrule, alts)
	}
	if defaultUsed == len(subrules) {
		return fmt.Errorf("%s must specify at least one of %s", context, strings.Join(subrules, ", "))
	}
	return nil
}

// PlugRule holds the rule of what is allowed, wrt installation and
// connection, for a plug of a specific interface for a snap.
type PlugRule struct {
	Interface string

	AllowInstallation []*PlugInstallationConstraints
	DenyInstallation  []*PlugInstallationConstraints

	AllowConnection []*PlugConnectionConstraints
	DenyConnection  []*PlugConnectionConstraints

	AllowAutoConnection []*PlugConnectionConstraints
	DenyAutoConnection  []*PlugConnectionConstraints
}

func (r *PlugRule) feature(flabel string) bool {
	for _, cs := range [][]*PlugInstallationConstraints{r.AllowInstallation, r.DenyInstallation} {
		for _, c := range cs {
			if c.feature(flabel) {
				return true
			}
		}
	}

	for _, cs := range [][]*PlugConnectionConstraints{r.AllowConnection, r.DenyConnection, r.AllowAutoConnection, r.DenyAutoConnection} {
		for _, c := range cs {
			if c.feature(flabel) {
				return true
			}
		}
	}

	return false
}

func castPlugInstallationConstraints(cstrs []constraintsHolder) (res []*PlugInstallationConstraints) {
	res = make([]*PlugInstallationConstraints, len(cstrs))
	for i, cstr := range cstrs {
		res[i] = cstr.(*PlugInstallationConstraints)
	}
	return res
}

func castPlugConnectionConstraints(cstrs []constraintsHolder) (res []*PlugConnectionConstraints) {
	res = make([]*PlugConnectionConstraints, len(cstrs))
	for i, cstr := range cstrs {
		res[i] = cstr.(*PlugConnectionConstraints)
	}
	return res
}

func (r *PlugRule) setConstraints(field string, cstrs []constraintsHolder) {
	if len(cstrs) == 0 {
		panic(fmt.Sprintf("cannot set PlugRule field %q to empty", field))
	}
	switch cstrs[0].(type) {
	case *PlugInstallationConstraints:
		switch field {
		case "allow-installation":
			r.AllowInstallation = castPlugInstallationConstraints(cstrs)
			return
		case "deny-installation":
			r.DenyInstallation = castPlugInstallationConstraints(cstrs)
			return
		}
	case *PlugConnectionConstraints:
		switch field {
		case "allow-connection":
			r.AllowConnection = castPlugConnectionConstraints(cstrs)
			return
		case "deny-connection":
			r.DenyConnection = castPlugConnectionConstraints(cstrs)
			return
		case "allow-auto-connection":
			r.AllowAutoConnection = castPlugConnectionConstraints(cstrs)
			return
		case "deny-auto-connection":
			r.DenyAutoConnection = castPlugConnectionConstraints(cstrs)
			return
		}
	}
	panic(fmt.Sprintf("cannot set PlugRule field %q with %T elements", field, cstrs[0]))
}

// PlugInstallationConstraints specifies a set of constraints on an interface plug relevant to the installation of snap.
type PlugInstallationConstraints struct {
	PlugSnapTypes []string
	PlugSnapIDs   []string

	PlugNames *NameConstraints

	PlugAttributes *AttributeConstraints

	OnClassic     *OnClassicConstraint
	OnCoreDesktop *OnCoreDesktopConstraint

	DeviceScope *DeviceScopeConstraint
}

func (c *PlugInstallationConstraints) feature(flabel string) bool {
	if flabel == deviceScopeConstraintsFeature {
		return c.DeviceScope != nil
	}
	if flabel == nameConstraintsFeature {
		return c.PlugNames != nil
	}
	return c.PlugAttributes.feature(flabel)
}

func (c *PlugInstallationConstraints) setNameConstraints(field string, cstrs *NameConstraints) {
	switch field {
	case "plug-names":
		c.PlugNames = cstrs
	default:
		panic("unknown PlugInstallationConstraints field " + field)
	}
}

func (c *PlugInstallationConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "plug-attributes":
		c.PlugAttributes = cstrs
	default:
		panic("unknown PlugInstallationConstraints field " + field)
	}
}

func (c *PlugInstallationConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "plug-snap-type":
		c.PlugSnapTypes = cstrs
	case "plug-snap-id":
		c.PlugSnapIDs = cstrs
	default:
		panic("unknown PlugInstallationConstraints field " + field)
	}
}

func (c *PlugInstallationConstraints) setOnClassicConstraint(onClassic *OnClassicConstraint) {
	c.OnClassic = onClassic
}

func (c *PlugInstallationConstraints) setOnCoreDesktopConstraint(onCoreDesktop *OnCoreDesktopConstraint) {
	c.OnCoreDesktop = onCoreDesktop
}

func (c *PlugInstallationConstraints) setDeviceScopeConstraint(deviceScope *DeviceScopeConstraint) {
	c.DeviceScope = deviceScope
}

func compilePlugInstallationConstraints(context *subruleContext, cDef constraintsDef) (constraintsHolder, error) {
	plugInstCstrs := &PlugInstallationConstraints{}
	// plug-snap-id is supported here mainly for symmetry with the slot case
	// see discussion there
	err := baseCompileConstraints(context, cDef, plugInstCstrs, []string{"plug-names"}, []string{"plug-attributes"}, []string{"plug-snap-type", "plug-snap-id"})
	if err != nil {
		return nil, err
	}
	return plugInstCstrs, nil
}

// PlugConnectionConstraints specfies a set of constraints on an
// interface plug for a snap relevant to its connection or
// auto-connection.
type PlugConnectionConstraints struct {
	SlotSnapTypes    []string
	SlotSnapIDs      []string
	SlotPublisherIDs []string

	PlugNames *NameConstraints
	SlotNames *NameConstraints

	PlugAttributes *AttributeConstraints
	SlotAttributes *AttributeConstraints

	// SlotsPerPlug defaults to 1 for auto-connection, can be * (any)
	SlotsPerPlug SideArityConstraint
	// PlugsPerSlot is always * (any) (for now)
	PlugsPerSlot SideArityConstraint

	OnClassic     *OnClassicConstraint
	OnCoreDesktop *OnCoreDesktopConstraint

	DeviceScope *DeviceScopeConstraint
}

func (c *PlugConnectionConstraints) feature(flabel string) bool {
	if flabel == deviceScopeConstraintsFeature {
		return c.DeviceScope != nil
	}
	if flabel == nameConstraintsFeature {
		return c.PlugNames != nil || c.SlotNames != nil
	}
	return c.PlugAttributes.feature(flabel) || c.SlotAttributes.feature(flabel)
}

func (c *PlugConnectionConstraints) setNameConstraints(field string, cstrs *NameConstraints) {
	switch field {
	case "plug-names":
		c.PlugNames = cstrs
	case "slot-names":
		c.SlotNames = cstrs
	default:
		panic("unknown PlugConnectionConstraints field " + field)
	}
}

func (c *PlugConnectionConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "plug-attributes":
		c.PlugAttributes = cstrs
	case "slot-attributes":
		c.SlotAttributes = cstrs
	default:
		panic("unknown PlugConnectionConstraints field " + field)
	}
}

func (c *PlugConnectionConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "slot-snap-type":
		c.SlotSnapTypes = cstrs
	case "slot-snap-id":
		c.SlotSnapIDs = cstrs
	case "slot-publisher-id":
		c.SlotPublisherIDs = cstrs
	default:
		panic("unknown PlugConnectionConstraints field " + field)
	}
}

func (c *PlugConnectionConstraints) setSlotsPerPlug(a SideArityConstraint) {
	c.SlotsPerPlug = a
}

func (c *PlugConnectionConstraints) setPlugsPerSlot(a SideArityConstraint) {
	c.PlugsPerSlot = a
}

func (c *PlugConnectionConstraints) slotsPerPlug() SideArityConstraint {
	return c.SlotsPerPlug
}

func (c *PlugConnectionConstraints) plugsPerSlot() SideArityConstraint {
	return c.PlugsPerSlot
}

func (c *PlugConnectionConstraints) setOnClassicConstraint(onClassic *OnClassicConstraint) {
	c.OnClassic = onClassic
}

func (c *PlugConnectionConstraints) setOnCoreDesktopConstraint(onCoreDesktop *OnCoreDesktopConstraint) {
	c.OnCoreDesktop = onCoreDesktop
}

func (c *PlugConnectionConstraints) setDeviceScopeConstraint(deviceScope *DeviceScopeConstraint) {
	c.DeviceScope = deviceScope
}

var (
	nameConstraints      = []string{"plug-names", "slot-names"}
	attributeConstraints = []string{"plug-attributes", "slot-attributes"}
	plugIDConstraints    = []string{"slot-snap-type", "slot-publisher-id", "slot-snap-id"}
)

func compilePlugConnectionConstraints(context *subruleContext, cDef constraintsDef) (constraintsHolder, error) {
	plugConnCstrs := &PlugConnectionConstraints{}
	err := baseCompileConstraints(context, cDef, plugConnCstrs, nameConstraints, attributeConstraints, plugIDConstraints)
	if err != nil {
		return nil, err
	}
	normalizeSideArityConstraints(context, plugConnCstrs)
	return plugConnCstrs, nil
}

var (
	defaultOutcome = map[string]interface{}{
		"allow-installation":    "true",
		"allow-connection":      "true",
		"allow-auto-connection": "true",
		"deny-installation":     "false",
		"deny-connection":       "false",
		"deny-auto-connection":  "false",
	}

	invertedOutcome = map[string]interface{}{
		"allow-installation":    "false",
		"allow-connection":      "false",
		"allow-auto-connection": "false",
		"deny-installation":     "true",
		"deny-connection":       "true",
		"deny-auto-connection":  "true",
	}

	ruleSubrules = []string{"allow-installation", "deny-installation", "allow-connection", "deny-connection", "allow-auto-connection", "deny-auto-connection"}
)

var plugRuleCompilers = map[string]subruleCompiler{
	"allow-installation":    compilePlugInstallationConstraints,
	"deny-installation":     compilePlugInstallationConstraints,
	"allow-connection":      compilePlugConnectionConstraints,
	"deny-connection":       compilePlugConnectionConstraints,
	"allow-auto-connection": compilePlugConnectionConstraints,
	"deny-auto-connection":  compilePlugConnectionConstraints,
}

func compilePlugRule(interfaceName string, rule interface{}) (*PlugRule, error) {
	context := fmt.Sprintf("plug rule for interface %q", interfaceName)
	plugRule := &PlugRule{
		Interface: interfaceName,
	}
	err := baseCompileRule(context, rule, plugRule, ruleSubrules, plugRuleCompilers, defaultOutcome, invertedOutcome)
	if err != nil {
		return nil, err
	}
	return plugRule, nil
}

// SlotRule holds the rule of what is allowed, wrt installation and
// connection, for a slot of a specific interface for a snap.
type SlotRule struct {
	Interface string

	AllowInstallation []*SlotInstallationConstraints
	DenyInstallation  []*SlotInstallationConstraints

	AllowConnection []*SlotConnectionConstraints
	DenyConnection  []*SlotConnectionConstraints

	AllowAutoConnection []*SlotConnectionConstraints
	DenyAutoConnection  []*SlotConnectionConstraints
}

func castSlotInstallationConstraints(cstrs []constraintsHolder) (res []*SlotInstallationConstraints) {
	res = make([]*SlotInstallationConstraints, len(cstrs))
	for i, cstr := range cstrs {
		res[i] = cstr.(*SlotInstallationConstraints)
	}
	return res
}

func (r *SlotRule) feature(flabel string) bool {
	for _, cs := range [][]*SlotInstallationConstraints{r.AllowInstallation, r.DenyInstallation} {
		for _, c := range cs {
			if c.feature(flabel) {
				return true
			}
		}
	}

	for _, cs := range [][]*SlotConnectionConstraints{r.AllowConnection, r.DenyConnection, r.AllowAutoConnection, r.DenyAutoConnection} {
		for _, c := range cs {
			if c.feature(flabel) {
				return true
			}
		}
	}

	return false
}

func castSlotConnectionConstraints(cstrs []constraintsHolder) (res []*SlotConnectionConstraints) {
	res = make([]*SlotConnectionConstraints, len(cstrs))
	for i, cstr := range cstrs {
		res[i] = cstr.(*SlotConnectionConstraints)
	}
	return res
}

func (r *SlotRule) setConstraints(field string, cstrs []constraintsHolder) {
	if len(cstrs) == 0 {
		panic(fmt.Sprintf("cannot set SlotRule field %q to empty", field))
	}
	switch cstrs[0].(type) {
	case *SlotInstallationConstraints:
		switch field {
		case "allow-installation":
			r.AllowInstallation = castSlotInstallationConstraints(cstrs)
			return
		case "deny-installation":
			r.DenyInstallation = castSlotInstallationConstraints(cstrs)
			return
		}
	case *SlotConnectionConstraints:
		switch field {
		case "allow-connection":
			r.AllowConnection = castSlotConnectionConstraints(cstrs)
			return
		case "deny-connection":
			r.DenyConnection = castSlotConnectionConstraints(cstrs)
			return
		case "allow-auto-connection":
			r.AllowAutoConnection = castSlotConnectionConstraints(cstrs)
			return
		case "deny-auto-connection":
			r.DenyAutoConnection = castSlotConnectionConstraints(cstrs)
			return
		}
	}
	panic(fmt.Sprintf("cannot set SlotRule field %q with %T elements", field, cstrs[0]))
}

// SlotInstallationConstraints specifies a set of constraints on an
// interface slot relevant to the installation of snap.
type SlotInstallationConstraints struct {
	SlotSnapTypes []string
	SlotSnapIDs   []string

	SlotNames *NameConstraints

	SlotAttributes *AttributeConstraints

	OnClassic     *OnClassicConstraint
	OnCoreDesktop *OnCoreDesktopConstraint

	DeviceScope *DeviceScopeConstraint
}

func (c *SlotInstallationConstraints) feature(flabel string) bool {
	if flabel == deviceScopeConstraintsFeature {
		return c.DeviceScope != nil
	}
	if flabel == nameConstraintsFeature {
		return c.SlotNames != nil
	}
	return c.SlotAttributes.feature(flabel)
}

func (c *SlotInstallationConstraints) setNameConstraints(field string, cstrs *NameConstraints) {
	switch field {
	case "slot-names":
		c.SlotNames = cstrs
	default:
		panic("unknown SlotInstallationConstraints field " + field)
	}
}

func (c *SlotInstallationConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "slot-attributes":
		c.SlotAttributes = cstrs
	default:
		panic("unknown SlotInstallationConstraints field " + field)
	}
}

func (c *SlotInstallationConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "slot-snap-type":
		c.SlotSnapTypes = cstrs
	case "slot-snap-id":
		c.SlotSnapIDs = cstrs
	default:
		panic("unknown SlotInstallationConstraints field " + field)
	}
}

func (c *SlotInstallationConstraints) setOnClassicConstraint(onClassic *OnClassicConstraint) {
	c.OnClassic = onClassic
}

func (c *SlotInstallationConstraints) setOnCoreDesktopConstraint(onCoreDesktop *OnCoreDesktopConstraint) {
	c.OnCoreDesktop = onCoreDesktop
}

func (c *SlotInstallationConstraints) setDeviceScopeConstraint(deviceScope *DeviceScopeConstraint) {
	c.DeviceScope = deviceScope
}

func compileSlotInstallationConstraints(context *subruleContext, cDef constraintsDef) (constraintsHolder, error) {
	slotInstCstrs := &SlotInstallationConstraints{}
	// slot-snap-id here is mostly useful to restrict a relaxed
	// base-declaration slot-snap-type constraint because the latter is used
	// also for --dangerous installations. So in rare complex situations
	// slot-snap-type might constraint to core and app
	// but the intention is really that only system snaps should have the
	// slot without a snap-declaration rule, slot-snap-id then can
	// be used to limit to the known system snap snap-ids.
	// This means we want app-slots to be super-privileged but we have
	// slots for the interface on the system snaps as well.
	// An example of this is shared-memory.
	err := baseCompileConstraints(context, cDef, slotInstCstrs, []string{"slot-names"}, []string{"slot-attributes"}, []string{"slot-snap-type", "slot-snap-id"})
	if err != nil {
		return nil, err
	}
	return slotInstCstrs, nil
}

// SlotConnectionConstraints specfies a set of constraints on an
// interface slot for a snap relevant to its connection or
// auto-connection.
type SlotConnectionConstraints struct {
	// SlotSnapTypes constraints on the slot side for connections
	// are only useful in the base-declaration,
	// as the snap-declaration is for one given snap with its type.
	// So there is no (new) format iteration to cover this.
	SlotSnapTypes []string

	PlugSnapTypes    []string
	PlugSnapIDs      []string
	PlugPublisherIDs []string

	SlotNames *NameConstraints
	PlugNames *NameConstraints

	SlotAttributes *AttributeConstraints
	PlugAttributes *AttributeConstraints

	// SlotsPerPlug defaults to 1 for auto-connection, can be * (any)
	SlotsPerPlug SideArityConstraint
	// PlugsPerSlot is always * (any) (for now)
	PlugsPerSlot SideArityConstraint

	OnClassic     *OnClassicConstraint
	OnCoreDesktop *OnCoreDesktopConstraint

	DeviceScope *DeviceScopeConstraint
}

func (c *SlotConnectionConstraints) feature(flabel string) bool {
	if flabel == deviceScopeConstraintsFeature {
		return c.DeviceScope != nil
	}
	if flabel == nameConstraintsFeature {
		return c.PlugNames != nil || c.SlotNames != nil
	}
	return c.PlugAttributes.feature(flabel) || c.SlotAttributes.feature(flabel)
}

func (c *SlotConnectionConstraints) setNameConstraints(field string, cstrs *NameConstraints) {
	switch field {
	case "plug-names":
		c.PlugNames = cstrs
	case "slot-names":
		c.SlotNames = cstrs
	default:
		panic("unknown SlotConnectionConstraints field " + field)
	}
}

func (c *SlotConnectionConstraints) setAttributeConstraints(field string, cstrs *AttributeConstraints) {
	switch field {
	case "plug-attributes":
		c.PlugAttributes = cstrs
	case "slot-attributes":
		c.SlotAttributes = cstrs
	default:
		panic("unknown SlotConnectionConstraints field " + field)
	}
}

func (c *SlotConnectionConstraints) setIDConstraints(field string, cstrs []string) {
	switch field {
	case "slot-snap-type":
		c.SlotSnapTypes = cstrs
	case "plug-snap-type":
		c.PlugSnapTypes = cstrs
	case "plug-snap-id":
		c.PlugSnapIDs = cstrs
	case "plug-publisher-id":
		c.PlugPublisherIDs = cstrs
	default:
		panic("unknown SlotConnectionConstraints field " + field)
	}
}

var (
	slotIDConstraints = []string{"slot-snap-type", "plug-snap-type", "plug-publisher-id", "plug-snap-id"}
)

func (c *SlotConnectionConstraints) setSlotsPerPlug(a SideArityConstraint) {
	c.SlotsPerPlug = a
}

func (c *SlotConnectionConstraints) setPlugsPerSlot(a SideArityConstraint) {
	c.PlugsPerSlot = a
}

func (c *SlotConnectionConstraints) slotsPerPlug() SideArityConstraint {
	return c.SlotsPerPlug
}

func (c *SlotConnectionConstraints) plugsPerSlot() SideArityConstraint {
	return c.PlugsPerSlot
}

func (c *SlotConnectionConstraints) setOnClassicConstraint(onClassic *OnClassicConstraint) {
	c.OnClassic = onClassic
}

func (c *SlotConnectionConstraints) setOnCoreDesktopConstraint(onCoreDesktop *OnCoreDesktopConstraint) {
	c.OnCoreDesktop = onCoreDesktop
}

func (c *SlotConnectionConstraints) setDeviceScopeConstraint(deviceScope *DeviceScopeConstraint) {
	c.DeviceScope = deviceScope
}

func compileSlotConnectionConstraints(context *subruleContext, cDef constraintsDef) (constraintsHolder, error) {
	slotConnCstrs := &SlotConnectionConstraints{}
	err := baseCompileConstraints(context, cDef, slotConnCstrs, nameConstraints, attributeConstraints, slotIDConstraints)
	if err != nil {
		return nil, err
	}
	normalizeSideArityConstraints(context, slotConnCstrs)
	return slotConnCstrs, nil
}

var slotRuleCompilers = map[string]subruleCompiler{
	"allow-installation":    compileSlotInstallationConstraints,
	"deny-installation":     compileSlotInstallationConstraints,
	"allow-connection":      compileSlotConnectionConstraints,
	"deny-connection":       compileSlotConnectionConstraints,
	"allow-auto-connection": compileSlotConnectionConstraints,
	"deny-auto-connection":  compileSlotConnectionConstraints,
}

func compileSlotRule(interfaceName string, rule interface{}) (*SlotRule, error) {
	context := fmt.Sprintf("slot rule for interface %q", interfaceName)
	slotRule := &SlotRule{
		Interface: interfaceName,
	}
	err := baseCompileRule(context, rule, slotRule, ruleSubrules, slotRuleCompilers, defaultOutcome, invertedOutcome)
	if err != nil {
		return nil, err
	}
	return slotRule, nil
}
