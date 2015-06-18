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

package i18n

import (
	"path/filepath"
	"testing"

	"launchpad.net/snappy/helpers"
	
	"github.com/gosexy/gettext"
	. "gopkg.in/check.v1"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type i18nTestSuite struct {
}

var _ = Suite(&i18nTestSuite{})

func (s *i18nTestSuite) TestTranslates(c *C) {
	localeDir, err := filepath.Abs("../share/locale")
	c.Assert(err, IsNil)
	if !helpers.FileExists(localeDir) {
		c.Skip("no locale dir, please run 'go generate'")
	}
	
	gettext.BindTextdomain("snappy", localeDir)
	locale := gettext.SetLocale(gettext.LC_ALL, "en_DK.UTF-8")
	defer gettext.SetLocale(gettext.LC_ALL, "")
	if locale != "en_DK.UTF-8" {
		c.Skip("can not init locale")
	}

	//TRANSLATORS: ignore please, this is just used to test the i18n
	//             system

	c.Assert(G("!!!"), Equals, "???")
}
