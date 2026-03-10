package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"
)

// newTestStorage returns an in-memory Storage (no file I/O) with a discarded logger.
func newTestStorage(t *testing.T) *Storage {
	t.Helper()
	s, err := NewStorage(":memory:", slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})))
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	return s
}

// makeEvent returns a minimal Event for the given fingerprint / exception type.
func makeEvent(exType, exValue, project, env string) *Event {
	return &Event{
		ID:          generateID(),
		Timestamp:   time.Now().UTC(),
		Level:       "error",
		Platform:    "go",
		Environment: env,
		ProjectKey:  project,
		Exception:   &Exception{Type: exType, Value: exValue},
	}
}

// ── SaveEvent ─────────────────────────────────────────────────────────────────

func TestSaveEvent_NewIssueCreated(t *testing.T) {
	s := newTestStorage(t)
	ev := makeEvent("TypeError", "nil pointer", "proj1", "production")

	if err := s.SaveEvent(ev); err != nil {
		t.Fatalf("SaveEvent: %v", err)
	}

	issues, total, err := s.ListIssues("", "", "", 10, 0)
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if total != 1 {
		t.Errorf("expected 1 issue, got %d", total)
	}
	if issues[0].Count != 1 {
		t.Errorf("expected count 1, got %d", issues[0].Count)
	}
	if issues[0].Status != "unresolved" {
		t.Errorf("expected status unresolved, got %s", issues[0].Status)
	}
}

func TestSaveEvent_DuplicateFingerprintIncrementsCount(t *testing.T) {
	s := newTestStorage(t)
	ev1 := makeEvent("TypeError", "nil pointer", "proj1", "production")
	ev2 := makeEvent("TypeError", "nil pointer", "proj1", "production")
	// Same exception type+value → same fingerprint → same issue.

	s.SaveEvent(ev1)
	s.SaveEvent(ev2)

	issues, total, _ := s.ListIssues("", "", "", 10, 0)
	if total != 1 {
		t.Errorf("expected 1 issue (deduped), got %d", total)
	}
	if issues[0].Count != 2 {
		t.Errorf("expected count 2, got %d", issues[0].Count)
	}
}

func TestSaveEvent_DifferentExceptionCreatesNewIssue(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("TypeError", "nil pointer", "proj1", "production"))
	s.SaveEvent(makeEvent("IOError", "file not found", "proj1", "production"))

	_, total, _ := s.ListIssues("", "", "", 10, 0)
	if total != 2 {
		t.Errorf("expected 2 distinct issues, got %d", total)
	}
}

func TestSaveEvent_LastEventUpdated(t *testing.T) {
	s := newTestStorage(t)
	ev1 := makeEvent("TypeError", "nil pointer", "proj1", "production")
	ev1.Message = "first"
	ev2 := makeEvent("TypeError", "nil pointer", "proj1", "production")
	ev2.Message = "second"

	s.SaveEvent(ev1)
	s.SaveEvent(ev2)

	issues, _, _ := s.ListIssues("", "", "", 10, 0)
	if issues[0].LastEvent == nil || issues[0].LastEvent.Message != "second" {
		t.Errorf("expected LastEvent to be second event")
	}
}

func TestSaveEvent_EventsCapAt100(t *testing.T) {
	s := newTestStorage(t)
	for i := range 110 {
		ev := makeEvent("TypeError", "nil pointer", "proj1", "production")
		ev.Message = fmt.Sprintf("msg-%d", i)
		s.SaveEvent(ev)
	}

	issues, _, _ := s.ListIssues("", "", "", 10, 0)
	issueID := issues[0].ID
	events, _ := s.GetIssueEvents(issueID, 200)
	if len(events) > 100 {
		t.Errorf("expected at most 100 stored events, got %d", len(events))
	}
}

// ── ListIssues ────────────────────────────────────────────────────────────────

func TestListIssues_FilterByStatus(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("ErrA", "a", "p1", "production"))
	s.SaveEvent(makeEvent("ErrB", "b", "p1", "production"))

	issues, _, _ := s.ListIssues("", "", "", 10, 0)
	s.UpdateIssueStatus(issues[0].ID, "resolved")

	unresolved, uTotal, _ := s.ListIssues("unresolved", "", "", 10, 0)
	if uTotal != 1 {
		t.Errorf("expected 1 unresolved, got %d", uTotal)
	}
	_ = unresolved

	resolved, rTotal, _ := s.ListIssues("resolved", "", "", 10, 0)
	if rTotal != 1 {
		t.Errorf("expected 1 resolved, got %d", rTotal)
	}
	_ = resolved
}

func TestStorage_ListIssues_FilterByProject(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("Err", "x", "alpha", "production"))
	s.SaveEvent(makeEvent("Err", "y", "beta", "production"))

	issues, total, _ := s.ListIssues("", "alpha", "", 10, 0)
	if total != 1 {
		t.Errorf("expected 1 issue for project alpha, got %d", total)
	}
	if issues[0].ProjectKey != "alpha" {
		t.Errorf("expected project alpha, got %s", issues[0].ProjectKey)
	}
}

func TestListIssues_FilterByEnvironment(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("Err", "a", "p", "production"))
	s.SaveEvent(makeEvent("Err", "b", "p", "staging"))

	_, prodTotal, _ := s.ListIssues("", "", "production", 10, 0)
	if prodTotal != 1 {
		t.Errorf("expected 1 production issue, got %d", prodTotal)
	}

	_, stagTotal, _ := s.ListIssues("", "", "staging", 10, 0)
	if stagTotal != 1 {
		t.Errorf("expected 1 staging issue, got %d", stagTotal)
	}
}

func TestListIssues_Pagination(t *testing.T) {
	s := newTestStorage(t)
	for i := range 5 {
		s.SaveEvent(makeEvent("Err", fmt.Sprintf("err-%d", i), "p", "production"))
	}

	page1, total, _ := s.ListIssues("", "", "", 2, 0)
	if total != 5 {
		t.Errorf("expected total 5, got %d", total)
	}
	if len(page1) != 2 {
		t.Errorf("expected 2 items on page 1, got %d", len(page1))
	}

	page3, _, _ := s.ListIssues("", "", "", 2, 4)
	if len(page3) != 1 {
		t.Errorf("expected 1 item on last page, got %d", len(page3))
	}
}

func TestListIssues_OffsetBeyondTotal(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("Err", "x", "p", "production"))

	issues, total, err := s.ListIssues("", "", "", 10, 999)
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if total != 1 {
		t.Errorf("expected total 1, got %d", total)
	}
	if len(issues) != 0 {
		t.Errorf("expected 0 items, got %d", len(issues))
	}
}

// ── GetIssue ──────────────────────────────────────────────────────────────────

func TestGetIssue_KnownID(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("Err", "oops", "p", "production"))

	all, _, _ := s.ListIssues("", "", "", 1, 0)
	id := all[0].ID

	issue, err := s.GetIssue(id)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if issue == nil {
		t.Fatal("expected issue, got nil")
	}
	if issue.ID != id {
		t.Errorf("ID mismatch: want %s, got %s", id, issue.ID)
	}
}

func TestGetIssue_UnknownIDReturnsNil(t *testing.T) {
	s := newTestStorage(t)

	issue, err := s.GetIssue("nonexistent-id")
	if err != nil {
		t.Fatalf("GetIssue should not error on unknown id: %v", err)
	}
	if issue != nil {
		t.Errorf("expected nil for unknown id, got %+v", issue)
	}
}

// ── GetIssueEvents ────────────────────────────────────────────────────────────

func TestGetIssueEvents_LimitRespected(t *testing.T) {
	s := newTestStorage(t)
	for range 10 {
		s.SaveEvent(makeEvent("Err", "x", "p", "production"))
	}

	all, _, _ := s.ListIssues("", "", "", 1, 0)
	events, err := s.GetIssueEvents(all[0].ID, 3)
	if err != nil {
		t.Fatalf("GetIssueEvents: %v", err)
	}
	if len(events) > 3 {
		t.Errorf("expected at most 3 events, got %d", len(events))
	}
}

// ── UpdateIssueStatus ─────────────────────────────────────────────────────────

func TestUpdateIssueStatus_ValidTransitions(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("Err", "x", "p", "production"))
	all, _, _ := s.ListIssues("", "", "", 1, 0)
	id := all[0].ID

	for _, status := range []string{"resolved", "ignored", "unresolved"} {
		if err := s.UpdateIssueStatus(id, status); err != nil {
			t.Errorf("UpdateIssueStatus(%q): %v", status, err)
		}
		issue, _ := s.GetIssue(id)
		if issue.Status != status {
			t.Errorf("expected status %q, got %q", status, issue.Status)
		}
	}
}

func TestUpdateIssueStatus_UnknownIDReturnsError(t *testing.T) {
	s := newTestStorage(t)

	err := s.UpdateIssueStatus("ghost-id", "resolved")
	if err == nil {
		t.Error("expected error for unknown issue ID, got nil")
	}
}

// ── SearchIssues ──────────────────────────────────────────────────────────────

func TestSearchIssues_CaseInsensitiveMatch(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("NullPointerException", "object is null", "p", "production"))

	// Search lowercase
	results, _ := s.SearchIssues("nullpointer", 10)
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'nullpointer', got %d", len(results))
	}

	// Search mixed case
	results, _ = s.SearchIssues("NullPointer", 10)
	if len(results) != 1 {
		t.Errorf("expected 1 result for 'NullPointer', got %d", len(results))
	}
}

func TestSearchIssues_NoMatch(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("TypeError", "nil pointer", "p", "production"))

	results, _ := s.SearchIssues("database", 10)
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchIssues_LimitRespected(t *testing.T) {
	s := newTestStorage(t)
	for i := range 5 {
		s.SaveEvent(makeEvent("Err", fmt.Sprintf("common error %d", i), "p", "production"))
	}

	results, _ := s.SearchIssues("common", 2)
	if len(results) > 2 {
		t.Errorf("expected at most 2 results, got %d", len(results))
	}
}

// ── GetStats ──────────────────────────────────────────────────────────────────

func TestGetStats_Counts(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("ErrA", "a", "p", "production"))
	s.SaveEvent(makeEvent("ErrB", "b", "p", "production"))
	s.SaveEvent(makeEvent("ErrA", "a", "p", "production")) // duplicate → count++

	all, _, _ := s.ListIssues("", "", "", 10, 0)
	s.UpdateIssueStatus(all[0].ID, "resolved")

	stats, err := s.GetStats("")
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalIssues != 2 {
		t.Errorf("expected TotalIssues=2, got %d", stats.TotalIssues)
	}
	if stats.TotalEvents != 3 {
		t.Errorf("expected TotalEvents=3, got %d", stats.TotalEvents)
	}
	if stats.UnresolvedIssues != 1 {
		t.Errorf("expected UnresolvedIssues=1, got %d", stats.UnresolvedIssues)
	}
}

func TestGetStats_24hWindow(t *testing.T) {
	s := newTestStorage(t)

	// Recent event
	recent := makeEvent("Err", "recent", "p", "production")
	recent.Timestamp = time.Now().UTC()
	s.SaveEvent(recent)

	// Old event (outside 24h window) — insert directly into SQLite with a past timestamp
	old := makeEvent("Err", "old-issue-unique", "p", "production")
	old.Fingerprint = "old-fingerprint-unique"
	old.Timestamp = time.Now().UTC().Add(-48 * time.Hour)
	oldJSON, _ := json.Marshal(old)
	oldTime := old.Timestamp.Format(time.RFC3339Nano)
	oldID := fmt.Sprintf("%x", md5.Sum([]byte(old.Fingerprint)))[:16]
	s.db.Exec(
		`INSERT INTO issues(id,fingerprint,title,level,platform,environment,project_key,count,status,first_seen,last_seen,last_event)
		 VALUES(?,?,?,?,?,?,?,1,'unresolved',?,?,?)`,
		oldID, old.Fingerprint, "Old error", "error", "go", "production", "p",
		oldTime, oldTime, string(oldJSON),
	)

	stats, _ := s.GetStats("")
	if stats.TotalIssues != 2 {
		t.Errorf("expected TotalIssues=2, got %d", stats.TotalIssues)
	}
	if stats.EventsLast24h != 1 {
		t.Errorf("expected EventsLast24h=1, got %d", stats.EventsLast24h)
	}
}

func TestGetStats_FilterByProject(t *testing.T) {
	s := newTestStorage(t)
	s.SaveEvent(makeEvent("Err", "x", "alpha", "production"))
	s.SaveEvent(makeEvent("Err", "y", "beta", "production"))

	stats, _ := s.GetStats("alpha")
	if stats.TotalIssues != 1 {
		t.Errorf("expected 1 issue for alpha, got %d", stats.TotalIssues)
	}
}

// ── Concurrency ───────────────────────────────────────────────────────────────

func TestSaveEvent_ConcurrentSafety(t *testing.T) {
	s := newTestStorage(t)
	done := make(chan struct{})

	const goroutines = 50
	for i := range goroutines {
		go func(i int) {
			s.SaveEvent(makeEvent("Err", fmt.Sprintf("err-%d", i), "p", "production"))
			done <- struct{}{}
		}(i)
	}
	for range goroutines {
		<-done
	}

	_, total, _ := s.ListIssues("", "", "", 100, 0)
	if total != goroutines {
		t.Errorf("expected %d issues after concurrent saves, got %d", goroutines, total)
	}
}
