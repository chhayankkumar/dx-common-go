package sts

import (
	"encoding/json"
	"testing"
)

func TestPrefixPolicy(t *testing.T) {
	t.Run("read-only scopes to the prefix", func(t *testing.T) {
		raw, err := PrefixReadOnlyPolicy("my-bucket", "databanks/abc")
		if err != nil {
			t.Fatalf("PrefixReadOnlyPolicy: %v", err)
		}
		var p struct {
			Statement []struct {
				Action   []string `json:"Action"`
				Resource []string `json:"Resource"`
			} `json:"Statement"`
		}
		if err := json.Unmarshal([]byte(raw), &p); err != nil {
			t.Fatalf("policy is not valid JSON: %v", err)
		}
		if len(p.Statement) != 2 {
			t.Fatalf("want 2 statements (objects + listing), got %d", len(p.Statement))
		}
		// Object statement: GetObject on the prefix, trailing slash normalised.
		if got := p.Statement[0].Resource[0]; got != "arn:aws:s3:::my-bucket/databanks/abc/*" {
			t.Fatalf("object resource = %q", got)
		}
		if p.Statement[0].Action[0] != "s3:GetObject" || len(p.Statement[0].Action) != 1 {
			t.Fatalf("read-only actions = %v", p.Statement[0].Action)
		}
		// List statement is bucket-scoped.
		if got := p.Statement[1].Resource[0]; got != "arn:aws:s3:::my-bucket" {
			t.Fatalf("list resource = %q", got)
		}
	})

	t.Run("read-write includes put/delete", func(t *testing.T) {
		raw, err := PrefixPolicy("b", "p/", ReadWriteActions)
		if err != nil {
			t.Fatalf("PrefixPolicy: %v", err)
		}
		for _, want := range []string{"s3:GetObject", "s3:PutObject", "s3:DeleteObject"} {
			if !contains(raw, want) {
				t.Fatalf("policy missing action %q: %s", want, raw)
			}
		}
	})

	t.Run("validates inputs", func(t *testing.T) {
		if _, err := PrefixPolicy("", "p", ReadActions); err == nil {
			t.Fatal("expected error for empty bucket")
		}
		if _, err := PrefixPolicy("b", "p", nil); err == nil {
			t.Fatal("expected error for empty actions")
		}
	})
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
