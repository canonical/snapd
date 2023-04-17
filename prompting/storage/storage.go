package storage

import (
	"errors"
	"path/filepath"

	//"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/prompting/notifier"
)

var ErrNoSavedDecision = errors.New("no saved prompt decision")

type userDB struct {
	perLabelDB map[string]*labelDB
}

type labelDB struct {
	allowWithSubdirs map[string]bool
}

// TODO: make this an interface
type PromptsDB struct {
	perUser map[uint32]*userDB
}

// TODO: take a dir as argument to store prompt decisions
func New() *PromptsDB {
	return &PromptsDB{
		perUser: make(map[uint32]*userDB),
	}
}

func findPathInSubdirs(paths map[string]bool, path string) bool {
	for path != filepath.Dir(path) {
		if paths[path] {
			return true
		}
		path = filepath.Dir(path)
	}
	return false
}

// TODO: unexport
func (pd *PromptsDB) PathsForUidAndLabel(uid uint32, label string) map[string]bool {
	userEntries := pd.perUser[uid]
	if userEntries == nil {
		userEntries = &userDB{
			perLabelDB: make(map[string]*labelDB),
		}
		pd.perUser[uid] = userEntries
	}
	labelEntries := userEntries.perLabelDB[label]
	if labelEntries == nil {
		labelEntries = &labelDB{
			allowWithSubdirs: make(map[string]bool),
		}
		userEntries.perLabelDB[label] = labelEntries
	}
	return labelEntries.allowWithSubdirs
}

// TODO: extras is ways too loosly typed right now
func (pd *PromptsDB) Set(req *notifier.Request, allow bool, extras map[string]string) error {
	// nothing to store in the db
	if extras["always-prompt"] == "yes" {
		return nil
	}
	allowWithSubdirs := pd.PathsForUidAndLabel(req.SubjectUid, req.Label)

	path := req.Path
	if !osutil.IsDirectory(path) {
		path = filepath.Dir(path)
	}
	if findPathInSubdirs(allowWithSubdirs, path) {
		return nil
	}
	allowWithSubdirs[path] = allow

	return nil
}

func (pd *PromptsDB) Get(req *notifier.Request) (bool, error) {
	allowWithSubdirs := pd.PathsForUidAndLabel(req.SubjectUid, req.Label)
	if findPathInSubdirs(allowWithSubdirs, req.Path) {
		return true, nil
	}

	return false, ErrNoSavedDecision
}
