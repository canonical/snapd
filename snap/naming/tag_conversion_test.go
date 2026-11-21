// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package naming_test

import (
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap/naming"
)

func FuzzBPFMapNameToSecurityTag(f *testing.F) {
	for _, securityTag := range []string{
		"snap.foo.bar",
		"snap.foo_inst.bar",
		"snap.foo.hook.configure",
		"snap.foo_hook.configure",
		"snap.foo_hook.App",
		"snap.foo_hook.123",
		"snap.foo.hook",
		"snap.foo.hook.hook",
		"snap.foo_hook.hook",
		"snap.foo_hook.hook.install",
		"snap.foo_inst.hook.pre-refresh",
		"snap.foo+comp.hook.install",
		"snap.foo_inst+comp.hook.pre-refresh",
	} {
		f.Add(securityTag)
	}
	f.Fuzz(func(t *testing.T, securityTag string) {
		// Intentionally test only valid tags so that an unexpected rejection
		// is a failure, rather than accepting every error as invalid input.
		sourceTag, err := naming.ParseSecurityTag(securityTag)
		if err != nil {
			t.Skip()
		}
		mapName := naming.SecurityTagToBPFMapName(sourceTag.String())
		tag, err := naming.BPFMapNameToSecurityTag(mapName)
		if err != nil {
			t.Fatalf("valid map name %q for tag %q returned an error: %v", mapName, securityTag, err)
		}
		if tag == nil {
			t.Fatalf("map name %q returned a nil tag without an error", mapName)
		}
		if err := naming.ValidateSecurityTag(tag.String()); err != nil {
			t.Fatalf("map name %q returned invalid tag %q: %v", mapName, tag.String(), err)
		}
		expected := expectedTag(sourceTag)
		if tag.String() != expected {
			t.Fatalf("map name %q returned tag %q, expected %q", mapName, tag.String(), expected)
		}
	})
}

func expectedTag(sourceTag naming.SecurityTag) string {
	// An app of the instance with key "hook" is interpreted as a snap
	// hook when its app name is also a valid hook name.
	if app, ok := sourceTag.(naming.AppSecurityTag); ok {
		snapName, instanceKey, _ := strings.Cut(app.InstanceName(), "_")
		if instanceKey == "hook" && naming.ValidateHook(app.AppName()) == nil {
			return "snap." + snapName + ".hook." + app.AppName()
		}
	}
	return sourceTag.String()
}

func (s *tagSuite) TestSecurityTagToBPFMapName(c *C) {
	for _, t := range []struct {
		tag, bpf string
	}{
		{"snap.name.app", "snap_name_app"},
		{"snap.name_key.app", "snap_name_key_app"},
		{"snap.name.APP", "snap_name_APP"},
		{"snap.other-name.app", "snap_other-name_app"},
		{"snap.my-snap.my-app", "snap_my-snap_my-app"},
		{"snap.name.hook.pre-refresh", "snap_name_hook_pre-refresh"},
		{"snap.name+comp.hook.install", "snap_name+comp_hook_install"},
		{"snap.name_key+comp.hook.install", "snap_name_key+comp_hook_install"},
	} {
		cmt := Commentf("tag %q", t.tag)
		c.Assert(naming.ValidateSecurityTag(t.tag), IsNil, cmt)
		c.Check(naming.SecurityTagToBPFMapName(t.tag), Equals, t.bpf, cmt)
	}
}

func (s *tagSuite) TestBPFMapNameToSecurityTag(c *C) {
	tests := []struct {
		name     string
		expected string
		err      string
	}{
		{
			name:     "snap_foo_bar",
			expected: "snap.foo.bar",
		}, {
			// Instance keys cannot contain hyphens, so the key "my-app"
			// is rejected by ParseSecurityTag inside the converter.
			name: "snap_foo_my-app_bar",
			err:  "cannot identify security tag from name \"snap_foo_my-app_bar\"",
		}, {
			name:     "snap_foo_inst_bar",
			expected: "snap.foo_inst.bar",
		}, {
			// A snap hook (snap.foo.hook.configure). The same pin name is
			// also the valid pin of the app "configure" of the instance
			// whose key is "hook" (snap.foo_hook.configure); it is resolved
			// as a hook, following the snap-device-helper convention.
			name:     "snap_foo_hook_configure",
			expected: "snap.foo.hook.configure",
		}, {
			// Uppercase is valid in app names but not hook names.
			name:     "snap_foo_hook_App",
			expected: "snap.foo_hook.App",
		}, {
			// App names may start with a digit; hook names may not.
			name:     "snap_foo_hook_123",
			expected: "snap.foo_hook.123",
		}, {
			name:     "snap_foo_inst_hook_pre-refresh",
			expected: "snap.foo_inst.hook.pre-refresh",
		}, {
			// Instance keys cannot contain hyphens, so the key "my-app"
			// is rejected by ParseSecurityTag inside the converter.
			name: "snap_foo_bad-key_hook_configure",
			err:  "cannot identify security tag from name \"snap_foo_bad-key_hook_configure\"",
		}, {
			// Only two segments after "snap_": app shape, so an app
			// literally named "hook".
			name:     "snap_foo_hook",
			expected: "snap.foo.hook",
		}, {
			// A hook literally named "hook".
			name:     "snap_foo_hook_hook",
			expected: "snap.foo.hook.hook",
		}, {
			name:     "snap_foo+comp_hook_install",
			expected: "snap.foo+comp.hook.install",
		}, {
			name:     "snap_foo_inst+comp_hook_pre-refresh",
			expected: "snap.foo_inst+comp.hook.pre-refresh",
		}, {
			name: "snap_foo",
			err:  "cannot identify security tag from name \"snap_foo\"",
		}, {
			name: "snap_",
			err:  "cannot identify security tag from name \"snap_\"",
		}, {
			name: "snap__",
			err:  "cannot identify security tag from name \"snap__\"",
		}, {
			name: "nosnap_foo_bar",
			err:  "cannot identify security tag from name \"nosnap_foo_bar\"",
		}, {
			name: "foo_bar",
			err:  "cannot identify security tag from name \"foo_bar\"",
		}, {
			name: "snap_foo_",
			err:  "cannot identify security tag from name \"snap_foo_\"",
		}, {
			name: "snap_foo_hook_",
			err:  "cannot identify security tag from name \"snap_foo_hook_\"",
		}, {
			name: "snap_foo_a_b_c",
			err:  "cannot identify security tag from name \"snap_foo_a_b_c\"",
		}, {
			name: "snap_foo__comp_hook_install",
			err:  "cannot identify security tag from name \"snap_foo__comp_hook_install\"",
		}, {
			name: "snap_foo+comp_app",
			err:  `cannot identify security tag from name "snap_foo\+comp_app"`,
		}, {
			// Uppercase is valid in app names but not hook names.
			name: "snap_foo+comp_hook_App",
			err:  `cannot identify security tag from name "snap_foo\+comp_hook_App"`,
		}, {
			name: "snap_foo+_hook_install",
			err:  `cannot identify security tag from name "snap_foo\+_hook_install"`,
		}, {
			name: "snap_foo+comp+other_hook_install",
			err:  `cannot identify security tag from name "snap_foo\+comp\+other_hook_install"`,
		}, {
			name: "snap_Foo_app",
			err:  "cannot identify security tag from name \"snap_Foo_app\"",
		}, {
			name: "snap_foo_app/name",
			err:  "cannot identify security tag from name \"snap_foo_app/name\"",
		}, {
			name: "snap_foo__app",
			err:  "cannot identify security tag from name \"snap_foo__app\"",
		}, {
			name: "snap_foo__hook_install",
			err:  "cannot identify security tag from name \"snap_foo__hook_install\"",
		}, {
			name: "snap_foo_inst_hook__install",
			err:  "cannot identify security tag from name \"snap_foo_inst_hook__install\"",
		}}
	for _, t := range tests {
		tag, err := naming.BPFMapNameToSecurityTag(t.name)
		cmt := Commentf("name %q", t.name)
		if t.err == "" {
			c.Assert(err, IsNil, cmt)
			c.Check(tag.String(), Equals, t.expected, cmt)
		} else {
			c.Check(err, ErrorMatches, t.err, cmt)
			c.Check(tag, IsNil, cmt)
		}
	}
}
