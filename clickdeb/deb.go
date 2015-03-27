/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package clickdeb

import (
	"archive/tar"
	"compress/bzip2"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"launchpad.net/snappy/helpers"

	"github.com/blakesmith/ar"
)

var (
	// ErrSnapInvalidContent is returned if a snap package contains
	// invalid content
	ErrSnapInvalidContent = errors.New("snap contains invalid content")
)

// simple pipe based xz reader
func xzPipeReader(r io.Reader) io.Reader {
	pr, pw := io.Pipe()
	cmd := exec.Command("xz", "--decompress", "--stdout")
	cmd.Stdin = r
	cmd.Stdout = pw

	// run xz in its own go-routine
	go func() {
		pw.CloseWithError(cmd.Run())
	}()

	return pr
}

// ensure that the content of our data is valid:
// - no relative path allowed to prevent writing outside of the parent dir
func clickVerifyContentFn(path string) (string, error) {
	path = filepath.Clean(path)
	if strings.Contains(path, "..") {
		return "", ErrSnapInvalidContent
	}

	return path, nil
}

// ClickDeb provides support for the "click" containers (a special kind of
// deb package)
type ClickDeb struct {
	Path string
}

// ControlMember returns the content of the given control member file
// (e.g. the content of the "manifest" file in the control.tar.gz ar member)
func (d *ClickDeb) ControlMember(controlMember string) (content []byte, err error) {
	return d.member("control.tar", controlMember)
}

// MetaMember returns the content of the given meta file (e.g. the content of
// the "package.yaml" file) from the data.tar.gz ar member's meta/ directory
func (d *ClickDeb) MetaMember(metaMember string) (content []byte, err error) {
	return d.member("data.tar", filepath.Join("meta", metaMember))
}

// member(arMember, tarMember) returns the content of the given tar member of
// the given ar member tar.
//
// Confused? look at ControlMember and MetaMember, which this generalises.
func (d *ClickDeb) member(arMember, tarMember string) (content []byte, err error) {
	file, err := os.Open(d.Path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	arReader := ar.NewReader(file)
	dataReader, err := skipToArMember(arReader, arMember)
	if err != nil {
		return nil, err
	}

	err = helpers.TarIterate(dataReader, func(tr *tar.Reader, hdr *tar.Header) error {
		if filepath.Clean(hdr.Name) == tarMember {
			content, err = ioutil.ReadAll(tr)
			if err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return content, nil
}

// Unpack unpacks the data.tar.{gz,bz2,xz} into the given target directory
// with click specific verification, i.e. no files will be extracted outside
// of the targetdir (no ".." inside the data.tar is allowed)
func (d *ClickDeb) Unpack(targetDir string) error {
	var err error

	file, err := os.Open(d.Path)
	if err != nil {
		return err
	}
	defer file.Close()

	arReader := ar.NewReader(file)
	dataReader, err := skipToArMember(arReader, "data.tar")
	if err != nil {
		return err
	}

	// and unpack
	return helpers.UnpackTar(dataReader, targetDir, clickVerifyContentFn)
}

// FIXME: this should move into the "ar" library itself
func addFileToAr(arWriter *ar.Writer, filename string) error {
	dataF, err := os.Open(filename)
	if err != nil {
		return nil
	}
	defer dataF.Close()

	stat, err := dataF.Stat()
	if err != nil {
		return err
	}

	size := stat.Size()
	hdr := &ar.Header{
		Name:    filepath.Base(filename),
		ModTime: time.Now(),
		Mode:    int64(stat.Mode()),
		Size:    size,
	}
	arWriter.WriteHeader(hdr)
	_, err = io.Copy(arWriter, dataF)
	// io.Copy() is confused by the fact that a ar file must be even
	// aligned, so it may return a bigger amounts of bytes written
	// than requested. This is not a error so we ignore it
	if err != io.ErrShortWrite {
		return err
	}

	return nil
}

// FIXME: this should move into the "ar" library itself
func addDataToAr(arWriter *ar.Writer, filename string, data []byte) error {
	size := int64(len(data))
	hdr := &ar.Header{
		Name:    filename,
		ModTime: time.Now(),
		Mode:    0644,
		Size:    size,
	}
	arWriter.WriteHeader(hdr)
	_, err := arWriter.Write([]byte(data))
	if err != nil {
		return err
	}

	return nil
}

func isSymlink(mode os.FileMode) bool {
	return (mode & os.ModeSymlink) == os.ModeSymlink
}

// tarExcludeFunc is a helper for tarCreate that is called for each file
// that is about to be added. If it returns "false" the file is skipped
type tarExcludeFunc func(path string) bool

// tarCreate creates a tarfile for a clickdeb, all files in the archive
// belong to root (same as dpkg-deb)
func tarGzCreate(tarname string, sourceDir string, fn tarExcludeFunc) error {
	w, err := os.Create(tarname)
	if err != nil {
		return err
	}
	defer w.Close()

	// we can only support gzip for now because we want this code to
	// run on anything that go supports (including macos, windows).
	// so the only option is using a native compressor
	gzipWriter, err := gzip.NewWriterLevel(w, 9)
	if err != nil {
		return err
	}
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	err = filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		st, err := os.Lstat(path)
		if err != nil {
			return err
		}

		// check if we support this type
		if !st.Mode().IsRegular() && !isSymlink(st.Mode()) && !st.Mode().IsDir() {
			return nil
		}

		// check our exclude function
		if fn != nil {
			if !fn(path) {
				return nil
			}
		}

		// huh? golang, come on! the symlink stuff is a bit complicated
		target, _ := os.Readlink(path)
		hdr, err := tar.FileInfoHeader(info, target)
		if err != nil {
			return err
		}

		// exclude "."
		relativePath := "." + path[len(sourceDir):]
		if relativePath == "." {
			return nil
		}

		// add header, note that all files belong to root
		hdr.Name = relativePath
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = "root"
		hdr.Gname = "root"

		if err := tarWriter.WriteHeader(hdr); err != nil {
			return err
		}

		// add content
		if st.Mode().IsRegular() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tarWriter, f)
			if err != nil {
				return err
			}
		}

		return nil
	})

	return err
}

// Build takes a build debian directory with DEBIAN/ dir and creates a
// clickdeb from it
func (d *ClickDeb) Build(sourceDir string, dataTarFinishedCallback func(dataName string) error) error {
	var err error

	// create file
	file, err := os.Create(d.Path)
	if err != nil {
		return err
	}
	defer file.Close()

	// tmp
	tempdir, err := ioutil.TempDir("", "data")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempdir)

	// create content data
	dataName := filepath.Join(tempdir, "data.tar.gz")
	err = tarGzCreate(dataName, sourceDir, func(path string) bool {
		return !strings.HasPrefix(path, filepath.Join(sourceDir, "DEBIAN"))
	})
	if err != nil {
		return err
	}

	// this allows us to add hashes
	if dataTarFinishedCallback != nil {
		if err := dataTarFinishedCallback(dataName); err != nil {
			return err
		}
	}

	// create control data (for click compat)
	controlName := filepath.Join(tempdir, "control.tar.gz")
	if err := tarGzCreate(controlName, filepath.Join(sourceDir, "DEBIAN"), nil); err != nil {
		return err
	}

	// create ar
	arWriter := ar.NewWriter(file)
	arWriter.WriteGlobalHeader()

	// debian magic
	if err := addDataToAr(arWriter, "debian-binary", []byte("2.0\n")); err != nil {
		return err
	}

	// click magic
	if err := addDataToAr(arWriter, "_click-binary", []byte("0.4\n")); err != nil {
		return err
	}

	// control file
	if err := addFileToAr(arWriter, controlName); err != nil {
		return err
	}

	// data file
	if err := addFileToAr(arWriter, dataName); err != nil {
		return err
	}

	return nil
}

func skipToArMember(arReader *ar.Reader, memberPrefix string) (io.Reader, error) {
	var err error

	// find the right ar member
	var header *ar.Header
	for {
		header, err = arReader.Next()
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(header.Name, memberPrefix) {
			break
		}
	}

	// figure out what compression to use
	var dataReader io.Reader
	switch {
	case strings.HasSuffix(header.Name, ".gz"):
		dataReader, err = gzip.NewReader(arReader)
		if err != nil {
			return nil, err
		}
	case strings.HasSuffix(header.Name, ".bz2"):
		dataReader = bzip2.NewReader(arReader)
	case strings.HasSuffix(header.Name, ".xz"):
		dataReader = xzPipeReader(arReader)
	default:
		return nil, fmt.Errorf("Can not handle %s", header.Name)
	}

	return dataReader, nil
}
