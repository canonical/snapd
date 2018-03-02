// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package puritan

import (
	"encoding/json"
	"fmt"
	"strconv"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"github.com/snapcore/snapd/strutil"
)

// SimpleStrings are JSON strings that are representable with no backslashes
// (any backslash will be rejected, even if it represents an ASCII character).
type SimpleString struct {
	s string
}

func NewSimpleString(s string) SimpleString {
	return SimpleString{s}
}

func (un *SimpleString) UnmarshalJSON(in []byte) error {
	s, err := unmarshal(in, uOpt{basic: true})
	if err != nil {
		return err
	}
	un.s = s
	return nil
}

// Clean returns the string.
func (un SimpleString) Clean() string {
	return un.s
}

// SimpleStringSlice is a slice of SimpleStrings, for convenience
type SimpleStringSlice []SimpleString

// Clean returns a slice of Clean()ed Strings.
func (un SimpleStringSlice) Clean() []string {
	out := make([]string, len(un))
	for i := range un {
		out[i] = un[i].Clean()
	}

	return out
}

// String accepts any valid JSON string
type String struct {
	s string
}

func NewString(s string) String {
	return String{s}
}

func (un *String) UnmarshalJSON(in []byte) (err error) {
	un.s, err = unmarshal(in, uOpt{})
	return
}

// Clean returns the string, with any control characters stripped.
func (un String) Clean() string {
	return un.s
}

// Paragraph accepts any valid JSON string
type Paragraph struct {
	s string
}

func (un *Paragraph) UnmarshalJSON(in []byte) (err error) {
	un.s, err = unmarshal(in, uOpt{nlok: true})
	return
}

// Clean returns the string, with any control characters except \n stripped.
func (un Paragraph) Clean() string {
	return un.s
}

// StringSlice is a slice of Strings, for convenience.
type StringSlice []String

// Clean returns a slice of Clean()ed Strings.
func (un StringSlice) Clean() []string {
	out := make([]string, len(un))
	for i := range un {
		out[i] = un[i].Clean()
	}

	return out
}

// OldPriceMap is a map for prices in the old API.
type OldPriceMap struct {
	s map[string]float64
}

func (un *OldPriceMap) UnmarshalJSON(in []byte) error {
	var m map[string]float64
	if err := json.Unmarshal(in, &m); err != nil {
		return err
	}
	for k := range m {
		if !okCurrency(k) {
			return fmt.Errorf("invalid currency name %q", k)
		}
	}
	un.s = m

	return nil
}

// Clean returns a Cleaned map[string]float64.
func (un OldPriceMap) Clean() map[string]float64 {
	return un.s
}

// PriceMap is a map used for prices.
//
// the keys are ISO currency codes; the values are strings of floats.
type PriceMap struct {
	s map[string]string
}

func (un *PriceMap) UnmarshalJSON(in []byte) error {
	var m map[string]string
	if err := json.Unmarshal(in, &m); err != nil {
		return err
	}
	for k, v := range m {
		if !okCurrency(k) {
			return fmt.Errorf("invalid currency name %q", k)
		}
		if !okPrice(v) {
			return fmt.Errorf("invalid price %q", v)
		}
	}
	un.s = m

	return nil
}

// Clean returns a map[string]string of cleaned Strings.
func (un PriceMap) Clean() map[string]string {
	return un.s
}

func okCurrency(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r < 'A' || r > 'Z' {
			return false
		}
	}
	return true
}

func okPrice(s string) bool {
	// limits to what strconv.ParseFloat accepts
	for _, r := range s {
		switch r {
		case '.', '+', '-', 'e', 'E':
			// ok
		default:
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

func u4(in []byte) rune {
	if len(in) < 6 || in[0] != '\\' || in[1] != 'u' {
		return -1
	}
	u, err := strconv.ParseUint(string(in[2:6]), 16, 32)
	if err != nil {
		return -1
	}
	return rune(u)
}

type uOpt struct {
	nlok  bool
	basic bool
}

func unmarshal(in []byte, o uOpt) (string, error) {
	// heavily based on (inspired by?) unquoteBytes from encoding/json

	if len(in) < 2 || in[0] != '"' || in[len(in)-1] != '"' {
		// maybe it's a null and that's alright
		if len(in) == 4 && in[0] == 'n' && in[1] == 'u' && in[2] == 'l' && in[3] == 'l' {
			return "", nil
		}
		return "", fmt.Errorf("missing string delimiters: %q", in)
	}
	// prune the quotes
	in = in[1 : len(in)-1]
	i := 0
	// try the fast track
	for i < len(in) {
		// 0x00..0x19 is the first of Cc
		// 0x20..0x7e is all of printable ASCII (minus control chars)
		if in[i] < 0x20 || in[i] > 0x7e || in[i] == '\\' {
			break
		}
		i++
	}
	if i == len(in) {
		// wee
		return string(in), nil
	}
	if o.basic {
		return "", fmt.Errorf("invalid simple JSON string %q: not simple at %d", in, i)
	}
	// in[i] is the first problematic one
	out := make([]byte, i, len(in)+2*utf8.UTFMax)
	copy(out, in)
	var r, r2 rune
	var sz int
	var c byte
	var ubuf [utf8.UTFMax]byte
	for i < len(in) {
		switch c = in[i]; {
		case c == '"' || c < 0x20:
			return "", fmt.Errorf("unexpected unescaped quote at %d in %q", i, in)
		case c == '\\':
			// handle escapes
			i++
			if i == len(in) {
				return "", fmt.Errorf("unexpected end of string (trailing backslash) in %q", in)
			}
			switch in[i] {
			case 'u':
				// oh dear, a unicode wotsit
				r = u4(in[i-1:])
				if r < 0 {
					x := in[i:]
					if len(x) > 6 {
						x = x[:6]
					}
					return "", fmt.Errorf("badly formed \\u escape %q at %d of %q", x, i, in)
				}
				i += 5
				if utf16.IsSurrogate(r) {
					// sigh
					r2 = u4(in[i:])
					if r2 < 0 {
						x := in[i:]
						if len(x) > 6 {
							x = x[:6]
						}
						return "", fmt.Errorf("badly formed \\u escape %q at %d of %q", x, i, in)
					}
					i += 6
					r = utf16.DecodeRune(r, r2)
				}
				if r <= 0x9f {
					// otherwise, it's Cc (both halves, as we're looking at runes)
					if (o.nlok && r == '\n') || (r >= 0x20 && r <= 0x7e) {
						out = append(out, byte(r))
					}
				} else if r != unicode.ReplacementChar && !unicode.Is(strutil.Ctrl, r) {
					sz = utf8.EncodeRune(ubuf[:], r)
					out = append(out, ubuf[:sz]...)
				}
			case 'b', 'f', 'r', 't':
				// do nothing
				i++
			case 'n':
				if o.nlok {
					out = append(out, '\n')
				}
				i++
			case '"', '/', '\\':
				// the spec says just ", / and \ can be backslash-escaped
				// but go adds ' to the list (why?!)
				out = append(out, in[i])
				i++

			default:
				return "", fmt.Errorf("unknown escape %q at %d of %q", c, i, in)
			}
		case c <= 0x7e:
			// printable ASCII, except " or \
			out = append(out, c)
			i++
		default:
			r, sz = utf8.DecodeRune(in[i:])
			j := i + sz
			if r > 0x9f && r != unicode.ReplacementChar && !unicode.Is(strutil.Ctrl, r) {
				out = append(out, in[i:j]...)
			}
			i = j
		}
	}

	out = out[:len(out):len(out)]
	return *(*string)(unsafe.Pointer(&out)), nil
}
