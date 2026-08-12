package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---------- helpers ----------

// okEnvelope returns a JSON envelope with code=ok and data.
func okEnvelope(data any) []byte {
	b, _ := json.Marshal(map[string]any{"code": "ok", "message": "ok", "data": data})
	return b
}

// errEnvelope returns a JSON envelope with an error code.
func errEnvelope(code, msg string) []byte {
	b, _ := json.Marshal(map[string]any{"code": code, "message": msg})
	return b
}

// feishuOK returns a Feishu-style success response (code=0, data).
func feishuOK(data any) []byte {
	b, _ := json.Marshal(map[string]any{"code": 0, "msg": "ok", "data": data})
	return b
}

// newGatewayTestClient creates a GatewayClient pointed at the given httptest server.
func newGatewayTestClient(ts *httptest.Server) *GatewayClient {
	c := NewGatewayClient(ts.URL)
	c.SetSessionToken("test-token")
	return c
}

// newLocalTestClient creates a LocalClient pointed at the given httptest server
// with valid tokens that won't expire soon.
func newLocalTestClient(ts *httptest.Server) *LocalClient {
	c := NewLocalClient("app-id", "app-secret")
	c.baseURL = ts.URL
	c.SetTokens("test-access-token", "test-refresh-token", time.Now().Add(1*time.Hour))
	return c
}

// ==================== APIError tests ====================

func TestAPIError_Error(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  *APIError
		want string
	}{
		{"nil", nil, ""},
		{"no code", &APIError{Status: 500, Message: "internal"}, "gateway error (status=500): internal"},
		{"with code", &APIError{Status: 401, Code: "unauthorized", Message: "bad token"}, "gateway error [unauthorized] (status=401): bad token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.err.Error()
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

// ==================== Gateway Client tests ====================

func TestGateway_CheckVersion(t *testing.T) {
	t.Parallel()

	t.Run("matching version", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/version" {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			w.Write(okEnvelope(map[string]any{"protocol_version": ProtocolVersion, "max_security_level": 4}))
		}))
		defer ts.Close()

		c := NewGatewayClient(ts.URL)
		v, err := c.CheckVersion(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v.ProtocolVersion != ProtocolVersion {
			t.Fatalf("version = %q, want %q", v.ProtocolVersion, ProtocolVersion)
		}
		if v.MaxSecurityLevel != 4 {
			t.Fatalf("max_security_level = %d, want 4", v.MaxSecurityLevel)
		}
	})

	t.Run("version mismatch", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(okEnvelope(map[string]any{"protocol_version": "v999"}))
		}))
		defer ts.Close()

		c := NewGatewayClient(ts.URL)
		_, err := c.CheckVersion(context.Background())
		if err == nil || !strings.Contains(err.Error(), "protocol version mismatch") {
			t.Fatalf("expected version mismatch error, got: %v", err)
		}
	})
}

func TestGateway_StartDeviceLogin(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/device/start" || r.Method != http.MethodPost {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["client"] != "larkctl" {
			t.Fatalf("missing client field")
		}
		w.Write(okEnvelope(map[string]any{
			"device_code":               "DEV123",
			"user_code":                 "USER456",
			"verification_uri":          "https://example.com/verify",
			"verification_uri_complete": "https://example.com/verify?code=USER456",
			"interval_seconds":          5,
			"expires_in_seconds":        600,
		}))
	}))
	defer ts.Close()

	c := NewGatewayClient(ts.URL)
	resp, err := c.StartDeviceLogin(context.Background(), "docs", "old-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.DeviceCode != "DEV123" || resp.UserCode != "USER456" {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.IntervalSeconds != 5 || resp.ExpiresInSeconds != 600 {
		t.Fatalf("unexpected timing: %+v", resp)
	}
}

func TestGateway_PollDeviceLogin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		respCode    string
		respData    map[string]any
		wantPending bool
		wantErr     bool
		wantWaitMS  int
	}{
		{
			name:     "completed",
			respCode: "ok",
			respData: map[string]any{
				"pending":      false,
				"client_token": "ct-abc",
			},
		},
		{
			name:        "pending",
			respCode:    "authorization_pending",
			wantPending: true,
		},
		{
			name:        "slow_down",
			respCode:    "slow_down",
			wantPending: true,
			wantWaitMS:  5000,
		},
		{
			name:     "error",
			respCode: "expired_token",
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				resp := map[string]any{"code": tt.respCode, "message": "msg"}
				if tt.respData != nil {
					data, _ := json.Marshal(tt.respData)
					resp["data"] = json.RawMessage(data)
				}
				b, _ := json.Marshal(resp)
				w.Write(b)
			}))
			defer ts.Close()

			c := NewGatewayClient(ts.URL)
			resp, err := c.PollDeviceLogin(context.Background(), "device-code-123")
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Pending != tt.wantPending {
				t.Fatalf("pending = %v, want %v", resp.Pending, tt.wantPending)
			}
			if tt.wantWaitMS > 0 && resp.RecommendedWaitMS != tt.wantWaitMS {
				t.Fatalf("wait_ms = %d, want %d", resp.RecommendedWaitMS, tt.wantWaitMS)
			}
			if tt.respData != nil && !tt.wantPending {
				if resp.ClientToken != "ct-abc" {
					t.Fatalf("client_token = %q", resp.ClientToken)
				}
			}
		})
	}
}

func TestGateway_WhoAmI(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/whoami" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Fatalf("bad auth header: %s", r.Header.Get("Authorization"))
		}
		w.Write(okEnvelope(map[string]any{"user_id": "u123", "name": "Test User"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	m, err := c.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["user_id"] != "u123" {
		t.Fatalf("unexpected user_id: %v", m["user_id"])
	}
}

func TestGateway_Logout(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete || r.URL.Path != "/v1/session" {
				t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			}
			w.Write(errEnvelope("ok", "logged out"))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		if err := c.Logout(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("session_not_found is ok", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(errEnvelope("session_not_found", "no session"))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		if err := c.Logout(context.Background()); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("other error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write(errEnvelope("internal_error", "oops"))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		err := c.Logout(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestGateway_GetDocumentBlocks(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/docs/blocks" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("document_id") != "doc123" {
			t.Fatalf("missing document_id query param")
		}
		w.Write(okEnvelope(map[string]any{"items": []any{"block1", "block2"}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	data, err := c.GetDocumentBlocks(context.Background(), "doc123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := data.(map[string]any)
	items := m["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestGateway_GetWikiNode(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "wiki-token-123" {
			t.Fatalf("bad token query: %s", r.URL.Query().Get("token"))
		}
		w.Write(okEnvelope(map[string]any{"node": map[string]any{"obj_token": "obj123"}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	data, err := c.GetWikiNode(context.Background(), "wiki-token-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	node := data.(map[string]any)["node"].(map[string]any)
	if node["obj_token"] != "obj123" {
		t.Fatalf("unexpected obj_token: %v", node["obj_token"])
	}
}

func TestGateway_GetSheetsMeta(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("spreadsheet_token") != "sheet-abc" {
			t.Fatalf("bad query param")
		}
		w.Write(okEnvelope(map[string]any{"spreadsheet": map[string]any{"title": "My Sheet"}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	data, err := c.GetSheetsMeta(context.Background(), "sheet-abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("expected data")
	}
}

func TestGateway_GetSheetValues(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("spreadsheet_token") != "stoken" || q.Get("sheet_id") != "sheet1" || q.Get("range") != "A1:B2" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"values": []any{[]any{"a", "b"}}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	data, err := c.GetSheetValues(context.Background(), "stoken", "sheet1", "A1:B2", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("expected data")
	}
}

func TestGateway_GetDocumentComments(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("file_token") != "ft1" || q.Get("file_type") != "docx" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"comments": []any{}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.GetDocumentComments(context.Background(), "ft1", "docx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_CreateDocumentBlocks(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/docs/blocks/create" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("document_id") != "doc1" || q.Get("block_id") != "blk1" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"created": true}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	data, err := c.CreateDocumentBlocks(context.Background(), "doc1", "blk1", map[string]any{"children": []any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("expected data")
	}
}

func TestGateway_DeleteDocumentBlock(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("document_id") != "doc1" || q.Get("block_id") != "blk1" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"deleted": true}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.DeleteDocumentBlock(context.Background(), "doc1", "blk1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_UpdateDocumentBlocks(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("document_id") != "doc1" {
			t.Fatal("missing document_id")
		}
		w.Write(okEnvelope(map[string]any{"updated": true}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.UpdateDocumentBlocks(context.Background(), "doc1", map[string]any{"blocks": []any{}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_SearchDocs(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/docs/search" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Write(okEnvelope(map[string]any{"docs": []any{}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.SearchDocs(context.Background(), map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_CreateDocument(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/docs/create" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Write(okEnvelope(map[string]any{"document_id": "new-doc-123"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	data, err := c.CreateDocument(context.Background(), map[string]any{"title": "New Doc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := data.(map[string]any)
	if m["document_id"] != "new-doc-123" {
		t.Fatalf("unexpected document_id: %v", m["document_id"])
	}
}

func TestGateway_CreateDocumentComment(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("file_token") != "ft1" || q.Get("file_type") != "docx" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"comment_id": "c1"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.CreateDocumentComment(context.Background(), "ft1", "docx", map[string]any{"content": "hello"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_ReplyDocumentComment(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("file_token") != "ft1" || q.Get("comment_id") != "c1" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"reply_id": "r1"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.ReplyDocumentComment(context.Background(), "ft1", "c1", "docx", map[string]any{"content": "reply"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_ResolveDocumentComment(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("file_token") != "ft1" || q.Get("comment_id") != "c1" {
			t.Fatalf("bad query: %v", q)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["is_solved"] != true {
			t.Fatalf("is_solved should be true, got %v", body["is_solved"])
		}
		w.Write(okEnvelope(map[string]any{"resolved": true}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.ResolveDocumentComment(context.Background(), "ft1", "c1", "docx", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_ListDocumentPermissions(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("token") != "tok1" || q.Get("file_type") != "docx" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"members": []any{}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.ListDocumentPermissions(context.Background(), "tok1", "docx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_ListDriveFiles(t *testing.T) {
	t.Parallel()

	t.Run("with folder token", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("folder_token") != "fld1" {
				t.Fatalf("missing folder_token")
			}
			w.Write(okEnvelope(map[string]any{"files": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListDriveFiles(context.Background(), "fld1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("without folder token", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("folder_token") != "" {
				t.Fatalf("should not have folder_token")
			}
			w.Write(okEnvelope(map[string]any{"files": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListDriveFiles(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGateway_CreateFolder(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.Write(okEnvelope(map[string]any{"token": "new-folder-token"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.CreateFolder(context.Background(), map[string]any{"name": "New Folder"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_BitableMethods(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(okEnvelope(map[string]any{"ok": true}))
	})

	t.Run("GetBitableMeta", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.GetBitableMeta(context.Background(), "app1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListBitableTables", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListBitableTables(context.Background(), "app1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListBitableFields", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListBitableFields(context.Background(), "app1", "tbl1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListBitableRecords", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("app_token") != "app1" || q.Get("table_id") != "tbl1" {
				t.Fatalf("bad query: %v", q)
			}
			if q.Get("filter") != "value > 10" {
				t.Fatalf("missing extra param filter: %v", q)
			}
			w.Write(okEnvelope(map[string]any{"records": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		extra := url.Values{"filter": {"value > 10"}}
		_, err := c.ListBitableRecords(context.Background(), "app1", "tbl1", extra)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateBitableRecord", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.CreateBitableRecord(context.Background(), "app1", "tbl1", map[string]any{"fields": map[string]any{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("UpdateBitableRecord", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("record_id") != "rec1" {
				t.Fatalf("missing record_id: %v", q)
			}
			w.Write(okEnvelope(map[string]any{"updated": true}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.UpdateBitableRecord(context.Background(), "app1", "tbl1", "rec1", map[string]any{"fields": map[string]any{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateBitableField", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.CreateBitableField(context.Background(), "app1", "tbl1", map[string]any{"field_name": "F", "type": 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("UpdateBitableField", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("field_id") != "fld1" {
				t.Fatalf("missing field_id: %v", q)
			}
			w.Write(okEnvelope(map[string]any{"field": map[string]any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.UpdateBitableField(context.Background(), "app1", "tbl1", "fld1", map[string]any{"field_name": "F2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("DeleteBitableField", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("field_id") != "fld1" {
				t.Fatalf("missing field_id: %v", q)
			}
			w.Write(okEnvelope(map[string]any{"deleted": true}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.DeleteBitableField(context.Background(), "app1", "tbl1", "fld1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGateway_SheetWriteMethods(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("spreadsheet_token") != "st1" {
			t.Fatalf("missing spreadsheet_token")
		}
		w.Write(okEnvelope(map[string]any{"updated": true}))
	})

	t.Run("UpdateSheetValues", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.UpdateSheetValues(context.Background(), "st1", map[string]any{"values": []any{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("AppendSheetValues", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.AppendSheetValues(context.Background(), "st1", map[string]any{"values": []any{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGateway_WikiMethods(t *testing.T) {
	t.Parallel()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(okEnvelope(map[string]any{"items": []any{}}))
	})

	t.Run("ListWikiSpaces", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListWikiSpaces(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListWikiNodes", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("space_id") != "sp1" {
				t.Fatal("missing space_id")
			}
			if q.Get("parent_node_token") != "pn1" {
				t.Fatal("missing parent_node_token")
			}
			w.Write(okEnvelope(map[string]any{"nodes": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListWikiNodes(context.Background(), "sp1", "pn1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListWikiNodes no parent", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("parent_node_token") != "" {
				t.Fatal("should not have parent_node_token")
			}
			w.Write(okEnvelope(map[string]any{"nodes": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListWikiNodes(context.Background(), "sp1", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateWikiNode", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("space_id") != "sp1" {
				t.Fatal("missing space_id")
			}
			w.Write(okEnvelope(map[string]any{"node_token": "new-node"}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.CreateWikiNode(context.Background(), "sp1", map[string]any{"title": "New Node"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGateway_GetBoardNodes(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("whiteboard_id") != "wb1" {
			t.Fatal("missing whiteboard_id")
		}
		w.Write(okEnvelope(map[string]any{"nodes": []any{}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.GetBoardNodes(context.Background(), "wb1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_CreateTask(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/tasks/create" {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Write(okEnvelope(map[string]any{"task_id": "task-1"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.CreateTask(context.Background(), map[string]any{"summary": "Do something"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_SearchUsers(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["query"] != "john" {
			t.Fatalf("unexpected query: %v", body["query"])
		}
		w.Write(okEnvelope(map[string]any{"users": []any{map[string]any{"user_id": "u1"}}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.SearchUsers(context.Background(), "john", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_MessageReactions(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/im/messages/reactions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["emoji_type"] != "OK" || body["message_id"] != "om_1" {
				t.Fatalf("unexpected body: %v", body)
			}
		case http.MethodGet:
			if r.URL.Query().Get("message_id") != "om_1" {
				t.Fatalf("missing message_id")
			}
		case http.MethodDelete:
			if r.URL.Query().Get("reaction_id") != "rid_1" {
				t.Fatalf("missing reaction_id")
			}
		}
		w.Write(okEnvelope(map[string]any{"reaction_id": "rid_1"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	if _, err := c.ReactMessage(context.Background(), "om_1", "OK"); err != nil {
		t.Fatalf("react: %v", err)
	}
	if _, err := c.ListMessageReactions(context.Background(), "om_1", nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := c.DeleteMessageReaction(context.Background(), "om_1", "rid_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestGateway_UploadIMResources(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		switch r.URL.Path {
		case "/v1/im/images/upload":
			if r.FormValue("image_type") != "message" {
				t.Fatalf("image_type = %q", r.FormValue("image_type"))
			}
			w.Write(okEnvelope(map[string]any{"image_key": "img_v3_ok"}))
		case "/v1/im/files/upload":
			if r.FormValue("file_type") != "pdf" || r.FormValue("file_name") != "report.pdf" {
				t.Fatalf("file fields = %q %q", r.FormValue("file_type"), r.FormValue("file_name"))
			}
			w.Write(okEnvelope(map[string]any{"file_key": "file_v3_ok"}))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	imageKey, err := c.UploadIMImage(context.Background(), "pic.png", strings.NewReader("png-bytes"))
	if err != nil || imageKey != "img_v3_ok" {
		t.Fatalf("image: key=%q err=%v", imageKey, err)
	}
	fileKey, err := c.UploadIMFile(context.Background(), "pdf", "report.pdf", strings.NewReader("pdf-bytes"))
	if err != nil || fileKey != "file_v3_ok" {
		t.Fatalf("file: key=%q err=%v", fileKey, err)
	}
}

func TestGateway_CalendarMethods(t *testing.T) {
	t.Parallel()

	t.Run("GetCalendarPrimary", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost || r.URL.Path != "/v1/calendar/primary" {
				t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
			}
			w.Write(okEnvelope(map[string]any{"calendar_id": "cal1"}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.GetCalendarPrimary(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListCalendarEvents", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("calendar_id") != "cal1" || q.Get("start_time") != "2025-01-01" || q.Get("end_time") != "2025-01-31" {
				t.Fatalf("bad query: %v", q)
			}
			w.Write(okEnvelope(map[string]any{"events": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListCalendarEvents(context.Background(), "cal1", "2025-01-01", "2025-01-31")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListCalendarEvents no times", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("start_time") != "" || q.Get("end_time") != "" {
				t.Fatal("should not have time params")
			}
			w.Write(okEnvelope(map[string]any{"events": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListCalendarEvents(context.Background(), "cal1", "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateCalendarEvent", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("calendar_id") != "cal1" {
				t.Fatal("missing calendar_id")
			}
			w.Write(okEnvelope(map[string]any{"event_id": "evt1"}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.CreateCalendarEvent(context.Background(), "cal1", map[string]any{"summary": "Meeting"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("AddCalendarAttendees", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("calendar_id") != "cal1" || q.Get("event_id") != "evt1" {
				t.Fatalf("bad query: %v", q)
			}
			w.Write(okEnvelope(map[string]any{"attendees": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.AddCalendarAttendees(context.Background(), "cal1", "evt1", map[string]any{"attendees": []any{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGateway_GetFreebusy(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(okEnvelope(map[string]any{"freebusy": []any{}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.GetFreebusy(context.Background(), map[string]any{"user_ids": []string{"u1"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_ListRooms(t *testing.T) {
	t.Parallel()

	t.Run("with keyword", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("keyword") != "room-a" {
				t.Fatal("missing keyword")
			}
			w.Write(okEnvelope(map[string]any{"rooms": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListRooms(context.Background(), "room-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no keyword", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("keyword") != "" {
				t.Fatal("should not have keyword")
			}
			w.Write(okEnvelope(map[string]any{"rooms": []any{}}))
		}))
		defer ts.Close()
		c := newGatewayTestClient(ts)
		_, err := c.ListRooms(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestGateway_UpgradeScopes(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["scopes"] != "calendar:event" {
			t.Fatalf("unexpected scopes: %v", body["scopes"])
		}
		w.Write(okEnvelope(map[string]any{"upgraded": true}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	m, err := c.UpgradeScopes(context.Background(), "calendar:event")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["upgraded"] != true {
		t.Fatalf("unexpected response: %v", m)
	}
}

func TestGateway_GetSessionScopes(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(okEnvelope(map[string]any{"scopes": "docs,wiki,sheets"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	scopes, err := c.GetSessionScopes(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if scopes != "docs,wiki,sheets" {
		t.Fatalf("unexpected scopes: %q", scopes)
	}
}

func TestGateway_ExportCreate(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["token"] != "doc1" || body["type"] != "docx" || body["file_extension"] != "pdf" {
			t.Fatalf("bad body: %v", body)
		}
		w.Write(okEnvelope(map[string]any{"ticket": "ticket-abc"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	ticket, err := c.ExportCreate(context.Background(), "doc1", "docx", "pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticket != "ticket-abc" {
		t.Fatalf("unexpected ticket: %q", ticket)
	}
}

func TestGateway_ExportStatus(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			if q.Get("ticket") != "t1" || q.Get("token") != "tok1" {
				t.Fatalf("bad query: %v", q)
			}
			w.Write(okEnvelope(map[string]any{
				"result": map[string]any{
					"job_status": float64(2),
					"file_token": "file-tok",
					"file_size":  float64(1024),
					"file_name":  "export.pdf",
				},
			}))
		}))
		defer ts.Close()

		c := newGatewayTestClient(ts)
		result, err := c.ExportStatus(context.Background(), "t1", "tok1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.JobStatus != 2 || result.FileToken != "file-tok" || result.FileSize != 1024 || result.FileName != "export.pdf" {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("no result", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(okEnvelope(map[string]any{}))
		}))
		defer ts.Close()

		c := newGatewayTestClient(ts)
		_, err := c.ExportStatus(context.Background(), "t1", "tok1")
		if err == nil || !strings.Contains(err.Error(), "no result") {
			t.Fatalf("expected 'no result' error, got: %v", err)
		}
	})
}

func TestGateway_ExportDownload(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("file_token") != "ft1" {
				t.Fatal("missing file_token")
			}
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Fatal("bad auth")
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("file-content-here"))
		}))
		defer ts.Close()

		c := newGatewayTestClient(ts)
		var buf bytes.Buffer
		err := c.ExportDownload(context.Background(), "ft1", &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.String() != "file-content-here" {
			t.Fatalf("unexpected content: %q", buf.String())
		}
	})

	t.Run("missing session token", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatal("should not reach server")
		}))
		defer ts.Close()

		c := NewGatewayClient(ts.URL) // no session token set
		var buf bytes.Buffer
		err := c.ExportDownload(context.Background(), "ft1", &buf)
		if err == nil || !strings.Contains(err.Error(), "missing session token") {
			t.Fatalf("expected missing token error, got: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("server error"))
		}))
		defer ts.Close()

		c := newGatewayTestClient(ts)
		var buf bytes.Buffer
		err := c.ExportDownload(context.Background(), "ft1", &buf)
		if err == nil || !strings.Contains(err.Error(), "status=500") {
			t.Fatalf("expected download failed error, got: %v", err)
		}
	})
}

func TestGateway_GetSheetsFloatImages(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("spreadsheet_token") != "st1" || q.Get("sheet_id") != "s1" {
			t.Fatalf("bad query: %v", q)
		}
		w.Write(okEnvelope(map[string]any{"images": []any{}}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, err := c.GetSheetsFloatImages(context.Background(), "st1", "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_DownloadMedia(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("file_token") != "media1" {
				t.Fatal("missing file_token")
			}
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("PNG-DATA"))
		}))
		defer ts.Close()

		c := newGatewayTestClient(ts)
		var buf bytes.Buffer
		ct, err := c.DownloadMedia(context.Background(), "media1", &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ct != "image/png" {
			t.Fatalf("unexpected content type: %q", ct)
		}
		if buf.String() != "PNG-DATA" {
			t.Fatalf("unexpected content: %q", buf.String())
		}
	})

	t.Run("missing token", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
		defer ts.Close()

		c := NewGatewayClient(ts.URL)
		var buf bytes.Buffer
		_, err := c.DownloadMedia(context.Background(), "media1", &buf)
		if err == nil || !strings.Contains(err.Error(), "missing session token") {
			t.Fatalf("expected missing token error, got: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}))
		defer ts.Close()

		c := newGatewayTestClient(ts)
		var buf bytes.Buffer
		_, err := c.DownloadMedia(context.Background(), "media1", &buf)
		if err == nil || !strings.Contains(err.Error(), "status=403") {
			t.Fatalf("expected download failed error, got: %v", err)
		}
	})
}

// ==================== callJSONWithRefresh edge cases ====================

func TestGateway_callJSONWithRefresh_NoRefreshOnNonAuthError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return a non-401 error - should not trigger refresh
		w.WriteHeader(http.StatusBadRequest)
		w.Write(errEnvelope("bad_request", "invalid input"))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	var out any
	err := c.callJSONWithRefresh(context.Background(), "GET", "/v1/test", nil, nil, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *APIError
	if !strings.Contains(err.Error(), "bad_request") {
		t.Fatalf("expected bad_request error, got: %v", err)
	}
	_ = apiErr
}

func TestGateway_callJSONWithRefresh_RefreshOnAccessTokenRefreshFailed(t *testing.T) {
	t.Parallel()

	var calls int32
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/test":
			n := atomic.AddInt32(&calls, 1)
			if n == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				w.Write(errEnvelope("access_token_refresh_failed", "token expired"))
				return
			}
			w.Write(okEnvelope(map[string]any{"ok": true}))
		case "/v1/session/refresh":
			w.Write(okEnvelope(map[string]any{"refreshed": true}))
		}
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	var out any
	err := c.callJSONWithRefresh(context.Background(), "GET", "/v1/test", nil, nil, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_callJSONWithRefresh_RefreshFails(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/test":
			w.WriteHeader(http.StatusUnauthorized)
			w.Write(errEnvelope("session_expired", "expired"))
		case "/v1/session/refresh":
			// Refresh also fails
			w.WriteHeader(http.StatusUnauthorized)
			w.Write(errEnvelope("invalid_token", "bad token"))
		}
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	var out any
	err := c.callJSONWithRefresh(context.Background(), "GET", "/v1/test", nil, nil, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	// Should return the original error, not the refresh error
	if !strings.Contains(err.Error(), "session_expired") {
		t.Fatalf("expected session_expired error, got: %v", err)
	}
}

func TestGateway_callJSONWithRefresh_NonAPIError(t *testing.T) {
	t.Parallel()

	// Server that immediately closes the connection
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Close the connection abruptly by hijacking
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	var out any
	err := c.callJSONWithRefresh(context.Background(), "GET", "/v1/test", nil, nil, &out)
	if err == nil {
		t.Fatal("expected error")
	}
}

// ==================== callJSON error paths ====================

func TestGateway_callJSON_ErrorResponse(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write(errEnvelope("forbidden", "access denied"))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	var out any
	err := c.callJSON(context.Background(), "GET", "/v1/test", nil, nil, true, &out)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != "forbidden" || apiErr.Status != 403 {
		t.Fatalf("unexpected error: %+v", apiErr)
	}
}

func TestGateway_callJSON_NilOutput(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(okEnvelope(map[string]any{"data": "value"}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	// nil output should not fail
	err := c.callJSON(context.Background(), "GET", "/v1/test", nil, nil, true, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_callJSON_EmptyData(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":"ok","message":"ok"}`))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	var out map[string]any
	err := c.callJSON(context.Background(), "GET", "/v1/test", nil, nil, true, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ==================== requestEnvelope edge cases ====================

func TestGateway_requestEnvelope_NonJSONSuccess(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("plain text response"))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	status, env, err := c.requestEnvelope(context.Background(), "GET", "/v1/test", nil, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 200 || env.Code != "ok" {
		t.Fatalf("unexpected status=%d code=%q", status, env.Code)
	}
	if string(env.Data) != `plain text response` {
		t.Fatalf("unexpected data: %s", string(env.Data))
	}
}

func TestGateway_requestEnvelope_NonJSONError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>Bad Gateway</html>"))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	_, _, err := c.requestEnvelope(context.Background(), "GET", "/v1/test", nil, nil, true)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != "invalid_response" {
		t.Fatalf("expected invalid_response code, got %q", apiErr.Code)
	}
}

func TestGateway_requestEnvelope_MissingAuth(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach server")
	}))
	defer ts.Close()

	c := NewGatewayClient(ts.URL)
	// No session token, auth=true
	_, _, err := c.requestEnvelope(context.Background(), "GET", "/v1/test", nil, nil, true)
	if err == nil || !strings.Contains(err.Error(), "missing session token") {
		t.Fatalf("expected missing token error, got: %v", err)
	}
}

func TestGateway_requestEnvelope_NoAuthOK(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Fatal("should not have Authorization header")
		}
		w.Write(okEnvelope(nil))
	}))
	defer ts.Close()

	c := NewGatewayClient(ts.URL) // no token
	_, env, err := c.requestEnvelope(context.Background(), "GET", "/v1/test", nil, nil, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.Code != "ok" {
		t.Fatalf("expected ok, got %q", env.Code)
	}
}

func TestGateway_requestEnvelope_EmptyCodeHttp4xx(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"message":"not found"}`))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	status, env, err := c.requestEnvelope(context.Background(), "GET", "/v1/test", nil, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != 404 {
		t.Fatalf("expected 404, got %d", status)
	}
	if env.Code != "http_error" {
		t.Fatalf("expected http_error, got %q", env.Code)
	}
	if env.Message != "not found" {
		t.Fatalf("expected message 'not found', got %q", env.Message)
	}
}

func TestGateway_requestEnvelope_QueryParams(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("key1") != "val1" || r.URL.Query().Get("key2") != "val2" {
			t.Fatalf("bad query: %v", r.URL.Query())
		}
		w.Write(okEnvelope(nil))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	query := url.Values{"key1": {"val1"}, "key2": {"val2"}}
	_, _, err := c.requestEnvelope(context.Background(), "GET", "/v1/test", query, nil, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGateway_requestEnvelope_MarshalError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach server")
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	// Channel cannot be marshaled to JSON
	_, _, err := c.requestEnvelope(context.Background(), "POST", "/v1/test", nil, make(chan int), true)
	if err == nil || !strings.Contains(err.Error(), "marshal request body") {
		t.Fatalf("expected marshal error, got: %v", err)
	}
}

func TestGateway_ClientVersionHeader(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Client-Version") != ProtocolVersion {
			t.Fatalf("expected X-Client-Version=%q, got %q", ProtocolVersion, r.Header.Get("X-Client-Version"))
		}
		w.Write(okEnvelope(nil))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	c.WhoAmI(context.Background())
}

func TestGateway_RefreshSession(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(okEnvelope(map[string]any{"refreshed": true}))
		}))
		defer ts.Close()

		c := newGatewayTestClient(ts)
		err := c.RefreshSession(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no token", func(t *testing.T) {
		c := NewGatewayClient("http://unused")
		err := c.RefreshSession(context.Background())
		if err == nil || !strings.Contains(err.Error(), "no client token") {
			t.Fatalf("expected no client token error, got: %v", err)
		}
	})
}

// ==================== Local Client tests ====================

func TestLocal_EnsureToken_NotLoggedIn(t *testing.T) {
	t.Parallel()
	c := NewLocalClient("app", "secret")
	err := c.ensureToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("expected not logged in error, got: %v", err)
	}
}

func TestLocal_EnsureToken_ValidToken(t *testing.T) {
	t.Parallel()
	c := NewLocalClient("app", "secret")
	c.SetTokens("access", "refresh", time.Now().Add(1*time.Hour))
	err := c.ensureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_EnsureToken_ExpiredNoRefreshToken(t *testing.T) {
	t.Parallel()
	c := NewLocalClient("app", "secret")
	c.SetTokens("access", "", time.Now().Add(-1*time.Hour))
	err := c.ensureToken(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no refresh token") {
		t.Fatalf("expected no refresh token error, got: %v", err)
	}
}

func TestLocal_EnsureToken_RefreshesExpiredToken(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/authen/v2/oauth/token" {
			w.Write(feishuOK(map[string]any{
				"access_token":  "new-access-token",
				"refresh_token": "new-refresh-token",
				"expires_in":    float64(7200),
			}))
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer ts.Close()

	c := NewLocalClient("app", "secret")
	c.baseURL = ts.URL
	c.SetTokens("old-access", "old-refresh", time.Now().Add(-1*time.Minute)) // expired

	err := c.ensureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token() != "new-access-token" {
		t.Fatalf("token not refreshed: %q", c.token())
	}
}

func TestLocal_EnsureToken_RefreshFallbackV1(t *testing.T) {
	t.Parallel()

	var v2Called bool
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authen/v2/oauth/token":
			v2Called = true
			// V2 fails
			w.Write([]byte(`{"code":99999,"msg":"v2 not supported"}`))
		case "/authen/v1/refresh_access_token":
			if !v2Called {
				t.Fatal("v2 should be called first")
			}
			w.Write(feishuOK(map[string]any{
				"access_token":  "v1-access-token",
				"refresh_token": "v1-refresh-token",
				"expires_in":    float64(3600),
			}))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	c := NewLocalClient("app", "secret")
	c.baseURL = ts.URL
	c.SetTokens("old-access", "old-refresh", time.Now().Add(-1*time.Minute))

	err := c.ensureToken(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.token() != "v1-access-token" {
		t.Fatalf("token not refreshed via v1: %q", c.token())
	}
}

func TestLocal_FeishuRequest_Success(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-access-token" {
			t.Fatalf("bad auth: %s", r.Header.Get("Authorization"))
		}
		w.Write(feishuOK(map[string]any{"result": "ok"}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	data, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	m := data.(map[string]any)
	if m["result"] != "ok" {
		t.Fatalf("unexpected data: %v", data)
	}
}

func TestLocal_FeishuRequest_FeishuError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":99991668,"msg":"not authorized"}`))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "99991668") {
		t.Fatalf("expected feishu error, got: %v", err)
	}
}

func TestLocal_FeishuRequest_HTTP4xxWithMsg(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":0,"msg":"forbidden resource"}`))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "forbidden resource") {
		t.Fatalf("expected forbidden error, got: %v", err)
	}
}

func TestLocal_FeishuRequest_NonJSONSuccess(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("raw text"))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	data, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data != "raw text" {
		t.Fatalf("expected raw text, got: %v", data)
	}
}

func TestLocal_FeishuRequest_NonJSONError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("<html>error</html>"))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "upstream status=502") {
		t.Fatalf("expected upstream error, got: %v", err)
	}
}

func TestLocal_FeishuRequest_NoDataKey(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"code":0,"msg":"ok","items":[1,2,3]}`))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	data, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should return the entire payload when no "data" key
	m := data.(map[string]any)
	items := m["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
}

func TestLocal_FeishuRequest_WithQueryAndBody(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page_size") != "10" {
			t.Fatalf("missing query param")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["name"] != "test" {
			t.Fatalf("bad body: %v", body)
		}
		w.Write(feishuOK(map[string]any{"created": true}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	data, err := c.feishuRequest(context.Background(), "POST", "/test",
		url.Values{"page_size": {"10"}},
		map[string]any{"name": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data == nil {
		t.Fatal("expected data")
	}
}

// ==================== Local Client API methods ====================

func TestLocal_WhoAmI(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/authen/v1/user_info" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{
			"user_id":    "uid1",
			"name":       "John",
			"open_id":    "ou_abc",
			"email":      "john@test.com",
			"tenant_key": "tk1",
		}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	m, err := c.WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m["user_id"] != "uid1" || m["name"] != "John" {
		t.Fatalf("unexpected result: %v", m)
	}
}

func TestLocal_GetDocumentInfo(t *testing.T) {
	t.Parallel()

	t.Run("wiki type", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/wiki/") {
				t.Fatalf("expected wiki path, got %s", r.URL.Path)
			}
			w.Write(feishuOK(map[string]any{"node": map[string]any{"obj_token": "obj1"}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.GetDocumentInfo(context.Background(), "https://feishu.cn/wiki/abc123", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("docx type", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/docx/") {
				t.Fatalf("expected docx path, got %s", r.URL.Path)
			}
			w.Write(feishuOK(map[string]any{"document": map[string]any{"title": "Doc"}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.GetDocumentInfo(context.Background(), "doc-token-123", "document")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_GetDocumentBlocks(t *testing.T) {
	t.Parallel()

	t.Run("single page", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"items":      []any{"block1", "block2"},
				"page_token": "",
			}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		data, err := c.GetDocumentBlocks(context.Background(), "doc1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := data.(map[string]any)
		items := m["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
	})

	t.Run("paginated", func(t *testing.T) {
		var callNum int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&callNum, 1)
			if n == 1 {
				w.Write(feishuOK(map[string]any{
					"items":      []any{"block1"},
					"page_token": "page2",
				}))
			} else {
				w.Write(feishuOK(map[string]any{
					"items":      []any{"block2"},
					"page_token": "",
				}))
			}
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		data, err := c.GetDocumentBlocks(context.Background(), "doc1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := data.(map[string]any)
		items := m["items"].([]any)
		if len(items) != 2 {
			t.Fatalf("expected 2 items, got %d", len(items))
		}
		if m["total"] != 2 {
			t.Fatalf("expected total=2, got %v", m["total"])
		}
	})

	t.Run("wiki URL resolves token", func(t *testing.T) {
		var paths []string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			paths = append(paths, r.URL.Path)
			if strings.Contains(r.URL.Path, "get_node") {
				w.Write(feishuOK(map[string]any{
					"node": map[string]any{"obj_token": "resolved-doc-id"},
				}))
				return
			}
			w.Write(feishuOK(map[string]any{
				"items":      []any{"block1"},
				"page_token": "",
			}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.GetDocumentBlocks(context.Background(), "https://feishu.cn/wiki/abc123")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should have called get_node first
		if len(paths) < 2 {
			t.Fatalf("expected at least 2 calls, got %d", len(paths))
		}
	})
}

func TestLocal_GetWikiNode(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "wikitoken" {
			t.Fatalf("bad token: %s", r.URL.Query().Get("token"))
		}
		w.Write(feishuOK(map[string]any{"node": map[string]any{"title": "Wiki"}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetWikiNode(context.Background(), "wikitoken")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_GetSheetsMeta(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/sheets/v3/spreadsheets/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"spreadsheet": map[string]any{"title": "Sheet"}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetSheetsMeta(context.Background(), "sheet-token")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_GetSheetValues(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/values/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"values": []any{[]any{"a1"}}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetSheetValues(context.Background(), "sheet-token", "Sheet1", "A1:B2", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_GetSheetValues_NoRange(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Path should not contain "!"
		if strings.Contains(r.URL.Path, "!") {
			t.Fatalf("should not have range separator: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"values": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetSheetValues(context.Background(), "sheet-token", "Sheet1", "", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_GetDocumentComments(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/comments") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"items": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetDocumentComments(context.Background(), "ft1", "docx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_CreateDocumentBlocks(t *testing.T) {
	t.Parallel()

	t.Run("with block_id", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/blocks/blk1/children") {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Write(feishuOK(map[string]any{"created": true}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateDocumentBlocks(context.Background(), "doc1", "blk1", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("empty block_id defaults to doc_id", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// When blockID is empty, it should use docID
			if !strings.Contains(r.URL.Path, "/blocks/doc1/children") {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Write(feishuOK(map[string]any{"created": true}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateDocumentBlocks(context.Background(), "doc1", "", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_DeleteDocumentBlock(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		var callNum int32
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := atomic.AddInt32(&callNum, 1)
			if n == 1 {
				// GET block to find parent
				w.Write(feishuOK(map[string]any{
					"block": map[string]any{
						"block_id":  "blk1",
						"parent_id": "parent1",
					},
				}))
			} else {
				// DELETE
				if r.Method != http.MethodDelete {
					t.Fatalf("expected DELETE, got %s", r.Method)
				}
				w.Write(feishuOK(map[string]any{"deleted": true}))
			}
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.DeleteDocumentBlock(context.Background(), "doc1", "blk1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no parent", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"block": map[string]any{"block_id": "blk1"},
			}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.DeleteDocumentBlock(context.Background(), "doc1", "blk1")
		if err == nil || !strings.Contains(err.Error(), "cannot determine parent") {
			t.Fatalf("expected parent error, got: %v", err)
		}
	})
}

func TestLocal_UpdateDocumentBlocks(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Fatalf("expected PATCH, got %s", r.Method)
		}
		w.Write(feishuOK(map[string]any{"updated": true}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.UpdateDocumentBlocks(context.Background(), "doc1", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_SearchDocs(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/search/object") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"results": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.SearchDocs(context.Background(), map[string]any{"query": "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_CreateDocument(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		w.Write(feishuOK(map[string]any{"document_id": "new-doc"}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.CreateDocument(context.Background(), map[string]any{"title": "Doc"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_CommentMethods(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(feishuOK(map[string]any{"ok": true}))
	})

	t.Run("CreateDocumentComment", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateDocumentComment(context.Background(), "ft1", "docx", map[string]any{"content": "hi"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ReplyDocumentComment", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ReplyDocumentComment(context.Background(), "ft1", "c1", "docx", map[string]any{"content": "reply"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ResolveDocumentComment", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPatch {
				t.Fatalf("expected PATCH, got %s", r.Method)
			}
			w.Write(feishuOK(map[string]any{"resolved": true}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ResolveDocumentComment(context.Background(), "ft1", "c1", "docx", true)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_ListDocumentPermissions(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/permissions/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"members": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.ListDocumentPermissions(context.Background(), "tok1", "docx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_DriveMethods(t *testing.T) {
	t.Parallel()

	t.Run("ListDriveFiles with folder", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("folder_token") == "" {
				t.Fatal("expected folder_token")
			}
			w.Write(feishuOK(map[string]any{"files": []any{}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListDriveFiles(context.Background(), "fld1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListDriveFiles without folder", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{"files": []any{}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListDriveFiles(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateFolder", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{"token": "new-fld"}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateFolder(context.Background(), map[string]any{"name": "folder"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_BitableMethods(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(feishuOK(map[string]any{"ok": true}))
	})

	t.Run("GetBitableMeta", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.GetBitableMeta(context.Background(), "app1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListBitableTables", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListBitableTables(context.Background(), "app1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListBitableFields", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListBitableFields(context.Background(), "app1", "tbl1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListBitableRecords", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListBitableRecords(context.Background(), "app1", "tbl1", nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateBitableRecord", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateBitableRecord(context.Background(), "app1", "tbl1", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("UpdateBitableRecord", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				t.Fatalf("expected PUT, got %s", r.Method)
			}
			w.Write(feishuOK(map[string]any{"updated": true}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.UpdateBitableRecord(context.Background(), "app1", "tbl1", "rec1", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateBitableField", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			w.Write(feishuOK(map[string]any{"field": map[string]any{}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateBitableField(context.Background(), "app1", "tbl1", map[string]any{"field_name": "F", "type": 1})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("UpdateBitableField", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				t.Fatalf("expected PUT, got %s", r.Method)
			}
			if !strings.HasSuffix(r.URL.Path, "/fields/fld1") {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Write(feishuOK(map[string]any{"field": map[string]any{}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.UpdateBitableField(context.Background(), "app1", "tbl1", "fld1", map[string]any{"field_name": "F2"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("DeleteBitableField", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				t.Fatalf("expected DELETE, got %s", r.Method)
			}
			if !strings.HasSuffix(r.URL.Path, "/fields/fld1") {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Write(feishuOK(map[string]any{"deleted": true}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.DeleteBitableField(context.Background(), "app1", "tbl1", "fld1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_SheetWriteMethods(t *testing.T) {
	t.Parallel()

	t.Run("UpdateSheetValues", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPut {
				t.Fatalf("expected PUT, got %s", r.Method)
			}
			w.Write(feishuOK(map[string]any{"updated": true}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.UpdateSheetValues(context.Background(), "st1", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("AppendSheetValues", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				t.Fatalf("expected POST, got %s", r.Method)
			}
			w.Write(feishuOK(map[string]any{"appended": true}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.AppendSheetValues(context.Background(), "st1", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_WikiMethods(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(feishuOK(map[string]any{"ok": true}))
	})

	t.Run("ListWikiSpaces", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListWikiSpaces(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListWikiNodes with parent", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("parent_node_token") != "pn1" {
				t.Fatal("missing parent_node_token")
			}
			w.Write(feishuOK(map[string]any{"nodes": []any{}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListWikiNodes(context.Background(), "sp1", "pn1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListWikiNodes without parent", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListWikiNodes(context.Background(), "sp1", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateWikiNode", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateWikiNode(context.Background(), "sp1", map[string]any{"title": "Node"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_GetBoardNodes(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/whiteboards/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"nodes": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetBoardNodes(context.Background(), "wb1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_CreateTask(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/task/v2/tasks" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"task_id": "t1"}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.CreateTask(context.Background(), map[string]any{"summary": "task"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_SearchUsers(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search/v1/user" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"users": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.SearchUsers(context.Background(), "john", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_MessageReactions(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/im/v1/messages/om_1/reactions":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			rt, _ := body["reaction_type"].(map[string]any)
			if rt["emoji_type"] != "Yes" {
				t.Fatalf("emoji_type = %v", rt["emoji_type"])
			}
		case r.Method == http.MethodGet && r.URL.Path == "/im/v1/messages/om_1/reactions":
		case r.Method == http.MethodDelete && r.URL.Path == "/im/v1/messages/om_1/reactions/rid_1":
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"reaction_id": "rid_1"}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	if _, err := c.ReactMessage(context.Background(), "om_1", "Yes"); err != nil {
		t.Fatalf("react: %v", err)
	}
	if _, err := c.ListMessageReactions(context.Background(), "om_1", nil); err != nil {
		t.Fatalf("list: %v", err)
	}
	if _, err := c.DeleteMessageReaction(context.Background(), "om_1", "rid_1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

func TestLocal_UploadIMResources(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		switch r.URL.Path {
		case "/im/v1/images":
			if r.FormValue("image_type") != "message" {
				t.Fatalf("image_type = %q", r.FormValue("image_type"))
			}
			w.Write(feishuOK(map[string]any{"image_key": "img_v3_local"}))
		case "/im/v1/files":
			if r.FormValue("file_type") != "stream" || r.FormValue("file_name") != "data.bin" {
				t.Fatalf("file fields = %q %q", r.FormValue("file_type"), r.FormValue("file_name"))
			}
			w.Write(feishuOK(map[string]any{"file_key": "file_v3_local"}))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	imageKey, err := c.UploadIMImage(context.Background(), "pic.png", strings.NewReader("png-bytes"))
	if err != nil || imageKey != "img_v3_local" {
		t.Fatalf("image: key=%q err=%v", imageKey, err)
	}
	fileKey, err := c.UploadIMFile(context.Background(), "stream", "data.bin", strings.NewReader("bin-bytes"))
	if err != nil || fileKey != "file_v3_local" {
		t.Fatalf("file: key=%q err=%v", fileKey, err)
	}
}

func TestLocal_CalendarMethods(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(feishuOK(map[string]any{"ok": true}))
	})

	t.Run("GetCalendarPrimary", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.GetCalendarPrimary(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("ListCalendarEvents", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListCalendarEvents(context.Background(), "cal1", "2025-01-01", "2025-01-31")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("CreateCalendarEvent", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("user_id_type") != "user_id" {
				t.Fatal("missing user_id_type")
			}
			w.Write(feishuOK(map[string]any{"event_id": "e1"}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.CreateCalendarEvent(context.Background(), "cal1", map[string]any{"summary": "event"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("AddCalendarAttendees", func(t *testing.T) {
		ts := httptest.NewServer(handler)
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.AddCalendarAttendees(context.Background(), "cal1", "evt1", map[string]any{"attendees": []any{}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_GetFreebusy(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/calendar/v4/freebusy/list" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"freebusy": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetFreebusy(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_ListRooms(t *testing.T) {
	t.Parallel()

	t.Run("with keyword", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["keyword"] != "room-a" {
				t.Fatalf("expected keyword, got: %v", body)
			}
			if body["page_size"] != float64(20) {
				t.Fatalf("expected page_size=20 for keyword search, got: %v", body["page_size"])
			}
			w.Write(feishuOK(map[string]any{"rooms": []any{}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListRooms(context.Background(), "room-a")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("no keyword", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if _, ok := body["keyword"]; ok {
				t.Fatal("should not have keyword")
			}
			if body["page_size"] != float64(100) {
				t.Fatalf("expected page_size=100 for no keyword, got: %v", body["page_size"])
			}
			w.Write(feishuOK(map[string]any{"rooms": []any{}}))
		}))
		defer ts.Close()
		c := newLocalTestClient(ts)
		_, err := c.ListRooms(context.Background(), "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func TestLocal_ExportCreate(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/drive/v1/export_tasks" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"ticket": "ticket-123"}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	ticket, err := c.ExportCreate(context.Background(), "doc1", "docx", "pdf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ticket != "ticket-123" {
		t.Fatalf("unexpected ticket: %q", ticket)
	}
}

func TestLocal_ExportStatus(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"result": map[string]any{
					"job_status": float64(2),
					"file_token": "ft1",
					"file_size":  float64(2048),
					"file_name":  "export.pdf",
				},
			}))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		result, err := c.ExportStatus(context.Background(), "ticket1", "tok1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.JobStatus != 2 || result.FileToken != "ft1" || result.FileSize != 2048 {
			t.Fatalf("unexpected result: %+v", result)
		}
	})

	t.Run("no result", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{}))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.ExportStatus(context.Background(), "ticket1", "tok1")
		if err == nil || !strings.Contains(err.Error(), "no result") {
			t.Fatalf("expected no result error, got: %v", err)
		}
	})
}

func TestLocal_ExportDownload(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("pdf-content"))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		var buf bytes.Buffer
		err := c.ExportDownload(context.Background(), "ft1", &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if buf.String() != "pdf-content" {
			t.Fatalf("unexpected content: %q", buf.String())
		}
	})

	t.Run("server error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("error"))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		var buf bytes.Buffer
		err := c.ExportDownload(context.Background(), "ft1", &buf)
		if err == nil || !strings.Contains(err.Error(), "status=500") {
			t.Fatalf("expected error, got: %v", err)
		}
	})

	t.Run("not logged in", func(t *testing.T) {
		c := NewLocalClient("app", "secret")
		c.baseURL = "http://unused"
		var buf bytes.Buffer
		err := c.ExportDownload(context.Background(), "ft1", &buf)
		if err == nil || !strings.Contains(err.Error(), "not logged in") {
			t.Fatalf("expected not logged in error, got: %v", err)
		}
	})
}

func TestLocal_GetSheetsFloatImages(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/float_images/query") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"images": []any{}}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)
	_, err := c.GetSheetsFloatImages(context.Background(), "st1", "s1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLocal_DownloadMedia(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/medias/") {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "image/jpeg")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("JPEG-DATA"))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		var buf bytes.Buffer
		ct, err := c.DownloadMedia(context.Background(), "media1", &buf)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if ct != "image/jpeg" {
			t.Fatalf("unexpected content type: %q", ct)
		}
		if buf.String() != "JPEG-DATA" {
			t.Fatalf("unexpected content: %q", buf.String())
		}
	})

	t.Run("not logged in", func(t *testing.T) {
		c := NewLocalClient("app", "secret")
		var buf bytes.Buffer
		_, err := c.DownloadMedia(context.Background(), "media1", &buf)
		if err == nil || !strings.Contains(err.Error(), "not logged in") {
			t.Fatalf("expected not logged in error, got: %v", err)
		}
	})

	t.Run("server error", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("not found"))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		var buf bytes.Buffer
		_, err := c.DownloadMedia(context.Background(), "media1", &buf)
		if err == nil || !strings.Contains(err.Error(), "status=404") {
			t.Fatalf("expected download error, got: %v", err)
		}
	})
}

func TestLocal_ResolveWikiDocToken(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"node": map[string]any{"obj_token": "resolved-token"},
			}))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		tok, err := c.resolveWikiDocToken(context.Background(), "wiki-token")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if tok != "resolved-token" {
			t.Fatalf("unexpected token: %q", tok)
		}
	})

	t.Run("no obj_token", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"node": map[string]any{"title": "Some Wiki"},
			}))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.resolveWikiDocToken(context.Background(), "wiki-token")
		if err == nil || !strings.Contains(err.Error(), "could not resolve") {
			t.Fatalf("expected resolve error, got: %v", err)
		}
	})

	t.Run("unexpected response type", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK("unexpected-string"))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.resolveWikiDocToken(context.Background(), "wiki-token")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// ==================== parseTokenResponse tests ====================

func TestParseTokenResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		input       any
		wantAccess  string
		wantRefresh string
		wantExpiry  int
		wantErr     bool
	}{
		{
			name:        "standard response",
			input:       map[string]any{"access_token": "at1", "refresh_token": "rt1", "expires_in": float64(7200)},
			wantAccess:  "at1",
			wantRefresh: "rt1",
			wantExpiry:  7200,
		},
		{
			name:        "user_access_token key",
			input:       map[string]any{"user_access_token": "uat1", "expires_in": float64(3600)},
			wantAccess:  "uat1",
			wantRefresh: "",
			wantExpiry:  3600,
		},
		{
			name: "nested data fallback",
			input: map[string]any{
				"data": map[string]any{
					"access_token":  "nested-at",
					"refresh_token": "nested-rt",
					"expires_in":    float64(1800),
				},
			},
			wantAccess:  "nested-at",
			wantRefresh: "nested-rt",
			wantExpiry:  1800,
		},
		{
			name:    "no access token",
			input:   map[string]any{"refresh_token": "rt1"},
			wantErr: true,
		},
		{
			name:    "wrong type",
			input:   "not a map",
			wantErr: true,
		},
		{
			name:       "default expires_in",
			input:      map[string]any{"access_token": "at1"},
			wantAccess: "at1",
			wantExpiry: 7200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := parseTokenResponse(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.AccessToken != tt.wantAccess {
				t.Fatalf("access_token = %q, want %q", resp.AccessToken, tt.wantAccess)
			}
			if resp.RefreshToken != tt.wantRefresh {
				t.Fatalf("refresh_token = %q, want %q", resp.RefreshToken, tt.wantRefresh)
			}
			if resp.ExpiresIn != tt.wantExpiry {
				t.Fatalf("expires_in = %d, want %d", resp.ExpiresIn, tt.wantExpiry)
			}
		})
	}
}

// ==================== SetSessionToken / session tests ====================

func TestGateway_SetSessionToken(t *testing.T) {
	t.Parallel()
	c := NewGatewayClient("http://example.com")
	c.SetSessionToken("  token-with-spaces  ")
	if got := c.session(); got != "token-with-spaces" {
		t.Fatalf("expected trimmed token, got %q", got)
	}
}

// ==================== feishuRequestNoAuth tests ====================

func TestLocal_FeishuRequestNoAuth(t *testing.T) {
	t.Parallel()

	t.Run("success with data key", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "" {
				t.Fatal("should not have Authorization header")
			}
			w.Write(feishuOK(map[string]any{"token": "abc"}))
		}))
		defer ts.Close()

		c := NewLocalClient("app", "secret")
		c.baseURL = ts.URL
		data, err := c.feishuRequestNoAuth(context.Background(), "POST", "/test", nil, map[string]any{"key": "val"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := data.(map[string]any)
		if m["token"] != "abc" {
			t.Fatalf("unexpected data: %v", data)
		}
	})

	t.Run("feishu error code", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"code":20029,"msg":"redirect_uri mismatch"}`))
		}))
		defer ts.Close()

		c := NewLocalClient("app", "secret")
		c.baseURL = ts.URL
		_, err := c.feishuRequestNoAuth(context.Background(), "POST", "/test", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "20029") {
			t.Fatalf("expected feishu error, got: %v", err)
		}
	})

	t.Run("non-json response", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte("not json"))
		}))
		defer ts.Close()

		c := NewLocalClient("app", "secret")
		c.baseURL = ts.URL
		_, err := c.feishuRequestNoAuth(context.Background(), "POST", "/test", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "upstream") {
			t.Fatalf("expected upstream error, got: %v", err)
		}
	})

	t.Run("no data key returns payload", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"code":0,"msg":"ok","token":"direct"}`))
		}))
		defer ts.Close()

		c := NewLocalClient("app", "secret")
		c.baseURL = ts.URL
		data, err := c.feishuRequestNoAuth(context.Background(), "POST", "/test", nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		m := data.(map[string]any)
		if m["token"] != "direct" {
			t.Fatalf("unexpected data: %v", data)
		}
	})
}

// ==================== exchangeAuthCode test ====================

func TestLocal_ExchangeAuthCode(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["grant_type"] != "authorization_code" || body["code"] != "auth-code-123" {
			t.Fatalf("bad body: %v", body)
		}
		if body["client_id"] != "app-id" || body["client_secret"] != "app-secret" {
			t.Fatalf("bad credentials: %v", body)
		}
		w.Write(feishuOK(map[string]any{
			"access_token":  "new-at",
			"refresh_token": "new-rt",
			"expires_in":    float64(7200),
		}))
	}))
	defer ts.Close()

	c := NewLocalClient("app-id", "app-secret")
	c.baseURL = ts.URL
	resp, err := c.exchangeAuthCode(context.Background(), "auth-code-123", "http://localhost/callback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.AccessToken != "new-at" || resp.RefreshToken != "new-rt" {
		t.Fatalf("unexpected response: %+v", resp)
	}
}

// ==================== fetchUser test ====================

func TestLocal_FetchUser(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"user_id":    "uid1",
				"name":       "John Doe",
				"open_id":    "ou_123",
				"email":      "john@test.com",
				"tenant_key": "tk1",
			}))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		user, err := c.fetchUser(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.UserID != "uid1" || user.Name != "John Doe" {
			t.Fatalf("unexpected user: %+v", user)
		}
	})

	t.Run("missing user_id uses open_id", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"open_id":    "ou_123",
				"name":       "Test",
				"tenant_key": "tk1",
			}))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		user, err := c.fetchUser(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.UserID != "ou_123" {
			t.Fatalf("expected user_id=ou_123, got %q", user.UserID)
		}
	})

	t.Run("missing name defaults to unknown", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write(feishuOK(map[string]any{
				"user_id": "uid1",
			}))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		user, err := c.fetchUser(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if user.Name != "unknown" {
			t.Fatalf("expected name=unknown, got %q", user.Name)
		}
	})
}

// ==================== Concurrent access test ====================

func TestGateway_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(okEnvelope(map[string]any{"ok": true}))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	ctx := context.Background()

	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func(n int) {
			_, err := c.WhoAmI(ctx)
			done <- err
		}(i)
	}

	for i := 0; i < 10; i++ {
		if err := <-done; err != nil {
			t.Fatalf("concurrent call %d failed: %v", i, err)
		}
	}
}

// ==================== Context cancellation ====================

func TestGateway_ContextCancellation(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.Write(okEnvelope(nil))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := c.WhoAmI(ctx)
	if err == nil {
		t.Fatal("expected context deadline error")
	}
}

// ==================== normalizeBaseURL ====================

func TestNormalizeBaseURL_Client(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input string
		want  string
	}{
		{"http://example.com/", "http://example.com"},
		{"http://example.com///", "http://example.com"},
		{"  http://example.com  ", "http://example.com"},
		{"", fmt.Sprintf("%s", "http://127.0.0.1:8787")},
	}
	for _, tt := range tests {
		got := normalizeBaseURL(tt.input)
		if got != tt.want {
			t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// ==================== FeishuClient interface satisfaction ====================

func TestGatewayClientImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ FeishuClient = (*GatewayClient)(nil)
}

func TestLocalClientImplementsInterface(t *testing.T) {
	t.Parallel()
	var _ FeishuClient = (*LocalClient)(nil)
}

// ==================== HTTP method verification tests ====================

func TestGateway_CorrectHTTPMethods(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		method   string
		callFunc func(c *GatewayClient) error
	}{
		{
			name:   "WhoAmI is GET",
			method: "GET",
			callFunc: func(c *GatewayClient) error {
				_, err := c.WhoAmI(context.Background())
				return err
			},
		},
		{
			name:   "SearchDocs is POST",
			method: "POST",
			callFunc: func(c *GatewayClient) error {
				_, err := c.SearchDocs(context.Background(), map[string]any{})
				return err
			},
		},
		{
			name:   "CreateTask is POST",
			method: "POST",
			callFunc: func(c *GatewayClient) error {
				_, err := c.CreateTask(context.Background(), map[string]any{})
				return err
			},
		},
		{
			name:   "ListWikiSpaces is GET",
			method: "GET",
			callFunc: func(c *GatewayClient) error {
				_, err := c.ListWikiSpaces(context.Background())
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != tt.method {
					t.Fatalf("expected %s, got %s", tt.method, r.Method)
				}
				w.Write(okEnvelope(map[string]any{}))
			}))
			defer ts.Close()
			c := newGatewayTestClient(ts)
			_ = tt.callFunc(c)
		})
	}
}

// ==================== Local client URL extraction integration ====================

func TestLocal_URLExtraction(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify the token is extracted from the URL
		if strings.Contains(r.URL.Path, "https://") || strings.Contains(r.URL.Path, "feishu.cn") {
			t.Fatalf("URL not extracted, path contains raw URL: %s", r.URL.Path)
		}
		w.Write(feishuOK(map[string]any{"ok": true}))
	}))
	defer ts.Close()

	c := newLocalTestClient(ts)

	// GetSheetsMeta with URL input
	_, err := c.GetSheetsMeta(context.Background(), "https://feishu.cn/sheets/abc123def")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// GetBitableMeta with URL
	_, err = c.GetBitableMeta(context.Background(), "https://feishu.cn/base/app-token-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ==================== doRefreshToken tests ====================

func TestLocal_DoRefreshToken(t *testing.T) {
	t.Parallel()

	t.Run("v2 success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/authen/v2/oauth/token" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["grant_type"] != "refresh_token" {
				t.Fatalf("unexpected grant_type: %v", body["grant_type"])
			}
			w.Write(feishuOK(map[string]any{
				"access_token":  "new-at",
				"refresh_token": "new-rt",
				"expires_in":    float64(7200),
			}))
		}))
		defer ts.Close()

		c := NewLocalClient("app", "secret")
		c.baseURL = ts.URL
		resp, err := c.doRefreshToken(context.Background(), "old-rt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.AccessToken != "new-at" {
			t.Fatalf("unexpected access_token: %q", resp.AccessToken)
		}
	})

	t.Run("v2 fails, v1 succeeds", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/authen/v2/oauth/token":
				w.Write([]byte(`{"code":99999,"msg":"v2 error"}`))
			case "/authen/v1/refresh_access_token":
				w.Write(feishuOK(map[string]any{
					"access_token":  "v1-at",
					"refresh_token": "v1-rt",
					"expires_in":    float64(3600),
				}))
			}
		}))
		defer ts.Close()

		c := NewLocalClient("app", "secret")
		c.baseURL = ts.URL
		resp, err := c.doRefreshToken(context.Background(), "old-rt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.AccessToken != "v1-at" {
			t.Fatalf("unexpected access_token: %q", resp.AccessToken)
		}
	})

	t.Run("both fail", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"code":99999,"msg":"auth error"}`))
		}))
		defer ts.Close()

		c := NewLocalClient("app", "secret")
		c.baseURL = ts.URL
		_, err := c.doRefreshToken(context.Background(), "old-rt")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

// ==================== callJSON unmarshal error ====================

func TestGateway_callJSON_UnmarshalError(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return data that can't be unmarshaled into the target type
		w.Write(okEnvelope("a string, not an object"))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	var out map[string]any // expects object but data is a string
	err := c.callJSON(context.Background(), "GET", "/v1/test", nil, nil, true, &out)
	if err == nil || !strings.Contains(err.Error(), "decode response data") {
		t.Fatalf("expected decode error, got: %v", err)
	}
}

// ==================== FeishuRequest message extraction ====================

func TestLocal_FeishuRequest_MessageKey(t *testing.T) {
	t.Parallel()

	t.Run("msg key", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"code":10001,"msg":"specific error message"}`))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "specific error message") {
			t.Fatalf("expected error with msg, got: %v", err)
		}
	})

	t.Run("message key fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"code":10001,"message":"fallback message"}`))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "fallback message") {
			t.Fatalf("expected error with message, got: %v", err)
		}
	})

	t.Run("unknown error fallback", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Write([]byte(`{"code":10001}`))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "unknown Feishu error") {
			t.Fatalf("expected unknown error, got: %v", err)
		}
	})
}

// ==================== HTTP 4xx with code=0 ====================

func TestLocal_FeishuRequest_HTTP4xxCodeZero(t *testing.T) {
	t.Parallel()

	t.Run("with message", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"code":0,"message":"no permission"}`))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "no permission") {
			t.Fatalf("expected error, got: %v", err)
		}
	})

	t.Run("without message uses status text", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"code":0}`))
		}))
		defer ts.Close()

		c := newLocalTestClient(ts)
		_, err := c.feishuRequest(context.Background(), "GET", "/test", nil, nil)
		if err == nil || !strings.Contains(err.Error(), "Forbidden") {
			t.Fatalf("expected Forbidden error, got: %v", err)
		}
	})
}

// ==================== io.Writer error for downloads ====================

func TestGateway_ExportDownload_WriterError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Write a large chunk
		w.Write(bytes.Repeat([]byte("x"), 1024))
	}))
	defer ts.Close()

	c := newGatewayTestClient(ts)
	err := c.ExportDownload(context.Background(), "ft1", &failWriter{})
	if err == nil {
		t.Fatal("expected write error")
	}
}

type failWriter struct{}

func (fw *failWriter) Write(p []byte) (int, error) {
	return 0, io.ErrClosedPipe
}
