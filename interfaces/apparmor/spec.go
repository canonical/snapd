// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package apparmor

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/snap"
)

// Specification assists in collecting apparmor entries associated with an interface.
type Specification struct {
	// scope for various Add{...}Snippet functions
	securityTags []string

	// snippets are indexed by security tag and describe parts of apparmor policy
	// for snap application and hook processes. The security tag encodes the identity
	// of the application or hook.
	snippets map[string][]string
	// updateNS describe parts of apparmor policy for snap-update-ns executing
	// on behalf of a given snap.
	updateNS []string
}

// setScope sets the scope of subsequent AddSnippet family functions.
// The returned function resets the scope to an empty scope.
func (spec *Specification) setScope(securityTags []string) (restore func()) {
	spec.securityTags = securityTags
	return func() {
		spec.securityTags = nil
	}
}

// AddSnippet adds a new apparmor snippet to all applications and hooks using the interface.
func (spec *Specification) AddSnippet(snippet string) {
	if len(spec.securityTags) == 0 {
		return
	}
	if spec.snippets == nil {
		spec.snippets = make(map[string][]string)
	}
	for _, tag := range spec.securityTags {
		spec.snippets[tag] = append(spec.snippets[tag], snippet)
		sort.Strings(spec.snippets[tag])
	}
}

// AddUpdateNS adds a new apparmor snippet for the snap-update-ns program.
func (spec *Specification) AddUpdateNS(snippet string) {
	spec.updateNS = append(spec.updateNS, snippet)
}

// AddSnapLayout adds apparmor snippets based on the layout of the snap.
//
// The per-snap snap-update-ns profiles are composed via a template and
// snippets for the snap. The snippets may allow (depending on the snippet):
// - mount profiles via the content interface
// - creating missing mount point directories under $SNAP* (the 'tree'
//   of permissions is needed for SecureMkDirAll that uses
//   open(..., O_NOFOLLOW) and mkdirat() using the resulting file descriptor)
// - creating a placeholder directory in /tmp/.snap/ in the per-snap mount
//   namespace to support writable mimic which uses tmpfs and bind mount to
//   poke holes in arbitrary read-only locations
// - mounting/unmounting any part of $SNAP into placeholder directory
// - mounting/unmounting tmpfs over the original $SNAP/** location
// - mounting/unmounting from placeholder back to $SNAP/** (for reconstructing
//   the data)
// Importantly, the above mount operations are happening within the per-snap
// mount namespace.
func (spec *Specification) AddSnapLayout(si *snap.Info) {
	if len(si.Layout) == 0 {
		return
	}

	// Walk the layout elements in deterministic order, by mount point name.
	paths := make([]string, 0, len(si.Layout))
	for path := range si.Layout {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Get tags describing all apps and hooks.
	tags := make([]string, 0, len(si.Apps)+len(si.Hooks))
	for _, app := range si.Apps {
		tags = append(tags, app.SecurityTag())
	}
	for _, hook := range si.Hooks {
		tags = append(tags, hook.SecurityTag())
	}

	// Append layout snippets to all tags; the layout applies equally to the
	// entire snap as the entire snap uses one mount namespace.
	if spec.snippets == nil {
		spec.snippets = make(map[string][]string)
	}
	for _, tag := range tags {
		for _, path := range paths {
			snippet := snippetFromLayout(si.Layout[path])
			spec.snippets[tag] = append(spec.snippets[tag], snippet)
		}
		sort.Strings(spec.snippets[tag])
	}
	// Append update-ns snippets that allow constructing the layout.
	for _, path := range paths {
		var buf bytes.Buffer
		l := si.Layout[path]
		fmt.Fprintf(&buf, "  # Layout %s\n", l.String())
		path := si.ExpandSnapVariables(l.Path)
		switch {
		case l.Bind != "":
			bind := si.ExpandSnapVariables(l.Bind)
			// Allow bind mounting the layout element.
			fmt.Fprintf(&buf, "  mount options=(rbind, rw) %s/ -> %s/,\n", bind, path)
			fmt.Fprintf(&buf, "  umount %s/,\n", path)
			// Allow constructing writable mimic in both bind-mount source and mount point.
			WritableProfile(&buf, path)
			WritableProfile(&buf, bind)
		case l.BindFile != "":
			bindFile := si.ExpandSnapVariables(l.BindFile)
			// Allow bind mounting the layout element.
			fmt.Fprintf(&buf, "  mount options=(bind, rw) %s -> %s,\n", bindFile, path)
			fmt.Fprintf(&buf, "  umount %s,\n", path)
			// Allow constructing writable mimic in both bind-mount source and mount point.
			WritableFileProfile(&buf, path)
			WritableFileProfile(&buf, bindFile)
		case l.Type == "tmpfs":
			fmt.Fprintf(&buf, "  mount fstype=tmpfs tmpfs -> %s/,\n", path)
			fmt.Fprintf(&buf, "  umount %s/,\n", path)
			// Allow constructing writable mimic to mount point.
			WritableProfile(&buf, path)
		case l.Symlink != "":
			// Allow constructing writable mimic to symlink parent directory.
			fmt.Fprintf(&buf, "  %s rw,\n", path)
			WritableProfile(&buf, path)
		}
		spec.AddUpdateNS(buf.String())
	}
}

// isProbably writable returns true if the path is probably representing writable area.
func isProbablyWritable(path string) bool {
	return strings.HasPrefix(path, "/var/snap/") || strings.HasPrefix(path, "/home/") || strings.HasPrefix(path, "/root/")
}

// isProbablyPresent returns true if the path is probably already present.
//
// This is used as a simple hint to not inject writable path rules for things
// that we don't expect to create as they are already present in the skeleton
// file-system tree.
func isProbablyPresent(path string) bool {
	return path == "/" || path == "/snap" || path == "/var" || path == "/var/snap" || path == "/tmp" || path == "/usr" || path == "/etc"
}

// WritableFileProfile writes a profile for snap-update-ns for making given file writable.
func WritableFileProfile(buf *bytes.Buffer, path string) {
	if path == "/" {
		return
	}
	if isProbablyWritable(path) {
		fmt.Fprintf(buf, "  # Writable file %s\n", path)
		fmt.Fprintf(buf, "  %s rw,\n", path)
		for p := parent(path); !isProbablyPresent(p); p = parent(p) {
			fmt.Fprintf(buf, "  %s/ rw,\n", p)
		}
	} else {
		parentPath := parent(path)
		fmt.Fprintf(buf, "  # Writable mimic %s\n", parentPath)
		// Allow setting the read-only directory aside via a bind mount.
		fmt.Fprintf(buf, "  mount options=(rbind, rw) %s/ -> /tmp/.snap%s/,\n", parentPath, parentPath)
		// Allow mounting tmpfs over the read-only directory.
		fmt.Fprintf(buf, "  mount fstype=tmpfs options=(rw) tmpfs -> %s/,\n", parentPath)
		// Allow bind mounting things to reconstruct the now-writable parent directory.
		fmt.Fprintf(buf, "  mount options=(rbind, rw) /tmp/.snap%s/** -> %s/**,\n", parentPath, parentPath)
		fmt.Fprintf(buf, "  mount options=(bind, rw) /tmp/.snap%s/* -> %s/*,\n", parentPath, parentPath)
		// Allow unmounting the temporary directory.
		fmt.Fprintf(buf, "  umount /tmp/.snap%s/,\n", parentPath)
		// Allow unmounting the destination directory as well as anything inside.
		// This lets us perform the undo plan in case the writable mimic fails.
		fmt.Fprintf(buf, "  umount %s{,/**},\n", parentPath)
		// Allow creating directories on demand.
		fmt.Fprintf(buf, "  %s/** rw,\n", parentPath)
		for p := parentPath; !isProbablyPresent(p); p = parent(p) {
			fmt.Fprintf(buf, "  %s/ rw,\n", p)
		}
		fmt.Fprintf(buf, "  /tmp/.snap%s/** rw,\n", parentPath)
		for p := filepath.Join("/tmp/.snap/", parentPath); !isProbablyPresent(p); p = parent(p) {
			fmt.Fprintf(buf, "  %s/ rw,\n", p)
		}
	}
}

// WritableProfile writes a profile for snap-update-ns for making given directory writable.
func WritableProfile(buf *bytes.Buffer, path string) {
	if path == "/" {
		return
	}
	if isProbablyWritable(path) {
		fmt.Fprintf(buf, "  # Writable directory %s\n", path)
		for p := path; !isProbablyPresent(p); p = parent(p) {
			fmt.Fprintf(buf, "  %s/ rw,\n", p)
		}
	} else {
		parentPath := parent(path)
		fmt.Fprintf(buf, "  # Writable mimic %s\n", parentPath)
		// Allow setting the read-only directory aside via a bind mount.
		fmt.Fprintf(buf, "  mount options=(rbind, rw) %s/ -> /tmp/.snap%s/,\n", parentPath, parentPath)
		// Allow mounting tmpfs over the read-only directory.
		fmt.Fprintf(buf, "  mount fstype=tmpfs options=(rw) tmpfs -> %s/,\n", parentPath)
		// Allow bind mounting things to reconstruct the now-writable parent directory.
		fmt.Fprintf(buf, "  mount options=(rbind, rw) /tmp/.snap%s/** -> %s/**,\n", parentPath, parentPath)
		fmt.Fprintf(buf, "  mount options=(bind, rw) /tmp/.snap%s/* -> %s/*,\n", parentPath, parentPath)
		// Allow unmounting the temporary directory.
		fmt.Fprintf(buf, "  umount /tmp/.snap%s/,\n", parentPath)
		// Allow unmounting the destination directory as well as anything inside.
		// This lets us perform the undo plan in case the writable mimic fails.
		fmt.Fprintf(buf, "  umount %s{,/**},\n", parentPath)
		// Allow creating directories on demand.
		fmt.Fprintf(buf, "  %s/** rw,\n", parentPath)
		for p := parentPath; !isProbablyPresent(p); p = parent(p) {
			fmt.Fprintf(buf, "  %s/ rw,\n", p)
		}
		fmt.Fprintf(buf, "  /tmp/.snap%s/** rw,\n", parentPath)
		for p := filepath.Join("/tmp/.snap/", parentPath); !isProbablyPresent(p); p = parent(p) {
			fmt.Fprintf(buf, "  %s/ rw,\n", p)
		}
	}
}

// parent returns the parent directory of a given path.
func parent(path string) string {
	result, _ := filepath.Split(path)
	result = filepath.Clean(result)
	return result
}

// Snippets returns a deep copy of all the added application snippets.
func (spec *Specification) Snippets() map[string][]string {
	return copySnippets(spec.snippets)
}

// SnippetForTag returns a combined snippet for given security tag with individual snippets
// joined with newline character. Empty string is returned for non-existing security tag.
func (spec *Specification) SnippetForTag(tag string) string {
	return strings.Join(spec.snippets[tag], "\n")
}

// SecurityTags returns a list of security tags which have a snippet.
func (spec *Specification) SecurityTags() []string {
	var tags []string
	for t := range spec.snippets {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// UpdateNS returns a deep copy of all the added snap-update-ns snippets.
func (spec *Specification) UpdateNS() []string {
	cp := make([]string, len(spec.updateNS))
	copy(cp, spec.updateNS)
	return cp
}

func snippetFromLayout(layout *snap.Layout) string {
	mountPoint := layout.Snap.ExpandSnapVariables(layout.Path)
	if layout.Bind != "" || layout.Type == "tmpfs" {
		return fmt.Sprintf("# Layout path: %s\n%s{,/**} mrwklix,", mountPoint, mountPoint)
	} else if layout.BindFile != "" {
		return fmt.Sprintf("# Layout path: %s\n%s mrwklix,", mountPoint, mountPoint)
	}
	return fmt.Sprintf("# Layout path: %s\n# (no extra permissions required for symlink)", mountPoint)
}

func copySnippets(m map[string][]string) map[string][]string {
	result := make(map[string][]string, len(m))
	for k, v := range m {
		result[k] = append([]string(nil), v...)
	}
	return result
}

// Implementation of methods required by interfaces.Specification

// AddConnectedPlug records apparmor-specific side-effects of having a connected plug.
func (spec *Specification) AddConnectedPlug(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		AppArmorConnectedPlug(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(plug.SecurityTags())
		defer restore()
		return iface.AppArmorConnectedPlug(spec, plug, slot)
	}
	return nil
}

// AddConnectedSlot records mount-specific side-effects of having a connected slot.
func (spec *Specification) AddConnectedSlot(iface interfaces.Interface, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error {
	type definer interface {
		AppArmorConnectedSlot(spec *Specification, plug *interfaces.ConnectedPlug, slot *interfaces.ConnectedSlot) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(slot.SecurityTags())
		defer restore()
		return iface.AppArmorConnectedSlot(spec, plug, slot)
	}
	return nil
}

// AddPermanentPlug records mount-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	type definer interface {
		AppArmorPermanentPlug(spec *Specification, plug *snap.PlugInfo) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(plug.SecurityTags())
		defer restore()
		return iface.AppArmorPermanentPlug(spec, plug)
	}
	return nil
}

// AddPermanentSlot records mount-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	type definer interface {
		AppArmorPermanentSlot(spec *Specification, slot *snap.SlotInfo) error
	}
	if iface, ok := iface.(definer); ok {
		restore := spec.setScope(slot.SecurityTags())
		defer restore()
		return iface.AppArmorPermanentSlot(spec, slot)
	}
	return nil
}
