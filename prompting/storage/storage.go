package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/prompting/apparmor"
	"github.com/snapcore/snapd/prompting/notifier"
)

var ErrNoPermissions = errors.New("request has no permissions set")
var ErrNoSavedDecision = errors.New("no saved prompt decision")
var ErrMultipleDecisions = errors.New("multiple prompt decisions for the same path")
var ErrUnknownMap = errors.New("map name does not match a known allow map")

const (
	// must match the json annotations for entries in labelDB
	jsonAllow            = "allow"
	jsonAllowWithDir     = "allow-with-dir"
	jsonAllowWithSubdirs = "allow-with-subdir"
	// must match the specification for extra information map returned by the prompt
	extrasAlwaysPrompt     = "always-prompt"
	extrasAllowWithDir     = "allow-directory"
	extrasAllowWithSubdirs = "allow-subdirectories"
	extrasAllowExtraPerms  = "allow-extra-permissions"
	extrasDenyWithDir      = "deny-directory"
	extrasDenyWithSubdirs  = "deny-subdirectories"
	extrasDenyExtraPerms   = "deny-extra-permissions"
)

type permissionDB struct {
	Allow            map[string]bool `json:"allow"`
	AllowWithDir     map[string]bool `json:"allow-with-dir"`
	AllowWithSubdirs map[string]bool `json:"allow-with-subdir"`
	// XXX: Always check with the following priority: Allow, then AllowWithDir, then AllowWithSubdirs
}

type labelDB struct {
	PerPermissionDB map[string]*permissionDB `json:"per-permission-db"`
}

type userDB struct {
	PerLabel map[string]*labelDB `json:"per-label"`
}

// TODO: make this an interface
type PromptsDB struct {
	PerUser map[uint32]*userDB `json:"per-user"`
}

// TODO: take a dir as argument to store prompt decisions
func New() *PromptsDB {
	pd := &PromptsDB{PerUser: make(map[uint32]*userDB)}
	// TODO: error handling
	pd.load()
	return pd
}

func findPathInPermissionDB(db *permissionDB, path string) (bool, error, string, string) {
	// Returns:
	// bool: allow
	// error: (nil | ErrMultipleDecisions | ErrNoSavedDecision)
	// string: (jsonAllow | jsonAllowWithDir | jsonAllowWithSubdirs) -- json name of map which contained match
	// string: matching path current in the db
	path = filepath.Clean(path)
	storedAllow := true
	which := ""
	var err error
	// Check if original path has exact match in db.Allow
	if allow, exists := db.Allow[path]; exists {
		which = jsonAllow
		storedAllow = storedAllow && allow
	}
outside:
	for i := 0; i < 2; i++ {
		// Check if original path and parent of path has match in db.AllowWithDir
		// Thus, run twice
		if allow, exists := db.AllowWithDir[path]; exists {
			if which != "" {
				err = ErrMultipleDecisions
				which = which + "," + jsonAllowWithDir
			} else {
				which = jsonAllowWithDir
			}
			storedAllow = storedAllow && allow
		}
		for {
			// Check if any ancestor of path has match in db.AllowWithSubdirs
			// Thus, loop until path is "/" or "."
			if allow, exists := db.AllowWithSubdirs[path]; exists {
				if which != "" {
					err = ErrMultipleDecisions
					which = which + "," + jsonAllowWithSubdirs
				} else {
					which = jsonAllowWithSubdirs
				}
				storedAllow = storedAllow && allow
			}
			if which != "" {
				return storedAllow, err, which, path
			}
			path = filepath.Dir(path)
			if path == "/" || path == "." {
				break outside
			}
			// Only run once during the first loop for AllowWithDir
			if i == 0 {
				break
			}
			// Otherwise, loop until path is "/" or "."
		}
	}
	return false, ErrNoSavedDecision, "", ""
}

// TODO: unexport, possibly reintegrate into MapsForUidAndLabelAndPermission
func (pd *PromptsDB) PermissionMapForUidAndLabel(uid uint32, label string) map[string]*permissionDB {
	userEntries := pd.PerUser[uid]
	if userEntries == nil {
		userEntries = &userDB{
			PerLabel: make(map[string]*labelDB),
		}
		pd.PerUser[uid] = userEntries
	}
	labelEntries := userEntries.PerLabel[label]
	if labelEntries == nil {
		labelEntries = &labelDB{
			PerPermissionDB: make(map[string]*permissionDB),
		}
		userEntries.PerLabel[label] = labelEntries
	}
	return labelEntries.PerPermissionDB
}

// TODO: unexport
func (pd *PromptsDB) MapsForUidAndLabelAndPermission(uid uint32, label string, permission string) *permissionDB {
	permissionMap := pd.PermissionMapForUidAndLabel(uid, label)
	permissionEntries := permissionMap[permission]
	if permissionEntries == nil {
		permissionEntries = &permissionDB{
			Allow:            make(map[string]bool),
			AllowWithDir:     make(map[string]bool),
			AllowWithSubdirs: make(map[string]bool),
		}
		permissionMap[permission] = permissionEntries
	}
	return permissionEntries
}

func (pd *PromptsDB) dbpath() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "prompt.json")
}

func (pd *PromptsDB) save() error {
	b, err := json.Marshal(pd.PerUser)
	if err != nil {
		return err
	}
	target := pd.dbpath()
	if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(target, b, 0600, 0)
}

func (pd *PromptsDB) load() error {
	target := pd.dbpath()
	f, err := os.Open(target)
	if err != nil {
		return err
	}
	defer f.Close()

	return json.NewDecoder(f).Decode(&pd.PerUser)
}

func whichMap(allow bool, extras map[string]string) string {
	if (allow && extras[extrasAllowWithSubdirs] == "yes") || (!allow && extras[extrasDenyWithSubdirs] == "yes") {
		return jsonAllowWithSubdirs
	}
	if (allow && extras[extrasAllowWithDir] == "yes") || (!allow && extras[extrasDenyWithDir] == "yes") {
		return jsonAllowWithDir
	}
	return jsonAllow
}

func parseRequestPermissions(req *notifier.Request) []string {
	return strings.Split(req.Permission.(apparmor.FilePermission).String(), "|")
}

func appendUnique(list []string, other []string) []string {
	combinedList := append(list, other...)
	uniqueList := make([]string, 0, len(combinedList))
	set := make(map[string]bool)
	for _, item := range combinedList {
		if _, exists := set[item]; !exists {
			set[item] = true
			uniqueList = append(uniqueList, item)
		}
	}
	return uniqueList
}

func WhichPermissions(req *notifier.Request, allow bool, extras map[string]string) []string {
	perms := parseRequestPermissions(req)
	if extraAllow := extras[extrasAllowExtraPerms]; allow && extraAllow != "" {
		perms = appendUnique(perms, strings.Split(extraAllow, ","))
	} else if extraDeny := extras[extrasDenyExtraPerms]; extraDeny != "" {
		perms = appendUnique(perms, strings.Split(extraDeny, ","))
	}
	return perms
}

// Checks whether the new rule corresponding to the decision map given by
// which, the given path, and the decision given by allow, is already implied
// by previous rules in the decision maps given by permissionEntries
func newDecisionImpliedByPreviousDecision(permissionEntries *permissionDB, which string, path string, allow bool) (bool, error) {
	alreadyAllowed, err, matchingMap, matchingPath := findPathInPermissionDB(permissionEntries, path)
	if err != nil && err != ErrNoSavedDecision {
		return false, err
	}

	// if path matches entry already in a different map (XXX means can't return early):
	// new Allow, old Allow -> replace if different
	// new Allow, old AllowWithDir, exact match -> replace if different (forces prompt for entries in directory of path)
	// new Allow, old AllowWithSubdirs, exact match -> same as ^^
	// new Allow, old AllowWithDir, parent match -> insert if different
	// new Allow, old AllowWithSubdirs, ancestor match -> same as ^^
	// new AllowWithDir, old Allow -> replace always XXX
	// new AllowWithDir, old AllowWithDir, exact match -> replace if different
	// new AllowWithDir, old AllowWithSubdirs, exact match -> same as ^^
	// new AllowWithDir, old AllowWithDir, parent match -> insert always XXX
	// new AllowWithDir, old AllowWithSubdirs, ancestor match -> insert if different
	// new AllowWithSubdirs, old Allow -> replace always XXX
	// new AllowWithSubdirs, old AllowWithDir, exact match -> replace always XXX
	// new AllowWithSubdirs, old AllowWithSubdirs, exact match -> replace if different
	// new AllowWithSubdirs, old AllowWithDir, parent match -> insert always XXX
	// new AllowWithSubdirs, old AllowWithSubdirs, ancestor match -> insert if different

	// in summary:
	// do nothing if decision matches and _not_ one of:
	//  1. new AllowWithDir, old Allow
	//  2. new AllowWithDir, old AllowWithDir, parent match
	//  3. new AllowWithSubdirs, old _not_ AllowWithSubdirs

	if (err == nil) && (alreadyAllowed == allow) {
		// already in db and decision matches
		if !((which == jsonAllowWithDir && (matchingMap == jsonAllow || (matchingMap == jsonAllowWithDir && matchingPath != path))) || (which == jsonAllowWithSubdirs && matchingMap != jsonAllowWithSubdirs)) {
			// don't need to do anything
			return true, nil
		}
	}
	return false, nil
}

// Returns a map of entries in allowMap which are children of the path, along
// with the corresponding stored decision
// TODO: unexport
func FindChildrenInMap(path string, allowMap map[string]bool) map[string]bool {
	matches := make(map[string]bool)
	for p, decision := range allowMap {
		if filepath.Dir(p) == path {
			matches[p] = decision
		}
	}
	return matches
}

// Returns a map of entries in allowMap which are descendants of the path, along
// with the corresponding stored decision
// TODO: unexport
func FindDescendantsInMap(path string, allowMap map[string]bool) map[string]bool {
	matches := make(map[string]bool)
	for pathEntry, decision := range allowMap {
		p := pathEntry
		for len(p) > len(path) {
			p = filepath.Dir(p)
		}
		if p == path {
			matches[pathEntry] = decision
		}
	}
	return matches
}

// Insert a new decision into the given permissionEntries and remove all
// previous decisions which are are more specific than the new decision.
// Returns a permissionDB with the rules which have been deleted or pruned
func insertAndPrune(permissionEntries *permissionDB, which string, path string, allow bool) (*permissionDB, error) {
	deleted := &permissionDB{
		Allow:            make(map[string]bool),
		AllowWithDir:     make(map[string]bool),
		AllowWithSubdirs: make(map[string]bool),
	}
	if decision, exists := permissionEntries.Allow[path]; exists {
		deleted.Allow[path] = decision
		delete(permissionEntries.Allow, path)
	}
	if decision, exists := permissionEntries.AllowWithDir[path]; exists {
		deleted.AllowWithDir[path] = decision
		delete(permissionEntries.AllowWithDir, path)
	}
	if decision, exists := permissionEntries.AllowWithSubdirs[path]; exists {
		deleted.AllowWithSubdirs[path] = decision
		delete(permissionEntries.AllowWithSubdirs, path)
	}

	// check if new decision is now implied by an existing one (since removing
	// exact matches), and only insert new decision if necessary
	skipNewDecision, err := newDecisionImpliedByPreviousDecision(permissionEntries, which, path, allow)
	if err != nil {
		return deleted, err
	}

	switch which {
	case jsonAllow:
		// only delete direct match from other maps -- done above
		if !skipNewDecision {
			permissionEntries.Allow[path] = allow
		}
	case jsonAllowWithDir:
		// delete direct match from other maps -- done above
		// delete direct children from Allow map
		toDeleteAllow := FindChildrenInMap(path, permissionEntries.Allow)
		for p, decision := range toDeleteAllow {
			delete(permissionEntries.Allow, p)
			deleted.Allow[p] = decision
		}
		if !skipNewDecision {
			permissionEntries.AllowWithDir[path] = allow
		}
	case jsonAllowWithSubdirs:
		// delete direct match from other maps -- done above
		// delete descendants from all other maps
		toDeleteAllow := FindDescendantsInMap(path, permissionEntries.Allow)
		for p, decision := range toDeleteAllow {
			delete(permissionEntries.Allow, p)
			deleted.Allow[p] = decision
		}
		toDeleteAllowWithDir := FindDescendantsInMap(path, permissionEntries.AllowWithDir)
		for p, decision := range toDeleteAllowWithDir {
			delete(permissionEntries.AllowWithDir, p)
			deleted.AllowWithDir[p] = decision
		}
		toDeleteAllowWithSubdirs := FindDescendantsInMap(path, permissionEntries.AllowWithSubdirs)
		for p, decision := range toDeleteAllowWithSubdirs {
			delete(permissionEntries.AllowWithSubdirs, p)
			deleted.AllowWithSubdirs[p] = decision
		}
		if !skipNewDecision {
			permissionEntries.AllowWithSubdirs[path] = allow
		}
	default:
		err = ErrUnknownMap
	}
	return deleted, err
}

// TODO: extras is ways too loosly typed right now
func (pd *PromptsDB) Set(req *notifier.Request, allow bool, extras map[string]string) (map[string]*permissionDB, error) {
	deleted := make(map[string]*permissionDB)
	// nothing to store in the db
	if extras[extrasAlwaysPrompt] == "yes" {
		return deleted, nil
	}
	// what if matching entry is already in the db?
	// should it be removed since we want to "always prompt"?

	which := whichMap(allow, extras)
	path := req.Path

	if strings.HasSuffix(path, "/") || ((which == jsonAllowWithDir || which == jsonAllowWithSubdirs) && !osutil.IsDirectory(path)) {
		path = filepath.Dir(path)
	}
	path = filepath.Clean(path)

	permissions := WhichPermissions(req, allow, extras)

	noChange := true

	for _, permission := range permissions {
		permissionEntries := pd.MapsForUidAndLabelAndPermission(req.SubjectUid, req.Label, permission)

		skipNewDecision, err := newDecisionImpliedByPreviousDecision(permissionEntries, which, path, allow)
		if err != nil {
			return deleted, err
		}
		if skipNewDecision {
			continue
		}

		noChange = false

		deletedForPermission, err := insertAndPrune(permissionEntries, which, path, allow)
		deleted[permission] = deletedForPermission
		if err != nil {
			return deleted, err
		}
	}

	if noChange {
		return deleted, nil
	}

	return deleted, pd.save()
}

func (pd *PromptsDB) Get(req *notifier.Request) (bool, error) {
	allAllow := true
	permissions := parseRequestPermissions(req)
	if len(permissions) == 0 {
		return false, ErrNoPermissions
	}
	for _, permission := range permissions {
		permissionEntries := pd.MapsForUidAndLabelAndPermission(req.SubjectUid, req.Label, permission)
		allow, err, _, _ := findPathInPermissionDB(permissionEntries, req.Path)
		allAllow = allAllow && allow
		if err != nil {
			return allow, err
		}
	}
	logger.Noticef("found promptDB decision %v for %v (uid %v)", allAllow, req.Path, req.SubjectUid)
	return allAllow, nil
}
