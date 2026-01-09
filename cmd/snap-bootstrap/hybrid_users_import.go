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
	shell  string

	// these are the original lines from their respective files
	passwdEntry string
	shadowEntry string
}

var (
	// groupsToImport is the list of groups that will have their users imported
	// from the hybrid system into the ephemeral recovery system.
	groupsToImport = []string{"sudo", "admin"}

	// defaultLoginShells is the list of shells that we will allow for logging
	// into the system if the /etc/shells file is not readable.
	defaultLoginShells = []string{"/bin/bash", "/bin/sh"}
)

// importHybridUserData merges users and groups from the hybrid rootfs with the
// users and groups from the base snap. The merged login files are written into
// [dirs.SnapRunDir]/hybrid-users. As an attempt to only import users that have
// elevated privileges, only users from the sudo and admin groups are imported.
func importHybridUserData(hybridRoot, baseRoot string) error {
	outputDir := filepath.Join(dirs.SnapRunDir, "hybrid-users")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	return mergeAndWriteLoginFiles(hybridRoot, baseRoot, groupsToImport, outputDir)
}

func userFilter(
	targetGroups []string,
	baseUsers map[string]user,
	baseGroups map[string]group,
	loginShells []string,
) func(user) bool {
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

		if _, ok := baseUsers[u.name]; ok {
			logger.Noticef("skipping importing user %q because it shares a name with a user in the base", u.name)
			return false
		}

		if checkForUIDConflict(u.uid, baseUsers) {
			logger.Noticef("skipping importing user %q with UID %d because it conflicts with a user in the base", u.name, u.uid)
			return false
		}

		if checkForGIDConflict(u.gid, baseGroups) {
			logger.Noticef("skipping importing user %q with GID %d because it conflicts with a group in the base", u.name, u.gid)
			return false
		}

		if u.shell != "" && !strutil.ListContains(loginShells, u.shell) {
			logger.Noticef("skipping importing user %q with shell %q because it is not in the list of valid login shells", u.name, u.shell)
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

func checkForGIDConflict(gid int, groups map[string]group) bool {
	for _, g := range groups {
		if g.gid == gid {
			return true
		}
	}
	return false
}

func checkForUIDConflict(uid int, users map[string]user) bool {
	for _, u := range users {
		if u.uid == uid {
			return true
		}
	}
	return false
}

func groupFilter(importUsers map[string]user, baseGroups map[string]group) func(group) bool {
	return func(g group) bool {
		// always import the root group
		if g.gid == 0 {
			return true
		}

		if _, ok := baseGroups[g.name]; ok {
			logger.Noticef("skipping importing group %q because it shares a name with a group in the base", g.name)
			return false
		}

		// in addition to importing the users that are in the given groups, we also
		// import the primary groups that those users are in. these will most likely
		// all be unique, but there is a chance that users share a primary group.
		for _, u := range importUsers {
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
	baseUsers, err := parseUsers(baseRoot, nil)
	if err != nil {
		return err
	}

	baseGroups, err := parseGroups(baseRoot, nil)
	if err != nil {
		return err
	}

	allowedLoginShells := parseShellsWithDefaults(importRoot, defaultLoginShells)

	importUsers, err := parseUsers(importRoot, userFilter(
		targetGroups,
		baseUsers,
		baseGroups,
		allowedLoginShells,
	))
	if err != nil {
		return err
	}

	importGroups, err := parseGroups(importRoot, groupFilter(importUsers, baseGroups))
	if err != nil {
		return err
	}

	// update the passwd and shadow files to contain the lines for the users
	// that we're importing
	err = mergeAndWriteUserFiles(baseUsers, importUsers, outputDir)
	if err != nil {
		return err
	}

	// update the group and gshadow files to add our users to any existing
	// groups, and add any new primary groups that we're importing.
	if err := mergeAndWriteGroupFiles(baseGroups, importUsers, importGroups, outputDir); err != nil {
		return err
	}

	return nil
}

// mergeAndWriteUserFiles merges the base users with the given users to be
// imported. The caller must ensure that the UIDs and GIDs of any of the given
// users do not conflict with any of the given base users. The merged files are
// written into the given output directory.
func mergeAndWriteUserFiles(baseUsers map[string]user, importUsers map[string]user, outputDir string) error {
	var passwdBuffer bytes.Buffer
	var shadowBuffer bytes.Buffer
	for name, user := range baseUsers {
		if name != user.name {
			return fmt.Errorf("internal error: user entry inconsistent with parsed data")
		}

		if _, ok := importUsers[name]; ok {
			// as a special case, we replace the root user in the base with the
			// root user from the hybrid rootfs. all other conflicting users are
			// disallowed.
			if name == "root" {
				continue
			}
			return fmt.Errorf("internal error: cannot import user %q that conflicts with a user in the base", name)
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
	if err := os.WriteFile(destinationPasswd, passwdBuffer.Bytes(), 0o644); err != nil {
		return err
	}

	destinationShadow := filepath.Join(outputDir, "shadow")
	return os.WriteFile(destinationShadow, shadowBuffer.Bytes(), 0o600)
}

// mergeAndWriteUserFiles merges the given base groups with the given users and
// groups that are to be imported. The caller must ensure that UIDs and GIDs of
// any of the given groups and users to import do not conflict with any of the
// existing groups and users in the base groups. The merged files are written
// into the given output directory.
func mergeAndWriteGroupFiles(baseGroups map[string]group, importUsers map[string]user, importGroups map[string]group, outputDir string) error {
	groupsToNewUsers := make(map[string][]string)
	for _, user := range importUsers {
		for _, group := range user.groups {
			groupsToNewUsers[group] = append(groupsToNewUsers[group], user.name)
		}
	}

	var groupBuffer bytes.Buffer
	var gshadowBuffer bytes.Buffer
	for name, group := range baseGroups {
		if name != group.name {
			return errors.New("internal error: group entry inconsistent with parsed data")
		}

		if _, ok := importGroups[name]; ok {
			// as a special case, we replace the root group in the base with the
			// root user from the hybrid rootfs. all other conflicting groups
			// are disallowed.
			if name == "root" {
				continue
			}
			return fmt.Errorf("internal error: cannot import group %q that conflicts with a group in the base", name)
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
	if err := os.WriteFile(destinationGroup, groupBuffer.Bytes(), 0o644); err != nil {
		return err
	}

	destinationGShadow := filepath.Join(outputDir, "gshadow")
	return os.WriteFile(destinationGShadow, gshadowBuffer.Bytes(), 0o600)
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
	if filter == nil {
		filter = func(user) bool { return true }
	}

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

		var shell string
		if len(parts) == 7 {
			shell = parts[6]
		}

		u := user{
			name:   name,
			uid:    uid,
			gid:    gid,
			groups: userToGroups[name],
			shell:  shell,

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
	if filter == nil {
		filter = func(group) bool { return true }
	}

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

func parseShellsWithDefaults(root string, defaults []string) []string {
	shells, err := parseShells(root)
	if err != nil {
		logger.Noticef("cannot parse %q, using default login shells: %v", filepath.Join(root, "etc/shells"), err)
		return defaults
	}
	return shells
}

// parseShells parses a file in the format of /etc/shells and returns a list of
// the valid login shells.
func parseShells(root string) ([]string, error) {
	f, err := os.Open(filepath.Join(root, "etc/shells"))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)

	var shells []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		// split the line into the shell and the comment
		shell, _, _ := strings.Cut(line, "#")
		shell = strings.TrimSpace(shell)

		// if the line was entirely a comment, then shell will be empty
		if shell != "" {
			shells = append(shells, shell)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return shells, nil
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
