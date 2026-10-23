package store

import (
	"context"
	"testing"
	"time"
)

func TestEnumerateMediaMembershipIncludesEveryReference(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	chat := "membership@s.whatsapp.net"
	if err := db.UpsertChat(chat, "dm", "membership", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessage(UpsertMessageParams{ChatJID: chat, MsgID: "m1", Timestamp: time.Now(), MediaType: "image", FileSHA256: []byte{1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`UPDATE messages SET local_path=? WHERE chat_jid=? AND msg_id=?`, "/same", chat, "m1"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO message_local_media_aliases(chat_jid,msg_id,local_path) VALUES(?,?,?), (?,?,?)`, chat, "m1", "/same", chat, "m1", "/alias"); err != nil {
		t.Fatal(err)
	}
	refs, err := db.EnumerateMediaMembership(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 3 {
		t.Fatalf("reference count = %d, want 3", len(refs))
	}
	for _, ref := range refs {
		if ref.LocalPath == "" {
			t.Fatal("empty path returned")
		}
	}
}

func TestEnumerateMediaMembershipDoesNotDropMissingDigest(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	chat := "missing@s.whatsapp.net"
	if err := db.UpsertChat(chat, "dm", "missing", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessage(UpsertMessageParams{ChatJID: chat, MsgID: "m", Timestamp: time.Now(), MediaType: "image"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`UPDATE messages SET local_path=? WHERE chat_jid=? AND msg_id=?`, "/missing", chat, "m"); err != nil {
		t.Fatal(err)
	}
	refs, err := db.EnumerateMediaMembership(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || len(refs[0].ExpectedSHA256) != 0 {
		t.Fatalf("refs = %#v, want one malformed-metadata reference", refs)
	}
}

func TestEnumerateMediaMembershipRetainsOrphanAlias(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	chat := "orphan@s.whatsapp.net"
	if err := db.UpsertChat(chat, "dm", "orphan", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessage(UpsertMessageParams{ChatJID: chat, MsgID: "missing", Timestamp: time.Now(), MediaType: "image"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`INSERT INTO message_local_media_aliases(chat_jid,msg_id,local_path) VALUES(?,?,?)`, chat, "missing", "/orphan"); err != nil {
		t.Fatal(err)
	}
	db.sql.SetMaxOpenConns(1)
	if _, err := db.sql.Exec(`PRAGMA foreign_keys=OFF; DELETE FROM messages WHERE chat_jid=? AND msg_id=?`, chat, "missing"); err != nil {
		t.Fatal(err)
	}
	refs, err := db.EnumerateMediaMembership(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].LocalPath != "/orphan" || len(refs[0].ExpectedSHA256) != 0 || refs[0].ExpectedLength != nil {
		t.Fatalf("refs = %#v, want retained orphan with missing metadata", refs)
	}
}

func TestEnumerateMediaMembershipLimitFailsWithoutPartialSet(t *testing.T) {
	db := openTestDB(t)
	defer db.Close()
	chat := "bounded@s.whatsapp.net"
	if err := db.UpsertChat(chat, "dm", "bounded", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertMessage(UpsertMessageParams{ChatJID: chat, MsgID: "m", Timestamp: time.Now()}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.sql.Exec(`WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<1025)
		INSERT INTO message_local_media_aliases(chat_jid,msg_id,local_path) SELECT ?, ?, '/synthetic/' || x FROM n`, chat, "m"); err != nil {
		t.Fatal(err)
	}
	refs, err := db.EnumerateMediaMembership(context.Background())
	if err == nil || refs != nil {
		t.Fatalf("oversized membership returned partial success: len=%d, err=%v", len(refs), err)
	}
	var count int
	if err := db.sql.QueryRow(`SELECT COUNT(*) FROM message_local_media_aliases`).Scan(&count); err != nil || count != 1025 {
		t.Fatalf("original references modified: count=%d, err=%v", count, err)
	}
}
