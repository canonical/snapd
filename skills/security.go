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

package skills

import (
	"fmt"
)

// securityHelper is an interface for common aspects of generating security files.
type securityHelper interface {
	securitySystem() SecuritySystem
	pathForApp(snapName, appName string) string
	headerForApp(snapName, appName string) []byte
	footerForApp(snapName, appName string) []byte
}

// appArmor is a security subsystem that writes apparmor profiles.
//
// Each apparmor profile contains a simple <header><content><footer> structure.
// The header specified an identifier that is relevant to the kernel. The
// identifier can be either the full path of the executable or an abstract
// identifier not related to the executable name.
//
// File containing apparmor profile has to be parsed, compiled and loaded into
// the running kernel using apparmor_parser. After this is done the actual file
// is irrelevant and can be removed. To improve performance certain command
// line options to apparmor_parser can be used to cache compiled profile across
// reboots.
//
// NOTE: ubuntu-core-launcher only uses the profile identifier. It doesn't handle
// loading the profile into the kernel or compiling it from source.
type appArmor struct{}

func (aa *appArmor) securitySystem() SecuritySystem {
	return SecurityAppArmor
}

func (aa *appArmor) pathForApp(snapName, appName string) string {
	return fmt.Sprintf("/run/snappy/security/apparmor/%s/%s.profile", snapName, appName)
}

func (aa *appArmor) headerForApp(snapName, appName string) []byte {
	// TODO: use a real header here
	return []byte(fmt.Sprintf("fake \"/snaps/%s/current/%s\" {\n", snapName, appName))
}

func (aa *appArmor) footerForApp(snapName, appName string) []byte {
	return []byte("}\n")
}

// secComp is a security subsystem that writes additional seccomp rules.
//
// Rules use a simple line-oriented record structure.  Each line specifies a
// system call that is allowed.  Lines starting with "deny" specify system
// calls that are explicitly not allowed. Lines starting with '#' are treated
// as comments are ignored.
//
// NOTE: This subsystem interacts with ubuntu-core-launcher. The launcher reads
// a single profile from a specific path, parses it and loads a seccomp profile
// (using Berkley packet filter as low level mechanism).
type secComp struct{}

func (sc *secComp) securitySystem() SecuritySystem {
	return SecuritySecComp
}

func (sc *secComp) pathForApp(snapName, appName string) string {
	// NOTE: This path has to be synchronized with ubuntu-core-launcher.
	// TODO: Use the path that ubuntu-core-launcher actually looks at.
	return fmt.Sprintf("/run/snappy/security/seccomp/%s/%s.profile", snapName, appName)
}

func (sc *secComp) headerForApp(snapName, appName string) []byte {
	// TODO: Inject the real profile as the header.
	// e.g. /usr/share/seccomp/templates/ubuntu-core/16.04/default
	return []byte("# TODO: add default seccomp profile here\n")
}

func (sc *secComp) footerForApp(snapName, appName string) []byte {
	return nil // seccomp doesn't require a footer
}
