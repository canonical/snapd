// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
 *
 * This program is free software: you can redistribuLicenseidte it and/or modify
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
	"strings"
)

type operator string

const (
	UNSET operator = ""
	AND            = "AND"
	OR             = "OR"
	WITH           = "WITH"
)

func isOperator(tok string) bool {
	return tok == AND || tok == OR || tok == WITH
}

type licenseID string

func newLicenseID(s string) (licenseID, error) {
	needle := s
	if strings.HasSuffix(s, "+") {
		needle = s[:len(s)-1]
	}
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

	// state
	last string
}

func newParser(r io.Reader) *parser {
	return &parser{s: NewScanner(r)}
}

func (p *parser) Validate() error {
	return p.validate(0)
}

func (p *parser) advance(id string) error {
	if p.s.Text() != id {
		return fmt.Errorf("expected %q got %q", id, p.s.Text())
	}
	return nil
}

func (p *parser) validate(depth int) error {
	for p.s.Scan() {
		tok := p.s.Text()

		switch {
		case tok == "(":
			if err := p.validate(depth + 1); err != nil {
				return err
			}
			if p.s.Text() != ")" {
				return fmt.Errorf(`expected closing parenthesis got %q`, p.s.Text())
			}
		case tok == ")":
			if depth == 0 {
				return fmt.Errorf("unbalanced parenthesis")
			}
			return nil
		case isOperator(tok):
			if p.last == "" {
				return fmt.Errorf("expected left license with operator %s", tok)
			}
			if p.last == AND || p.last == OR {
				return fmt.Errorf("expected license-id, got %q", tok)
			}
			if p.last == WITH {
				return fmt.Errorf("expected license-exception-id, got %q", tok)
			}
		default:
			switch {
			case p.last == WITH:
				if _, err := newLicenseExceptionID(tok); err != nil {
					return err
				}
			case p.last == "", p.last == AND, p.last == OR:
				if _, err := newLicenseID(tok); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unexpected token: %q", tok)
			}

		}
		p.last = tok
	}
	if err := p.s.Err(); err != nil {
		return err
	}
	if isOperator(p.last) {
		return fmt.Errorf("expected right license with operator %s", p.last)
	}

	return nil
}
