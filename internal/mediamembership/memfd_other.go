//go:build !linux

package mediamembership

import (
	"context"
	"crypto/sha256"
	"errors"
	"os"
)

type Reference struct {
	Key            string
	LocalPath      string
	ExpectedSHA256 []byte
	ExpectedLength *int64
}

type Blob struct {
	Reference
	File   *os.File
	Size   int64
	SHA256 [sha256.Size]byte
}

var ErrUnsupported = errors.New("immutable media staging requires Linux memfd sealing")

func Stage(context.Context, []Reference) ([]Blob, error) { return nil, ErrUnsupported }
func (b Blob) Close() error {
	if b.File == nil {
		return nil
	}
	return b.File.Close()
}
