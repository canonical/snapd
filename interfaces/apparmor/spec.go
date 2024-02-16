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

// UnconfinedMode describes the states of support for the AppArmor unconfined
// profile mode - this is only enabled when the interface supports it as a
// static property and it is then enabled via SetUnconfinedEnabled() method
type UnconfinedMode int

const (
	UnconfinedIgnored UnconfinedMode = iota
	UnconfinedSupported
	UnconfinedEnabled
)

// Specification assists in collecting apparmor entries associated with an interface.
type Specification struct {
	// scope for various Add{...}Snippet functions
	securityTags []string

	// snippets are indexed by security tag and describe parts of apparmor policy
	// for snap application and hook processes. The security tag encodes the identity
	// of the application or hook.
	snippets map[string][]string

	// dedupSnippets are just like snippets but are added only once to the
	// resulting policy in an effort to avoid certain expensive to de-duplicate
	// rules by apparmor_parser.
	dedupSnippets map[string]*strutil.OrderedSet

	// parametricSnippets are like snippets but are further parametrized where
	// one template is instantiated with multiple values that end up producing
	// a single apparmor rule that is computationally cheaper than naive
	// repetition of the template alone. The first map index is the security
	// tag, the second map index is the template. The final map value is the
	// set of strings that the template is instantiated with across all the
	// interfaces.
	//
	// As a simple example, it can be used to craft rules like
	// "/sys/**/foo{1,2,3}/** r,", which do not triggering the exponential
	// cost of parsing "/sys/**/foo1/** r,", followed by two similar rules for
	// "2" and "3".
	parametricSnippets map[string]map[string]*strutil.OrderedSet

	// updateNS describe parts of apparmor policy for snap-update-ns executing
	// on behalf of a given snap.
	updateNS strutil.OrderedSet

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

	// Same as the above, but for the sys_module capability
	suppressSysModuleCapability bool
	usesSysModuleCapability     bool

	// The home interface typically should have 'ix' as part of its rules,
	// but specifying certain change_profile rules with these rules cases
	// a 'conflicting x modifiers' parser error. Allow interfaces that
	// require this type of change_profile rule to suppress 'ix' so that
	// the calling interface can be used with the home interface. Ideally,
	// we would not need this, but we currently do (LP: #1797786)
	suppressHomeIx bool

	// Same as the above, but for the pycache deny rule which breaks docker
	suppressPycacheDeny bool

	// Unconfined profile mode allows a profile to be applied without any
	// real confinement
	unconfined UnconfinedMode
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

// AddDeduplicatedSnippet adds a new apparmor snippet to all applications and hooks using the interface.
//
// Certain combinations of snippets may be computationally expensive for
// apparmor_parser in its de-duplication step. This function lets snapd
// perform de-duplication of identical rules at the potential cost of a
// somewhat more complex auditing process of the text of generated
// apparmor profile. Identical mount rules should typically use this, but
// this function can also be used to avoid repeated rules that inhibit
// auditability.
func (spec *Specification) AddDeduplicatedSnippet(snippet string) {
	if len(spec.securityTags) == 0 {
		return
	}
	if spec.dedupSnippets == nil {
		spec.dedupSnippets = make(map[string]*strutil.OrderedSet)
	}
	for _, tag := range spec.securityTags {
		bag := spec.dedupSnippets[tag]
		if bag == nil {
			bag = &strutil.OrderedSet{}
			spec.dedupSnippets[tag] = bag
		}
		bag.Put(snippet)
	}
}

// AddParametricSnippet adds a new apparmor snippet both de-duplicated and optimized for the parser.
//
// Conceptually the function takes a parametric template and a single value to
// remember. The resulting snippet text is a single entry resulting from the
// expanding the template and all the unique values observed, in the order they
// were observed.
//
// The template is expressed as a slice of strings, with the parameter
// automatically injected between any two of them, or in the special case of
// only one fragment, after that fragment.
//
// The resulting expansion depends on the number of values seen. If only one
// value is seen the resulting snippet is just the plain string one would
// expect if no parametric optimization had taken place. If more than one
// distinct value was seen then the resulting apparmor rule uses alternation
// syntax {param1,param2,...,paramN} which has better compilation time and
// memory complexity as compared to a set of naive expansions of the full
// snippet one after another.
//
// For example the code:
//
//	AddParametricSnippet([]string{"/dev/", "rw,"}, "sda1")
//	AddParametricSnippet([]string{"/dev/", "rw,"}, "sda3")
//	AddParametricSnippet([]string{"/dev/", "rw,"}, "sdb2")
//
// Results in a single apparmor rule:
//
//	"/dev/{sda1,sda3,sdb2} rw,"
//
// This function should be used whenever the apparmor template features more
// than one use of "**" syntax (which represent arbitrary many directories or
// files) and a variable component, like a device name or similar. Repeated
// instances of this pattern slow down the apparmor parser in the default
// "expr-simplify" mode (see PR#12943 for measurements).
func (spec *Specification) AddParametricSnippet(templateFragment []string, value string) {
	if len(spec.securityTags) == 0 {
		return
	}

	// We need to build a template string from the templateFragment.
	//
	// If only a single fragment is given we just  append our "###PARM###":
	//  []string{"prefix"} becomes -> "prefix###PARAM###"
	//
	// Otherwise we join the strings:
	//  []string{"pre","post"} becomes -> "pre###PARAM###post"
	//
	// This seems to be the most natural way of doing this.
	var template string
	switch len(templateFragment) {
	case 0:
		return
	case 1:
		template = templateFragment[0] + "###PARAM###"
	default:
		template = strings.Join(templateFragment, "###PARAM###")
	}

	// Expand the spec's parametric snippets, initializing each
	// part of the map as needed
	if spec.parametricSnippets == nil {
		spec.parametricSnippets = make(map[string]map[string]*strutil.OrderedSet)
	}
	for _, tag := range spec.securityTags {
		expansions := spec.parametricSnippets[tag]
		if expansions == nil {
			expansions = make(map[string]*strutil.OrderedSet)
			spec.parametricSnippets[tag] = expansions
		}
		values := expansions[template]
		if values == nil {
			values = &strutil.OrderedSet{}
			expansions[template] = values
		}
		// Now that everything is initialized, insert value into the
		// spec.parametricSnippets[<tag>][<template>]'s OrderedSet.
		values.Put(value)
	}
}

// AddUpdateNS adds a new apparmor snippet for the snap-update-ns program.
func (spec *Specification) AddUpdateNS(snippet string) {
	spec.updateNS.Put(snippet)
}

// AddUpdateNSf formats and adds a new apparmor snippet for the snap-update-ns program.
func (spec *Specification) AddUpdateNSf(f string, args ...interface{}) {
	spec.AddUpdateNS(fmt.Sprintf(f, args...))
}

// UpdateNSIndexOf returns the index of a previously added snippet.
func (spec *Specification) UpdateNSIndexOf(snippet string) (idx int, ok bool) {
	return spec.updateNS.IndexOf(snippet)
}

func (spec *Specification) emitLayout(si *snap.Info, layout *snap.Layout) {
	emit := spec.AddUpdateNSf

	emit("  # Layout %s\n", layout.String())
	path := si.ExpandSnapVariables(layout.Path)
	switch {
	case layout.Bind != "":
		bind := si.ExpandSnapVariables(layout.Bind)
		// Allow bind mounting the layout element.
		emit("  mount options=(rbind, rw) \"%s/\" -> \"%s/\",\n", bind, path)
		emit("  mount options=(rprivate) -> \"%s/\",\n", path)
		emit("  umount \"%s/\",\n", path)
		// Allow constructing writable mimic in both bind-mount source and mount point.
		GenWritableProfile(emit, path, 2) // At least / and /some-top-level-directory
		GenWritableProfile(emit, bind, 4) // At least /, /snap/, /snap/$SNAP_NAME and /snap/$SNAP_NAME/$SNAP_REVISION
	case layout.BindFile != "":
		bindFile := si.ExpandSnapVariables(layout.BindFile)
		// Allow bind mounting the layout element.
		emit("  mount options=(bind, rw) \"%s\" -> \"%s\",\n", bindFile, path)
		emit("  mount options=(rprivate) -> \"%s\",\n", path)
		emit("  umount \"%s\",\n", path)
		// Allow constructing writable mimic in both bind-mount source and mount point.
		GenWritableFileProfile(emit, path, 2)     // At least / and /some-top-level-directory
		GenWritableFileProfile(emit, bindFile, 4) // At least /, /snap/, /snap/$SNAP_NAME and /snap/$SNAP_NAME/$SNAP_REVISION
	case layout.Type == "tmpfs":
		emit("  mount fstype=tmpfs tmpfs -> \"%s/\",\n", path)
		emit("  mount options=(rprivate) -> \"%s/\",\n", path)
		emit("  umount \"%s/\",\n", path)
		// Allow constructing writable mimic to mount point.
		GenWritableProfile(emit, path, 2) // At least / and /some-top-level-directory
	case layout.Symlink != "":
		// Allow constructing writable mimic to symlink parent directory.
		emit("  \"%s\" rw,\n", path)
		GenWritableProfile(emit, path, 2) // At least / and /some-top-level-directory
	}
}

// AddLayout adds apparmor snippets based on the layout of the snap.
//
// The per-snap snap-update-ns profiles are composed via a template and
// snippets for the snap. The snippets may allow (depending on the snippet):
//   - mount profiles via the content interface
//   - creating missing mount point directories under $SNAP* (the 'tree'
//     of permissions is needed for SecureMkDirAll that uses
//     open(..., O_NOFOLLOW) and mkdirat() using the resulting file descriptor)
//   - creating a placeholder directory in /tmp/.snap/ in the per-snap mount
//     namespace to support writable mimic which uses tmpfs and bind mount to
//     poke holes in arbitrary read-only locations
//   - mounting/unmounting any part of $SNAP into placeholder directory
//   - mounting/unmounting tmpfs over the original $SNAP/** location
//   - mounting/unmounting from placeholder back to $SNAP/** (for reconstructing
//     the data)
//
// Importantly, the above mount operations are happening within the per-snap
// mount namespace.
func (spec *Specification) AddLayout(snapInfo *snap.Info) {
	if len(snapInfo.Layout) == 0 {
		return
	}

	// Walk the layout elements in deterministic order, by mount point name.
	paths := make([]string, 0, len(snapInfo.Layout))
	for path := range snapInfo.Layout {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// Get tags describing all apps and hooks.
	tags := make([]string, 0, len(snapInfo.Apps)+len(snapInfo.Hooks))
	for _, app := range snapInfo.Apps {
		tags = append(tags, app.SecurityTag())
	}
	for _, hook := range snapInfo.Hooks {
		tags = append(tags, hook.SecurityTag())
	}

	// Append layout snippets to all tags; the layout applies equally to the
	// entire snap as the entire snap uses one mount namespace.
	if spec.snippets == nil {
		spec.snippets = make(map[string][]string)
	}
	for _, tag := range tags {
		for _, path := range paths {
			snippet := snippetFromLayout(snapInfo.Layout[path])
			spec.snippets[tag] = append(spec.snippets[tag], snippet)
		}
		sort.Strings(spec.snippets[tag])
	}

	// Append update-ns snippets that allow constructing the layout.
	for _, path := range paths {
		layout := snapInfo.Layout[path]
		spec.emitLayout(snapInfo, layout)
	}
}

// AddExtraLayouts adds additional apparmor snippets based on the provided layouts.
// The function is in part identical to AddLayout, except that it considers only the
// layouts passed as parameters instead of those declared in the snap.Info structure.
// XXX: Should we just combine this into AddLayout instead of this separate
// function?
func (spec *Specification) AddExtraLayouts(si *snap.Info, layouts []snap.Layout) {
	for _, layout := range layouts {
		spec.emitLayout(si, &layout)
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
			emit("  \"%s\" r,\n", iter.CurrentPath())
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
		mimicPath := filepath.Join(iter.CurrentBaseNoSlash(), iter.CurrentNameNoSlash()) + "/"
		mimicAuxPath := filepath.Join("/tmp/.snap", iter.CurrentPath()) + "/"
		emit("  # .. variant with mimic at %s\n", mimicPath)
		emit("  # Allow reading the mimic directory, it must exist in the first place.\n")
		emit("  \"%s\" r,\n", mimicPath)
		emit("  # Allow setting the read-only directory aside via a bind mount.\n")
		emit("  \"%s\" rw,\n", mimicAuxPath)
		emit("  mount options=(rbind, rw) \"%s\" -> \"%s\",\n", mimicPath, mimicAuxPath)
		emit("  # Allow mounting tmpfs over the read-only directory.\n")
		emit("  mount fstype=tmpfs options=(rw) tmpfs -> \"%s\",\n", mimicPath)
		emit("  # Allow creating empty files and directories for bind mounting things\n" +
			"  # to reconstruct the now-writable parent directory.\n")
		emit("  \"%s*/\" rw,\n", mimicAuxPath)
		emit("  \"%s*/\" rw,\n", mimicPath)
		emit("  mount options=(rbind, rw) \"%s*/\" -> \"%s*/\",\n", mimicAuxPath, mimicPath)
		emit("  \"%s*\" rw,\n", mimicAuxPath)
		emit("  \"%s*\" rw,\n", mimicPath)
		emit("  mount options=(bind, rw) \"%s*\" -> \"%s*\",\n", mimicAuxPath, mimicPath)
		emit("  # Allow unmounting the auxiliary directory.\n" +
			"  # TODO: use fstype=tmpfs here for more strictness (LP: #1613403)\n")
		emit("  mount options=(rprivate) -> \"%s\",\n", mimicAuxPath)
		emit("  umount \"%s\",\n", mimicAuxPath)
		emit("  # Allow unmounting the destination directory as well as anything\n" +
			"  # inside.  This lets us perform the undo plan in case the writable\n" +
			"  # mimic fails.\n")
		emit("  mount options=(rprivate) -> \"%s\",\n", mimicPath)
		emit("  mount options=(rprivate) -> \"%s*\",\n", mimicPath)
		emit("  mount options=(rprivate) -> \"%s*/\",\n", mimicPath)
		emit("  umount \"%s\",\n", mimicPath)
		emit("  umount \"%s*\",\n", mimicPath)
		emit("  umount \"%s*/\",\n", mimicPath)
	}
}

// GenWritableFileProfile writes a profile for snap-update-ns for making given file writable.
func GenWritableFileProfile(emit func(f string, args ...interface{}), path string, assumedPrefixDepth int) {
	if path == "/" {
		return
	}
	if isProbablyWritable(path) {
		emit("  # Writable file %s\n", path)
		emit("  \"%s\" rw,\n", path)
		for p := parent(path); !isProbablyPresent(p); p = parent(p) {
			emit("  \"%s/\" rw,\n", p)
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
			emit("  \"%s/\" rw,\n", p)
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
	tags := spec.SecurityTags()
	snippets := make(map[string][]string, len(tags))
	for _, tag := range tags {
		snippets[tag] = spec.snippetsForTag(tag)
	}
	return snippets
}

// SnippetForTag returns a combined snippet for given security tag with
// individual snippets joined with the newline character. Empty string is
// returned for non-existing security tag.
func (spec *Specification) SnippetForTag(tag string) string {
	return strings.Join(spec.snippetsForTag(tag), "\n")
}

// SecurityTags returns a list of security tags which have a snippet.
func (spec *Specification) SecurityTags() []string {
	var tags []string
	seen := make(map[string]bool, len(spec.snippets))
	for t := range spec.snippets {
		tags = append(tags, t)
		seen[t] = true
	}
	for t := range spec.dedupSnippets {
		if !seen[t] {
			tags = append(tags, t)
		}
	}
	for t := range spec.parametricSnippets {
		if !seen[t] {
			tags = append(tags, t)
		}
	}
	sort.Strings(tags)
	return tags
}

func (spec *Specification) snippetsForTag(tag string) []string {
	snippets := append([]string(nil), spec.snippets[tag]...)
	// First add any deduplicated snippets
	if bag := spec.dedupSnippets[tag]; bag != nil {
		snippets = append(snippets, bag.Items()...)
	}
	templates := make([]string, 0, len(spec.parametricSnippets[tag]))
	// Then add any parametric snippets
	for template := range spec.parametricSnippets[tag] {
		templates = append(templates, template)
	}
	sort.Strings(templates)
	for _, template := range templates {
		bag := spec.parametricSnippets[tag][template]
		if bag != nil {
			values := bag.Items()
			switch len(values) {
			case 0:
				/* no values, nothing to do */
			case 1:
				snippet := strings.Replace(template, "###PARAM###", values[0], -1)
				snippets = append(snippets, snippet)
			default:
				snippet := strings.Replace(template, "###PARAM###",
					fmt.Sprintf("{%s}", strings.Join(values, ",")), -1)
				snippets = append(snippets, snippet)
			}
		}
	}
	return snippets
}

// UpdateNS returns a deep copy of all the added snap-update-ns snippets.
func (spec *Specification) UpdateNS() []string {
	return spec.updateNS.Items()
}

func snippetFromLayout(layout *snap.Layout) string {
	mountPoint := layout.Snap.ExpandSnapVariables(layout.Path)
	if layout.Bind != "" || layout.Type == "tmpfs" {
		return fmt.Sprintf("# Layout path: %s\n\"%s{,/**}\" mrwklix,", mountPoint, mountPoint)
	} else if layout.BindFile != "" {
		return fmt.Sprintf("# Layout path: %s\n\"%s\" mrwklix,", mountPoint, mountPoint)
	}
	return fmt.Sprintf("# Layout path: %s\n# (no extra permissions required for symlink)", mountPoint)
}

// emitEnsureDir creates an apparmor snippet that permits snap-update-ns to create
// missing directories for the calling user according to the provided ensure directory spec.
// This function is currently used as counterpart for AddUserEnsureDirs, but can also be used
// for permitting non-user ensure directory specs.
func emitEnsureDir(spec *Specification, ifaceName string, ensureDirSpec *interfaces.EnsureDirSpec) {
	ensureDir := ensureDirSpec.EnsureDir
	mustExistDir := ensureDirSpec.MustExistDir
	if ensureDir == mustExistDir {
		return
	}

	// Add additional expansion here as required
	replacePrefixHome := func(path string) string {
		if strings.HasPrefix(path, "$HOME") {
			return strings.Replace(path, "$HOME", "@{HOME}", -1)
		}
		return path
	}
	appArmorDir := func(path string) string {
		if path != "/" {
			path = path + "/"
		}
		return path
	}
	emit := spec.AddUpdateNSf

	// Create entry for MustExistDir
	iter, err := strutil.NewPathIterator(ensureDir)
	if err != nil {
		return
	}
	for iter.Next() {
		if iter.CurrentPathNoSlash() == mustExistDir {
			emit("  # Allow the %s interface to create potentially missing directories", ifaceName)
			emit("  owner %s rw,", appArmorDir(replacePrefixHome(mustExistDir)))
			break
		}
	}

	// Create entries for the remaining directories after MustExistDir up to and including EnsureDir
	for iter.Next() {
		emit("  owner %s/ rw,", replacePrefixHome(iter.CurrentPathNoSlash()))
	}
}

// AddEnsureDirMounts adds snap-update-ns snippets that permit snap-update-ns to create
// missing directories according to the provided ensure directory mount specs.
func (spec *Specification) AddEnsureDirMounts(ifaceName string, ensureDirSpecs []*interfaces.EnsureDirSpec) {
	// Walk the path specs in deterministic order, by EnsureDir (the mount point).
	sort.Slice(ensureDirSpecs, func(i, j int) bool {
		return ensureDirSpecs[i].EnsureDir < ensureDirSpecs[j].EnsureDir
	})

	for _, ensureDirSpec := range ensureDirSpecs {
		emitEnsureDir(spec, ifaceName, ensureDirSpec)
	}
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

// AddConnectedSlot records apparmor-specific side-effects of having a connected slot.
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

// AddPermanentPlug records apparmor-specific side-effects of having a plug.
func (spec *Specification) AddPermanentPlug(iface interfaces.Interface, plug *snap.PlugInfo) error {
	si := interfaces.StaticInfoOf(iface)
	if si.AppArmorUnconfinedPlugs {
		spec.setUnconfinedSupported()
	}
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

// AddPermanentSlot records apparmor-specific side-effects of having a slot.
func (spec *Specification) AddPermanentSlot(iface interfaces.Interface, slot *snap.SlotInfo) error {
	si := interfaces.StaticInfoOf(iface)
	if si.AppArmorUnconfinedSlots {
		spec.setUnconfinedSupported()
	}
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

// SetUsesPtraceTrace records when to omit explicit ptrace deny rules.
func (spec *Specification) SetUsesPtraceTrace() {
	spec.usesPtraceTrace = true
}

// UsesPtraceTrace returns whether ptrace is being used by any of the interfaces
// in the spec.
func (spec *Specification) UsesPtraceTrace() bool {
	return spec.usesPtraceTrace
}

// SetSuppressPtraceTrace to request explicit ptrace deny rules
func (spec *Specification) SetSuppressPtraceTrace() {
	spec.suppressPtraceTrace = true
}

// SuppressPtraceTrace returns whether ptrace should be suppressed as dictated
// by any of the interfaces in the spec.
func (spec *Specification) SuppressPtraceTrace() bool {
	return spec.suppressPtraceTrace
}

// SetUsesSysModuleCapability records that some interface has granted the
// sys_module capability
func (spec *Specification) SetUsesSysModuleCapability() {
	spec.usesSysModuleCapability = true
}

// UsesSysModuleCapability returns whether the sys_module capability is being
// used by any of the interfaces in the spec.
func (spec *Specification) UsesSysModuleCapability() bool {
	return spec.usesSysModuleCapability
}

// SetSuppressSysModuleCapability to request explicit denial of the sys_module
// capability
func (spec *Specification) SetSuppressSysModuleCapability() {
	spec.suppressSysModuleCapability = true
}

// SuppressSysModuleCapability returns whether any interface has asked the
// sys_module capability to be explicitly denied
func (spec *Specification) SuppressSysModuleCapability() bool {
	return spec.suppressSysModuleCapability
}

// SetSuppressHomeIx records suppression of the ix rules for the home
// interface.
func (spec *Specification) SetSuppressHomeIx() {
	spec.suppressHomeIx = true
}

// SuppressHomeIx returns whether the ix rules of the home interface should be
// suppressed.
func (spec *Specification) SuppressHomeIx() bool {
	return spec.suppressHomeIx
}

// SetSuppressPycacheDeny records suppression of the ix rules for the home
// interface.
func (spec *Specification) SetSuppressPycacheDeny() {
	spec.suppressPycacheDeny = true
}

// SuppressPycacheDeny returns whether the ix rules of the home interface should be
// suppressed.
func (spec *Specification) SuppressPycacheDeny() bool {
	return spec.suppressPycacheDeny
}

// setUnconfinedSuported records whether a profile perhaps should be applied
// without any real confinement - this will only occur if the spec also enables
// this by calling SetEnableUnconfined()
func (spec *Specification) setUnconfinedSupported() {
	spec.unconfined = UnconfinedSupported
}

// SetUnconfinedEnabled records whether a profile should be applied without any
// real confinement - the spec must already support unconfined profiles via a
// previous call to setUnconfinedSupported()
func (spec *Specification) SetUnconfinedEnabled() error {
	if spec.unconfined != UnconfinedSupported {
		return fmt.Errorf("unconfined profiles not supported")
	}
	spec.unconfined = UnconfinedEnabled
	return nil
}

// Unconfined returns whether a profile should be applied without any real
// confinement
func (spec *Specification) Unconfined() UnconfinedMode {
	return spec.unconfined
}
