//go:build linux

package mediamembership

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestStageSealsAndPreservesBytes(t *testing.T) {
	p := t.TempDir() + "/media"
	original := []byte("immutable media bytes")
	if err := os.WriteFile(p, original, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(original)
	n := int64(len(original))
	blobs, err := Stage(context.Background(), []Reference{{Key: "one", LocalPath: p, ExpectedSHA256: sum[:], ExpectedLength: &n}})
	if err != nil {
		t.Fatal(err)
	}
	defer blobs[0].Close()
	if _, err := unixWrite(int(blobs[0].File.Fd()), []byte("x")); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("write error = %v, want EPERM", err)
	}
	if err := syscall.Ftruncate(int(blobs[0].File.Fd()), 0); !errors.Is(err, syscall.EPERM) {
		t.Fatalf("truncate error = %v, want EPERM", err)
	}
	if err := os.WriteFile(p, []byte("changed externally"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := blobs[0].File.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(blobs[0].File)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("held bytes = %q, want %q", got, original)
	}
}

func unixWrite(fd int, p []byte) (int, error) { return syscall.Write(fd, p) }

func TestStageCancellationIncludesEmptyAndFinal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Stage(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("empty canceled Stage = %v", err)
	}
	path := t.TempDir() + "/media"
	data := []byte("cancel")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	ctx, cancel = context.WithCancel(context.Background())
	// A canceled context must not return staged descriptors, even at completion.
	cancel()
	if _, err := Stage(ctx, []Reference{{LocalPath: path, ExpectedSHA256: sum[:]}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Stage = %v", err)
	}
}

func TestStageDuplicateReferencesRemainDistinct(t *testing.T) {
	path := t.TempDir() + "/media"
	data := []byte("same")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	blobs, err := Stage(context.Background(), []Reference{{Key: "a", LocalPath: path, ExpectedSHA256: sum[:]}, {Key: "b", LocalPath: path, ExpectedSHA256: sum[:]}})
	if err != nil || len(blobs) != 2 {
		t.Fatalf("duplicate Stage = %d, %v", len(blobs), err)
	}
	defer blobs[0].Close()
	defer blobs[1].Close()
}

func TestStageRefusesMissingWrongAndFIFO(t *testing.T) {
	path := t.TempDir() + "/media"
	data := []byte("valid")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if blobs, err := Stage(context.Background(), []Reference{{LocalPath: path + ".missing", ExpectedSHA256: sum[:]}}); err == nil || blobs != nil {
		t.Fatalf("missing source = %v, %v", blobs, err)
	}
	wrong := sha256.Sum256([]byte("wrong"))
	if blobs, err := Stage(context.Background(), []Reference{{LocalPath: path, ExpectedSHA256: wrong[:]}}); err == nil || blobs != nil {
		t.Fatalf("wrong digest = %v, %v", blobs, err)
	}
	length := int64(len(data) + 1)
	if blobs, err := Stage(context.Background(), []Reference{{LocalPath: path, ExpectedSHA256: sum[:], ExpectedLength: &length}}); err == nil || blobs != nil {
		t.Fatalf("wrong length = %v, %v", blobs, err)
	}
	fifo := t.TempDir() + "/fifo"
	if err := unix.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if blobs, err := Stage(context.Background(), []Reference{{LocalPath: fifo, ExpectedSHA256: sum[:]}}); err == nil || blobs != nil {
		t.Fatalf("FIFO = %v, %v", blobs, err)
	}
}

func TestStageResourceLimitsAbortWholeSet(t *testing.T) {
	path := t.TempDir() + "/media"
	data := []byte("small")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	tooMany := make([]Reference, MaxReferences+1)
	for i := range tooMany {
		tooMany[i] = Reference{LocalPath: path, ExpectedSHA256: sum[:]}
	}
	if blobs, err := Stage(context.Background(), tooMany); err == nil || blobs != nil {
		t.Fatalf("count limit = %v, %v", blobs, err)
	}
	tooLong := MaxTotalStagedBytes + 1
	if blobs, err := Stage(context.Background(), []Reference{{LocalPath: path, ExpectedSHA256: sum[:], ExpectedLength: &tooLong}}); err == nil || blobs != nil {
		t.Fatalf("byte limit = %v, %v", blobs, err)
	}
}

func TestStageFailsWholeManifestAndRetainsNoSuccess(t *testing.T) {
	p := t.TempDir() + "/media"
	data := []byte("x")
	if err := os.WriteFile(p, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	blobs, err := Stage(context.Background(), []Reference{{Key: "ok", LocalPath: p, ExpectedSHA256: sum[:]}, {Key: "bad", LocalPath: p, ExpectedSHA256: []byte("bad")}})
	if err == nil || blobs != nil {
		t.Fatalf("Stage = (%v, %v), want whole-manifest failure", blobs, err)
	}
}
