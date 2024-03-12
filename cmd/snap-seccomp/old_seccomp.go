// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build oldseccomp

/*
 * Copyright (C) 2021 Canonical Ltd
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

package main

// On 14.04 we need to use forked libseccomp-golang, as recent
// upstream libseccomp-golang does not support building against
// libseecomp 2.1.1. This is patched in via packaging patch. But to
// continue vendoring the modules in go.mod any golang file must still
// reference the old forked libseccomp-golang. Which is here.  This
// file and import can be safely removed, once 14.04 build support of
// master is deemed to never be required again.
import "github.com/mvo5/libseccomp-golang"
