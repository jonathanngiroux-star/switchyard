package store

import (
	"context"
	"testing"
)

// Regression: ListAudit must return the MOST RECENT `limit` entries (the
// doc comment and audit API contract), newest last. The old query returned
// the OLDEST — on any instance with more rows than the limit, /audit
// froze on the first page forever and new mutations never appeared.
func TestListAuditReturnsMostRecentWindow(t *testing.T) {
	st := newTestStore(t)
	defer st.Close()
	ctx := context.Background()

	// Write 5 entries; ask for the most recent 3.
	for i := 1; i <= 5; i++ {
		if err := st.PutAudit(ctx, AuditEntry{
			Actor: "a", Action: "toggle", Resource: "flag",
			Key: "k", Env: "production",
			Before: itoa(i), After: "",
		}); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}
	entries, err := st.ListAudit(ctx, 3)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}
	if entries[0].ID != 3 || entries[2].ID != 5 {
		t.Fatalf("want ids [3 4 5] (most recent window, append order), got [%d %d %d]",
			entries[0].ID, entries[1].ID, entries[2].ID)
	}
	// And the full window is still in append order for SIEM diffing.
	if !(entries[0].ID < entries[1].ID && entries[1].ID < entries[2].ID) {
		t.Fatalf("append order violated: %v", entries)
	}
}

// itoa avoids strconv import churn in this test file.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
