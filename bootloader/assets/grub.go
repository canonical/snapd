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

package assets

// TODO:UC20 extract common and template parts of command line

// scripts content from https://github.com/snapcore/pc-amd64-gadget, commit:
//
// commit e4d63119322691f14a3f9dfa36a3a075e941ec9d (HEAD -> 20, origin/HEAD, origin/20)
// Merge: b70d2ae d113aca
// Author: Dimitri John Ledkov <xnox@ubuntu.com>
// Date:   Thu May 7 19:30:00 2020 +0100
//
//     Merge pull request #47 from xnox/production-keys
//
//     gadget: bump edition to 2, using production signing keys for everything.

func init() {
	registerSnippetForEditions("grub.cfg:static-cmdline", []forEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	})
	registerSnippetForEditions("grub-recovery.cfg:static-cmdline", []forEditions{
		{FirstEdition: 1, Snippet: []byte("console=ttyS0 console=tty1 panic=-1")},
	})
}
