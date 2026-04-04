package zip

import (
	"archive/tar"
	"compress/bzip2"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// check for path traversal and correct forward slashes
func validRelPath(p string) bool {
	if p == "" || strings.Contains(p, `\`) || strings.HasPrefix(p, "/") || strings.Contains(p, "../") {
		return false
	}
	return true
}

func Decompress(src io.Reader, dst string) error {
	// unbzip2
	bzr := bzip2.NewReader(src)
	// untar
	tr := tar.NewReader(bzr)

	// uncompress each element
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break // End of archive
		}
		if err != nil {
			return err
		}
		target := header.Name

		// validate name against path traversal
		if !validRelPath(header.Name) {
			return fmt.Errorf("tar contained invalid name error %q", target)
		}

		// add dst + re-format slashes according to system
		target = filepath.Join(dst, header.Name)
		// if no join is needed, replace with ToSlash:
		// target = filepath.ToSlash(header.Name)

		// check the type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it (with 0755 permission)
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
		// if it's a file create it (with same permission)
		case tar.TypeReg:
			fileToWrite, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			// copy over contents
			if _, err := io.Copy(fileToWrite, tr); err != nil {
				return err
			}
			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			fileToWrite.Close()
		}
	}

	//
	return nil
}
