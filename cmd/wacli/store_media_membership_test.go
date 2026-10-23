//go:build linux

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openclaw/wacli/internal/store"
)

func TestStoreVerifyMediaSyntheticReferencesAndRefusal(t *testing.T) {
	root := t.TempDir()
	media := filepath.Join(t.TempDir(), "synthetic-media")
	data := []byte("synthetic media bytes only")
	if err := os.WriteFile(media, data, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	db, err := store.Open(filepath.Join(root, "wacli.db"))
	if err != nil {
		t.Fatal(err)
	}
	chat := "synthetic@s.whatsapp.net"
	if err := db.UpsertChat(chat, "dm", "synthetic", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessage(store.UpsertMessageParams{ChatJID: chat, MsgID: "m", Timestamp: time.Now(), MediaType: "document", FileSHA256: sum[:], FileLength: uint64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if err := db.MarkMediaDownloaded(chat, "m", media, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	cmd := newStoreVerifyMediaCmd(&rootFlags{storeDir: root, asJSON: true})
	cmd.SetOut(&output)
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	dataResult, ok := result["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing diagnostic data: %v", result)
	}
	result = dataResult
	if result["checked_references"] != float64(1) || result["complete"] != false || result["capture_enabled"] != false || result["publishable"] != false || result["boundary"] != "UNVERIFIED" {
		t.Fatalf("misleading report: %v", result)
	}
	if _, err := os.Stat(filepath.Join(root, "session.db")); !os.IsNotExist(err) {
		t.Fatalf("session source was opened: %v", err)
	}
	if err := os.WriteFile(media, []byte("foreign mutation"), 0600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	cmd = newStoreVerifyMediaCmd(&rootFlags{storeDir: root, asJSON: true})
	cmd.SetOut(&output)
	cmd.SetErr(&output)
	if err := cmd.Execute(); err == nil {
		t.Fatal("changed media was accepted")
	}
	if bytes.Contains(output.Bytes(), []byte("verified-byte-objects-only")) {
		t.Fatal("partial success report after failure")
	}
	got, err := os.ReadFile(media)
	if err != nil || string(got) != "foreign mutation" {
		t.Fatalf("original changed or removed: %q, %v", got, err)
	}
}

func TestStoreVerifyMediaWired(t *testing.T) {
	command, _, err := newStoreCmd(&rootFlags{}).Find([]string{"verify-media"})
	if err != nil || command == nil || command.Name() != "verify-media" {
		t.Fatalf("command = %v, %v", command, err)
	}
}
