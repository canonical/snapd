package storage

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/prompting/notifier"
)

var ErrNoSavedDecision = errors.New("no saved prompt decision")

type userDB struct {
	PerLabelDB map[string]*labelDB
}

type labelDB struct {
	AllowWithSubdirs map[string]bool
}

// TODO: make this an interface
type PromptsDB struct {
	PerUser map[uint32]*userDB
}

// TODO: take a dir as argument to store prompt decisions
func New() *PromptsDB {
	pd := &PromptsDB{PerUser: make(map[uint32]*userDB)}
	// TODO: error handling
	pd.load()
	return pd
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
	userEntries := pd.PerUser[uid]
	if userEntries == nil {
		userEntries = &userDB{
			PerLabelDB: make(map[string]*labelDB),
		}
		pd.PerUser[uid] = userEntries
	}
	labelEntries := userEntries.PerLabelDB[label]
	if labelEntries == nil {
		labelEntries = &labelDB{
			AllowWithSubdirs: make(map[string]bool),
		}
		userEntries.PerLabelDB[label] = labelEntries
	}
	return labelEntries.AllowWithSubdirs
}

func (pd *PromptsDB) dbpath() string {
	return filepath.Join(dirs.SnapdStateDir(dirs.GlobalRootDir), "prompt.db")
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

	return pd.save()
}

func (pd *PromptsDB) Get(req *notifier.Request) (bool, error) {
	allowWithSubdirs := pd.PathsForUidAndLabel(req.SubjectUid, req.Label)
	if findPathInSubdirs(allowWithSubdirs, req.Path) {
		return true, nil
	}

	return false, ErrNoSavedDecision
}
