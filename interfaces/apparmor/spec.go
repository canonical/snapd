// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2018 Canonical Ltd
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
	"github.com/snapcore/snapd/strutil"
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

	// AppArmor deny rules cannot be undone by allow rules which makes
	// deny rules difficult to work with arbitrary combinations of
	// interfaces. Sometimes it is useful to suppress noisy denials and
	// because that can currently only be done with explicit deny rules,
	// adding explicit deny rules unconditionally makes it difficult for
	// interfaces to be used in combination. Define the suppressPtraceTrace
	// to allow an interface to request suppression and define
	// usesPtraceTrace to omit the explicit deny rules such that:
	//   if suppressPtraceTrace && !usesPtraceTrace {
	//       add 'deny ptrace (trace),'
	//   }
	suppressPtraceTrace bool
	usesPtraceTrace     bool

	// The home interface typically should have 'ix' as part of its rules,
	// but specifying certain change_profile rules with these rules cases
	// a 'conflicting x modifiers' parser error. Allow interfaces that
	// require this type of change_profile rule to suppress 'ix' so that
	// the calling interface can be used with the home interface. Ideally,
	// we would not need this, but we currently do (LP: #1797786)
	suppressHomeIx bool
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

// AddLayout adds apparmor snippets based on the layout of the snap.
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
func (spec *Specification) AddLayout(si *snap.Info) {
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
		emit := func(f string, args ...interface{}) {
			fmt.Fprintf(&buf, f, args...)
		}
		l := si.Layout[path]
		emit("  # Layout %s\n", l.String())
		path := si.ExpandSnapVariables(l.Path)
		switch {
		case l.Bind != "":
			bind := si.ExpandSnapVariables(l.Bind)
			// Allow bind mounting the layout element.
			emit("  mount options=(rbind, rw) %s/ -> %s/,\n", bind, path)
			emit("  mount options=(rprivate) -> %s/,\n", path)
			emit("  umount %s/,\n", path)
			// Allow constructing writable mimic in both bind-mount source and mount point.
			GenWritableProfile(emit, path, 2) // At least / and /some-top-level-directory
			GenWritableProfile(emit, bind, 4) // At least /, /snap/, /snap/$SNAP_NAME and /snap/$SNAP_NAME/$SNAP_REVISION
		case l.BindFile != "":
			bindFile := si.ExpandSnapVariables(l.BindFile)
			// Allow bind mounting the layout element.
			emit("  mount options=(bind, rw) %s -> %s,\n", bindFile, path)
			emit("  mount options=(rprivate) -> %s,\n", path)
			emit("  umount %s,\n", path)
			// Allow constructing writable mimic in both bind-mount source and mount point.
			GenWritableFileProfile(emit, path, 2)     // At least / and /some-top-level-directory
			GenWritableFileProfile(emit, bindFile, 4) // At least /, /snap/, /snap/$SNAP_NAME and /snap/$SNAP_NAME/$SNAP_REVISION
		case l.Type == "tmpfs":
			emit("  mount fstype=tmpfs tmpfs -> %s/,\n", path)
			emit("  mount options=(rprivate) -> %s/,\n", path)
			emit("  umount %s/,\n", path)
			// Allow constructing writable mimic to mount point.
			GenWritableProfile(emit, path, 2) // At least / and /some-top-level-directory
		case l.Symlink != "":
			// Allow constructing writable mimic to symlink parent directory.
			emit("  %s rw,\n", path)
			GenWritableProfile(emit, path, 2) // At least / and /some-top-level-directory
		}
		spec.AddUpdateNS(buf.String())
	}
}

// AddOvername adds AppArmor snippets allowing remapping of snap
// directories for parallel installed snaps
//
// Specifically snap-update-ns will apply the following bind mounts
// - /snap/foo_bar -> /snap/foo
// - /var/snap/foo_bar -> /var/snap/foo
// - /home/joe/snap/foo_bar -> /home/joe/snap/foo
func (spec *Specification) AddOvername(si *snap.Info) {
	if si.InstanceKey == "" {
		return
	}
	var buf bytes.Buffer

	// /snap/foo_bar -> /snap/foo
	fmt.Fprintf(&buf, "  # Allow parallel instance snap mount namespace adjustments\n")
	fmt.Fprintf(&buf, "  mount options=(rw rbind) /snap/%s/ -> /snap/%s/,\n", si.InstanceName(), si.SnapName())
	// /var/snap/foo_bar -> /var/snap/foo
	fmt.Fprintf(&buf, "  mount options=(rw rbind) /var/snap/%s/ -> /var/snap/%s/,\n", si.InstanceName(), si.SnapName())
	spec.AddUpdateNS(buf.String())
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

// GenWritableMimicProfile generates apparmor rules for a writable mimic at the given path.
func GenWritableMimicProfile(emit func(f string, args ...interface{}), path string, assumedPrefixDepth int) {
	emit("  # Writable mimic %s\n", path)

	iter, err := strutil.NewPathIterator(path)
	if err != nil {
		panic(err)
	}

	// Handle the prefix that is assumed to exist first.
	emit("  # .. permissions for traversing the prefix that is assumed to exist\n")
	for iter.Next() {
		if iter.Depth() < assumedPrefixDepth {
			emit("  %s r,\n", iter.CurrentPath())
		}
	}

	// Rewind the iterator and handle the part that needs to be created.
	iter.Rewind()
	for iter.Next() {
		if iter.Depth() < assumedPrefixDepth {
			continue
		}
		// Assume that the mimic needs to be created at the given prefix of the
		// full mimic path. This is called a mimic "variant". Both of the paths
		// must end with a slash as this is important for apparmor file vs
		// directory path semantics.
		mimicPath := filepath.Join(iter.CurrentBase(), iter.CurrentCleanName()) + "/"
		mimicAuxPath := filepath.Join("/tmp/.snap", iter.CurrentPath()) + "/"
		emit("  # .. variant with mimic at %s\n", mimicPath)
		emit("  # Allow reading the mimic directory, it must exist in the first place.\n")
		emit("  %s r,\n", mimicPath)
		emit("  # Allow setting the read-only directory aside via a bind mount.\n")
		emit("  %s rw,\n", mimicAuxPath)
		emit("  mount options=(rbind, rw) %s -> %s,\n", mimicPath, mimicAuxPath)
		emit("  mount options=(rprivate) -> %s,\n", mimicAuxPath)
		emit("  # Allow mounting tmpfs over the read-only directory.\n")
		emit("  mount fstype=tmpfs options=(rw) tmpfs -> %s,\n", mimicPath)
		emit("  # Allow creating empty files and directories for bind mounting things\n" +
			"  # to reconstruct the now-writable parent directory.\n")
		emit("  %s*/ rw,\n", mimicAuxPath)
		emit("  %s*/ rw,\n", mimicPath)
		emit("  mount options=(rbind, rw) %s*/ -> %s*/,\n", mimicAuxPath, mimicPath)
		emit("  %s* rw,\n", mimicAuxPath)
		emit("  %s* rw,\n", mimicPath)
		emit("  mount options=(bind, rw) %s* -> %s*,\n", mimicAuxPath, mimicPath)
		emit("  # Allow unmounting the auxiliary directory.\n" +
			"  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)\n")
		emit("  mount options=(rprivate) -> %s,\n", mimicAuxPath)
		emit("  umount %s,\n", mimicAuxPath)
		emit("  # Allow unmounting the destination directory as well as anything\n" +
			"  # inside.  This lets us perform the undo plan in case the writable\n" +
			"  # mimic fails.\n")
		emit("  mount options=(rprivate) -> %s,\n", mimicPath)
		emit("  mount options=(rprivate) -> %s*,\n", mimicPath)
		emit("  mount options=(rprivate) -> %s*/,\n", mimicPath)
		emit("  umount %s,\n", mimicPath)
		emit("  umount %s*,\n", mimicPath)
		emit("  umount %s*/,\n", mimicPath)
	}
}

// GenWritableFileProfile writes a profile for snap-update-ns for making given file writable.
func GenWritableFileProfile(emit func(f string, args ...interface{}), path string, assumedPrefixDepth int) {
	if path == "/" {
		return
	}
	if isProbablyWritable(path) {
		emit("  # Writable file %s\n", path)
		emit("  %s rw,\n", path)
		for p := parent(path); !isProbablyPresent(p); p = parent(p) {
			emit("  %s/ rw,\n", p)
		}
	} else {
		parentPath := parent(path)
		GenWritableMimicProfile(emit, parentPath, assumedPrefixDepth)
	}
}

// GenWritableProfile generates a profile for snap-update-ns for making given directory writable.
func GenWritableProfile(emit func(f string, args ...interface{}), path string, assumedPrefixDepth int) {
	if path == "/" {
		return
	}
	if isProbablyWritable(path) {
		emit("  # Writable directory %s\n", path)
		for p := path; !isProbablyPresent(p); p = parent(p) {
			emit("  %s/ rw,\n", p)
		}
	} else {
		parentPath := parent(path)
		GenWritableMimicProfile(emit, parentPath, assumedPrefixDepth)
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

// SetUsesPtraceTrace records when to omit explicit ptrace deny rules
func (spec *Specification) SetUsesPtraceTrace() {
	spec.usesPtraceTrace = true
}

func (spec *Specification) UsesPtraceTrace() bool {
	return spec.usesPtraceTrace
}

// SetSuppressPtraceTrace to request explicit ptrace deny rules
func (spec *Specification) SetSuppressPtraceTrace() {
	spec.suppressPtraceTrace = true
}

func (spec *Specification) SuppressPtraceTrace() bool {
	return spec.suppressPtraceTrace
}

// SetSuppressHomeIx to request explicit ptrace deny rules
func (spec *Specification) SetSuppressHomeIx() {
	spec.suppressHomeIx = true
}

func (spec *Specification) SuppressHomeIx() bool {
	return spec.suppressHomeIx
}
