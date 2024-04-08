// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package kcmdline

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

var (
	procCmdline = "/proc/cmdline"
)

// MockProcCmdline overrides the path to /proc/cmdline. For use in tests.
func MockProcCmdline(newPath string) (restore func()) {
	osutil.MustBeTestBinary("mocking can only be done from tests")
	oldProcCmdline := procCmdline
	procCmdline = newPath
	return func() {
		procCmdline = oldProcCmdline
	}
}

// Split tries to split the string comprising full or a part
// of a kernel command line into a list of individual arguments. Returns an
// error when the input string is incorrectly formatted.
//
// See https://www.kernel.org/doc/html/latest/admin-guide/kernel-parameters.html for details.
func Split(s string) (out []string, err error) {
	const (
		argNone            int = iota // initial state
		argName                       // looking at argument name
		argAssign                     // looking at =
		argValue                      // looking at unquoted value
		argValueQuoteStart            // looking at start of quoted value
		argValueQuoted                // looking at quoted value
		argValueQuoteEnd              // looking at end of quoted value
	)
	var b bytes.Buffer
	var rs = []rune(s)
	var last = len(rs) - 1
	var errUnexpectedQuote = fmt.Errorf("unexpected quoting")
	var errUnbalancedQUote = fmt.Errorf("unbalanced quoting")
	var errUnexpectedArgument = fmt.Errorf("unexpected argument")
	var errUnexpectedAssignment = fmt.Errorf("unexpected assignment")
	// arguments are:
	// - arg
	// - arg=value, where value can be any string, spaces are preserve when quoting ".."
	var state = argNone
	for idx, r := range rs {
		maybeSplit := false
		switch state {
		case argNone:
			switch r {
			case '"':
				return nil, errUnexpectedQuote
			case '=':
				return nil, errUnexpectedAssignment
			case ' ':
				maybeSplit = true
			default:
				state = argName
				b.WriteRune(r)
			}
		case argName:
			switch r {
			case '"':
				return nil, errUnexpectedQuote
			case ' ':
				maybeSplit = true
				state = argNone
			case '=':
				state = argAssign
				fallthrough
			default:
				b.WriteRune(r)
			}
		case argAssign:
			switch r {
			case '=':
				return nil, errUnexpectedAssignment
			case ' ':
				// no value: arg=
				maybeSplit = true
				state = argNone
			case '"':
				// arg="..
				state = argValueQuoteStart
				b.WriteRune(r)
			default:
				// arg=v..
				state = argValue
				b.WriteRune(r)
			}
		case argValue:
			switch r {
			case '"':
				// arg=foo"
				return nil, errUnexpectedQuote
			case ' ':
				state = argNone
				maybeSplit = true
			default:
				// arg=value...
				b.WriteRune(r)
			}
		case argValueQuoteStart:
			switch r {
			case '"':
				// closing quote: arg=""
				state = argValueQuoteEnd
				b.WriteRune(r)
			default:
				state = argValueQuoted
				b.WriteRune(r)
			}
		case argValueQuoted:
			switch r {
			case '"':
				// closing quote: arg="foo"
				state = argValueQuoteEnd
				fallthrough
			default:
				b.WriteRune(r)
			}
		case argValueQuoteEnd:
			switch r {
			case ' ':
				maybeSplit = true
				state = argNone
			case '"':
				// arg="foo""
				return nil, errUnexpectedQuote
			case '=':
				// arg="foo"=
				return nil, errUnexpectedAssignment
			default:
				// arg="foo"bar
				return nil, errUnexpectedArgument
			}
		}
		if maybeSplit || idx == last {
			// split now
			if b.Len() != 0 {
				out = append(out, b.String())
				b.Reset()
			}
		}
	}
	switch state {
	case argValueQuoteStart, argValueQuoted:
		// ended at arg=" or arg="foo
		return nil, errUnbalancedQUote
	}
	return out, nil
}

// KeyValues returns a map of the specified keys to the values
// set for them in the kernel command line (eg. panic=-1). If the key is missing
// from the kernel command line, it is omitted from the returned map, but it is
// added if present even if it has no value.
func KeyValues(keys ...string) (map[string]string, error) {
	cmdline, err := KernelCommandLine()
	if err != nil {
		return nil, err
	}

	parsed := Parse(cmdline)
	m := make(map[string]string, len(keys))

	for _, arg := range parsed {
		for _, key := range keys {
			if arg.Param != key {
				continue
			}
			m[key] = arg.Value
			break
		}
	}
	return m, nil
}

// Argument represents a parsed kernel argument.
type Argument struct {
	Param  string
	Value  string
	Quoted bool
}

// UnmarshalYAML implements the Unmarshaler interface.
func (ka *Argument) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var arg string
	if err := unmarshal(&arg); err != nil {
		return errors.New("cannot unmarshal kernel argument")
	}

	parsed := Parse(arg)
	if len(parsed) != 1 {
		return fmt.Errorf("%q is not a unique kernel argument", arg)
	}
	*ka = parsed[0]

	return nil
}

func quoteIfNeeded(input string, force bool) string {
	if force || strings.Contains(input, " ") {
		return "\"" + input + "\""
	} else {
		return input
	}
}

func (ka *Argument) String() string {
	if ka.Value == "" {
		return quoteIfNeeded(ka.Param, false)
	} else {
		return fmt.Sprintf("%s=%s", quoteIfNeeded(ka.Param, false), quoteIfNeeded(ka.Value, ka.Quoted))
	}
}

// Parse parses a kernel command line, returning a
// slice with the arguments in the same order as in cmdline. Note that
// kernel arguments can be repeated. We follow the same algorithm as in
// linux kernel's function lib/cmdline.c:next_arg as far as possible.
// TODO Replace Split with this eventually
func Parse(cmdline string) (args []Argument) {
	cmdlineBy := []byte(cmdline)
	args = []Argument{}
	start := firstNotSpace(cmdlineBy)
	for start < len(cmdlineBy) {
		argument, end := parseArgument(cmdlineBy[start:])
		args = append(args, argument)
		start += end
		start += firstNotSpace(cmdlineBy[start:])
	}

	return args
}

// Does the same as isspace() in tools/include/nolibc/ctype.h from the
// linux kernel
func isSpace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	}
	return false
}

// Similar to skip_spaces() in lib/string_helpers.c from the linux kernel
func firstNotSpace(args []byte) int {
	var i int
	var b byte
	for i, b = range args {
		if !isSpace(b) {
			return i
		}
	}
	return i + 1
}

// parseArgument parses a kernel argument that is known to start at
// the beginning of args, returning a Argument with the
// parameter, the assigned value if any and information on whether
// there was quoting or not, plus where the argument ends in args.
//
// This follows the same algorithm as the next_arg function from
// lib/cmdline.c in the linux kernel, to make sure we handle the
// arguments in exactly the same way.
func parseArgument(args []byte) (argument Argument, end int) {
	var i, equals, startArg int
	var argQuoted, inQuote bool
	var param, val string
	var quoted bool

	if args[0] == '"' {
		startArg++
		argQuoted = true
		inQuote = true
	}

	for i = startArg; i < len(args); i++ {
		if isSpace(args[i]) && !inQuote {
			break
		}
		if args[i] == '=' && equals == 0 {
			equals = i
		}
		if args[i] == '"' {
			inQuote = !inQuote
		}
	}

	end = i
	endParam := i
	// subsVal tells us if we need to remove a '"' at the end of the value.
	// subsParam tells us if we need to remove a '"' at the end of the parameter,
	// which is needed only if the argument started with '"', but no value is set.
	var subsVal, subsParam int
	if argQuoted && end > startArg && args[end-1] == '"' {
		quoted = true
		if equals != 0 {
			subsVal = 1
		} else {
			subsParam = 1
		}
	}
	if equals != 0 {
		endParam = equals
		startVal := equals + 1
		endVal := end
		if startVal < end && args[startVal] == '"' {
			quoted = true
			startVal++
			if args[end-1] == '"' {
				subsVal = 1
			}
		}
		val = string(args[startVal : endVal-subsVal])
	}

	param = string(args[startArg : endParam-subsParam])
	argument = Argument{Param: param, Value: val, Quoted: quoted}
	return argument, end
}

// KernelCommandLine returns the command line reported by the running kernel.
func KernelCommandLine() (string, error) {
	buf, err := os.ReadFile(procCmdline)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}

type valuePattern interface {
	Match(value string) bool
	String() string
}

type valuePatternAny struct{}

func (any valuePatternAny) Match(value string) bool {
	return true
}

func (any valuePatternAny) String() string {
	return "*"
}

type valuePatternConstant struct {
	constantValue string
}

func (constant valuePatternConstant) Match(value string) bool {
	return constant.constantValue == value
}

func (constant valuePatternConstant) String() string {
	return fmt.Sprintf(`"%s"`, constant.constantValue)
}

// ArgumentPattern represents a pattern which can match a Argument
// This is intended to be used with Matcher
type ArgumentPattern struct {
	param string
	value valuePattern
}

// Matcher matches a Argument with multiple ArgumentPatterns
type Matcher struct {
	patterns map[string]valuePattern
}

func (m *Matcher) Match(arg Argument) bool {
	pattern, ok := m.patterns[arg.Param]
	if !ok {
		return false
	}
	return pattern.Match(arg.Value)
}

func NewMatcher(allowed []ArgumentPattern) Matcher {
	patterns := map[string]valuePattern{}

	for _, p := range allowed {
		patterns[p.param] = p.value
	}

	return Matcher{patterns}
}

// This constructor is needed mainly for test instead of unmarshaling from yaml
func NewConstantPattern(param string, value string) ArgumentPattern {
	return ArgumentPattern{param, valuePatternConstant{value}}
}

// This constructor is needed mainly for test instead of unmarshaling from yaml
func NewAnyPattern(param string) ArgumentPattern {
	return ArgumentPattern{param, valuePatternAny{}}
}

func (kap *ArgumentPattern) unmarshalFromString(arg string) error {
	parsed := Parse(arg)
	if len(parsed) != 1 {
		return fmt.Errorf("%q is not a unique kernel argument", arg)
	}
	// To make parsing future proof in case we support full
	// globbing in the future, do not allow unquoted globbing
	// characters, except the currently only supported case ('*').
	if !parsed[0].Quoted && parsed[0].Value != "*" &&
		strings.ContainsAny(parsed[0].Value, `*?[]\{}`) {
		return fmt.Errorf("%q contains globbing characters and is not quoted",
			parsed[0].Value)
	}
	kap.param = parsed[0].Param
	if parsed[0].Quoted || parsed[0].Value != "*" {
		kap.value = valuePatternConstant{parsed[0].Value}
	} else {
		kap.value = valuePatternAny{}
	}

	return nil
}

func (kap *ArgumentPattern) marshalToString() string {
	return fmt.Sprintf(`%s=%s`, kap.param, kap.value.String())
}

func (kap *ArgumentPattern) UnmarshalBinary(data []byte) error {
	return kap.unmarshalFromString(string(data))
}

func (kap *ArgumentPattern) MarshalBinary() ([]byte, error) {
	return []byte(kap.marshalToString()), nil
}

func (kap *ArgumentPattern) UnmarshalText(data []byte) error {
	return kap.unmarshalFromString(string(data))
}

func (kap *ArgumentPattern) MarshalText() ([]byte, error) {
	return []byte(kap.marshalToString()), nil
}

func (kap *ArgumentPattern) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var arg string
	if err := unmarshal(&arg); err != nil {
		return fmt.Errorf("cannot unmarshal kernel argument: %v", err)
	}
	return kap.unmarshalFromString(arg)
}

// RemoveMatchingFilter parses a kernel command line and remove arguments that
// matches match patterns.
func RemoveMatchingFilter(cmdline string, match []ArgumentPattern) []string {
	parsedDefault := Parse(cmdline)
	matcher := NewMatcher(match)
	var keepArgs []string
	for _, arg := range parsedDefault {
		if !matcher.Match(arg) {
			keepArgs = append(keepArgs, arg.String())
		}
	}

	return keepArgs
}
