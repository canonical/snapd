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
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

const (
	// feature label for $SLOT()/$PLUG()/$MISSING
	dollarAttrConstraintsFeature = "dollar-attr-constraints"
	// feature label for alt attribute matcher usage
	altAttrMatcherFeature = "alt-attr-matcher"
)

type attrMatchingContext struct {
	// attrWord is the usage context word for "attribute", mainly
	// useful in errors
	attrWord string
	helper   AttrMatchContext
}

type attrMatcher interface {
	match(apath string, v interface{}, ctx *attrMatchingContext) error

	feature(flabel string) bool
}

func chain(path, k string) string {
	if path == "" {
		return k
	}
	return fmt.Sprintf("%s.%s", path, k)
}

type compileAttrMatcherOptions struct {
	allowedOperations []string
}

type compileContext struct {
	dotted string
	hadMap bool
	wasAlt bool

	opts *compileAttrMatcherOptions
}

func (cc compileContext) String() string {
	return cc.dotted
}

func (cc compileContext) keyEntry(k string) compileContext {
	return compileContext{
		dotted: chain(cc.dotted, k),
		hadMap: true,
		wasAlt: false,
		opts:   cc.opts,
	}
}

func (cc compileContext) alt(alt int) compileContext {
	return compileContext{
		dotted: fmt.Sprintf("%s/alt#%d/", cc.dotted, alt+1),
		hadMap: cc.hadMap,
		wasAlt: true,
		opts:   cc.opts,
	}
}

// compileAttrMatcher compiles an attrMatcher derived from constraints,
func compileAttrMatcher(cc compileContext, constraints interface{}) (attrMatcher, error) {
	switch x := constraints.(type) {
	case map[string]interface{}:
		return compileMapAttrMatcher(cc, x)
	case []interface{}:
		if cc.wasAlt {
			return nil, fmt.Errorf("cannot nest alternative constraints directly at %q", cc)
		}
		return compileAltAttrMatcher(cc, x)
	case string:
		if !cc.hadMap {
			return nil, fmt.Errorf("first level of non alternative constraints must be a set of key-value contraints")
		}
		if strings.HasPrefix(x, "$") {
			if x == "$MISSING" {
				return missingAttrMatcher{}, nil
			}
			return compileEvalAttrMatcher(cc, x)
		}
		return compileRegexpAttrMatcher(cc, x)
	default:
		c := cc.String()
		if c == "" {
			c = "top constraint"
		} else {
			c = fmt.Sprintf("constraint %q", c)
		}
		return nil, fmt.Errorf("%s must be a key-value map, regexp or a list of alternative constraints: %v", c, x)
	}
}

type mapAttrMatcher map[string]attrMatcher

func compileMapAttrMatcher(cc compileContext, m map[string]interface{}) (attrMatcher, error) {
	matcher := make(mapAttrMatcher)
	for k, constraint := range m {
		matcher1 := mylog.Check2(compileAttrMatcher(cc.keyEntry(k), constraint))

		matcher[k] = matcher1
	}
	return matcher, nil
}

func matchEntry(apath, k string, matcher1 attrMatcher, v interface{}, ctx *attrMatchingContext) error {
	apath = chain(apath, k)
	// every entry matcher expects the attribute to be set except for $MISSING
	if _, ok := matcher1.(missingAttrMatcher); !ok && v == nil {
		return fmt.Errorf("%s %q has constraints but is unset", ctx.attrWord, apath)
	}
	mylog.Check(matcher1.match(apath, v, ctx))

	return nil
}

func matchList(apath string, matcher attrMatcher, l []interface{}, ctx *attrMatchingContext) error {
	for i, elem := range l {
		mylog.Check(matcher.match(chain(apath, strconv.Itoa(i)), elem, ctx))
	}
	return nil
}

func (matcher mapAttrMatcher) feature(flabel string) bool {
	for _, matcher1 := range matcher {
		if matcher1.feature(flabel) {
			return true
		}
	}
	return false
}

func (matcher mapAttrMatcher) match(apath string, v interface{}, ctx *attrMatchingContext) error {
	switch x := v.(type) {
	case Attrer:
		// we get Atter from root-level Check (apath is "")
		for k, matcher1 := range matcher {
			v, _ := x.Lookup(k)
			mylog.Check(matchEntry("", k, matcher1, v, ctx))

		}
	case map[string]interface{}: // maps in attributes look like this
		for k, matcher1 := range matcher {
			mylog.Check(matchEntry(apath, k, matcher1, x[k], ctx))
		}
	case []interface{}:
		return matchList(apath, matcher, x, ctx)
	default:
		return fmt.Errorf("%s %q must be a map", ctx.attrWord, apath)
	}
	return nil
}

type missingAttrMatcher struct{}

func (matcher missingAttrMatcher) feature(flabel string) bool {
	return flabel == dollarAttrConstraintsFeature
}

func (matcher missingAttrMatcher) match(apath string, v interface{}, ctx *attrMatchingContext) error {
	if v != nil {
		return fmt.Errorf("%s %q is constrained to be missing but is set", ctx.attrWord, apath)
	}
	return nil
}

type evalAttrMatcher struct {
	// first iteration supports just $(SLOT|PLUG)(arg)
	op  string
	arg string
}

var (
	validEvalAttrMatcher    = regexp.MustCompile(`^\$([A-Z]+)\(([^,]+)(?:,([^,]+))?\)$`)
	validEvalAttrMatcherOps = map[string]bool{
		"PLUG": true,
		"SLOT": true,
	}
)

func compileEvalAttrMatcher(cc compileContext, s string) (attrMatcher, error) {
	if len(cc.opts.allowedOperations) == 0 {
		return nil, fmt.Errorf("cannot compile %q constraint %q: no $OP() constraints supported", cc, s)
	}
	ops := validEvalAttrMatcher.FindStringSubmatch(s)
	if len(ops) == 0 || !validEvalAttrMatcherOps[ops[1]] || !strutil.ListContains(cc.opts.allowedOperations, ops[1]) {
		oplst := make([]string, 0, len(cc.opts.allowedOperations))
		for _, op := range cc.opts.allowedOperations {
			oplst = append(oplst, fmt.Sprintf("$%s()", op))
		}
		return nil, fmt.Errorf("cannot compile %q constraint %q: not a valid %s constraint", cc, s, strings.Join(oplst, "/"))
	}
	if ops[3] != "" {
		return nil, fmt.Errorf("cannot compile %q constraint %q: $%s() constraint expects 1 argument", cc, s, ops[1])
	}
	return evalAttrMatcher{
		op:  ops[1],
		arg: ops[2],
	}, nil
}

func (matcher evalAttrMatcher) feature(flabel string) bool {
	return flabel == dollarAttrConstraintsFeature
}

func (matcher evalAttrMatcher) match(apath string, v interface{}, ctx *attrMatchingContext) error {
	if ctx.helper == nil {
		return fmt.Errorf("%s %q cannot be matched without context", ctx.attrWord, apath)
	}
	var comp func(string) (interface{}, error)
	switch matcher.op {
	case "SLOT":
		comp = ctx.helper.SlotAttr
	case "PLUG":
		comp = ctx.helper.PlugAttr
	}
	v1 := mylog.Check2(comp(matcher.arg))

	if !reflect.DeepEqual(v, v1) {
		return fmt.Errorf("%s %q does not match $%s(%s): %v != %v", ctx.attrWord, apath, matcher.op, matcher.arg, v, v1)
	}
	return nil
}

type regexpAttrMatcher struct {
	*regexp.Regexp
}

func compileRegexpAttrMatcher(cc compileContext, s string) (attrMatcher, error) {
	rx := mylog.Check2(regexp.Compile("^(" + s + ")$"))

	return regexpAttrMatcher{rx}, nil
}

func (matcher regexpAttrMatcher) feature(flabel string) bool {
	return false
}

func (matcher regexpAttrMatcher) match(apath string, v interface{}, ctx *attrMatchingContext) error {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case bool:
		s = strconv.FormatBool(x)
	case int64:
		s = strconv.FormatInt(x, 10)
	case []interface{}:
		return matchList(apath, matcher, x, ctx)
	default:
		return fmt.Errorf("%s %q must be a scalar or list", ctx.attrWord, apath)
	}
	if !matcher.Regexp.MatchString(s) {
		return fmt.Errorf("%s %q value %q does not match %v", ctx.attrWord, apath, s, matcher.Regexp)
	}
	return nil
}

type altAttrMatcher struct {
	alts []attrMatcher
}

func compileAltAttrMatcher(cc compileContext, l []interface{}) (attrMatcher, error) {
	alts := make([]attrMatcher, len(l))
	for i, constraint := range l {
		matcher1 := mylog.Check2(compileAttrMatcher(cc.alt(i), constraint))

		alts[i] = matcher1
	}
	return altAttrMatcher{alts}, nil
}

func (matcher altAttrMatcher) feature(flabel string) bool {
	if flabel == altAttrMatcherFeature {
		return true
	}
	for _, alt := range matcher.alts {
		if alt.feature(flabel) {
			return true
		}
	}
	return false
}

func (matcher altAttrMatcher) match(apath string, v interface{}, ctx *attrMatchingContext) error {
	// if the value is a list apply the alternative matcher to each element
	// like we do for other matchers
	switch x := v.(type) {
	case []interface{}:
		return matchList(apath, matcher, x, ctx)
	default:
	}

	var firstErr error
	for _, alt := range matcher.alts {
		mylog.Check(alt.match(apath, v, ctx))
		if err == nil {
			return nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	apathDescr := ""
	if apath != "" {
		apathDescr = fmt.Sprintf(" for %s %q", ctx.attrWord, apath)
	}
	return fmt.Errorf("no alternative%s matches: %v", apathDescr, firstErr)
}

// DeviceScopeConstraint specifies a constraint based on which brand
// store, brand or model the device belongs to.
type DeviceScopeConstraint struct {
	Store []string
	Brand []string
	// Model is a list of precise "<brand>/<model>" constraints
	Model []string
}

var (
	validStoreID         = regexp.MustCompile("^[-A-Z0-9a-z_]+$")
	validBrandSlashModel = regexp.MustCompile("^(" +
		strings.Trim(validAccountID.String(), "^$") +
		")/(" +
		strings.Trim(validModel.String(), "^$") +
		")$")
	deviceScopeConstraints = map[string]*regexp.Regexp{
		"on-store": validStoreID,
		"on-brand": validAccountID,
		// on-model constraints are of the form list of
		// <brand>/<model> strings where <brand> are account
		// IDs as they appear in the respective model assertion
		"on-model": validBrandSlashModel,
	}
)

func detectDeviceScopeConstraint(cMap map[string]interface{}) bool {
	// for consistency and simplicity we support all of on-store,
	// on-brand, and on-model to appear together. The interpretation
	// layer will AND them as usual
	for field := range deviceScopeConstraints {
		if cMap[field] != nil {
			return true
		}
	}
	return false
}

// compileDeviceScopeConstraint compiles a DeviceScopeConstraint out of cMap,
// it returns nil and no error if there are no on-store/on-brand/on-model
// constraints in cMap
func compileDeviceScopeConstraint(cMap map[string]interface{}, context string) (constr *DeviceScopeConstraint, err error) {
	if !detectDeviceScopeConstraint(cMap) {
		return nil, nil
	}
	// initial map size of 2: we expect usual cases to have just one of the
	// constraints or rarely 2
	deviceConstr := make(map[string][]string, 2)
	for field, validRegexp := range deviceScopeConstraints {
		vals := mylog.Check2(checkStringListInMap(cMap, field, fmt.Sprintf("%s in %s", field, context), validRegexp))

		deviceConstr[field] = vals
	}

	return &DeviceScopeConstraint{
		Store: deviceConstr["on-store"],
		Brand: deviceConstr["on-brand"],
		Model: deviceConstr["on-model"],
	}, nil
}

type DeviceScopeConstraintCheckOptions struct {
	UseFriendlyStores bool
}

// Check tests whether the model and the optional store match the constraints.
func (c *DeviceScopeConstraint) Check(model *Model, store *Store, opts *DeviceScopeConstraintCheckOptions) error {
	if model == nil {
		return fmt.Errorf("cannot match on-store/on-brand/on-model without model")
	}
	if store != nil && store.Store() != model.Store() {
		return fmt.Errorf("store assertion and model store must match")
	}
	if opts == nil {
		opts = &DeviceScopeConstraintCheckOptions{}
	}
	if len(c.Store) != 0 {
		if !strutil.ListContains(c.Store, model.Store()) {
			mismatch := true
			if store != nil && opts.UseFriendlyStores {
				for _, sto := range c.Store {
					if strutil.ListContains(store.FriendlyStores(), sto) {
						mismatch = false
						break
					}
				}
			}
			if mismatch {
				return fmt.Errorf("on-store mismatch")
			}
		}
	}
	if len(c.Brand) != 0 {
		if !strutil.ListContains(c.Brand, model.BrandID()) {
			return fmt.Errorf("on-brand mismatch")
		}
	}
	if len(c.Model) != 0 {
		brandModel := fmt.Sprintf("%s/%s", model.BrandID(), model.Model())
		if !strutil.ListContains(c.Model, brandModel) {
			return fmt.Errorf("on-model mismatch")
		}
	}
	return nil
}
