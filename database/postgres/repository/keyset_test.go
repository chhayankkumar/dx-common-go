package repository

import (
	"strings"
	"testing"

	"github.com/datakaveri/dx-common-go/database/postgres/query"
)

func TestKeysetCursorCodec(t *testing.T) {
	in := KeysetCursor{Key: "2026-01-01T00:00:00Z", ID: "f-42"}
	token := EncodeKeysetCursor(in)
	out, err := DecodeKeysetCursor(token)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Key != in.Key || out.ID != in.ID {
		t.Fatalf("roundtrip = %+v, want %+v", out, in)
	}
	if _, err := DecodeKeysetCursor("!!not-base64!!"); err == nil {
		t.Fatal("garbage token must error")
	}
	if _, err := DecodeKeysetCursor("bm90LWpzb24"); err == nil { // "not-json"
		t.Fatal("non-JSON token must error")
	}
}

// TestKeysetCondition_SQL pins the seek predicate shape and parameterization
// through the shared renderer.
func TestKeysetCondition_SQL(t *testing.T) {
	cur := KeysetCursor{Key: "k1", ID: "id1"}

	sql, args := query.BuildWhere([]query.Condition{KeysetCondition("created_at", "id", false, cur)}, 1)
	want := "((created_at > $1) OR ((created_at = $2 AND id > $3)))"
	if !strings.Contains(sql, "created_at > $1") || !strings.Contains(sql, "created_at = $2 AND id > $3") {
		t.Fatalf("ascending sql = %q (want shape %q)", sql, want)
	}
	if len(args) != 3 || args[0] != "k1" || args[1] != "k1" || args[2] != "id1" {
		t.Fatalf("args = %v", args)
	}

	sql, _ = query.BuildWhere([]query.Condition{KeysetCondition("created_at", "id", true, cur)}, 1)
	if !strings.Contains(sql, "created_at < $1") || !strings.Contains(sql, "id < $3") {
		t.Fatalf("descending sql = %q", sql)
	}
}
