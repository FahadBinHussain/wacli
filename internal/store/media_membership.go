package store

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/openclaw/wacli/internal/mediamembership"
)

// MediaMembership is one persisted media.local_path reference. Rows are never
// deduplicated: aliases and repeated paths remain separate membership entries.
// This enumerator describes database membership only; it does not establish a
// lifecycle hold/frontier and must not be used as a capture receipt.
type MediaMembership struct {
	Key            string
	LocalPath      string
	ExpectedSHA256 []byte
	ExpectedLength *int64
}

// EnumerateMediaMembership returns every non-empty message local_path and every
// non-empty alias local_path, or fails if the whole set exceeds the staging
// reference limit. Alias metadata is inherited from
// its referenced message when present; orphan aliases are retained with missing
// metadata so the staging layer can refuse the whole set. A query or scan
// failure aborts enumeration.
func (d *DB) EnumerateMediaMembership(ctx context.Context) ([]MediaMembership, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	const query = `
SELECT 'message:' || CAST(rowid AS TEXT), local_path, file_sha256, file_length
  FROM messages
 WHERE local_path IS NOT NULL AND local_path <> ''
UNION ALL
SELECT 'alias:' || a.chat_jid || ':' || a.msg_id || ':' || a.local_path,
       a.local_path, m.file_sha256, m.file_length
  FROM message_local_media_aliases AS a
	  LEFT JOIN messages AS m ON m.chat_jid = a.chat_jid AND m.msg_id = a.msg_id
 WHERE a.local_path IS NOT NULL AND a.local_path <> ''`
	rows, err := d.sql.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("enumerate media membership: %w", err)
	}
	defer rows.Close()
	var result []MediaMembership
	for rows.Next() {
		if len(result) >= mediamembership.MaxReferences {
			return nil, fmt.Errorf("media membership reference limit exceeded; no partial membership returned")
		}
		var key, path string
		var digest []byte
		var length sql.NullInt64
		if err := rows.Scan(&key, &path, &digest, &length); err != nil {
			return nil, fmt.Errorf("scan media membership: %w", err)
		}
		// database/sql may reuse its byte buffer; retain independent bytes.
		copyDigest := append([]byte(nil), digest...)
		var expectedLength *int64
		if length.Valid {
			n := length.Int64
			expectedLength = &n
		}
		result = append(result, MediaMembership{Key: key, LocalPath: path, ExpectedSHA256: copyDigest, ExpectedLength: expectedLength})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("enumerate media membership rows: %w", err)
	}
	return result, nil
}
