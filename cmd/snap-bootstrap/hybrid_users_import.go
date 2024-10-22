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
	"github.com/snapcore/snapd/logger"
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

// importHybridUserData merges users and groups from the hybrid rootfs with the
// users and groups from the base snap. The merged login files are written into
// [dirs.SnapRunDir]/hybrid-users. As an attempt to only import users that have
// elevated privileges, only users from the sudo and admin groups are imported.
func importHybridUserData(hybridRoot, baseRoot string) error {
	outputDir := filepath.Join(dirs.SnapRunDir, "hybrid-users")
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return err
	}

	return mergeAndWriteLoginFiles(hybridRoot, baseRoot, []string{"sudo", "admin"}, outputDir)
}

func userFilter(targetGroups []string) func(user) bool {
	return func(u user) bool {
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
	}
}

func groupFilter(users map[string]user) func(group) bool {
	return func(g group) bool {
		// always import the root group
		if g.gid == 0 {
			return true
		}

		// in addition to importing the users that are in the given groups, we also
		// import the primary groups that those users are in. these will most likely
		// all be unique, but there is a chance that users share a primary group.
		for _, u := range users {
			if u.gid == g.gid {
				return true
			}
		}

		return false
	}
}

// mergeAndWriteLoginFiles considers the etc/passwd, etc/shadow, etc/group, and
// etc/gshadow files from the given importRoot and baseRoot directories, merges
// them together, and writes the merged files into the given outputDir.
//
// Only non-system (uid and gid < 1000) users that are members of any of the
// groups in the targetGroups slice are imported from the importRoot directory.
// All users and groups from the baseRoot are imported. As a special case, the
// root user is always imported from importRoot.
//
// If there are any conflicts between the users and groups in the importRoot and
// baseRoot directories, the importRoot users and groups are used.
func mergeAndWriteLoginFiles(importRoot, baseRoot string, targetGroups []string, outputDir string) error {
	users, err := parseUsers(importRoot, userFilter(targetGroups))
	if err != nil {
		return err
	}

	groups, err := parseGroups(importRoot, groupFilter(users))
	if err != nil {
		return err
	}

	// update the passwd and shadow files to contain the lines for the users
	// that we're importing
	err = mergeAndWriteUserFiles(baseRoot, outputDir, users)
	if err != nil {
		return err
	}

	// update the group and gshadow files to add our users to any existing
	// groups, and add any new primary groups that we're importing.
	if err := mergeAndWriteGroupFiles(baseRoot, outputDir, users, groups); err != nil {
		return err
	}

	return nil
}

// mergeAndWriteUserFiles takes the etc/passwd and etc/shadow files from the
// given root directory and merges them with the given users. The caller must
// ensure that the UIDs and GIDs of any of the given users do not conflict with
// any of the existing users in the source etc/passwd and etc/shadow files. The
// merged files are written into the given output directory.
func mergeAndWriteUserFiles(baseRoot string, outputDir string, importUsers map[string]user) error {
	// only import users that are either root or have a uid < 1000 and gid <
	// 1000. the base shouldn't contain anything that doesn't fit this criteria,
	// but it is best to be safe.
	sourceUsers, err := parseUsers(baseRoot, func(u user) bool {
		return u.uid < 1000 && u.gid < 1000
	})
	if err != nil {
		return err
	}

	var passwdBuffer bytes.Buffer
	var shadowBuffer bytes.Buffer
	for name, user := range sourceUsers {
		if name != user.name {
			return fmt.Errorf("internal error: user entry inconsistent with parsed data")
		}

		// if there is a conflict in the existing file, we will use the imported
		// entry instead of the old one. this is consistent with how we handle
		// groups, so any conflicting users will always be fully replaced.
		if _, ok := importUsers[name]; ok {
			continue
		}

		passwdBuffer.WriteString(user.passwdEntry + "\n")
		if user.shadowEntry != "" {
			shadowBuffer.WriteString(user.shadowEntry + "\n")
		}
	}

	for _, user := range importUsers {
		parts := strings.Split(user.passwdEntry, ":")

		// we're going to ignore the user's shell, since it could be anything,
		// we replace it with /bin/bash, since this should give a good default
		// experience and should always be available in the base.
		//
		// note that last field (the user's login shell) is optional, and the
		// trailing colon might not be present
		switch len(parts) {
		case 6:
			parts = append(parts, "/bin/bash")
		case 7:
			parts[6] = "/bin/bash"
		default:
			return fmt.Errorf("internal error: passwd entry inconsistent with parsed data")
		}

		passwdBuffer.WriteString(strings.Join(parts, ":") + "\n")
		if user.shadowEntry != "" {
			shadowBuffer.WriteString(user.shadowEntry + "\n")
		}
	}

	destinationPasswd := filepath.Join(outputDir, "passwd")
	if err := os.WriteFile(destinationPasswd, passwdBuffer.Bytes(), 0644); err != nil {
		return err
	}

	destinationShadow := filepath.Join(outputDir, "shadow")
	return os.WriteFile(destinationShadow, shadowBuffer.Bytes(), 0600)
}

// mergeAndWriteUserFiles takes the etc/group and etc/gshadow files from the
// given root directory and merges them with the given groups and users. The
// caller must ensure that UIDs and GIDs of any of the given groups and users do
// not conflict with any of the existing groups and users in the source
// etc/group and etc/gshadow files. The merged files are written into the given
// output directory.
func mergeAndWriteGroupFiles(baseRoot string, outputDir string, importUsers map[string]user, importGroups map[string]group) error {
	groupsToNewUsers := make(map[string][]string)
	for _, user := range importUsers {
		for _, group := range user.groups {
			groupsToNewUsers[group] = append(groupsToNewUsers[group], user.name)
		}
	}

	// we're not going to consider any of the source groups with a GID > 1000,
	// since those have a chance of conflicting with users that we're importing.
	// in practice, the base snap that we're importing from shouldn't have any
	// users/groups with a GID > 1000.
	sourceGroups, err := parseGroups(baseRoot, func(g group) bool {
		return g.gid < 1000
	})
	if err != nil {
		return err
	}

	var groupBuffer bytes.Buffer
	var gshadowBuffer bytes.Buffer
	for name, group := range sourceGroups {
		if name != group.name {
			return errors.New("internal error: group entry inconsistent with parsed data")
		}

		// if we're importing a group that already exists, we take the imported
		// group.
		if _, ok := importGroups[name]; ok {
			continue
		}

		// we combine the users that are already in the group with the new users
		// that we're importing
		usersInGroup := unique(append(group.users, groupsToNewUsers[name]...))
		sort.Strings(usersInGroup)

		usersPart := strings.Join(usersInGroup, ",")
		parts := strings.Split(group.groupEntry, ":")

		fmt.Fprintf(&groupBuffer, "%s:%s:%s:%s\n", parts[0], parts[1], parts[2], usersPart)

		// if this group has a gshadow entry, we need to update the list of
		// users there as well. not having a gshadow entry is fine, though.
		if group.gshadowEntry == "" {
			continue
		}

		gshadowParts := strings.Split(group.gshadowEntry, ":")
		if len(gshadowParts) != 4 {
			continue
		}

		fmt.Fprintf(&gshadowBuffer, "%s:%s:%s:%s\n", gshadowParts[0], gshadowParts[1], gshadowParts[2], usersPart)
	}

	// add the group entries for the users that we're importing. note that we
	// don't need to mess with the users in these groups, since they're coming
	// directly from the system we're importing from
	for _, g := range importGroups {
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
	return os.WriteFile(destinationGShadow, gshadowBuffer.Bytes(), 0600)
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

// parseUsers parses users from the given root directory, using the etc/passwd,
// etc/shadow, and etc/group files. The caller can provide a filter to omit some
// of the returned users by returning false from the given function.
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
		if len(parts) != 6 && len(parts) != 7 {
			logger.Noticef("skipping importing user with invalid entry: %v", entry)
			continue
		}

		if name != parts[0] {
			return nil, errors.New("internal error: passwd entry inconsistent with parsed data")
		}

		uid, err := strconv.Atoi(parts[2])
		if err != nil {
			logger.Noticef("skipping importing user %q with invalid uid: %v", name, err)
			continue
		}

		gid, err := strconv.Atoi(parts[3])
		if err != nil {
			logger.Noticef("skipping importing user %q with invalid gid: %v", name, err)
			continue
		}

		u := user{
			name:   name,
			uid:    uid,
			gid:    gid,
			groups: userToGroups[name],

			passwdEntry: entry,
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

// parseGroups parses groups from the given root directory, using the etc/group
// and etc/gshadow files. The caller can provide a filter to omit some of the
// returned groups by returning false from the given function.
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
			logger.Noticef("skipping importing group with invalid entry: %s", entry)
			continue
		}

		if name != parts[0] {
			return nil, errors.New("internal error: group entry inconsistent with parsed data")
		}

		gid, err := strconv.Atoi(parts[2])
		if err != nil {
			logger.Noticef("skipping importing group %q with invalid gid: %v", name, err)
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

// parseGroupsForUser reads the contents of a file in the style of /etc/group
// and returns a mapping of users in the file to the groups that each user is a
// member of.
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
		if parts[3] == "" {
			continue
		}

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

// entriesByName reads the contents of a file in the style of /etc/passwd and
// returns a mapping of the value of the first column of each line in the file
// to the entire line.
//
// Parsing "user:x:1000:1000::/home/user:/bin/bash" results in:
// "user" -> "user:x:1000:1000::/home/user:/bin/bash"
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
