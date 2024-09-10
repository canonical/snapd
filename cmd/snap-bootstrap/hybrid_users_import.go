package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil"
)

type user struct {
	name   string
	uid    int
	gid    int
	groups []string

	// these are the original lines from their respective files
	passwdEntry string
	shadowEntry string
}

func importHybridUserData(rootToImport, referenceRoot string) error {
	outputDir := filepath.Join(dirs.SnapRunDir, "hybrid-users")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	if err := mergeAndWriteUserFiles(rootToImport, referenceRoot, []string{"sudo", "admin"}, outputDir); err != nil {
		return err
	}

	if !osutil.FileExists("/lib/core/extra-paths") {
		return nil
	}

	// TODO: this is just a hack for now so that i don't have to repack this
	// script for testing. will be removed once kernel snap is updated to
	// include changes to extra-paths.
	script, err := os.OpenFile("/lib/core/extra-paths", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer script.Close()

	mounts := []string{"passwd", "shadow", "group", "gshadow"}
	for _, m := range mounts {
		_, err := fmt.Fprintf(
			script,
			"echo '%s %s none bind,x-initrd.mount 0 0' >> /sysroot/etc/fstab\n",
			filepath.Join(outputDir, m),
			filepath.Join("/etc", m),
		)
		if err != nil {
			return err
		}
	}

	return script.Close()
}

func mergeAndWriteUserFiles(importRoot, referenceRoot string, targetGroups []string, outputDir string) error {
	users, err := parseUsers(importRoot, func(u user) bool {
		// we always want to import the root user
		if u.uid == 0 {
			return true
		}

		// don't consider any system users or users that have a system group as
		// their primary group
		if u.uid < 1000 || u.gid < 1000 {
			return false
		}

		// only consider users that are in the groups that we're specifically
		// importing
		for _, tg := range targetGroups {
			if strutil.ListContains(u.groups, tg) {
				return true
			}
		}

		return false
	})
	if err != nil {
		return err
	}

	// in addition to importing the users that are in the given groups, we also
	// import the primary groups that those users are in. these will most likely
	// all be unique, but there is a chance that users share a primary group.
	groups, err := parseGroups(importRoot, func(g group) bool {
		if g.gid == 0 {
			return true
		}

		if g.gid < 1000 {
			return false
		}

		for _, u := range users {
			if u.gid == g.gid {
				return true
			}
		}

		return false
	})
	if err != nil {
		return err
	}

	// update the passwd and shadow files to contain the lines for the users
	// that we're importing
	err = addNamedEntriesToFile(
		filepath.Join(referenceRoot, "etc/passwd"),
		filepath.Join(outputDir, "passwd"),
		users,
		func(u user) string {
			return u.passwdEntry
		},
	)
	if err != nil {
		return err
	}

	err = addNamedEntriesToFile(
		filepath.Join(referenceRoot, "etc/shadow"),
		filepath.Join(outputDir, "shadow"),
		users,
		func(u user) string {
			return u.shadowEntry
		},
	)
	if err != nil {
		return err
	}

	// update the group and gshadow files to add our users to any existing
	// groups, and add any new primary groups that we're importing.
	if err := mergeAndWriteGroupFiles(referenceRoot, outputDir, users, groups); err != nil {
		return err
	}

	return nil
}

func mergeAndWriteGroupFiles(originalRoot string, outputDir string, users map[string]user, groups map[string]group) error {
	groupsToNewUsers := make(map[string][]string)
	for _, user := range users {
		for _, group := range user.groups {
			groupsToNewUsers[group] = append(groupsToNewUsers[group], user.name)
		}
	}

	groupEntries, err := entriesByName(filepath.Join(originalRoot, "etc/group"))
	if err != nil {
		return err
	}

	gshadowEntries, err := entriesByName(filepath.Join(originalRoot, "etc/gshadow"))
	if err != nil {
		return err
	}

	var groupBuffer bytes.Buffer
	var gshadowBuffer bytes.Buffer
	for group, entry := range groupEntries {
		parts := strings.Split(entry, ":")
		if len(parts) != 4 {
			continue
		}

		if group != parts[0] {
			return errors.New("internal error: group entry inconsistent with parsed data")
		}

		// if we're importing a group that already exists, we take the imported
		// group.
		if _, ok := groups[group]; ok {
			continue
		}

		var usersInGroup []string
		if len(parts[3]) > 0 {
			usersInGroup = strings.Split(parts[3], ",")
		}

		// we combine the users that are already in the group with the new users
		// that we're importing
		usersInGroup = unique(append(usersInGroup, groupsToNewUsers[group]...))
		sort.Strings(usersInGroup)

		usersPart := strings.Join(usersInGroup, ",")

		fmt.Fprintf(&groupBuffer, "%s:%s:%s:%s\n", parts[0], parts[1], parts[2], usersPart)

		// if this group has a gshadow entry, we need to update the list of
		// users there as well. not having a gshadow entry is fine, though.
		gshadowLine := gshadowEntries[group]
		if gshadowLine == "" {
			continue
		}

		gshadowParts := strings.Split(gshadowLine, ":")
		if len(gshadowParts) != 4 {
			continue
		}

		fmt.Fprintf(&gshadowBuffer, "%s:%s:%s:%s\n", gshadowParts[0], gshadowParts[1], gshadowParts[2], usersPart)
	}

	// add the group entries for the users that we're importing. note that we
	// don't need to mess with the users in these groups, since they're coming
	// directly from the system we're importing from.
	for _, g := range groups {
		groupBuffer.WriteString(g.groupEntry + "\n")
		if g.gshadowEntry != "" {
			gshadowBuffer.WriteString(g.gshadowEntry + "\n")
		}
	}

	destinationGroup := filepath.Join(outputDir, "group")
	if err := os.WriteFile(destinationGroup, groupBuffer.Bytes(), 0644); err != nil {
		return err
	}

	destinationGShadow := filepath.Join(outputDir, "gshadow")
	return os.WriteFile(destinationGShadow, gshadowBuffer.Bytes(), 0644)
}

// unique returns a slice with unique entries; the provided slice is modified
// and should not be re-used.
func unique(slice []string) []string {
	seen := make(map[string]bool)
	current := 0
	for _, entry := range slice {
		if _, ok := seen[entry]; ok {
			continue
		}

		seen[entry] = true
		slice[current] = entry
		current++
	}
	return slice[:current]
}

// addNamedEntriesToFile creates a new passwd or shadow file from the
// combination of the original file and the given set of users. The given entry
// function returns the line that should be added to the file. If an entry in
// the original file shares the same login name as a given user's login name,
// then the new entry is used.
func addNamedEntriesToFile(original, destination string, users map[string]user, entry func(user) string) error {
	f, err := os.Open(original)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	var buffer bytes.Buffer
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		name, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		// if there is a conflict in the existing file, we will use the new
		// entry instead of the old one. this is consistent with how we handle
		// groups, so any conflicting users will always be fully replaced.
		if _, ok := users[name]; ok {
			continue
		}

		buffer.WriteString(line + "\n")
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	for _, user := range users {
		line := entry(user)
		if line == "" {
			continue
		}

		buffer.WriteString(line + "\n")
	}

	return os.WriteFile(destination, buffer.Bytes(), 0644)
}

func parseUsers(root string, filter func(user) bool) (map[string]user, error) {
	passwdEntries, err := entriesByName(filepath.Join(root, "etc/passwd"))
	if err != nil {
		return nil, err
	}

	shadowEntries, err := entriesByName(filepath.Join(root, "etc/shadow"))
	if err != nil {
		return nil, err
	}

	userToGroups, err := parseGroupsForUser(filepath.Join(root, "etc/group"))
	if err != nil {
		return nil, err
	}

	users := make(map[string]user)
	for name, entry := range passwdEntries {
		parts := strings.Split(entry, ":")
		if len(parts) != 7 {
			continue
		}

		if name != parts[0] {
			return nil, errors.New("internal error: passwd entry inconsistent with parsed data")
		}

		uid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		gid, err := strconv.Atoi(parts[3])
		if err != nil {
			continue
		}

		// we're going to ignore the user's shell, since it could be anything,
		// and that shell might not be installed in the system that we're
		// importing this user into. if empty, /bin/sh will be used.
		parts[6] = ""

		u := user{
			name:   name,
			uid:    uid,
			gid:    gid,
			groups: userToGroups[name],

			passwdEntry: strings.Join(parts, ":"),
			shadowEntry: shadowEntries[name],
		}

		if !filter(u) {
			continue
		}

		users[name] = u
	}

	return users, nil
}

type group struct {
	name  string
	gid   int
	users []string

	groupEntry   string
	gshadowEntry string
}

func parseGroups(root string, filter func(group) bool) (map[string]group, error) {
	groupEntries, err := entriesByName(filepath.Join(root, "etc/group"))
	if err != nil {
		return nil, err
	}

	gshadowEntries, err := entriesByName(filepath.Join(root, "etc/gshadow"))
	if err != nil {
		return nil, err
	}

	groups := make(map[string]group)
	for name, entry := range groupEntries {
		parts := strings.Split(entry, ":")
		if len(parts) != 4 {
			continue
		}

		if name != parts[0] {
			return nil, errors.New("internal error: group entry inconsistent with parsed data")
		}

		gid, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		var users []string
		if len(parts[3]) > 0 {
			users = strings.Split(parts[3], ",")
		}

		g := group{
			name:         name,
			gid:          gid,
			users:        users,
			groupEntry:   entry,
			gshadowEntry: gshadowEntries[name],
		}

		if !filter(g) {
			continue
		}

		groups[name] = g
	}

	return groups, nil
}

func parseGroupsForUser(path string) (usersToGroups map[string][]string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	usersToGroups = make(map[string][]string)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		parts := strings.Split(line, ":")
		if len(parts) != 4 {
			continue
		}

		group := parts[0]
		users := strings.Split(parts[3], ",")
		for _, user := range users {
			usersToGroups[user] = append(usersToGroups[user], group)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return usersToGroups, nil
}

func entriesByName(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	entries := make(map[string]string)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		name, _, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}

		entries[name] = line
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}
