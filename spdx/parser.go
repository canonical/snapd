// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package spdx

import (
	"fmt"
	"io"

	"github.com/ddkwork/golibrary/mylog"
)

const (
	opAND  = "AND"
	opOR   = "OR"
	opWITH = "WITH"
)

func isOperator(tok string) bool {
	return tok == opAND || tok == opOR || tok == opWITH
}

type licenseID string

func newLicenseID(s string) (licenseID, error) {
	needle := s
	for _, known := range allLicenses {
		if needle == known {
			return licenseID(s), nil
		}
	}
	return "", fmt.Errorf("unknown license: %s", s)
}

type licenseExceptionID string

func newLicenseExceptionID(s string) (licenseExceptionID, error) {
	for _, known := range licenseExceptions {
		if s == known {
			return licenseExceptionID(s), nil
		}
	}
	return "", fmt.Errorf("unknown license exception: %s", s)
}

type parser struct {
	s *Scanner
}

func newParser(r io.Reader) *parser {
	return &parser{s: NewScanner(r)}
}

func (p *parser) Validate() error {
	return p.validate(0)
}

func (p *parser) validate(depth int) error {
	last := ""

	for p.s.Scan() {
		tok := p.s.Text()

		switch {
		case tok == "(":
			if last == opWITH {
				return fmt.Errorf("%q not allowed after WITH", tok)
			}
			mylog.Check(p.validate(depth + 1))

			if p.s.Text() != ")" {
				return fmt.Errorf(`expected ")" got %q`, p.s.Text())
			}
		case tok == ")":
			if depth == 0 {
				return fmt.Errorf(`unexpected ")"`)
			}
			if last == "" {
				return fmt.Errorf("empty expression")
			}
			return nil
		case isOperator(tok):
			if last == "" {
				return fmt.Errorf("missing license before %s", tok)
			}
			if last == opAND || last == opOR {
				return fmt.Errorf("expected license name, got %q", tok)
			}
			if tok == opWITH && last == "(" {
				return fmt.Errorf("expected license name before %s", tok)
			}
			if last == opWITH {
				return fmt.Errorf("expected exception name, got %q", tok)
			}
		default:
			switch {
			case last == opWITH:
				mylog.Check2(newLicenseExceptionID(tok))

			case last == "", last == opAND, last == opOR:
				mylog.Check2(newLicenseID(tok))

			default:
				if _ := mylog.Check2(newLicenseID(last)); err == nil {
					if _ := mylog.Check2(newLicenseID(tok)); err == nil {
						return fmt.Errorf("missing AND or OR between %q and %q", last, tok)
					}
				}
				return fmt.Errorf("unexpected string: %q", tok)
			}

		}
		last = tok
	}
	mylog.Check(p.s.Err())

	if isOperator(last) {
		return fmt.Errorf("missing license after %s", last)
	}
	if last == "" {
		return fmt.Errorf("empty expression")
	}

	return nil
}
