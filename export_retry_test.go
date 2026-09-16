package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// scriptedExport replays a fixed sequence of ExportStatus results so the retry
// path can be exercised without a live export task.
type scriptedExport struct {
	*mockClient
	statuses []exportResult
	errs     []error
	creates  int
	polls    int
}

func (s *scriptedExport) ExportCreate(ctx context.Context, token, fileType, fileExtension string) (string, error) {
	s.creates++
	return "ticket", nil
}

func (s *scriptedExport) ExportStatus(ctx context.Context, ticket, token string) (exportResult, error) {
	i := s.polls
	s.polls++
	if i < len(s.errs) && s.errs[i] != nil {
		return exportResult{}, s.errs[i]
	}
	if i < len(s.statuses) {
		return s.statuses[i], nil
	}
	return exportResult{JobStatus: 1}, nil
}

func withFastPolling(t *testing.T) {
	t.Helper()
	prev := exportPollInterval
	exportPollInterval = time.Millisecond
	t.Cleanup(func() { exportPollInterval = prev })
}

func TestExportJobRetryable(t *testing.T) {
	t.Parallel()
	// Feishu spells its server-side job timeout "job_timetout".
	for _, msg := range []string{"export failed: job_timetout", "Job Timeout", "timeout"} {
		if !exportJobRetryable(msg) {
			t.Errorf("exportJobRetryable(%q) = false, want true", msg)
		}
	}
	for _, msg := range []string{"export failed: permission denied", "job_status=3"} {
		if exportJobRetryable(msg) {
			t.Errorf("exportJobRetryable(%q) = true, want false", msg)
		}
	}
}

func TestRunExportRetriesServerTimeout(t *testing.T) {
	withFastPolling(t)
	c := &scriptedExport{
		mockClient: &mockClient{},
		statuses: []exportResult{
			{JobStatus: 3, ErrMsg: "job_timetout"},
			{JobStatus: 0, FileToken: "ft"},
		},
	}
	got, err := runExport(context.Background(), c, "tok", "docx", "pdf", time.Second, 2)
	if err != nil {
		t.Fatalf("runExport() error = %v", err)
	}
	if got.FileToken != "ft" {
		t.Errorf("FileToken = %q, want ft", got.FileToken)
	}
	if c.creates != 2 {
		t.Errorf("ExportCreate calls = %d, want 2 (task must be re-created)", c.creates)
	}
}

func TestRunExportDoesNotRetryHardFailure(t *testing.T) {
	withFastPolling(t)
	c := &scriptedExport{
		mockClient: &mockClient{},
		statuses:   []exportResult{{JobStatus: 3, ErrMsg: "permission denied"}},
	}
	_, err := runExport(context.Background(), c, "tok", "docx", "pdf", time.Second, 2)
	if err == nil {
		t.Fatal("expected an error")
	}
	if c.creates != 1 {
		t.Errorf("ExportCreate calls = %d, want 1 (non-retryable failure)", c.creates)
	}
}

func TestWaitForExportToleratesTransientPollErrors(t *testing.T) {
	withFastPolling(t)
	c := &scriptedExport{
		mockClient: &mockClient{},
		errs:       []error{errors.New("connection reset"), nil},
		statuses:   []exportResult{{}, {JobStatus: 0, FileToken: "ft"}},
	}
	got, err := waitForExport(context.Background(), c, "ticket", "tok", time.Second)
	if err != nil {
		t.Fatalf("waitForExport() error = %v", err)
	}
	if got.FileToken != "ft" {
		t.Errorf("FileToken = %q, want ft", got.FileToken)
	}
}

func TestWaitForExportTimeoutMessageIsActionable(t *testing.T) {
	withFastPolling(t)
	c := &scriptedExport{mockClient: &mockClient{}, statuses: []exportResult{{JobStatus: 1}}}
	_, err := waitForExport(context.Background(), c, "ticket", "tok", 5*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "--timeout") {
		t.Errorf("error = %v, want a hint about --timeout", err)
	}
}
