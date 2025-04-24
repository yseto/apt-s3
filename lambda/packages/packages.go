package packages

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"

	"pault.ag/go/debian/control"
	"pault.ag/go/debian/deb"
)

type Iface interface {
	io.Reader
	io.ReaderAt
	io.Seeker
}

type Package struct {
	ControlWithStat string
	CPU             string
	DestPath        string
}

func Load(fd Iface, components, filename string) (response Package, err error) {
	debFile, err := deb.Load(fd, "")
	if err != nil {
		return
	}
	defer debFile.Close() // nolint

	response.CPU = debFile.Control.Architecture.CPU

	var (
		md5hash    = md5.New()
		sha1hash   = sha1.New()
		sha256hash = sha256.New()
	)

	if _, err = fd.Seek(0, io.SeekStart); err != nil {
		return
	}

	size, err := io.Copy(io.MultiWriter(md5hash, sha1hash, sha256hash), fd)
	if err != nil {
		return
	}

	basePath := filepath.Base(filename)

	destPath := fmt.Sprintf("pool/%s/%s/%s/%s", components, basePath[0:1], debFile.Control.Package, basePath)
	response.DestPath = destPath

	debFile.Control.Paragraph.Set("Filename", destPath)
	debFile.Control.Paragraph.Set("Size", fmt.Sprint(size))
	debFile.Control.Paragraph.Set("MD5sum", hex.EncodeToString(md5hash.Sum(nil)))
	debFile.Control.Paragraph.Set("SHA1", hex.EncodeToString(sha1hash.Sum(nil)))
	debFile.Control.Paragraph.Set("SHA256", hex.EncodeToString(sha256hash.Sum(nil)))

	buf := bytes.NewBuffer([]byte{})
	if err = control.Marshal(buf, debFile.Control); err != nil {
		return
	}

	response.ControlWithStat = buf.String()
	return
}
