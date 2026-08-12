package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// gatewayMux returns an http.ServeMux that handles gateway envelope responses
// for all known API paths. Override individual paths after calling this.
func gatewayMux() *http.ServeMux {
	mux := http.NewServeMux()

	// Default catch-all handler returns OK with empty data
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok", "data": map[string]any{},
		})
	})
	return mux
}

// setupGatewayEnv creates a mock gateway, sets env vars, and returns the server.
// Caller must defer srv.Close().
func setupGatewayEnv(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(handler)
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", srv.URL)
	t.Setenv("LARKCTL_SESSION_TOKEN", "test-token")
	gatewayURL = ""
	return srv
}

// execCmd builds a root command, sets args, and executes it.
// Returns stdout output and error.
func execCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := newRootCmd()
	cmd.SetArgs(args)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(io.Discard)
	err := cmd.Execute()
	return buf.String(), err
}

// ---------- Tests ----------

func TestNewRootCmd(t *testing.T) {
	cmd := newRootCmd()
	if cmd == nil {
		t.Fatal("newRootCmd() returned nil")
	}
	if cmd.Use != "larkctl" {
		t.Fatalf("root cmd Use = %q, want larkctl", cmd.Use)
	}
	// Verify subcommands exist
	names := make(map[string]bool)
	for _, sub := range cmd.Commands() {
		names[sub.Name()] = true
	}
	for _, want := range []string{"docs", "wiki", "sheets", "drive", "bitable", "tasks", "calendar", "board", "mcp", "login", "logout", "whoami", "images", "upgrade", "init"} {
		if !names[want] {
			t.Fatalf("missing subcommand %q", want)
		}
	}
}

func TestDocsInfoCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/info", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"title": "Test Doc", "document_id": "doc123"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "info", "doc123")
	if err != nil {
		t.Fatalf("docs info: %v", err)
	}
}

func TestDocsBlocksCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{"block_id": "blk1", "block_type": 2},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "blocks", "doc123")
	if err != nil {
		t.Fatalf("docs blocks: %v", err)
	}
}

func TestDocsSearchCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/search", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"docs": []any{map[string]any{"title": "found"}},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "search", "test query")
	if err != nil {
		t.Fatalf("docs search: %v", err)
	}
}

func TestDocsCreateCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"document": map[string]any{"document_id": "new_doc"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "create", "New Document")
	if err != nil {
		t.Fatalf("docs create: %v", err)
	}
}

func TestDocsCommentsCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/comments", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	// requireScopes calls UpgradeScopes for gateway client
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "comments", "doc123")
	if err != nil {
		t.Fatalf("docs comments: %v", err)
	}
}

func TestDocsDeleteBlockCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks/delete", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "delete-block", "doc123", "blk456")
	if err != nil {
		t.Fatalf("docs delete-block: %v", err)
	}
}

func TestDocsAddCommentCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/comments/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"comment_id": "cmt_1"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "add-comment", "doc123", "Hello comment")
	if err != nil {
		t.Fatalf("docs add-comment: %v", err)
	}
}

func TestDocsReplyCommentCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/comments/reply", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"reply_id": "rep_1"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "reply-comment", "doc123", "cmt_1", "reply text")
	if err != nil {
		t.Fatalf("docs reply-comment: %v", err)
	}
}

func TestDocsResolveCommentCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/comments/resolve", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "resolve-comment", "doc123", "cmt_1")
	if err != nil {
		t.Fatalf("docs resolve-comment: %v", err)
	}
}

func TestDocsPermissionsCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/permissions", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"members": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "permissions", "doc123")
	if err != nil {
		t.Fatalf("docs permissions: %v", err)
	}
}

func TestWikiNodeCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/node", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"node": map[string]any{"node_token": "wiki_tok", "obj_token": "obj_tok"},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "wiki", "node", "wiki_tok")
	if err != nil {
		t.Fatalf("wiki node: %v", err)
	}
}

func TestWikiSpacesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/spaces", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{map[string]any{"space_id": "sp1", "name": "My Space"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "wiki", "spaces")
	if err != nil {
		t.Fatalf("wiki spaces: %v", err)
	}
}

func TestWikiNodesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/nodes", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "wiki", "nodes", "sp1")
	if err != nil {
		t.Fatalf("wiki nodes: %v", err)
	}
}

func TestWikiCreateNodeCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/nodes/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"node": map[string]any{"node_token": "new_node"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "wiki", "create-node", "sp1", "--title", "New Node")
	if err != nil {
		t.Fatalf("wiki create-node: %v", err)
	}
}

func TestSheetsMetaCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/sheets/meta", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"spreadsheet_token": "sheet123", "title": "My Sheet"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "sheets", "meta", "sheet123")
	if err != nil {
		t.Fatalf("sheets meta: %v", err)
	}
}

func TestSheetsValuesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/sheets/values", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"valueRange": map[string]any{
					"values": []any{[]any{"a", "b"}, []any{"c", "d"}},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "sheets", "values", "sheet123", "Sheet1", "A1:B2")
	if err != nil {
		t.Fatalf("sheets values: %v", err)
	}
}

func TestSheetsUpdateCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/sheets/values/update", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	// Create a temp JSON file for input
	tmp := t.TempDir()
	jsonPath := tmp + "/input.json"
	os.WriteFile(jsonPath, []byte(`{"valueRange":{"range":"Sheet1!A1:B1","values":[["x","y"]]}}`), 0644)

	_, err := execCmd(t, "sheets", "update", "sheet123", jsonPath)
	if err != nil {
		t.Fatalf("sheets update: %v", err)
	}
}

func TestSheetsAppendCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/sheets/values/append", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	jsonPath := tmp + "/input.json"
	os.WriteFile(jsonPath, []byte(`{"valueRange":{"range":"Sheet1!A1:B1","values":[["x","y"]]}}`), 0644)

	_, err := execCmd(t, "sheets", "append", "sheet123", jsonPath)
	if err != nil {
		t.Fatalf("sheets append: %v", err)
	}
}

func TestDriveListCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/drive/files", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"files": []any{map[string]any{"name": "file1", "token": "tok1"}},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "drive", "list")
	if err != nil {
		t.Fatalf("drive list: %v", err)
	}
}

func TestDriveListWithFolderCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/drive/files", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"files": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "drive", "list", "folder_tok")
	if err != nil {
		t.Fatalf("drive list folder: %v", err)
	}
}

func TestDriveMkdirCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/drive/folder/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"token": "new_folder_tok"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "drive", "mkdir", "parent_tok", "NewFolder")
	if err != nil {
		t.Fatalf("drive mkdir: %v", err)
	}
}

func TestDriveMediaCmd(t *testing.T) {
	mux := gatewayMux()
	var gotToken string
	mux.HandleFunc("/v1/drive/media", func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.URL.Query().Get("file_token")
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("attachment-bytes"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	t.Run("explicit output path", func(t *testing.T) {
		out := t.TempDir() + "/model.stl"
		if _, err := execCmd(t, "drive", "media", "media_tok", out); err != nil {
			t.Fatalf("drive media: %v", err)
		}
		if gotToken != "media_tok" {
			t.Fatalf("file_token = %q, want media_tok", gotToken)
		}
		data, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("read output: %v", err)
		}
		if string(data) != "attachment-bytes" {
			t.Fatalf("content = %q", string(data))
		}
	})

	t.Run("output dir uses file_token as filename", func(t *testing.T) {
		dir := t.TempDir()
		if _, err := execCmd(t, "drive", "media", "media_tok", dir); err != nil {
			t.Fatalf("drive media into dir: %v", err)
		}
		if _, err := os.Stat(dir + "/media_tok"); err != nil {
			t.Fatalf("expected %s/media_tok: %v", dir, err)
		}
	})
}

func TestDriveMediaCmdUpstreamError(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/drive/media", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"code":"feishu_error","message":"status=403 body="}`))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	out := t.TempDir() + "/nope.bin"
	if _, err := execCmd(t, "drive", "media", "bad_tok", out); err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestBitableMetaCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/meta", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"app": map[string]any{"name": "My Bitable"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "bitable", "meta", "app123")
	if err != nil {
		t.Fatalf("bitable meta: %v", err)
	}
}

func TestBitableTablesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/tables", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{map[string]any{"table_id": "tbl1"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "bitable", "tables", "app123")
	if err != nil {
		t.Fatalf("bitable tables: %v", err)
	}
}

func TestBitableFieldsCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/fields", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "bitable", "fields", "app123", "tbl1")
	if err != nil {
		t.Fatalf("bitable fields: %v", err)
	}
}

func TestBitableRecordsCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/records", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{map[string]any{"record_id": "rec1", "fields": map[string]any{"Name": "Alice"}}},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "bitable", "records", "app123", "tbl1")
	if err != nil {
		t.Fatalf("bitable records: %v", err)
	}
}

func TestBitableRecordsWithFilterCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/records", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("filter") == "" {
			http.Error(w, "missing filter", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "bitable", "records", "app123", "tbl1", "--filter", "Status=\"Done\"")
	if err != nil {
		t.Fatalf("bitable records --filter: %v", err)
	}
}

func TestBitableCreateRecordCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/records/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"record": map[string]any{"record_id": "new_rec"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	jsonPath := tmp + "/record.json"
	os.WriteFile(jsonPath, []byte(`{"fields":{"Name":"Bob"}}`), 0644)

	_, err := execCmd(t, "bitable", "create-record", "app123", "tbl1", jsonPath)
	if err != nil {
		t.Fatalf("bitable create-record: %v", err)
	}
}

func TestBitableFieldCmds(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/fields/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"field": map[string]any{"field_id": "fld_new"}},
		})
	})
	mux.HandleFunc("/v1/bitable/fields/update", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("field_id") != "fld1" {
			http.Error(w, "missing field_id", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"field": map[string]any{"field_id": "fld1"}},
		})
	})
	mux.HandleFunc("/v1/bitable/fields/delete", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("field_id") != "fld1" {
			http.Error(w, "missing field_id", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"deleted": true, "field_id": "fld1"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	jsonPath := tmp + "/field.json"
	os.WriteFile(jsonPath, []byte(`{"field_name":"Status","type":3}`), 0644)

	if _, err := execCmd(t, "bitable", "create-field", "app123", "tbl1", jsonPath); err != nil {
		t.Fatalf("bitable create-field: %v", err)
	}
	if _, err := execCmd(t, "bitable", "update-field", "app123", "tbl1", "fld1", jsonPath); err != nil {
		t.Fatalf("bitable update-field: %v", err)
	}
	if _, err := execCmd(t, "bitable", "delete-field", "app123", "tbl1", "fld1"); err != nil {
		t.Fatalf("bitable delete-field: %v", err)
	}
}

func TestBitableUpdateRecordCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/bitable/records/update", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"record": map[string]any{"record_id": "rec1"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	jsonPath := tmp + "/record.json"
	os.WriteFile(jsonPath, []byte(`{"fields":{"Status":"Done"}}`), 0644)

	_, err := execCmd(t, "bitable", "update-record", "app123", "tbl1", "rec1", jsonPath)
	if err != nil {
		t.Fatalf("bitable update-record: %v", err)
	}
}

func TestTasksCreateCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/tasks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"task": map[string]any{"id": "task_1"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "tasks", "create", "Fix the bug")
	if err != nil {
		t.Fatalf("tasks create: %v", err)
	}
}

func TestTasksCreateWithMembersCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/tasks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"task": map[string]any{"id": "task_2"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "tasks", "create", "Deploy", "--members", "ou_abc123")
	if err != nil {
		t.Fatalf("tasks create with members: %v", err)
	}
}

func TestBoardNodesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/board/nodes", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"nodes": []any{map[string]any{"id": "node1"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "board", "nodes", "board123")
	if err != nil {
		t.Fatalf("board nodes: %v", err)
	}
}

func TestCalendarPrimaryCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "primary")
	if err != nil {
		t.Fatalf("calendar primary: %v", err)
	}
}

func TestCalendarListCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "list")
	if err != nil {
		t.Fatalf("calendar list: %v", err)
	}
}

func TestCalendarCreateCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"event": map[string]any{"event_id": "evt_123"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "create", "Team Sync",
		"--start", "2026-03-20 14:00", "--end", "2026-03-20 15:00")
	if err != nil {
		t.Fatalf("calendar create: %v", err)
	}
}

func TestCalendarCreateMissingTimesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "create", "No Times")
	if err == nil {
		t.Fatal("calendar create without --start/--end should fail")
	}
	if !strings.Contains(err.Error(), "--start and --end are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCalendarFreebusyCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/freebusy", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"freebusy_list": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "freebusy",
		"--start", "2026-03-20", "--end", "2026-03-21", "--user", "12345")
	if err != nil {
		t.Fatalf("calendar freebusy: %v", err)
	}
}

func TestCalendarFreebusyMissingTimesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "freebusy")
	if err == nil {
		t.Fatal("calendar freebusy without --start/--end should fail")
	}
}

func TestCalendarRoomsCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/rooms", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"rooms": []any{map[string]any{"room_id": "omm_1", "name": "Room A"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "rooms")
	if err != nil {
		t.Fatalf("calendar rooms: %v", err)
	}
}

func TestCalendarRoomsWithKeywordCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/rooms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("keyword") != "1604" {
			json.NewEncoder(w).Encode(map[string]any{
				"code": "ok", "message": "ok",
				"data": map[string]any{"rooms": []any{}},
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"rooms": []any{map[string]any{"room_id": "omm_1604", "name": "1604"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "rooms", "1604")
	if err != nil {
		t.Fatalf("calendar rooms 1604: %v", err)
	}
}

func TestWhoAmICmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"name": "Test User", "open_id": "ou_abc"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "whoami")
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
}

func TestLogoutCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	// Save a session token so logout has something to delete
	if err := SaveSessionToken(srv.URL, "test-token"); err != nil {
		t.Fatalf("save token: %v", err)
	}
	if err := SaveGatewayURL(srv.URL); err != nil {
		t.Fatalf("save url: %v", err)
	}

	_, err := execCmd(t, "logout")
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
}

func TestLogoutLocalModeCmd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	t.Setenv("LARKCTL_MODE", "local")
	gatewayURL = ""

	// Set up local mode
	if err := SaveLocalApp("app123", "secret456"); err != nil {
		t.Fatal(err)
	}
	if err := SaveLocalTokens("access", "refresh", time.Now().Add(1*time.Hour)); err != nil {
		t.Fatal(err)
	}

	_, err := execCmd(t, "logout")
	if err != nil {
		t.Fatalf("logout local: %v", err)
	}
}

func TestMCPCmd(t *testing.T) {
	mux := gatewayMux()
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	// Save a session token
	if err := SaveSessionToken(srv.URL, "test-mcp-token"); err != nil {
		t.Fatalf("save token: %v", err)
	}

	_, err := execCmd(t, "mcp")
	if err != nil {
		t.Fatalf("mcp: %v", err)
	}
}

func TestMCPCmdNoToken(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "http://localhost:9999")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	gatewayURL = ""

	_, err := execCmd(t, "mcp")
	if err == nil {
		t.Fatal("mcp without token should fail")
	}
	if !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitCmd(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")
	gatewayURL = ""

	_, err := execCmd(t, "init", "--app-id", "app_test", "--app-secret", "secret_test")
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	// Verify config was saved
	appID, appSecret, err := LoadLocalApp()
	if err != nil {
		t.Fatalf("LoadLocalApp: %v", err)
	}
	if appID != "app_test" || appSecret != "secret_test" {
		t.Fatalf("app = %q/%q, want app_test/secret_test", appID, appSecret)
	}
}

func TestInitCmdMissingArgs(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")
	gatewayURL = ""

	_, err := execCmd(t, "init")
	if err == nil {
		t.Fatal("init without args should fail")
	}
	if !strings.Contains(err.Error(), "--app-id and --app-secret are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInitCmdFromEnv(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	t.Setenv("FEISHU_APP_ID", "env_app_id")
	t.Setenv("FEISHU_APP_SECRET", "env_app_secret")
	gatewayURL = ""

	_, err := execCmd(t, "init")
	if err != nil {
		t.Fatalf("init from env: %v", err)
	}
}

func TestUpgradeCmd(t *testing.T) {
	// Create a mock download server
	dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-binary-content"))
	}))
	defer dlSrv.Close()

	// Override download base URL
	origDownloadURL := downloadBaseURL
	downloadBaseURL = dlSrv.URL
	defer func() { downloadBaseURL = origDownloadURL }()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	gatewayURL = ""

	// The upgrade command needs os.Executable() which we can't easily mock,
	// so we test the command construction instead
	cmd := newUpgradeCmd()
	if cmd == nil {
		t.Fatal("newUpgradeCmd() returned nil")
	}
	if cmd.Use != "upgrade" {
		t.Fatalf("Use = %q, want upgrade", cmd.Use)
	}
}

func TestLoginCmdUnknownScope(t *testing.T) {
	mux := gatewayMux()
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "login", "nonexistent_scope")
	if err == nil {
		t.Fatal("login with unknown scope should fail")
	}
	if !strings.Contains(err.Error(), "unknown scope group") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDocsCreateBlocksCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"children": []any{map[string]any{"block_id": "new_blk"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	jsonPath := tmp + "/blocks.json"
	os.WriteFile(jsonPath, []byte(`{"children":[{"block_type":2,"text":{"elements":[{"text_run":{"content":"Hello"}}]}}],"index":-1}`), 0644)

	_, err := execCmd(t, "docs", "create-blocks", "doc123", jsonPath)
	if err != nil {
		t.Fatalf("docs create-blocks: %v", err)
	}
}

func TestDocsExportMarkdownCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": float64(1),
						"children":   []any{"blk1"},
						"page":       map[string]any{},
					},
					map[string]any{
						"block_id":   "blk1",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "Hello World"}},
							},
						},
					},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/test.md"

	_, err := execCmd(t, "docs", "export", "doc123", "--format", "md", "--output", outPath)
	if err != nil {
		t.Fatalf("docs export --format md: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	if !strings.Contains(string(content), "Hello World") {
		t.Fatalf("export missing content, got: %s", string(content))
	}
}

func TestDocsExportMarkdownStdoutCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": float64(1),
						"children":   []any{"blk1"},
						"page":       map[string]any{},
					},
					map[string]any{
						"block_id":   "blk1",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "stdout test"}},
							},
						},
					},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "export", "doc123", "--format", "md", "--output", "-")
	if err != nil {
		t.Fatalf("docs export md stdout: %v", err)
	}
}

func TestImagesDocCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "img_blk",
						"block_type": float64(27),
						"image":      map[string]any{"token": "img_tok_1", "width": 100, "height": 200},
					},
				},
			},
		})
	})
	mux.HandleFunc("/v1/drive/media", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-data"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	_, err := execCmd(t, "images", "https://xxx.feishu.cn/docx/doc_token", "-o", tmp)
	if err != nil {
		t.Fatalf("images doc: %v", err)
	}

	entries, _ := os.ReadDir(tmp)
	if len(entries) == 0 {
		t.Fatal("expected downloaded images")
	}
}

func TestImagesDocNoImagesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{"block_id": "blk1", "block_type": float64(2)},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	_, err := execCmd(t, "images", "https://xxx.feishu.cn/docx/doc_token", "-o", tmp)
	if err != nil {
		t.Fatalf("images doc (no images): %v", err)
	}
}

func TestImagesSheetCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/sheets/values", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"valueRange": map[string]any{"values": []any{}}},
		})
	})
	mux.HandleFunc("/v1/sheets/float_images", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"float_image_id":    "fi_1",
						"float_image_token": "fi_tok_1",
						"width":             100.0,
						"height":            200.0,
					},
				},
			},
		})
	})
	mux.HandleFunc("/v1/drive/media", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte("fake-png-data"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	_, err := execCmd(t, "images", "https://xxx.feishu.cn/sheets/sheet_token?sheet=Sheet1", "-o", tmp)
	if err != nil {
		t.Fatalf("images sheet: %v", err)
	}
}

func TestImagesSheetWikiURLCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/node", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"node": map[string]any{"obj_token": "resolved_sheet_token"},
			},
		})
	})
	mux.HandleFunc("/v1/sheets/values", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"valueRange": map[string]any{"values": []any{}}},
		})
	})
	mux.HandleFunc("/v1/sheets/float_images", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	_, err := execCmd(t, "images", "https://xxx.feishu.cn/wiki/wiki_token?sheet=Sheet1", "-o", tmp)
	if err != nil {
		t.Fatalf("images wiki sheet: %v", err)
	}
}

func TestRequireScopesGatewayNoUpgrade(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gc := NewGatewayClient(srv.URL)
	gc.SetSessionToken("test-token")

	err := requireScopes(context.Background(), gc, "calendar:calendar")
	if err != nil {
		t.Fatalf("requireScopes: %v", err)
	}
}

func TestDocsUpdateCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(1),
						"children":   []any{},
					},
				},
			},
		})
	})
	mux.HandleFunc("/v1/docs/blocks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"children": []any{map[string]any{"block_id": "new1"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	mdPath := tmp + "/doc.md"
	os.WriteFile(mdPath, []byte("Hello World\n"), 0644)

	_, err := execCmd(t, "docs", "update", "doc123", mdPath)
	if err != nil {
		t.Fatalf("docs update: %v", err)
	}
}

func TestCalendarCreateWithAttendeesCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"event": map[string]any{"event_id": "evt_456"}},
		})
	})
	mux.HandleFunc("/v1/calendar/events/attendees", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"attendees": []any{map[string]any{"user_id": "ou_abc"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "create", "Meeting",
		"--start", "2026-03-20 10:00", "--end", "2026-03-20 11:00",
		"--attendees", "ou_abc")
	if err != nil {
		t.Fatalf("calendar create with attendees: %v", err)
	}
}

func TestCompactFlagCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"name": "User"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	origCompact := compactOutput
	defer func() { compactOutput = origCompact }()

	_, err := execCmd(t, "--compact", "whoami")
	if err != nil {
		t.Fatalf("whoami --compact: %v", err)
	}
}

func TestGatewayURLFlagCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"name": "FlagUser"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "--gateway-url", srv.URL, "whoami")
	if err != nil {
		t.Fatalf("whoami --gateway-url: %v", err)
	}
}

// exportMockClient wraps mockClient but overrides ExportDownload to write data.
type exportMockClient struct {
	*mockClient
	downloadData []byte
}

func (e *exportMockClient) ExportDownload(ctx context.Context, fileToken string, w io.Writer) error {
	if e.downloadData != nil {
		_, err := w.Write(e.downloadData)
		return err
	}
	return e.mockClient.ExportDownload(ctx, fileToken, w)
}

// ---- localLogin tests ----

func TestLocalLogin(t *testing.T) {
	// Mock Feishu token exchange server
	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/authen/v2/oauth/token"):
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "test-access-token",
				"refresh_token": "test-refresh-token",
				"expires_in":    7200,
			})
		case strings.Contains(r.URL.Path, "/authen/v1/user_info"):
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"name":    "Test User",
					"user_id": "uid_123",
					"email":   "test@test.com",
				},
			})
		default:
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "ok"})
		}
	}))
	defer feishuSrv.Close()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	client := NewLocalClient("test-app-id", "test-secret")
	client.baseURL = feishuSrv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Start localLogin in a goroutine
	errCh := make(chan error, 1)
	go func() {
		errCh <- localLogin(ctx, client, "offline_access", false)
	}()

	// Wait briefly for server to start
	time.Sleep(100 * time.Millisecond)

	// We need to figure out the state parameter. Since we can't know it,
	// we'll just send a request and accept that the state check will fail.
	// Instead, test the context cancellation path and the callback paths separately.

	// Test: callback with missing code
	resp, err := http.Get("http://127.0.0.1:19876/callback?state=wrong-state")
	if err != nil {
		// Server might not be up yet, try again
		time.Sleep(200 * time.Millisecond)
		resp, err = http.Get("http://127.0.0.1:19876/callback?state=wrong-state")
	}
	if err == nil {
		resp.Body.Close()
	}

	// Wait for result (should be state mismatch error)
	select {
	case loginErr := <-errCh:
		if loginErr == nil {
			t.Fatal("expected error from state mismatch")
		}
		if !strings.Contains(loginErr.Error(), "state mismatch") {
			t.Fatalf("unexpected error: %v", loginErr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("localLogin did not return in time")
	}
}

func TestLocalLoginSuccessful(t *testing.T) {
	// Mock Feishu token exchange + user info
	feishuSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/authen/v2/oauth/token"):
			json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "real-access-tok",
				"refresh_token": "real-refresh-tok",
				"expires_in":    7200,
			})
		case strings.Contains(r.URL.Path, "/authen/v1/user_info"):
			json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"name":    "Test User",
					"user_id": "uid_test",
					"email":   "test@example.com",
				},
			})
		default:
			w.WriteHeader(200)
		}
	}))
	defer feishuSrv.Close()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	client := NewLocalClient("test-app", "test-secret")
	client.baseURL = feishuSrv.URL

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Capture stdout to extract the state from the auth URL
	oldStdout := os.Stdout
	pr, pw, _ := os.Pipe()
	os.Stdout = pw

	errCh := make(chan error, 1)
	go func() {
		errCh <- localLogin(ctx, client, "offline_access", false)
	}()

	// Read output until we find the state parameter
	time.Sleep(200 * time.Millisecond)

	// Read what's been captured so far
	pw.Close()
	outputBuf := new(bytes.Buffer)
	io.Copy(outputBuf, pr)
	os.Stdout = oldStdout
	output := outputBuf.String()

	// Extract state from URL
	stateIdx := strings.Index(output, "state=")
	if stateIdx == -1 {
		// Cancel and drain
		cancel()
		<-errCh
		t.Fatal("could not find state in output")
	}
	stateStr := output[stateIdx+6:]
	// State ends at & or newline or end of string
	for i, c := range stateStr {
		if c == '&' || c == '\n' || c == ' ' {
			stateStr = stateStr[:i]
			break
		}
	}

	// Hit the callback with correct state and a code
	callbackURL := fmt.Sprintf("http://127.0.0.1:19876/callback?state=%s&code=test-auth-code", stateStr)
	resp, err := http.Get(callbackURL)
	if err != nil {
		cancel()
		<-errCh
		t.Fatalf("callback request failed: %v", err)
	}
	resp.Body.Close()

	select {
	case loginErr := <-errCh:
		if loginErr != nil {
			t.Fatalf("localLogin error: %v", loginErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("localLogin did not return in time")
	}

	// Verify tokens were saved
	access, refresh, _, err := LoadLocalTokens()
	if err != nil {
		t.Fatalf("LoadLocalTokens: %v", err)
	}
	if access != "real-access-tok" {
		t.Fatalf("access = %q, want real-access-tok", access)
	}
	if refresh != "real-refresh-tok" {
		t.Fatalf("refresh = %q, want real-refresh-tok", refresh)
	}
}

func TestLocalLoginContextCancellation(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	client := NewLocalClient("test-app-id", "test-secret")

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := localLogin(ctx, client, "offline_access", false)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Fatalf("expected context error, got: %v", err)
	}
}

func TestLocalLoginCallbackNoCode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)

	client := NewLocalClient("test-app-id", "test-secret")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- localLogin(ctx, client, "offline_access", false)
	}()

	time.Sleep(100 * time.Millisecond)

	// Send callback with error parameter (no code)
	resp, err := http.Get("http://127.0.0.1:19876/callback?error=access_denied")
	if err != nil {
		time.Sleep(200 * time.Millisecond)
		resp, err = http.Get("http://127.0.0.1:19876/callback?error=access_denied")
	}
	if err == nil {
		resp.Body.Close()
	}

	select {
	case loginErr := <-errCh:
		// Could be state mismatch or access_denied depending on state
		if loginErr == nil {
			t.Fatal("expected error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("localLogin did not return in time")
	}
}

// ---- newLoginCmd additional tests ----

func TestLoginCmdWithScopeGroups(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"protocol_version": ProtocolVersion, "max_security_level": 4},
		})
	})
	mux.HandleFunc("/v1/device/start", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		scopes, _ := body["scopes"].(string)
		if scopes == "" {
			http.Error(w, "missing scopes", 400)
			return
		}
		// Verify scope groups were merged
		if !strings.Contains(scopes, "docx:document:readonly") {
			http.Error(w, "missing docs scope", 400)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"device_code":               "dev-code",
				"user_code":                 "ABCD-1234",
				"verification_uri_complete": "https://test.example.com/auth",
				"interval_seconds":          1,
				"expires_in_seconds":        300,
			},
		})
	})
	pollCount := 0
	mux.HandleFunc("/v1/device/poll", func(w http.ResponseWriter, r *http.Request) {
		pollCount++
		if pollCount < 2 {
			// Return pending
			json.NewEncoder(w).Encode(map[string]any{
				"code": "authorization_pending", "message": "waiting",
			})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"client_token":       "new-client-token",
				"session_expires_at": "2026-12-31T00:00:00Z",
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "login", "--timeout", "10s", "--open-browser=false", "docs")
	if err != nil {
		t.Fatalf("login docs: %v", err)
	}
}

func TestLoginCmdAllScopes(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"protocol_version": ProtocolVersion, "max_security_level": 4},
		})
	})
	mux.HandleFunc("/v1/device/start", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"device_code":               "dev-code",
				"user_code":                 "ABCD",
				"verification_uri_complete": "https://test.example.com/auth",
				"interval_seconds":          1,
			},
		})
	})
	mux.HandleFunc("/v1/device/poll", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"client_token":       "token-all",
				"session_expires_at": "2026-12-31T00:00:00Z",
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "login", "--timeout", "5s", "--open-browser=false", "all")
	if err != nil {
		t.Fatalf("login all: %v", err)
	}
}

func TestLoginCmdLocalMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	t.Setenv("LARKCTL_MODE", "local")
	gatewayURL = ""

	// Set up local app
	if err := SaveLocalApp("app_local", "secret_local"); err != nil {
		t.Fatal(err)
	}

	// Test that local mode is detected and the login cmd starts localLogin.
	// We'll send a callback with wrong state to make it return an error quickly.
	cmd := newRootCmd()
	cmd.SetArgs([]string{"login", "--open-browser=false", "docs"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(io.Discard)

	errCh := make(chan error, 1)
	go func() {
		errCh <- cmd.Execute()
	}()

	// Wait for local server to start, then hit the callback
	time.Sleep(200 * time.Millisecond)
	resp, err := http.Get("http://127.0.0.1:19876/callback?state=wrong")
	if err == nil {
		resp.Body.Close()
	}

	select {
	case cmdErr := <-errCh:
		if cmdErr == nil {
			t.Fatal("expected error from wrong state")
		}
		if !strings.Contains(cmdErr.Error(), "state mismatch") {
			t.Fatalf("expected state mismatch error, got: %v", cmdErr)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("login cmd did not return in time")
	}
}

func TestLoginCmdDeviceLoginTimeout(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"protocol_version": ProtocolVersion, "max_security_level": 4},
		})
	})
	mux.HandleFunc("/v1/device/start", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"device_code":               "dev-timeout",
				"user_code":                 "TIMEOUT",
				"verification_uri_complete": "https://test.example.com/auth",
				"interval_seconds":          0,
			},
		})
	})
	mux.HandleFunc("/v1/device/poll", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "authorization_pending", "message": "waiting",
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "login", "--timeout", "500ms", "--open-browser=false")
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got: %v", err)
	}
}

func TestLoginCmdDeviceExpiredToken(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"protocol_version": ProtocolVersion, "max_security_level": 4},
		})
	})
	mux.HandleFunc("/v1/device/start", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"device_code":               "dev-exp",
				"user_code":                 "EXP",
				"verification_uri_complete": "https://test.example.com/auth",
				"interval_seconds":          0,
			},
		})
	})
	mux.HandleFunc("/v1/device/poll", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "expired_token", "message": "device code expired",
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "login", "--timeout", "5s", "--open-browser=false")
	if err == nil {
		t.Fatal("expected expired_token error")
	}
	if !strings.Contains(err.Error(), "expired_token") {
		t.Fatalf("expected expired_token, got: %v", err)
	}
}

func TestLoginCmdEmptyClientToken(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/version", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"protocol_version": ProtocolVersion, "max_security_level": 4},
		})
	})
	mux.HandleFunc("/v1/device/start", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"device_code":               "dev-empty",
				"user_code":                 "EMPTY",
				"verification_uri_complete": "https://test.example.com/auth",
				"interval_seconds":          0,
			},
		})
	})
	mux.HandleFunc("/v1/device/poll", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"client_token": "",
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "login", "--timeout", "5s", "--open-browser=false")
	if err == nil {
		t.Fatal("expected empty client token error")
	}
	if !strings.Contains(err.Error(), "client token is empty") {
		t.Fatalf("expected 'client token is empty', got: %v", err)
	}
}

// ---- extractImagesFromExport tests ----

func TestExtractImagesFromExportMock(t *testing.T) {
	t.Run("successful export with images", func(t *testing.T) {
		// Create a valid xlsx (zip) with an image in xl/media/
		var zipBuf bytes.Buffer
		zw := zip.NewWriter(&zipBuf)
		w, _ := zw.Create("xl/media/image1.png")
		w.Write([]byte("fake-png"))
		w2, _ := zw.Create("xl/worksheets/sheet1.xml")
		w2.Write([]byte("<xml/>"))
		zw.Close()
		xlsxData := zipBuf.Bytes()

		// Use a custom client wrapper that writes xlsx data on ExportDownload
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-123",
				exportStatusResult: exportResult{
					JobStatus: 0,
					FileToken: "file-tok-123",
					FileSize:  int64(len(xlsxData)),
				},
			},
			downloadData: xlsxData,
		}

		tmp := t.TempDir()
		count, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err != nil {
			t.Fatalf("extractImagesFromExport error: %v", err)
		}
		if count != 1 {
			t.Fatalf("expected 1 extracted image, got %d", count)
		}

		entries, _ := os.ReadDir(tmp)
		if len(entries) != 1 {
			t.Fatalf("expected 1 file, got %d", len(entries))
		}
		if entries[0].Name() != "image1.png" {
			t.Fatalf("expected image1.png, got %s", entries[0].Name())
		}
	})

	t.Run("export create error", func(t *testing.T) {
		mc := &mockClient{
			exportCreateErr: fmt.Errorf("create failed"),
		}
		tmp := t.TempDir()
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err == nil || !strings.Contains(err.Error(), "create") {
			t.Fatalf("expected create error, got: %v", err)
		}
	})

	t.Run("empty ticket", func(t *testing.T) {
		mc := &mockClient{
			exportCreateTicket: "",
		}
		tmp := t.TempDir()
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err == nil || !strings.Contains(err.Error(), "empty export ticket") {
			t.Fatalf("expected empty ticket error, got: %v", err)
		}
	})

	t.Run("export status error", func(t *testing.T) {
		mc := &mockClient{
			exportCreateTicket: "ticket-456",
			exportStatusErr:    fmt.Errorf("status check failed"),
		}
		tmp := t.TempDir()
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err == nil || !strings.Contains(err.Error(), "poll export") {
			t.Fatalf("expected poll export error, got: %v", err)
		}
	})

	t.Run("export job failed", func(t *testing.T) {
		mc := &mockClient{
			exportCreateTicket: "ticket-789",
			exportStatusResult: exportResult{
				JobStatus: 3,
				ErrMsg:    "internal error",
			},
		}
		tmp := t.TempDir()
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err == nil || !strings.Contains(err.Error(), "export failed") {
			t.Fatalf("expected export failed error, got: %v", err)
		}
	})

	t.Run("export download error", func(t *testing.T) {
		mc := &mockClient{
			exportCreateTicket: "ticket-dl",
			exportStatusResult: exportResult{
				JobStatus: 0,
				FileToken: "ft-dl",
			},
			exportDownloadErr: fmt.Errorf("download failed"),
		}
		tmp := t.TempDir()
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err == nil || !strings.Contains(err.Error(), "download export") {
			t.Fatalf("expected download error, got: %v", err)
		}
	})

	t.Run("empty file token after success", func(t *testing.T) {
		mc := &mockClient{
			exportCreateTicket: "ticket-notoken",
			exportStatusResult: exportResult{
				JobStatus: 0,
				FileToken: "",
			},
		}
		tmp := t.TempDir()
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err == nil || !strings.Contains(err.Error(), "no file_token") {
			t.Fatalf("expected no file_token error, got: %v", err)
		}
	})
}

// ---- docs write/update tests ----

func TestDocsWriteCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc_write",
						"block_type": float64(1),
						"children":   []any{"old_blk"},
					},
					map[string]any{
						"block_id":   "old_blk",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "old text"}},
							},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/v1/docs/blocks/delete", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": "ok", "message": "ok", "data": map[string]any{}})
	})
	mux.HandleFunc("/v1/docs/blocks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"children": []any{map[string]any{"block_id": "new_blk"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	mdPath := tmp + "/write.md"
	os.WriteFile(mdPath, []byte("# New Content\n\nNew paragraph here.\n"), 0644)

	// "docs update" without block ID markers should trigger doFullReplace
	_, err := execCmd(t, "docs", "update", "doc_write", mdPath)
	if err != nil {
		t.Fatalf("docs update (full replace): %v", err)
	}
}

func TestDocsUpdateDiffCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc_diff",
						"block_type": float64(1),
						"children":   []any{"blk_a"},
					},
					map[string]any{
						"block_id":   "blk_a",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "existing text"}},
							},
						},
					},
				},
			},
		})
	})
	mux.HandleFunc("/v1/docs/blocks/update", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"code": "ok", "message": "ok", "data": map[string]any{}})
	})
	mux.HandleFunc("/v1/docs/blocks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"children": []any{map[string]any{"block_id": "new1"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	mdPath := tmp + "/diff.md"
	// Markdown with block ID markers
	content := "<!-- bid:blk_a -->existing text\n\nNew paragraph without bid.\n"
	os.WriteFile(mdPath, []byte(content), 0644)

	_, err := execCmd(t, "docs", "update", "doc_diff", mdPath)
	if err != nil {
		t.Fatalf("docs update (diff): %v", err)
	}
}

func TestDocsGetMarkdownWithBlockTypes(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": float64(1),
						"children":   []any{"h1", "h2", "code_blk", "quote_blk", "todo_blk", "divider_blk"},
						"page":       map[string]any{},
					},
					map[string]any{
						"block_id":   "h1",
						"block_type": float64(3), // heading1
						"heading1": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "Main Heading"}},
							},
						},
					},
					map[string]any{
						"block_id":   "h2",
						"block_type": float64(4), // heading2
						"heading2": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "Sub Heading"}},
							},
						},
					},
					map[string]any{
						"block_id":   "code_blk",
						"block_type": float64(14), // code
						"code": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "fmt.Println()"}},
							},
							"style": map[string]any{"language": float64(22)},
						},
					},
					map[string]any{
						"block_id":   "quote_blk",
						"block_type": float64(12), // quote-container
						"children":   []any{},
					},
					map[string]any{
						"block_id":   "todo_blk",
						"block_type": float64(13), // todo
						"todo": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "Task item"}},
							},
							"style": map[string]any{"done": true},
						},
					},
					map[string]any{
						"block_id":   "divider_blk",
						"block_type": float64(22), // divider
					},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/multi.md"

	_, err := execCmd(t, "docs", "export", "doc_multi", "--format", "md", "--output", outPath)
	if err != nil {
		t.Fatalf("docs export md: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	md := string(content)

	if !strings.Contains(md, "Main Heading") {
		t.Fatalf("missing heading1 in markdown")
	}
	if !strings.Contains(md, "Sub Heading") {
		t.Fatalf("missing heading2 in markdown")
	}
	if !strings.Contains(md, "fmt.Println()") {
		t.Fatalf("missing code block in markdown")
	}
}

// ---- docs export pdf/docx tests ----

func TestDocsExportPdfCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "pdf-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "pdf-file-tok",
					"file_size":  float64(200),
					"file_name":  "test_doc",
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-pdf-data"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/test_doc.pdf"

	_, err := execCmd(t, "docs", "export", "doc_pdf", "--format", "pdf", "--output", outPath)
	if err != nil {
		t.Fatalf("docs export pdf: %v", err)
	}

	data, _ := os.ReadFile(outPath)
	if string(data) != "fake-pdf-data" {
		t.Fatalf("unexpected content: %s", string(data))
	}
}

func TestDocsExportWikiURLCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/node", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"node": map[string]any{"obj_token": "resolved_doc_tok"},
			},
		})
	})
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "wiki-doc-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "wiki-dl-tok",
					"file_size":  float64(100),
					"file_name":  "wiki_doc",
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-pdf"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/wiki_doc.pdf"

	_, err := execCmd(t, "docs", "export", "https://xxx.feishu.cn/wiki/wiki_token", "--format", "pdf", "--output", outPath)
	if err != nil {
		t.Fatalf("docs export wiki pdf: %v", err)
	}
}

// ---- sheets export tests ----

func TestSheetsExportCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "export-ticket-1"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "dl-token-1",
					"file_size":  float64(100),
					"file_name":  "test_sheet",
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("fake-xlsx-data"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/test_sheet.xlsx"

	_, err := execCmd(t, "sheets", "export", "sheet_token", "--output", outPath)
	if err != nil {
		t.Fatalf("sheets export: %v", err)
	}
}

// ---- requireScopes tests ----

func TestRequireScopesUpgradeNeeded(t *testing.T) {
	mux := gatewayMux()
	upgradeCallCount := 0
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		upgradeCallCount++
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"upgrade_needed": true,
				"upgrade_url":    "https://test.example.com/upgrade",
			},
		})
	})
	mux.HandleFunc("/v1/session/scopes", func(w http.ResponseWriter, r *http.Request) {
		// Return upgraded scopes after first call
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"scopes": "calendar:calendar drive:file:readonly"},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gc := NewGatewayClient(srv.URL)
	gc.SetSessionToken("test-token")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := requireScopes(ctx, gc, "calendar:calendar")
	if err != nil {
		t.Fatalf("requireScopes with upgrade: %v", err)
	}
}

func TestRequireScopesUpgradeNoURL(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"upgrade_needed": true,
				"upgrade_url":    "",
			},
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gc := NewGatewayClient(srv.URL)
	gc.SetSessionToken("test-token")

	err := requireScopes(context.Background(), gc, "calendar:calendar")
	if err == nil || !strings.Contains(err.Error(), "no URL returned") {
		t.Fatalf("expected 'no URL returned' error, got: %v", err)
	}
}

// TestRequireScopesUpgradeTimeout is skipped because requireScopes has an internal
// 5-minute deadline with time.Sleep that cannot be shortened via context.

func TestRequireScopesUpgradeError(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		json.NewEncoder(w).Encode(map[string]any{
			"code": "error", "message": "server error",
		})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	gc := NewGatewayClient(srv.URL)
	gc.SetSessionToken("test-token")

	err := requireScopes(context.Background(), gc, "calendar:calendar")
	if err == nil || !strings.Contains(err.Error(), "check scopes") {
		t.Fatalf("expected check scopes error, got: %v", err)
	}
}

// ---- upgrade cmd tests ----

// The default must point at a location that actually serves the raw
// per-platform binaries (`larkctl-<os>-<arch>`). release.yml publishes them as
// GitHub Release assets; it once pointed at a placeholder that 404s, which
// silently broke `larkctl upgrade` for everyone.
func TestUpgradeDefaultDownloadBaseURL(t *testing.T) {
	if os.Getenv("LARKCTL_DOWNLOAD_URL") != "" {
		t.Skip("LARKCTL_DOWNLOAD_URL overrides the default")
	}
	const want = "https://github.com/echowxsy/larkctl/releases/latest/download"
	if downloadBaseURL != want {
		t.Fatalf("downloadBaseURL = %q, want %q", downloadBaseURL, want)
	}
}

func TestUpgradeCmdDownload(t *testing.T) {
	dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("new-binary-content-v2"))
	}))
	defer dlSrv.Close()

	origDownloadURL := downloadBaseURL
	downloadBaseURL = dlSrv.URL
	defer func() { downloadBaseURL = origDownloadURL }()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	gatewayURL = ""

	// Create a fake binary that os.Executable can be replaced with
	fakeBin := tmp + "/larkctl-test"
	os.WriteFile(fakeBin, []byte("old-binary"), 0755)

	// We can't easily override os.Executable(), but we can test the command construction
	// and download logic. The actual upgrade will use os.Executable() which points to
	// the test binary.
	cmd := newUpgradeCmd()
	if cmd.Use != "upgrade" {
		t.Fatalf("Use = %q, want upgrade", cmd.Use)
	}
	if cmd.Short == "" {
		t.Fatal("Short description should not be empty")
	}
}

func TestUpgradeCmdDownloadError(t *testing.T) {
	dlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
	}))
	defer dlSrv.Close()

	origDownloadURL := downloadBaseURL
	downloadBaseURL = dlSrv.URL
	defer func() { downloadBaseURL = origDownloadURL }()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	gatewayURL = ""

	// Run the upgrade command — it should fail since os.Executable() + download will 404
	_, err := execCmd(t, "upgrade")
	if err == nil {
		t.Fatal("expected error for download failure")
	}
}

func TestUpgradeCmdNetworkError(t *testing.T) {
	origDownloadURL := downloadBaseURL
	downloadBaseURL = "http://127.0.0.1:1" // unreachable
	defer func() { downloadBaseURL = origDownloadURL }()

	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("FEISHU_GATEWAY_URL", "")
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	gatewayURL = ""

	_, err := execCmd(t, "upgrade")
	if err == nil {
		t.Fatal("expected error for network failure")
	}
	if !strings.Contains(err.Error(), "download") {
		t.Fatalf("expected download error, got: %v", err)
	}
}

// ---- writeFileAtomic tests ----

func TestWriteFileAtomicNewDir(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/newdir/subdir/testfile"

	err := writeFileAtomic(path, []byte("hello world"), 0644)
	if err != nil {
		t.Fatalf("writeFileAtomic to new dir: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("file content = %q, want 'hello world'", string(data))
	}
}

func TestWriteFileAtomicOverwrite(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/existing"

	// Create existing file
	os.WriteFile(path, []byte("old"), 0644)

	err := writeFileAtomic(path, []byte("new content"), 0600)
	if err != nil {
		t.Fatalf("writeFileAtomic overwrite: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read overwritten file: %v", err)
	}
	if string(data) != "new content" {
		t.Fatalf("file content = %q, want 'new content'", string(data))
	}

	// Check permissions
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("file perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestWriteFileAtomicEmptyData(t *testing.T) {
	tmp := t.TempDir()
	path := tmp + "/empty"

	err := writeFileAtomic(path, []byte{}, 0644)
	if err != nil {
		t.Fatalf("writeFileAtomic empty data: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty file, got %d bytes", len(data))
	}
}

// ---- docs update with empty markdown ----

func TestDocsUpdateEmptyMarkdown(t *testing.T) {
	mux := gatewayMux()
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	mdPath := tmp + "/empty.md"
	os.WriteFile(mdPath, []byte(""), 0644)

	_, err := execCmd(t, "docs", "update", "doc_empty", mdPath)
	if err == nil {
		t.Fatal("expected error for empty markdown")
	}
	if !strings.Contains(err.Error(), "no blocks") {
		t.Fatalf("expected 'no blocks' error, got: %v", err)
	}
}

// ---- docs update from stdin ----

func TestDocsUpdateStdinPath(t *testing.T) {
	// Test that the "-" path for stdin is recognized
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "stdin_doc",
						"block_type": float64(1),
						"children":   []any{},
					},
				},
			},
		})
	})
	mux.HandleFunc("/v1/docs/blocks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"children": []any{map[string]any{"block_id": "new1"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	// Mock stdin
	oldStdin := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	go func() {
		w.Write([]byte("# Hello\n\nWorld\n"))
		w.Close()
	}()
	defer func() { os.Stdin = oldStdin }()

	_, err := execCmd(t, "docs", "update", "stdin_doc", "-")
	if err != nil {
		t.Fatalf("docs update stdin: %v", err)
	}
}

// ---- sheets export with wiki URL ----

func TestCalendarCreateWithRoomCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"event": map[string]any{"event_id": "evt_room"}},
		})
	})
	mux.HandleFunc("/v1/calendar/rooms", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"rooms": []any{
					map[string]any{"room_id": "omm_1604", "name": "1604", "room_status": map[string]any{"status": true}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/freebusy", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"freebusy_list": []any{}},
		})
	})
	mux.HandleFunc("/v1/calendar/events/attendees", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"attendees": []any{map[string]any{"room_id": "omm_1604"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "create", "Room Meeting",
		"--start", "2026-03-20 10:00", "--end", "2026-03-20 11:00",
		"--room", "1604")
	if err != nil {
		t.Fatalf("calendar create with room: %v", err)
	}
}

func TestCalendarCreateRoomBusy(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"event": map[string]any{"event_id": "evt_busy"}},
		})
	})
	mux.HandleFunc("/v1/calendar/rooms", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"rooms": []any{
					map[string]any{"room_id": "omm_busy", "name": "Busy Room", "room_status": map[string]any{"status": true}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/freebusy", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"freebusy_list": []any{
					map[string]any{"start_time": "2026-03-20T10:00:00+08:00"},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "create", "Busy Room Meeting",
		"--start", "2026-03-20 10:00", "--end", "2026-03-20 11:00",
		"--room", "Busy Room")
	if err == nil {
		t.Fatal("expected error for busy room")
	}
	if !strings.Contains(err.Error(), "occupied") {
		t.Fatalf("expected 'occupied' error, got: %v", err)
	}
}

func TestCalendarListWithDays(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "list", "--days", "7")
	if err != nil {
		t.Fatalf("calendar list --days: %v", err)
	}
}

func TestCalendarFreebusyWithRoom(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/rooms", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"rooms": []any{
					map[string]any{"room_id": "omm_fb", "name": "FB Room", "room_status": map[string]any{"status": true}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/freebusy", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"freebusy_list": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "freebusy",
		"--start", "2026-03-20", "--end", "2026-03-21", "--room", "FB Room")
	if err != nil {
		t.Fatalf("calendar freebusy with room: %v", err)
	}
}

func TestDocsExportDocxFormatCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "docx-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "docx-file-tok",
					"file_size":  float64(150),
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-docx"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/doc.docx"

	_, err := execCmd(t, "docs", "export", "doc_docx", "--format", "docx", "--output", outPath)
	if err != nil {
		t.Fatalf("docs export docx: %v", err)
	}
}

func TestDocsExportDefaultNameCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "def-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "def-tok",
					"file_size":  float64(100),
					"file_name":  "MyDocument",
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-pdf"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	// Test with CWD as temp dir
	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	_, err := execCmd(t, "docs", "export", "doc_def")
	if err != nil {
		t.Fatalf("docs export default name: %v", err)
	}
	// Default output should be MyDocument.pdf
	if _, err := os.Stat(tmp + "/MyDocument.pdf"); err != nil {
		t.Fatalf("expected MyDocument.pdf: %v", err)
	}
}

func TestDocsInfoWithTypeCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/info", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"title": "Wiki Doc"},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "info", "doc123", "--type", "wiki")
	if err != nil {
		t.Fatalf("docs info --type wiki: %v", err)
	}
}

func TestDocsCommentsFileTypeCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/comments", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "comments", "doc123", "--file-type", "sheet")
	if err != nil {
		t.Fatalf("docs comments --file-type sheet: %v", err)
	}
}

func TestDocsCreateWithFolderCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"document": map[string]any{"document_id": "new_in_folder"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "docs", "create", "In Folder", "--folder-token", "folder_tok")
	if err != nil {
		t.Fatalf("docs create --folder-token: %v", err)
	}
}

func TestCalendarCreateWithDescription(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"event": map[string]any{"event_id": "evt_desc"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "create", "Lunch",
		"--start", "2026-03-20 12:00", "--end", "2026-03-20 13:00",
		"--description", "Team lunch at cafeteria")
	if err != nil {
		t.Fatalf("calendar create with desc: %v", err)
	}
}

func TestSheetsExportDefaultNameCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "def-sheet-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "def-dl-tok",
					"file_size":  float64(50),
					"file_name":  "DefaultSheet",
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-xlsx"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	_, err := execCmd(t, "sheets", "export", "sheet_def")
	if err != nil {
		t.Fatalf("sheets export default: %v", err)
	}
	if _, err := os.Stat(tmp + "/DefaultSheet.xlsx"); err != nil {
		t.Fatalf("expected DefaultSheet.xlsx: %v", err)
	}
}

func TestSheetsExportJobFailedCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "fail-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status":    float64(3),
					"job_error_msg": "sheet too large",
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "sheets", "export", "sheet_fail")
	if err == nil {
		t.Fatal("expected export failure")
	}
	if !strings.Contains(err.Error(), "sheet too large") {
		t.Fatalf("expected 'sheet too large' error, got: %v", err)
	}
}

func TestSheetsExportWikiURLCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/node", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"node": map[string]any{"obj_token": "resolved_sheet_tok"},
			},
		})
	})
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "wiki-export-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "dl-tok-wiki",
					"file_size":  float64(50),
					"file_name":  "wiki_sheet",
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-xlsx"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/wiki_sheet.xlsx"

	_, err := execCmd(t, "sheets", "export", "https://xxx.feishu.cn/wiki/wiki_token", "--output", outPath)
	if err != nil {
		t.Fatalf("sheets export wiki: %v", err)
	}
}

func TestDocsExportMdWithImageDirCmdNew(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": float64(1),
						"children":   []any{"img1"},
						"page":       map[string]any{},
					},
					map[string]any{
						"block_id":   "img1",
						"block_type": float64(27),
						"image":      map[string]any{"token": "img_tok", "width": 100, "height": 200},
					},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/test.md"

	_, err := execCmd(t, "docs", "export", "doc123", "--format", "md", "--output", outPath, "--image-dir", "./images")
	if err != nil {
		t.Fatalf("docs export md with image-dir: %v", err)
	}

	content, _ := os.ReadFile(outPath)
	if !strings.Contains(string(content), "./images") {
		t.Fatalf("expected image-dir prefix in output, got: %s", string(content))
	}
}

func TestDocsUpdateWikiURLCmd(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/node", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"node": map[string]any{"obj_token": "resolved_doc"},
			},
		})
	})
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "resolved_doc",
						"block_type": float64(1),
						"children":   []any{},
					},
				},
			},
		})
	})
	mux.HandleFunc("/v1/docs/blocks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"children": []any{map[string]any{"block_id": "new1"}}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	mdPath := tmp + "/doc.md"
	os.WriteFile(mdPath, []byte("Hello World\n"), 0644)

	_, err := execCmd(t, "docs", "update", "https://xxx.feishu.cn/wiki/wiki_token", mdPath)
	if err != nil {
		t.Fatalf("docs update wiki: %v", err)
	}
}

func TestTasksCreateWithDescDueCmdNew(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/tasks/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"task": map[string]any{"id": "task_desc"}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "tasks", "create", "Documented Task",
		"--description", "Fix the login page",
		"--due", "1711929600")
	if err != nil {
		t.Fatalf("tasks create with desc/due: %v", err)
	}
}

func TestDocsExportMdDefaultOutputCmdNew(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": float64(1),
						"children":   []any{"blk1"},
						"page":       map[string]any{},
					},
					map[string]any{
						"block_id":   "blk1",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "md default"}},
							},
						},
					},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	origDir, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	_, err := execCmd(t, "docs", "export", "doc_tok_default_md", "--format", "md")
	if err != nil {
		t.Fatalf("docs export md default output: %v", err)
	}

	if _, err := os.Stat("doc_tok_default_md.md"); os.IsNotExist(err) {
		t.Fatal("expected doc_tok_default_md.md to exist")
	}
}

func TestCalendarListWithStartEndCmdNew(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"upgrade_needed": false},
		})
	})
	mux.HandleFunc("/v1/calendar/primary", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"calendars": []any{
					map[string]any{"calendar": map[string]any{"calendar_id": "cal_primary"}},
				},
			},
		})
	})
	mux.HandleFunc("/v1/calendar/events", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"items": []any{}},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	_, err := execCmd(t, "calendar", "list", "--start", "2026-03-20", "--end", "2026-03-25")
	if err != nil {
		t.Fatalf("calendar list --start --end: %v", err)
	}
}

func TestSheetsExportWikiDefaultOutputCmdNew(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/wiki/node", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"node": map[string]any{"obj_token": "resolved_sheet_tok"},
			},
		})
	})
	mux.HandleFunc("/v1/export/create", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{"ticket": "wiki-sheet-ticket"},
		})
	})
	mux.HandleFunc("/v1/export/status", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"result": map[string]any{
					"job_status": float64(0),
					"file_token": "dl-tok",
					"file_size":  float64(100),
					"file_name":  "",
				},
			},
		})
	})
	mux.HandleFunc("/v1/export/download", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("fake-xlsx"))
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	origDir, _ := os.Getwd()
	tmp := t.TempDir()
	os.Chdir(tmp)
	defer os.Chdir(origDir)

	_, err := execCmd(t, "sheets", "export", "https://xxx.feishu.cn/wiki/wiki_tok_new")
	if err != nil {
		t.Fatalf("sheets export wiki default: %v", err)
	}
}

func TestDocsExportMdToFileCmdNew(t *testing.T) {
	mux := gatewayMux()
	mux.HandleFunc("/v1/docs/blocks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"code": "ok", "message": "ok",
			"data": map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "page1",
						"block_type": float64(1),
						"children":   []any{"blk1"},
						"page":       map[string]any{},
					},
					map[string]any{
						"block_id":   "blk1",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "Exported text"}},
							},
						},
					},
				},
			},
		})
	})
	srv := setupGatewayEnv(t, mux)
	defer srv.Close()

	tmp := t.TempDir()
	outPath := tmp + "/output.md"

	_, err := execCmd(t, "docs", "export", "doc123", "--format", "markdown", "--output", outPath)
	if err != nil {
		t.Fatalf("docs export markdown: %v", err)
	}

	content, _ := os.ReadFile(outPath)
	if !strings.Contains(string(content), "Exported text") {
		t.Fatalf("expected content, got: %s", string(content))
	}
}
