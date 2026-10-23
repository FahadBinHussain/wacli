//go:build linux

// Package mediamembership provides a deliberately low-level, bounded media
// verifier. It does not claim a stable database frontier or enable capture.
package mediamembership

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
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

func Stage(ctx context.Context, refs []Reference) ([]Blob, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(refs) > MaxReferences || len(refs) > MaxStagedDescriptors {
		return nil, fmt.Errorf("media reference limit exceeded: %d (max %d)", len(refs), MaxReferences)
	}
	out := make([]Blob, 0, len(refs))
	cleanup := func() {
		for i := range out {
			_ = out[i].Close()
		}
	}
	var total int64
	for i, ref := range refs {
		if err := ctx.Err(); err != nil {
			cleanup()
			return nil, err
		}
		blob, err := stageOne(ctx, ref, MaxTotalStagedBytes-total)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("reference %d (%s): %w", i, ref.Key, err)
		}
		total += blob.Size
		out = append(out, blob)
	}
	if err := ctx.Err(); err != nil {
		cleanup()
		return nil, err
	}
	return out, nil
}

func stageOne(ctx context.Context, ref Reference, remaining int64) (Blob, error) {
	var zero Blob
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if ref.LocalPath == "" {
		return zero, errors.New("empty local path")
	}
	if len(ref.ExpectedSHA256) != sha256.Size {
		return zero, errors.New("malformed expected sha256")
	}
	if ref.ExpectedLength != nil && *ref.ExpectedLength < 0 {
		return zero, errors.New("negative expected length")
	}
	if remaining < 0 {
		return zero, errors.New("total staged byte limit exceeded")
	}

	fd, err := unix.Open(ref.LocalPath, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return zero, fmt.Errorf("open source: %w", err)
	}
	src := os.NewFile(uintptr(fd), ref.LocalPath)
	if src == nil {
		_ = unix.Close(fd)
		return zero, errors.New("open source: invalid descriptor")
	}
	defer src.Close()
	before, err := src.Stat()
	if err != nil {
		return zero, fmt.Errorf("stat source: %w", err)
	}
	if !before.Mode().IsRegular() {
		return zero, errors.New("source is not a regular file")
	}
	if before.Size() > remaining || before.Size() > MaxTotalStagedBytes {
		return zero, errors.New("total staged byte limit exceeded")
	}
	if ref.ExpectedLength != nil && *ref.ExpectedLength > remaining {
		return zero, errors.New("expected length exceeds total staged byte limit")
	}

	mfd, err := unix.MemfdCreate("wacli-media", unix.MFD_CLOEXEC|unix.MFD_ALLOW_SEALING)
	if err != nil {
		return zero, fmt.Errorf("memfd: %w", err)
	}
	f := os.NewFile(uintptr(mfd), "wacli-media")
	fail := func(e error) (Blob, error) { _ = f.Close(); return zero, e }
	h := sha256.New()
	w := io.MultiWriter(f, h)
	buf := make([]byte, 32*1024)
	bound := before.Size() + 1
	if ref.ExpectedLength != nil && *ref.ExpectedLength+1 < bound {
		bound = *ref.ExpectedLength + 1
	}
	lr := io.LimitReader(src, bound)
	var n int64
	for {
		if err := ctx.Err(); err != nil {
			return fail(err)
		}
		r, er := lr.Read(buf)
		if r > 0 {
			if n+int64(r) > remaining {
				return fail(errors.New("total staged byte limit exceeded"))
			}
			wn, ew := w.Write(buf[:r])
			n += int64(wn)
			if ew != nil {
				return fail(fmt.Errorf("stage bytes: %w", ew))
			}
			if wn != r {
				return fail(io.ErrShortWrite)
			}
		}
		if er == io.EOF {
			break
		}
		if er != nil {
			return fail(fmt.Errorf("read source: %w", er))
		}
	}
	after, err := src.Stat()
	if err != nil {
		return fail(fmt.Errorf("restat source: %w", err))
	}
	if before.Size() != after.Size() || before.ModTime() != after.ModTime() || before.Mode() != after.Mode() {
		return fail(errors.New("source changed during staging"))
	}
	if n != before.Size() {
		return fail(errors.New("source length changed or exceeds expected length"))
	}
	sum := h.Sum(nil)
	if !equalBytes(sum, ref.ExpectedSHA256) {
		return fail(fmt.Errorf("sha256 mismatch: got %s", hex.EncodeToString(sum)))
	}
	if ref.ExpectedLength != nil && n != *ref.ExpectedLength {
		return fail(fmt.Errorf("length mismatch: got %d", n))
	}
	if _, err := unix.FcntlInt(uintptr(mfd), unix.F_ADD_SEALS, unix.F_SEAL_WRITE|unix.F_SEAL_GROW|unix.F_SEAL_SHRINK|unix.F_SEAL_SEAL); err != nil {
		return fail(fmt.Errorf("seal memfd: %w", err))
	}
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	var verified [sha256.Size]byte
	copy(verified[:], sum)
	return Blob{Reference: ref, File: f, Size: n, SHA256: verified}, nil
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var v byte
	for i := range a {
		v |= a[i] ^ b[i]
	}
	return v == 0
}
func (b Blob) Close() error {
	if b.File == nil {
		return nil
	}
	return b.File.Close()
}
