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

//go:generate update-pot

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	gettext "github.com/chai2010/gettext-go"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

// TEXTDOMAIN is the message domain used by snappy; see dgettext(3)
// for more information.
var (
	TEXTDOMAIN = "snappy"
	locale     localeCatalog

	translationDomain string
	translationDir    string
)

type localeCatalog interface {
	Gettext(msgid string) string
	NGettext(msgid string, msgidPlural string, n uint32) string
}

func init() {
	bindTextDomain(TEXTDOMAIN, "/usr/share/locale")
	setLocale("")
}

func langpackResolver(baseRoot string, locale string, domain string) string {
	// first check for the real locale (e.g. de_DE)
	// then try to simplify the locale (e.g. de_DE -> de)
	locales := []string{locale, strings.SplitN(locale, "_", 2)[0]}
	for _, locale := range locales {
		r := filepath.Join(locale, "LC_MESSAGES", fmt.Sprintf("%s.mo", domain))

		// look into the core snaps first for translations,
		// then the main system
		candidateDirs := []string{
			filepath.Join(dirs.SnapMountDir, "/core/current/", baseRoot),
			baseRoot,
		}
		for _, root := range candidateDirs {
			// ubuntu uses /usr/lib/locale-langpack and patches the glibc gettext
			// implementation
			langpack := filepath.Join(root, "..", "locale-langpack", r)
			if osutil.FileExists(langpack) {
				return langpack
			}

			regular := filepath.Join(root, r)
			if osutil.FileExists(regular) {
				return regular
			}
		}
	}

	return ""
}

func bindTextDomain(domain, dir string) {
	translationDomain = domain
	translationDir = dir
}

func setLocale(loc string) {
	if loc == "" {
		loc = localeFromEnv()
	}

	locale = newGettextCatalog(translationDir, translationDomain, simplifyLocale(loc), langpackResolver)
}

func simplifyLocale(loc string) string {
	// de_DE.UTF-8, de_DE@euro all need to get simplified
	loc = strings.Split(loc, "@")[0]
	loc = strings.Split(loc, ".")[0]

	return loc
}

func localeFromEnv() string {
	loc := os.Getenv("LC_MESSAGES")
	if loc == "" {
		loc = os.Getenv("LANG")
	}

	return loc
}

// CurrentLocale returns the current locale without encoding or variants.
func CurrentLocale() string {
	return simplifyLocale(localeFromEnv())
}

// G is the shorthand for Gettext
func G(msgid string) string {
	return locale.Gettext(msgid)
}

// https://www.gnu.org/software/gettext/manual/html_node/Plural-forms.html
// (search for 1000)
func ngn(d int) uint32 {
	const max = 1000000
	if d < 0 {
		d = -d
	}
	if d > max {
		return uint32((d % max) + max)
	}
	return uint32(d)
}

// NG is the shorthand for NGettext
func NG(msgid string, msgidPlural string, n int) string {
	return locale.NGettext(msgid, msgidPlural, ngn(n))
}

func MockLocale(l interface {
	Gettext(string) string
	NGettext(string, string, uint32) string
}) (restore func()) {
	osutil.MustBeTestBinary("cannot mock locale in a non-test binary")
	old := locale
	locale = l
	return func() {
		locale = old
	}
}

type chaiCatalog struct {
	gettexter gettext.Gettexter
}

func (c chaiCatalog) Gettext(msgid string) string {
	return c.gettexter.Gettext(msgid)
}

func (c chaiCatalog) NGettext(msgid string, msgidPlural string, n uint32) string {
	translated := c.gettexter.NGettext(msgid, msgidPlural, int(n))
	if translated == msgid && msgidPlural != "" && n != 1 {
		return msgidPlural
	}
	return translated
}

type snapdLocaleFS struct {
	baseRoot string
	resolver func(baseRoot string, locale string, domain string) string
}

func (fs snapdLocaleFS) LocaleList() []string {
	return nil
}

func (fs snapdLocaleFS) LoadMessagesFile(domain, locale, ext string) ([]byte, error) {
	if ext != ".mo" {
		return nil, os.ErrNotExist
	}

	file := fs.resolver(fs.baseRoot, locale, domain)
	if file == "" {
		return nil, os.ErrNotExist
	}

	data, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func (fs snapdLocaleFS) LoadResourceFile(domain, locale, name string) ([]byte, error) {
	return nil, os.ErrNotExist
}

func (fs snapdLocaleFS) String() string {
	return fmt.Sprintf("snapdLocaleFS(%s)", fs.baseRoot)
}

func newGettextCatalog(baseDir, domain, loc string, resolver func(baseRoot string, locale string, domain string) string) localeCatalog {
	g := gettext.New(domain, "", snapdLocaleFS{
		baseRoot: baseDir,
		resolver: resolver,
	})
	g.SetLanguage(loc)
	return chaiCatalog{gettexter: g}
}
