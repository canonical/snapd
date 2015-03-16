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

	file *os.File
}

// ControlContent returns the content of the given control data member
// (e.g. the content of the "manifest" file in the control.tar.gz ar member)
func (d *ClickDeb) ControlContent(controlMember string) ([]byte, error) {
	var err error

	d.file, err = os.Open(d.Path)
	if err != nil {
		return nil, err
	}
	defer d.file.Close()

	dataReader, err := d.skipToArMember("control.tar")
	if err != nil {
		return nil, err
	}

	var content []byte
	err = helpers.TarIterate(dataReader, func(tr *tar.Reader, hdr *tar.Header) error {
		if filepath.Clean(hdr.Name) == controlMember {
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

	d.file, err = os.Open(d.Path)
	if err != nil {
		return err
	}
	defer d.file.Close()

	dataReader, err := d.skipToArMember("data.tar")
	if err != nil {
		return err
	}

	// and unpack
	return helpers.UnpackTar(dataReader, targetDir, clickVerifyContentFn)
}

// FIXME: this should move into the "ar" library itself
func addFileToAr(arWriter *ar.Writer, filename string) error {
	stat, err := os.Stat(filename)
	if err != nil {
		return err
	}
	dataF, err := os.Open(filename)
	if err != nil {
		return nil
	}
	defer dataF.Close()
	size := stat.Size()
	if size%2 == 1 {
		size++
	}
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
	if size%2 == 1 {
		size++
	}
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
		hdr.Name = "." + path[len(sourceDir):]
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
func (d *ClickDeb) Build(sourceDir string) error {
	var err error

	// create file
	d.file, err = os.Create(d.Path)
	if err != nil {
		return err
	}
	defer d.file.Close()

	// tmp
	tempdir, err := ioutil.TempDir("", "data")
	defer os.RemoveAll(tempdir)
	if err != nil {
		return err
	}

	// create control data (for click compat)
	controlName := filepath.Join(tempdir, "control.tar.gz")
	if err := tarGzCreate(controlName, filepath.Join(sourceDir, "DEBIAN"), nil); err != nil {
		return err
	}

	// create content data
	dataName := filepath.Join(tempdir, "data.tar.gz")
	err = tarGzCreate(dataName, sourceDir, func(path string) bool {
		return !strings.HasPrefix(path, filepath.Join(sourceDir, "DEBIAN"))
	})
	if err != nil {
		return err
	}

	// create ar
	arWriter := ar.NewWriter(d.file)
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

func (d *ClickDeb) skipToArMember(memberPrefix string) (io.Reader, error) {
	var err error

	// find the right ar member
	arReader := ar.NewReader(d.file)
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
		dataReader, err = gzip.NewReader(d.file)
		if err != nil {
			return nil, err
		}
	case strings.HasSuffix(header.Name, ".bz2"):
		dataReader = bzip2.NewReader(d.file)
	case strings.HasSuffix(header.Name, ".xz"):
		dataReader = xzPipeReader(d.file)
	default:
		return nil, fmt.Errorf("Can not handle %s", header.Name)
	}

	return dataReader, nil
}
