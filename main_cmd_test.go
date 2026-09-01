package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- mock client ----------

// uploadedDocsMedia records one UploadDocsMedia call for assertions.
type uploadedDocsMedia struct {
	ParentType string
	ParentNode string
	FileName   string
	Size       int64
}

// mockClient implements FeishuClient for testing pure logic functions.
type mockClient struct {
	// Canned responses
	whoAmIResp           map[string]any
	getDocBlocksResp     any
	getWikiNodeResp      any
	getSheetValuesResp   any
	getSheetsFloatImages any
	createBlocksResp     any
	deleteBlockResp      any
	updateBlocksResp     any
	searchDocsResp       any
	createDocResp        any
	createCommentResp    any
	replyCommentResp     any
	resolveCommentResp   any
	listPermsResp        any
	listDriveResp        any
	createFolderResp     any
	getBitableMetaResp   any
	listBitTablesResp    any
	listBitFieldsResp    any
	listBitRecordsResp   any
	createBitRecordResp  any
	updateBitRecordResp  any
	createBitFieldResp   any
	updateBitFieldResp   any
	deleteBitFieldResp   any
	updateSheetResp      any
	appendSheetResp      any
	listWikiSpacesResp   any
	listWikiNodesResp    any
	createWikiNodeResp   any
	getBoardNodesResp    any
	createTaskResp       any
	searchUsersResp      any
	calendarPrimaryResp  any
	listCalEventsResp    any
	createCalEventResp   any
	addAttendeesResp     any
	freebusyResp         any
	listRoomsResp        any
	docInfoResp          any
	getDocCommentsResp   any
	exportCreateTicket   string
	exportStatusResult   exportResult
	downloadMediaCT      string
	uploadDocsMediaToken string
	uploadedDocsMedia    []uploadedDocsMedia

	// Error responses
	whoAmIErr          error
	getDocBlocksErr    error
	createBlocksErr    error
	deleteBlockErr     error
	updateBlocksErr    error
	searchUsersErr     error
	calendarErr        error
	listRoomsErr       error
	freebusyErr        error
	exportCreateErr    error
	exportStatusErr    error
	exportDownloadErr  error
	uploadDocsMediaErr error
	downloadMediaErr   error
	getSheetValErr     error
	getFloatImgErr     error

	// Tracking
	createBlocksCalls  []createBlocksCall
	deleteBlockCalls   []deleteBlockCall
	updateBlocksCalls  []any
	downloadMediaCalls []string
}

type createBlocksCall struct {
	docID   string
	blockID string
	body    any
}

type deleteBlockCall struct {
	docID   string
	blockID string
}

func (m *mockClient) WhoAmI(ctx context.Context) (map[string]any, error) {
	return m.whoAmIResp, m.whoAmIErr
}
func (m *mockClient) GetDocumentInfo(ctx context.Context, input, docType string) (any, error) {
	return m.docInfoResp, nil
}
func (m *mockClient) GetDocumentBlocks(ctx context.Context, input string) (any, error) {
	return m.getDocBlocksResp, m.getDocBlocksErr
}
func (m *mockClient) GetWikiNode(ctx context.Context, input string) (any, error) {
	return m.getWikiNodeResp, nil
}
func (m *mockClient) GetSheetsMeta(ctx context.Context, spreadsheetToken string) (any, error) {
	return nil, nil
}
func (m *mockClient) GetSheetValues(ctx context.Context, spreadsheetToken, sheetID, rangeRef string, query url.Values) (any, error) {
	return m.getSheetValuesResp, m.getSheetValErr
}
func (m *mockClient) GetDocumentComments(ctx context.Context, fileToken, fileType string) (any, error) {
	return m.getDocCommentsResp, nil
}
func (m *mockClient) CreateDocumentBlocks(ctx context.Context, docID, blockID string, body any) (any, error) {
	m.createBlocksCalls = append(m.createBlocksCalls, createBlocksCall{docID, blockID, body})
	if m.createBlocksErr != nil {
		return nil, m.createBlocksErr
	}
	if m.createBlocksResp != nil {
		return m.createBlocksResp, nil
	}
	// Default: return children count based on body
	if bodyMap, ok := body.(map[string]any); ok {
		if children, ok := bodyMap["children"].([]map[string]any); ok {
			created := make([]any, len(children))
			return map[string]any{"children": created}, nil
		}
	}
	return map[string]any{"children": []any{}}, nil
}
func (m *mockClient) DeleteDocumentBlock(ctx context.Context, docID, blockID string) (any, error) {
	m.deleteBlockCalls = append(m.deleteBlockCalls, deleteBlockCall{docID, blockID})
	return m.deleteBlockResp, m.deleteBlockErr
}
func (m *mockClient) UpdateDocumentBlocks(ctx context.Context, docID string, body any) (any, error) {
	m.updateBlocksCalls = append(m.updateBlocksCalls, body)
	return m.updateBlocksResp, m.updateBlocksErr
}
func (m *mockClient) SearchDocs(ctx context.Context, body any) (any, error) {
	return m.searchDocsResp, nil
}
func (m *mockClient) CreateDocument(ctx context.Context, body any) (any, error) {
	return m.createDocResp, nil
}
func (m *mockClient) CreateDocumentComment(ctx context.Context, fileToken, fileType string, body any) (any, error) {
	return m.createCommentResp, nil
}
func (m *mockClient) ReplyDocumentComment(ctx context.Context, fileToken, commentID, fileType string, body any) (any, error) {
	return m.replyCommentResp, nil
}
func (m *mockClient) ResolveDocumentComment(ctx context.Context, fileToken, commentID, fileType string, resolved bool) (any, error) {
	return m.resolveCommentResp, nil
}
func (m *mockClient) ListDocumentPermissions(ctx context.Context, token, fileType string) (any, error) {
	return m.listPermsResp, nil
}
func (m *mockClient) ListDriveFiles(ctx context.Context, folderToken string) (any, error) {
	return m.listDriveResp, nil
}
func (m *mockClient) CreateFolder(ctx context.Context, body any) (any, error) {
	return m.createFolderResp, nil
}
func (m *mockClient) GetBitableMeta(ctx context.Context, appToken string) (any, error) {
	return m.getBitableMetaResp, nil
}
func (m *mockClient) ListBitableTables(ctx context.Context, appToken string) (any, error) {
	return m.listBitTablesResp, nil
}
func (m *mockClient) ListBitableFields(ctx context.Context, appToken, tableID string) (any, error) {
	return m.listBitFieldsResp, nil
}
func (m *mockClient) ListBitableRecords(ctx context.Context, appToken, tableID string, extra url.Values) (any, error) {
	return m.listBitRecordsResp, nil
}
func (m *mockClient) CreateBitableRecord(ctx context.Context, appToken, tableID string, body any) (any, error) {
	return m.createBitRecordResp, nil
}
func (m *mockClient) UpdateBitableRecord(ctx context.Context, appToken, tableID, recordID string, body any) (any, error) {
	return m.updateBitRecordResp, nil
}
func (m *mockClient) CreateBitableField(ctx context.Context, appToken, tableID string, body any) (any, error) {
	return m.createBitFieldResp, nil
}
func (m *mockClient) UpdateBitableField(ctx context.Context, appToken, tableID, fieldID string, body any) (any, error) {
	return m.updateBitFieldResp, nil
}
func (m *mockClient) DeleteBitableField(ctx context.Context, appToken, tableID, fieldID string) (any, error) {
	return m.deleteBitFieldResp, nil
}
func (m *mockClient) ListBitableViews(ctx context.Context, appToken, tableID string) (any, error) {
	return nil, nil
}
func (m *mockClient) GetBitableView(ctx context.Context, appToken, tableID, viewID string) (any, error) {
	return nil, nil
}
func (m *mockClient) CreateBitableView(ctx context.Context, appToken, tableID string, body any) (any, error) {
	return nil, nil
}
func (m *mockClient) UpdateBitableView(ctx context.Context, appToken, tableID, viewID string, body any) (any, error) {
	return nil, nil
}
func (m *mockClient) DeleteBitableView(ctx context.Context, appToken, tableID, viewID string) (any, error) {
	return nil, nil
}
func (m *mockClient) UpdateSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error) {
	return m.updateSheetResp, nil
}
func (m *mockClient) AppendSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error) {
	return m.appendSheetResp, nil
}
func (m *mockClient) ListWikiSpaces(ctx context.Context) (any, error) {
	return m.listWikiSpacesResp, nil
}
func (m *mockClient) ListWikiNodes(ctx context.Context, spaceID, parentNodeToken string) (any, error) {
	return m.listWikiNodesResp, nil
}
func (m *mockClient) CreateWikiNode(ctx context.Context, spaceID string, body any) (any, error) {
	return m.createWikiNodeResp, nil
}
func (m *mockClient) GetBoardNodes(ctx context.Context, whiteboardID string) (any, error) {
	return m.getBoardNodesResp, nil
}
func (m *mockClient) CreateBoardNodes(ctx context.Context, whiteboardID string, body any, clientToken string) (any, error) {
	return nil, nil
}
func (m *mockClient) DeleteBoardNodes(ctx context.Context, whiteboardID string, nodeIDs []string) (any, error) {
	return nil, nil
}
func (m *mockClient) CreateTask(ctx context.Context, body any) (any, error) {
	return m.createTaskResp, nil
}
func (m *mockClient) SearchUsers(ctx context.Context, query string, pageSize int) (any, error) {
	return m.searchUsersResp, m.searchUsersErr
}
func (m *mockClient) GetCalendarPrimary(ctx context.Context) (any, error) {
	return m.calendarPrimaryResp, m.calendarErr
}
func (m *mockClient) ListCalendarEvents(ctx context.Context, calendarID, startTime, endTime string) (any, error) {
	return m.listCalEventsResp, nil
}
func (m *mockClient) CreateCalendarEvent(ctx context.Context, calendarID string, body any) (any, error) {
	return m.createCalEventResp, nil
}
func (m *mockClient) AddCalendarAttendees(ctx context.Context, calendarID, eventID string, body any) (any, error) {
	return m.addAttendeesResp, nil
}
func (m *mockClient) GetFreebusy(ctx context.Context, body any) (any, error) {
	return m.freebusyResp, m.freebusyErr
}
func (m *mockClient) ListRooms(ctx context.Context, keyword string) (any, error) {
	return m.listRoomsResp, m.listRoomsErr
}
func (m *mockClient) GetSheetsFloatImages(ctx context.Context, spreadsheetToken, sheetID string) (any, error) {
	return m.getSheetsFloatImages, m.getFloatImgErr
}
func (m *mockClient) ExportCreate(ctx context.Context, token, fileType, fileExtension string) (string, error) {
	return m.exportCreateTicket, m.exportCreateErr
}
func (m *mockClient) ExportStatus(ctx context.Context, ticket, token string) (exportResult, error) {
	return m.exportStatusResult, m.exportStatusErr
}
func (m *mockClient) ExportDownload(ctx context.Context, fileToken string, w io.Writer) error {
	return m.exportDownloadErr
}
func (m *mockClient) DownloadMedia(ctx context.Context, fileToken string, w io.Writer) (string, error) {
	m.downloadMediaCalls = append(m.downloadMediaCalls, fileToken)
	if m.downloadMediaErr != nil {
		return "", m.downloadMediaErr
	}
	_, _ = w.Write([]byte("fake-image-data"))
	return m.downloadMediaCT, nil
}

func (m *mockClient) SendMessage(ctx context.Context, body any) (any, error) { return nil, nil }
func (m *mockClient) ListMessages(ctx context.Context, containerID string, query url.Values) (any, error) {
	return nil, nil
}
func (m *mockClient) SearchMessages(ctx context.Context, body any, query url.Values) (any, error) {
	return nil, nil
}
func (m *mockClient) ReplyMessage(ctx context.Context, messageID string, body any) (any, error) {
	return nil, nil
}
func (m *mockClient) ListTasks(ctx context.Context, query url.Values) (any, error) { return nil, nil }
func (m *mockClient) UpdateTask(ctx context.Context, taskID string, body any) (any, error) {
	return nil, nil
}
func (m *mockClient) CompleteTask(ctx context.Context, taskID string, completed bool) (any, error) {
	return nil, nil
}
func (m *mockClient) CommentTask(ctx context.Context, taskID, content string) (any, error) {
	return nil, nil
}
func (m *mockClient) SearchChats(ctx context.Context, query string, pageSize int) (any, error) {
	return nil, nil
}
func (m *mockClient) MGetMessages(ctx context.Context, messageIDs []string) (any, error) {
	return nil, nil
}
func (m *mockClient) DownloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string, w io.Writer) error {
	return nil
}
func (m *mockClient) ListThreadMessages(ctx context.Context, threadID string, query url.Values) (any, error) {
	return nil, nil
}
func (m *mockClient) UploadIMImage(ctx context.Context, fileName string, fileReader io.Reader) (string, error) {
	return "img_v3_mock", nil
}
func (m *mockClient) UploadIMFile(ctx context.Context, fileType, fileName string, fileReader io.Reader) (string, error) {
	return "file_v3_mock", nil
}
func (m *mockClient) ReactMessage(ctx context.Context, messageID, emojiType string) (any, error) {
	return nil, nil
}
func (m *mockClient) ListMessageReactions(ctx context.Context, messageID string, query url.Values) (any, error) {
	return nil, nil
}
func (m *mockClient) DeleteMessageReaction(ctx context.Context, messageID, reactionID string) (any, error) {
	return nil, nil
}
func (m *mockClient) ManageTaskMembers(ctx context.Context, taskID, action string, members []map[string]any) (any, error) {
	return nil, nil
}
func (m *mockClient) ManageTaskReminders(ctx context.Context, taskID, action string, reminders []map[string]any) (any, error) {
	return nil, nil
}
func (m *mockClient) CreateTasklist(ctx context.Context, name string) (any, error) {
	return nil, nil
}
func (m *mockClient) AddTaskToTasklist(ctx context.Context, taskID, tasklistID string) (any, error) {
	return nil, nil
}
func (m *mockClient) ManageTasklistMembers(ctx context.Context, tasklistID, action string, members []map[string]any) (any, error) {
	return nil, nil
}
func (m *mockClient) UploadDocsMedia(ctx context.Context, parentType, parentNode, fileName string, fileReader io.Reader, fileSize int64) (string, error) {
	if m.uploadDocsMediaErr != nil {
		return "", m.uploadDocsMediaErr
	}
	m.uploadedDocsMedia = append(m.uploadedDocsMedia, uploadedDocsMedia{
		ParentType: parentType,
		ParentNode: parentNode,
		FileName:   fileName,
		Size:       fileSize,
	})
	if m.uploadDocsMediaToken != "" {
		return m.uploadDocsMediaToken, nil
	}
	return "media_mock_token", nil
}
func (m *mockClient) UploadFile(ctx context.Context, parentToken, fileName string, fileReader io.Reader, fileSize int64) (any, error) {
	return nil, nil
}
func (m *mockClient) DownloadFile(ctx context.Context, fileToken string, w io.Writer) error {
	return nil
}
func (m *mockClient) ImportUpload(ctx context.Context, fileName, docType, fileExt string, fileReader io.Reader, fileSize int64) (string, error) {
	return "", nil
}
func (m *mockClient) ImportCreate(ctx context.Context, fileToken, fileType, fileExt, fileName, folderToken string) (string, error) {
	return "", nil
}
func (m *mockClient) ImportStatus(ctx context.Context, ticket string) (any, error) {
	return nil, nil
}
func (m *mockClient) RSVPCalendarEvent(ctx context.Context, calendarID, eventID, rsvpStatus string) (any, error) {
	return nil, nil
}

func (m *mockClient) ListMailMessages(ctx context.Context, query url.Values) (any, error) {
	return nil, nil
}
func (m *mockClient) GetMailMessage(ctx context.Context, messageID, format string) (any, error) {
	return nil, nil
}
func (m *mockClient) BatchGetMailMessages(ctx context.Context, body any) (any, error) {
	return nil, nil
}
func (m *mockClient) GetMailThread(ctx context.Context, threadID, format string, includeSpamTrash bool) (any, error) {
	return nil, nil
}
func (m *mockClient) SearchMail(ctx context.Context, body any, query url.Values) (any, error) {
	return nil, nil
}
func (m *mockClient) TrashMailMessage(ctx context.Context, messageID string) (any, error) {
	return nil, nil
}
func (m *mockClient) GetMailSendStatus(ctx context.Context, messageID string) (any, error) {
	return nil, nil
}
func (m *mockClient) GetMailAttachmentDownloadURL(ctx context.Context, messageID string, attachmentIDs []string) (any, error) {
	return nil, nil
}
func (m *mockClient) CreateMailDraft(ctx context.Context, rawEML string) (any, error) {
	return nil, nil
}
func (m *mockClient) UpdateMailDraft(ctx context.Context, draftID, rawEML string) (any, error) {
	return nil, nil
}
func (m *mockClient) SendMailDraft(ctx context.Context, draftID, sendTime string) (any, error) {
	return nil, nil
}
func (m *mockClient) DeleteMailDraft(ctx context.Context, draftID string) (any, error) {
	return nil, nil
}
func (m *mockClient) ListMailFolders(ctx context.Context, folderType int) (any, error) {
	return nil, nil
}
func (m *mockClient) ListAccessibleMailboxes(ctx context.Context) (any, error) {
	return nil, nil
}
func (m *mockClient) GetMailSendAs(ctx context.Context) (any, error) {
	return nil, nil
}
func (m *mockClient) GetMailProfile(ctx context.Context) (any, error) {
	return nil, nil
}

// ---------- tests ----------

func TestPrintJSON(t *testing.T) {
	// Capture stdout
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Save and restore compactOutput
	origCompact := compactOutput
	defer func() { compactOutput = origCompact }()

	t.Run("pretty output", func(t *testing.T) {
		compactOutput = false
		printJSON(map[string]string{"key": "value"})
	})

	t.Run("compact output", func(t *testing.T) {
		compactOutput = true
		printJSON(map[string]string{"key": "value"})
	})

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old

	output := buf.String()
	// Pretty output should contain indentation
	if !strings.Contains(output, "\"key\"") {
		t.Fatalf("printJSON output missing key: %s", output)
	}
}

func TestPrintJSON_Types(t *testing.T) {
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	origCompact := compactOutput
	compactOutput = true
	defer func() { compactOutput = origCompact }()

	printJSON(nil)
	printJSON(42)
	printJSON("hello")
	printJSON([]int{1, 2, 3})

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = old

	output := buf.String()
	if !strings.Contains(output, "null") {
		t.Fatalf("printJSON(nil) should output null, got: %s", output)
	}
	if !strings.Contains(output, "42") {
		t.Fatalf("printJSON(42) should output 42, got: %s", output)
	}
}

func TestEntryComparableContent(t *testing.T) {
	t.Run("returns comparable field", func(t *testing.T) {
		entry := mdBlockEntry{
			BlockID:    "blk_123",
			BlockType:  2,
			Comparable: "hello world",
		}
		got := entryComparableContent(entry)
		if got != "hello world" {
			t.Fatalf("entryComparableContent() = %q, want %q", got, "hello world")
		}
	})

	t.Run("empty comparable", func(t *testing.T) {
		entry := mdBlockEntry{BlockID: "blk_456", BlockType: 2}
		got := entryComparableContent(entry)
		if got != "" {
			t.Fatalf("entryComparableContent() = %q, want empty", got)
		}
	})
}

func TestDoFullReplace(t *testing.T) {
	t.Run("replaces existing blocks", func(t *testing.T) {
		mc := &mockClient{
			getDocBlocksResp: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(btPage),
						"children":   []any{"child1", "child2"},
					},
					map[string]any{"block_id": "child1", "block_type": float64(2)},
					map[string]any{"block_id": "child2", "block_type": float64(2)},
				},
			},
			createBlocksResp: map[string]any{
				"children": []any{
					map[string]any{"block_id": "new1"},
				},
			},
		}

		entries := []mdBlockEntry{
			{
				BlockType:  2,
				Body:       map[string]any{"block_type": 2, "text": map[string]any{"elements": []any{map[string]any{"text_run": map[string]any{"content": "new text"}}}}},
				Comparable: "new text",
			},
		}

		err := doFullReplace(context.Background(), mc, "doc123", entries)
		if err != nil {
			t.Fatalf("doFullReplace() error = %v", err)
		}

		// Should have deleted 2 existing blocks
		if len(mc.deleteBlockCalls) != 2 {
			t.Fatalf("expected 2 delete calls, got %d", len(mc.deleteBlockCalls))
		}

		// Should have created new blocks
		if len(mc.createBlocksCalls) == 0 {
			t.Fatal("expected create blocks calls")
		}
	})

	t.Run("no existing children", func(t *testing.T) {
		mc := &mockClient{
			getDocBlocksResp: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(btPage),
						"children":   []any{},
					},
				},
			},
			createBlocksResp: map[string]any{
				"children": []any{map[string]any{"block_id": "new1"}},
			},
		}

		entries := []mdBlockEntry{
			{BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "text"},
		}

		err := doFullReplace(context.Background(), mc, "doc123", entries)
		if err != nil {
			t.Fatalf("doFullReplace() error = %v", err)
		}
		if len(mc.deleteBlockCalls) != 0 {
			t.Fatalf("expected no delete calls, got %d", len(mc.deleteBlockCalls))
		}
	})

	t.Run("get blocks error", func(t *testing.T) {
		mc := &mockClient{
			getDocBlocksErr: fmt.Errorf("api error"),
		}
		entries := []mdBlockEntry{{BlockType: 2, Body: map[string]any{}, Comparable: "text"}}
		err := doFullReplace(context.Background(), mc, "doc123", entries)
		if err == nil || !strings.Contains(err.Error(), "get blocks") {
			t.Fatalf("expected 'get blocks' error, got: %v", err)
		}
	})
}

func TestDoDiffUpdate(t *testing.T) {
	t.Run("no changes needed", func(t *testing.T) {
		mc := &mockClient{
			getDocBlocksResp: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(btPage),
						"children":   []any{"blk1"},
					},
					map[string]any{
						"block_id":   "blk1",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "hello"}},
							},
						},
					},
				},
			},
		}

		entries := []mdBlockEntry{
			{BlockID: "blk1", BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "hello"},
		}

		err := doDiffUpdate(context.Background(), mc, "doc123", entries)
		if err != nil {
			t.Fatalf("doDiffUpdate() error = %v", err)
		}
		if len(mc.updateBlocksCalls) != 0 {
			t.Fatalf("expected no update calls, got %d", len(mc.updateBlocksCalls))
		}
		if len(mc.deleteBlockCalls) != 0 {
			t.Fatalf("expected no delete calls, got %d", len(mc.deleteBlockCalls))
		}
	})

	t.Run("delete removed blocks", func(t *testing.T) {
		mc := &mockClient{
			getDocBlocksResp: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(btPage),
						"children":   []any{"blk1", "blk2"},
					},
					map[string]any{
						"block_id":   "blk1",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "keep"}},
							},
						},
					},
					map[string]any{
						"block_id":   "blk2",
						"block_type": float64(2),
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "delete me"}},
							},
						},
					},
				},
			},
		}

		// Only keep blk1, blk2 should be deleted
		entries := []mdBlockEntry{
			{BlockID: "blk1", BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "keep"},
		}

		err := doDiffUpdate(context.Background(), mc, "doc123", entries)
		if err != nil {
			t.Fatalf("doDiffUpdate() error = %v", err)
		}
		if len(mc.deleteBlockCalls) != 1 {
			t.Fatalf("expected 1 delete call, got %d", len(mc.deleteBlockCalls))
		}
		if mc.deleteBlockCalls[0].blockID != "blk2" {
			t.Fatalf("expected delete blk2, got %s", mc.deleteBlockCalls[0].blockID)
		}
	})

	t.Run("create new blocks", func(t *testing.T) {
		mc := &mockClient{
			getDocBlocksResp: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(btPage),
						"children":   []any{},
					},
				},
			},
			createBlocksResp: map[string]any{
				"children": []any{map[string]any{"block_id": "new1"}},
			},
		}

		entries := []mdBlockEntry{
			{BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "new text"},
		}

		err := doDiffUpdate(context.Background(), mc, "doc123", entries)
		if err != nil {
			t.Fatalf("doDiffUpdate() error = %v", err)
		}
		if len(mc.createBlocksCalls) == 0 {
			t.Fatal("expected create blocks calls")
		}
	})

	t.Run("get blocks error", func(t *testing.T) {
		mc := &mockClient{getDocBlocksErr: fmt.Errorf("api error")}
		entries := []mdBlockEntry{{BlockID: "blk1", BlockType: 2, Body: map[string]any{}, Comparable: "text"}}
		err := doDiffUpdate(context.Background(), mc, "doc123", entries)
		if err == nil || !strings.Contains(err.Error(), "get blocks") {
			t.Fatalf("expected error, got: %v", err)
		}
	})

	t.Run("block type mismatch error", func(t *testing.T) {
		mc := &mockClient{
			getDocBlocksResp: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(btPage),
						"children":   []any{"blk1"},
					},
					map[string]any{
						"block_id":   "blk1",
						"block_type": float64(2), // text
						"text": map[string]any{
							"elements": []any{
								map[string]any{"text_run": map[string]any{"content": "text"}},
							},
						},
					},
				},
			},
		}

		entries := []mdBlockEntry{
			{BlockID: "blk1", BlockType: 3, Body: map[string]any{"block_type": 3}, Comparable: "heading", SourceLine: 5},
		}

		err := doDiffUpdate(context.Background(), mc, "doc123", entries)
		if err == nil || !strings.Contains(err.Error(), "cannot change block") {
			t.Fatalf("expected 'cannot change block' error, got: %v", err)
		}
	})
}

func TestBatchCreateBlocks(t *testing.T) {
	t.Run("single batch", func(t *testing.T) {
		mc := &mockClient{
			createBlocksResp: map[string]any{
				"children": []any{
					map[string]any{"block_id": "new1"},
					map[string]any{"block_id": "new2"},
				},
			},
		}

		blocks := []map[string]any{
			{"block_type": 2, "text": map[string]any{}},
			{"block_type": 2, "text": map[string]any{}},
		}

		err := batchCreateBlocks(context.Background(), mc, "doc123", "", blocks)
		if err != nil {
			t.Fatalf("batchCreateBlocks() error = %v", err)
		}
		if len(mc.createBlocksCalls) != 1 {
			t.Fatalf("expected 1 batch call, got %d", len(mc.createBlocksCalls))
		}
	})

	t.Run("multiple batches", func(t *testing.T) {
		mc := &mockClient{
			createBlocksResp: map[string]any{
				"children": []any{map[string]any{"block_id": "new"}},
			},
		}

		// Create 25 blocks -> should be 3 batches (10+10+5)
		blocks := make([]map[string]any, 25)
		for i := range blocks {
			blocks[i] = map[string]any{"block_type": 2}
		}

		err := batchCreateBlocks(context.Background(), mc, "doc123", "", blocks)
		if err != nil {
			t.Fatalf("batchCreateBlocks() error = %v", err)
		}
		if len(mc.createBlocksCalls) != 3 {
			t.Fatalf("expected 3 batch calls, got %d", len(mc.createBlocksCalls))
		}
	})

	t.Run("empty blocks", func(t *testing.T) {
		mc := &mockClient{}
		err := batchCreateBlocks(context.Background(), mc, "doc123", "", nil)
		if err != nil {
			t.Fatalf("batchCreateBlocks() error = %v", err)
		}
		if len(mc.createBlocksCalls) != 0 {
			t.Fatalf("expected no calls, got %d", len(mc.createBlocksCalls))
		}
	})

	t.Run("partial failure", func(t *testing.T) {
		callCount := 0
		mc := &mockClient{}
		// Override to fail on second call
		mc.createBlocksResp = nil // will use default
		origCreateBlocks := mc.CreateDocumentBlocks
		_ = origCreateBlocks // just to note that we can't easily override method

		// Use a client that alternates success/failure
		// We'll test with createBlocksErr set - all batches fail
		mc.createBlocksErr = fmt.Errorf("rate limit 429")
		_ = callCount

		blocks := make([]map[string]any, 5)
		for i := range blocks {
			blocks[i] = map[string]any{"block_type": 2}
		}

		err := batchCreateBlocks(context.Background(), mc, "doc123", "", blocks)
		if err == nil {
			t.Fatal("expected error for failed batches")
		}
	})
}

func TestResolveSymlink(t *testing.T) {
	t.Run("regular file", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "binary")
		if err := os.WriteFile(path, []byte("data"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := resolveSymlink(path)
		if err != nil {
			t.Fatalf("resolveSymlink() error = %v", err)
		}
		if got != path {
			t.Fatalf("resolveSymlink() = %q, want %q", got, path)
		}
	})

	t.Run("single symlink", func(t *testing.T) {
		tmp := t.TempDir()
		realPath := filepath.Join(tmp, "real-binary")
		if err := os.WriteFile(realPath, []byte("data"), 0o755); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(tmp, "link")
		if err := os.Symlink(realPath, linkPath); err != nil {
			t.Fatal(err)
		}

		got, err := resolveSymlink(linkPath)
		if err != nil {
			t.Fatalf("resolveSymlink() error = %v", err)
		}
		if got != realPath {
			t.Fatalf("resolveSymlink() = %q, want %q", got, realPath)
		}
	})

	t.Run("chained symlinks", func(t *testing.T) {
		tmp := t.TempDir()
		realPath := filepath.Join(tmp, "real")
		if err := os.WriteFile(realPath, []byte("data"), 0o755); err != nil {
			t.Fatal(err)
		}
		link1 := filepath.Join(tmp, "link1")
		if err := os.Symlink(realPath, link1); err != nil {
			t.Fatal(err)
		}
		link2 := filepath.Join(tmp, "link2")
		if err := os.Symlink(link1, link2); err != nil {
			t.Fatal(err)
		}

		got, err := resolveSymlink(link2)
		if err != nil {
			t.Fatalf("resolveSymlink() error = %v", err)
		}
		if got != realPath {
			t.Fatalf("resolveSymlink() = %q, want %q", got, realPath)
		}
	})

	t.Run("relative symlink", func(t *testing.T) {
		tmp := t.TempDir()
		realPath := filepath.Join(tmp, "real")
		if err := os.WriteFile(realPath, []byte("data"), 0o755); err != nil {
			t.Fatal(err)
		}
		linkPath := filepath.Join(tmp, "link")
		if err := os.Symlink("real", linkPath); err != nil {
			t.Fatal(err)
		}

		got, err := resolveSymlink(linkPath)
		if err != nil {
			t.Fatalf("resolveSymlink() error = %v", err)
		}
		if got != realPath {
			t.Fatalf("resolveSymlink() = %q, want %q", got, realPath)
		}
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := resolveSymlink("/nonexistent/path/binary")
		if err == nil {
			t.Fatal("expected error for nonexistent path")
		}
	})
}

func TestAuthedClient(t *testing.T) {
	t.Run("gateway mode with saved token", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)

		baseURL := "https://test.example.com"
		if err := SaveSessionToken(baseURL, "test-session-token"); err != nil {
			t.Fatalf("SaveSessionToken() error = %v", err)
		}

		client, err := authedClient(baseURL)
		if err != nil {
			t.Fatalf("authedClient() error = %v", err)
		}

		gc, ok := client.(*GatewayClient)
		if !ok {
			t.Fatal("expected GatewayClient")
		}
		if gc.session() != "test-session-token" {
			t.Fatalf("session = %q, want %q", gc.session(), "test-session-token")
		}
	})

	t.Run("gateway mode without token", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)

		_, err := authedClient("https://no-token.example.com")
		if err == nil {
			t.Fatal("expected error when no token saved")
		}
	})

	t.Run("local mode with saved credentials", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)

		if err := SaveLocalApp("app123", "secret456"); err != nil {
			t.Fatal(err)
		}
		expireAt := time.Now().Add(1 * time.Hour)
		if err := SaveLocalTokens("access-tok", "refresh-tok", expireAt); err != nil {
			t.Fatal(err)
		}

		client, err := authedClient("")
		if err != nil {
			t.Fatalf("authedClient() error = %v", err)
		}

		_, ok := client.(*LocalClient)
		if !ok {
			t.Fatal("expected LocalClient in local mode")
		}
	})
}

func TestLoadLocalClient(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)

		if err := SaveLocalApp("myapp", "mysecret"); err != nil {
			t.Fatal(err)
		}
		expireAt := time.Now().Add(1 * time.Hour)
		if err := SaveLocalTokens("access", "refresh", expireAt); err != nil {
			t.Fatal(err)
		}

		client, err := loadLocalClient()
		if err != nil {
			t.Fatalf("loadLocalClient() error = %v", err)
		}
		if client == nil {
			t.Fatal("expected non-nil client")
		}
	})

	t.Run("no app configured", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)

		_, err := loadLocalClient()
		if err == nil {
			t.Fatal("expected error when no app configured")
		}
	})

	t.Run("no tokens saved", func(t *testing.T) {
		tmp := t.TempDir()
		t.Setenv("HOME", tmp)

		if err := SaveLocalApp("myapp", "mysecret"); err != nil {
			t.Fatal(err)
		}

		_, err := loadLocalClient()
		if err == nil {
			t.Fatal("expected error when no tokens saved")
		}
	})
}

func TestGetPrimaryCalendarID(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		mc := &mockClient{
			calendarPrimaryResp: map[string]any{
				"calendars": []any{
					map[string]any{
						"calendar": map[string]any{
							"calendar_id": "cal_abc123",
						},
					},
				},
			},
		}

		id, err := getPrimaryCalendarID(context.Background(), mc)
		if err != nil {
			t.Fatalf("getPrimaryCalendarID() error = %v", err)
		}
		if id != "cal_abc123" {
			t.Fatalf("calendar_id = %q, want %q", id, "cal_abc123")
		}
	})

	t.Run("not found", func(t *testing.T) {
		mc := &mockClient{
			calendarPrimaryResp: map[string]any{
				"calendars": []any{},
			},
		}

		_, err := getPrimaryCalendarID(context.Background(), mc)
		if err == nil || !strings.Contains(err.Error(), "primary calendar not found") {
			t.Fatalf("expected 'primary calendar not found', got: %v", err)
		}
	})

	t.Run("api error", func(t *testing.T) {
		mc := &mockClient{
			calendarErr: fmt.Errorf("connection refused"),
		}

		_, err := getPrimaryCalendarID(context.Background(), mc)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("nil response", func(t *testing.T) {
		mc := &mockClient{
			calendarPrimaryResp: nil,
		}
		_, err := getPrimaryCalendarID(context.Background(), mc)
		if err == nil {
			t.Fatal("expected error for nil response")
		}
	})

	t.Run("empty calendar_id skipped", func(t *testing.T) {
		mc := &mockClient{
			calendarPrimaryResp: map[string]any{
				"calendars": []any{
					map[string]any{
						"calendar": map[string]any{
							"calendar_id": "",
						},
					},
					map[string]any{
						"calendar": map[string]any{
							"calendar_id": "cal_second",
						},
					},
				},
			},
		}
		id, err := getPrimaryCalendarID(context.Background(), mc)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if id != "cal_second" {
			t.Fatalf("got %q, want cal_second", id)
		}
	})
}

func TestResolveRoomID(t *testing.T) {
	t.Run("already an ID", func(t *testing.T) {
		mc := &mockClient{}
		id, err := resolveRoomID(context.Background(), mc, "omm_abc123")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if id != "omm_abc123" {
			t.Fatalf("got %q, want %q", id, "omm_abc123")
		}
	})

	t.Run("search by name", func(t *testing.T) {
		mc := &mockClient{
			listRoomsResp: map[string]any{
				"rooms": []any{
					map[string]any{
						"room_id": "omm_found",
						"name":    "Meeting Room A",
						"room_status": map[string]any{
							"status": true,
						},
					},
				},
			},
		}

		id, err := resolveRoomID(context.Background(), mc, "Meeting Room A")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if id != "omm_found" {
			t.Fatalf("got %q, want %q", id, "omm_found")
		}
	})

	t.Run("disabled room skipped", func(t *testing.T) {
		mc := &mockClient{
			listRoomsResp: map[string]any{
				"rooms": []any{
					map[string]any{
						"room_id": "omm_disabled",
						"name":    "Old Room",
						"room_status": map[string]any{
							"status": false,
						},
					},
				},
			},
		}

		_, err := resolveRoomID(context.Background(), mc, "Old Room")
		if err == nil || !strings.Contains(err.Error(), "no available room") {
			t.Fatalf("expected 'no available room' error, got: %v", err)
		}
	})

	t.Run("no rooms found", func(t *testing.T) {
		mc := &mockClient{
			listRoomsResp: map[string]any{
				"rooms": []any{},
			},
		}

		_, err := resolveRoomID(context.Background(), mc, "Nonexistent")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("api error", func(t *testing.T) {
		mc := &mockClient{
			listRoomsErr: fmt.Errorf("network error"),
		}

		_, err := resolveRoomID(context.Background(), mc, "Room")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("room without status field", func(t *testing.T) {
		mc := &mockClient{
			listRoomsResp: map[string]any{
				"rooms": []any{
					map[string]any{
						"room_id": "omm_nostatus",
						"name":    "Room No Status",
					},
				},
			},
		}
		id, err := resolveRoomID(context.Background(), mc, "Room No Status")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if id != "omm_nostatus" {
			t.Fatalf("got %q, want %q", id, "omm_nostatus")
		}
	})
}

func TestCheckRoomBusy(t *testing.T) {
	t.Run("room is busy", func(t *testing.T) {
		mc := &mockClient{
			freebusyResp: map[string]any{
				"freebusy_list": []any{
					map[string]any{"start_time": "2026-01-15T10:00:00+08:00"},
				},
			},
		}

		busy, err := checkRoomBusy(context.Background(), mc, "omm_123", "1700000000", "1700003600")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if !busy {
			t.Fatal("expected busy=true")
		}
	})

	t.Run("room is free", func(t *testing.T) {
		mc := &mockClient{
			freebusyResp: map[string]any{
				"freebusy_list": []any{},
			},
		}

		busy, err := checkRoomBusy(context.Background(), mc, "omm_123", "1700000000", "1700003600")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if busy {
			t.Fatal("expected busy=false")
		}
	})

	t.Run("nil freebusy list", func(t *testing.T) {
		mc := &mockClient{
			freebusyResp: map[string]any{},
		}

		busy, err := checkRoomBusy(context.Background(), mc, "omm_123", "1700000000", "1700003600")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if busy {
			t.Fatal("expected busy=false for nil list")
		}
	})

	t.Run("api error", func(t *testing.T) {
		mc := &mockClient{
			freebusyErr: fmt.Errorf("server error"),
		}

		_, err := checkRoomBusy(context.Background(), mc, "omm_123", "1700000000", "1700003600")
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestRequireScopes(t *testing.T) {
	t.Run("non-gateway client returns nil", func(t *testing.T) {
		mc := &mockClient{}
		err := requireScopes(context.Background(), mc, "some:scope")
		if err != nil {
			t.Fatalf("requireScopes() error = %v", err)
		}
	})
}

func TestResolveTaskMembers(t *testing.T) {
	t.Run("direct IDs", func(t *testing.T) {
		mc := &mockClient{}
		members, err := resolveTaskMembers(context.Background(), mc, []string{"ou_abc123", "12345"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(members) != 2 {
			t.Fatalf("expected 2 members, got %d", len(members))
		}
		if members[0]["id"] != "ou_abc123" {
			t.Fatalf("member[0].id = %v, want ou_abc123", members[0]["id"])
		}
		if members[0]["role"] != "assignee" {
			t.Fatalf("member[0].role = %v, want assignee", members[0]["role"])
		}
	})

	t.Run("with role prefix", func(t *testing.T) {
		mc := &mockClient{}
		members, err := resolveTaskMembers(context.Background(), mc, []string{"follower:ou_xyz"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(members) != 1 {
			t.Fatalf("expected 1 member, got %d", len(members))
		}
		if members[0]["role"] != "follower" {
			t.Fatalf("role = %v, want follower", members[0]["role"])
		}
		if members[0]["id"] != "ou_xyz" {
			t.Fatalf("id = %v, want ou_xyz", members[0]["id"])
		}
	})

	t.Run("name resolution", func(t *testing.T) {
		mc := &mockClient{
			searchUsersResp: map[string]any{
				"items": []any{
					map[string]any{"user_id": "uid_resolved", "name": "John"},
				},
			},
		}

		members, err := resolveTaskMembers(context.Background(), mc, []string{"John"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(members) != 1 {
			t.Fatalf("expected 1 member, got %d", len(members))
		}
		if members[0]["id"] != "uid_resolved" {
			t.Fatalf("id = %v, want uid_resolved", members[0]["id"])
		}
	})

	t.Run("empty strings skipped", func(t *testing.T) {
		mc := &mockClient{}
		members, err := resolveTaskMembers(context.Background(), mc, []string{"", "  "})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(members) != 0 {
			t.Fatalf("expected 0 members, got %d", len(members))
		}
	})

	t.Run("search error", func(t *testing.T) {
		mc := &mockClient{
			searchUsersErr: fmt.Errorf("search failed"),
		}

		_, err := resolveTaskMembers(context.Background(), mc, []string{"Unknown User"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestSearchUserByName(t *testing.T) {
	t.Run("found via items", func(t *testing.T) {
		mc := &mockClient{
			searchUsersResp: map[string]any{
				"items": []any{
					map[string]any{"user_id": "uid_1", "name": "Alice"},
				},
			},
		}

		id, err := searchUserByName(context.Background(), mc, "Alice")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if id != "uid_1" {
			t.Fatalf("got %q, want uid_1", id)
		}
	})

	t.Run("found via users key", func(t *testing.T) {
		mc := &mockClient{
			searchUsersResp: map[string]any{
				"users": []any{
					map[string]any{"open_id": "ou_bob", "name": "Bob"},
				},
			},
		}

		id, err := searchUserByName(context.Background(), mc, "Bob")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if id != "ou_bob" {
			t.Fatalf("got %q, want ou_bob", id)
		}
	})

	t.Run("found via user_list key", func(t *testing.T) {
		mc := &mockClient{
			searchUsersResp: map[string]any{
				"user_list": []any{
					map[string]any{"user_id": "uid_charlie", "name": "Charlie"},
				},
			},
		}

		id, err := searchUserByName(context.Background(), mc, "Charlie")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if id != "uid_charlie" {
			t.Fatalf("got %q, want uid_charlie", id)
		}
	})

	t.Run("no results", func(t *testing.T) {
		mc := &mockClient{
			searchUsersResp: map[string]any{},
		}

		_, err := searchUserByName(context.Background(), mc, "Nobody")
		if err == nil || !strings.Contains(err.Error(), "no user found") {
			t.Fatalf("expected 'no user found' error, got: %v", err)
		}
	})

	t.Run("api error", func(t *testing.T) {
		mc := &mockClient{
			searchUsersErr: fmt.Errorf("timeout"),
		}

		_, err := searchUserByName(context.Background(), mc, "Test")
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("user without ID", func(t *testing.T) {
		mc := &mockClient{
			searchUsersResp: map[string]any{
				"items": []any{
					map[string]any{"name": "No ID User"},
				},
			},
		}

		_, err := searchUserByName(context.Background(), mc, "No ID")
		if err == nil || !strings.Contains(err.Error(), "no user_id") {
			t.Fatalf("expected 'no user_id' error, got: %v", err)
		}
	})
}

func TestResolveUserIDs(t *testing.T) {
	t.Run("all IDs", func(t *testing.T) {
		mc := &mockClient{}
		ids, err := resolveUserIDs(context.Background(), mc, []string{"ou_a", "12345"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("expected 2 IDs, got %d", len(ids))
		}
		if ids[0] != "ou_a" || ids[1] != "12345" {
			t.Fatalf("ids = %v", ids)
		}
	})

	t.Run("mixed names and IDs", func(t *testing.T) {
		mc := &mockClient{
			searchUsersResp: map[string]any{
				"items": []any{
					map[string]any{"user_id": "uid_resolved", "name": "Alice"},
				},
			},
		}

		ids, err := resolveUserIDs(context.Background(), mc, []string{"ou_keep", "Alice"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(ids) != 2 {
			t.Fatalf("expected 2 IDs, got %d", len(ids))
		}
		if ids[0] != "ou_keep" {
			t.Fatalf("ids[0] = %q, want ou_keep", ids[0])
		}
		if ids[1] != "uid_resolved" {
			t.Fatalf("ids[1] = %q, want uid_resolved", ids[1])
		}
	})

	t.Run("empty strings skipped", func(t *testing.T) {
		mc := &mockClient{}
		ids, err := resolveUserIDs(context.Background(), mc, []string{"", "  ", "ou_ok"})
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(ids) != 1 || ids[0] != "ou_ok" {
			t.Fatalf("ids = %v, want [ou_ok]", ids)
		}
	})

	t.Run("search error", func(t *testing.T) {
		mc := &mockClient{
			searchUsersErr: fmt.Errorf("api error"),
		}
		_, err := resolveUserIDs(context.Background(), mc, []string{"Unknown"})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestExtractImagesFromExport(t *testing.T) {
	t.Run("successful extraction", func(t *testing.T) {
		tmp := t.TempDir()

		// Create a mock xlsx (zip) with images in xl/media/
		xlsxBuf := new(bytes.Buffer)
		zw := zip.NewWriter(xlsxBuf)
		fw, _ := zw.Create("xl/media/image1.png")
		fw.Write([]byte("fake-png-data"))
		fw2, _ := zw.Create("xl/media/image2.jpg")
		fw2.Write([]byte("fake-jpg-data"))
		fw3, _ := zw.Create("xl/worksheets/sheet1.xml")
		fw3.Write([]byte("<worksheet/>"))
		zw.Close()

		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "test-ticket",
				exportStatusResult: exportResult{
					JobStatus: 0,
					FileToken: "dl-tok",
					FileSize:  100,
					FileName:  "test",
				},
			},
			downloadData: xlsxBuf.Bytes(),
		}

		count, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", tmp)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if count != 2 {
			t.Fatalf("expected 2 images, got %d", count)
		}

		entries, _ := os.ReadDir(tmp)
		if len(entries) != 2 {
			t.Fatalf("expected 2 files, got %d", len(entries))
		}
	})

	t.Run("export create error", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateErr: fmt.Errorf("create failed"),
			},
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "create export") {
			t.Fatalf("expected 'create export' error, got: %v", err)
		}
	})

	t.Run("empty ticket", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "",
			},
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "empty export ticket") {
			t.Fatalf("expected 'empty export ticket' error, got: %v", err)
		}
	})

	t.Run("export status error", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-1",
				exportStatusErr:    fmt.Errorf("status failed"),
			},
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "poll export") {
			t.Fatalf("expected 'poll export' error, got: %v", err)
		}
	})

	t.Run("export failed status", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-2",
				exportStatusResult: exportResult{
					JobStatus: 3,
					ErrMsg:    "something went wrong",
				},
			},
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "export failed") {
			t.Fatalf("expected 'export failed' error, got: %v", err)
		}
	})

	t.Run("export failed without error msg", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-3",
				exportStatusResult: exportResult{
					JobStatus: 5,
				},
			},
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "job_status=5") {
			t.Fatalf("expected 'job_status=5' error, got: %v", err)
		}
	})

	t.Run("empty file token", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-4",
				exportStatusResult: exportResult{
					JobStatus: 0,
					FileToken: "",
				},
			},
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "no file_token") {
			t.Fatalf("expected 'no file_token' error, got: %v", err)
		}
	})

	t.Run("download error", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-5",
				exportStatusResult: exportResult{
					JobStatus: 0,
					FileToken: "dl-tok",
				},
				exportDownloadErr: fmt.Errorf("download failed"),
			},
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "download export") {
			t.Fatalf("expected 'download export' error, got: %v", err)
		}
	})

	t.Run("invalid zip", func(t *testing.T) {
		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-6",
				exportStatusResult: exportResult{
					JobStatus: 0,
					FileToken: "dl-tok",
				},
			},
			downloadData: []byte("not-a-zip"),
		}
		_, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err == nil || !strings.Contains(err.Error(), "open xlsx") {
			t.Fatalf("expected 'open xlsx' error, got: %v", err)
		}
	})

	t.Run("no images in zip", func(t *testing.T) {
		xlsxBuf := new(bytes.Buffer)
		zw := zip.NewWriter(xlsxBuf)
		fw, _ := zw.Create("xl/worksheets/sheet1.xml")
		fw.Write([]byte("<worksheet/>"))
		zw.Close()

		mc := &exportMockClient{
			mockClient: &mockClient{
				exportCreateTicket: "ticket-7",
				exportStatusResult: exportResult{
					JobStatus: 0,
					FileToken: "dl-tok",
				},
			},
			downloadData: xlsxBuf.Bytes(),
		}
		count, err := extractImagesFromExport(context.Background(), mc, "sheet_tok", t.TempDir())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if count != 0 {
			t.Fatalf("expected 0 images, got %d", count)
		}
	})
}

func TestWriteFileAtomic(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "subdir", "test.json")
		data := []byte(`{"key": "value"}`)
		err := writeFileAtomic(path, data, 0o600)
		if err != nil {
			t.Fatalf("writeFileAtomic() error = %v", err)
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read file: %v", err)
		}
		if string(got) != string(data) {
			t.Fatalf("got %q, want %q", string(got), string(data))
		}
		fi, _ := os.Stat(path)
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("perm = %v, want 0600", fi.Mode().Perm())
		}
	})
}

func TestDoDiffUpdate_UpdateBlocks(t *testing.T) {
	mc := &mockClient{
		getDocBlocksResp: map[string]any{
			"items": []any{
				map[string]any{
					"block_id":   "doc123",
					"block_type": float64(btPage),
					"children":   []any{"blk1"},
				},
				map[string]any{
					"block_id":   "blk1",
					"block_type": float64(2),
					"text": map[string]any{
						"elements": []any{
							map[string]any{"text_run": map[string]any{"content": "old text"}},
						},
					},
				},
			},
		},
		updateBlocksResp: map[string]any{"ok": true},
	}

	entries := []mdBlockEntry{
		{BlockID: "blk1", BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "new text", SourceLine: 1},
	}

	err := doDiffUpdate(context.Background(), mc, "doc123", entries)
	if err != nil {
		t.Fatalf("doDiffUpdate() error = %v", err)
	}
	if len(mc.updateBlocksCalls) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(mc.updateBlocksCalls))
	}
}

func TestDoDiffUpdate_UnsupportedBlockType(t *testing.T) {
	mc := &mockClient{
		getDocBlocksResp: map[string]any{
			"items": []any{
				map[string]any{
					"block_id":   "doc123",
					"block_type": float64(btPage),
					"children":   []any{"blk1"},
				},
				map[string]any{
					"block_id":   "blk1",
					"block_type": float64(2),
					"text": map[string]any{
						"elements": []any{
							map[string]any{"text_run": map[string]any{"content": "text"}},
						},
					},
				},
			},
		},
	}

	// Entry with changed content but nil Body (unsupported)
	entries := []mdBlockEntry{
		{BlockID: "blk1", BlockType: 2, Body: nil, Comparable: "changed text", SourceLine: 10},
	}

	err := doDiffUpdate(context.Background(), mc, "doc123", entries)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

func TestDoDiffUpdate_CreateNewWithNilBody(t *testing.T) {
	mc := &mockClient{
		getDocBlocksResp: map[string]any{
			"items": []any{
				map[string]any{
					"block_id":   "doc123",
					"block_type": float64(btPage),
					"children":   []any{},
				},
			},
		},
	}

	// New entry with nil Body (unsupported create)
	entries := []mdBlockEntry{
		{BlockType: 22, Body: nil, Comparable: "", SourceLine: 5},
	}

	err := doDiffUpdate(context.Background(), mc, "doc123", entries)
	if err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("expected unsupported error, got: %v", err)
	}
}

func TestDoDiffUpdate_UpdateFails(t *testing.T) {
	mc := &mockClient{
		getDocBlocksResp: map[string]any{
			"items": []any{
				map[string]any{
					"block_id":   "doc123",
					"block_type": float64(btPage),
					"children":   []any{"blk1"},
				},
				map[string]any{
					"block_id":   "blk1",
					"block_type": float64(2),
					"text": map[string]any{
						"elements": []any{
							map[string]any{"text_run": map[string]any{"content": "old"}},
						},
					},
				},
			},
		},
		updateBlocksErr: fmt.Errorf("update failed"),
	}

	entries := []mdBlockEntry{
		{BlockID: "blk1", BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "new", SourceLine: 1},
	}

	err := doDiffUpdate(context.Background(), mc, "doc123", entries)
	if err == nil || !strings.Contains(err.Error(), "batch update failed") {
		t.Fatalf("expected 'batch update failed' error, got: %v", err)
	}
}

func TestDownloadSheetImages_EmbedImages(t *testing.T) {
	tmp := t.TempDir()

	xlsxBuf := new(bytes.Buffer)
	zw := zip.NewWriter(xlsxBuf)
	fw, _ := zw.Create("xl/media/image1.png")
	fw.Write([]byte("fake-png"))
	zw.Close()

	mc := &exportMockClient{
		mockClient: &mockClient{
			getSheetValuesResp: map[string]any{
				"valueRange": map[string]any{
					"values": []any{
						[]any{
							[]any{
								map[string]any{"type": "embed-image", "url": "http://x.com/img.png"},
							},
						},
					},
				},
			},
			getSheetsFloatImages: map[string]any{
				"items": []any{},
			},
			exportCreateTicket: "embed-ticket",
			exportStatusResult: exportResult{
				JobStatus: 0,
				FileToken: "dl-tok",
			},
		},
		downloadData: xlsxBuf.Bytes(),
	}

	err := downloadSheetImages(context.Background(), mc, "https://xxx.feishu.cn/sheets/sheet_tok", "Sheet1", tmp)
	if err != nil {
		t.Fatalf("error = %v", err)
	}

	entries, _ := os.ReadDir(tmp)
	if len(entries) == 0 {
		t.Fatal("expected extracted images")
	}
}

func TestDownloadSheetImages_NoImages(t *testing.T) {
	tmp := t.TempDir()
	mc := &mockClient{
		getSheetValuesResp: map[string]any{
			"valueRange": map[string]any{
				"values": []any{},
			},
		},
		getSheetsFloatImages: map[string]any{
			"items": []any{},
		},
	}

	err := downloadSheetImages(context.Background(), mc, "https://xxx.feishu.cn/sheets/sheet_tok", "Sheet1", tmp)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
}

func TestDownloadSheetImages_FloatImgError(t *testing.T) {
	mc := &mockClient{
		getSheetValuesResp: map[string]any{
			"valueRange": map[string]any{"values": []any{}},
		},
		getFloatImgErr: fmt.Errorf("float images failed"),
	}

	err := downloadSheetImages(context.Background(), mc, "https://xxx.feishu.cn/sheets/sheet_tok", "Sheet1", t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "get float images") {
		t.Fatalf("expected 'get float images' error, got: %v", err)
	}
}

func TestDownloadDocImages_Error(t *testing.T) {
	mc := &mockClient{
		getDocBlocksErr: fmt.Errorf("api error"),
	}

	err := downloadDocImages(context.Background(), mc, "doc123", t.TempDir())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestReadJSONInput(t *testing.T) {
	t.Run("from file", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "input.json")
		os.WriteFile(path, []byte(`{"key":"value"}`), 0644)

		var out map[string]any
		err := readJSONInput([]string{"arg0", path}, 1, &out)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if out["key"] != "value" {
			t.Fatalf("got %v", out)
		}
	})

	t.Run("file not found", func(t *testing.T) {
		var out map[string]any
		err := readJSONInput([]string{"arg0", "/nonexistent/file.json"}, 1, &out)
		if err == nil || !strings.Contains(err.Error(), "open input file") {
			t.Fatalf("expected 'open input file' error, got: %v", err)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		tmp := t.TempDir()
		path := filepath.Join(tmp, "bad.json")
		os.WriteFile(path, []byte(`not json`), 0644)

		var out map[string]any
		err := readJSONInput([]string{"arg0", path}, 1, &out)
		if err == nil || !strings.Contains(err.Error(), "parse JSON input") {
			t.Fatalf("expected 'parse JSON input' error, got: %v", err)
		}
	})
}

func TestDoFullReplace_DeleteError(t *testing.T) {
	mc := &mockClient{
		getDocBlocksResp: map[string]any{
			"items": []any{
				map[string]any{
					"block_id":   "doc123",
					"block_type": float64(btPage),
					"children":   []any{"child1"},
				},
				map[string]any{"block_id": "child1", "block_type": float64(2)},
			},
		},
		deleteBlockErr: fmt.Errorf("delete failed"),
		createBlocksResp: map[string]any{
			"children": []any{map[string]any{"block_id": "new1"}},
		},
	}

	entries := []mdBlockEntry{
		{BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "new text"},
	}

	// doFullReplace continues even on delete errors (it doesn't check)
	err := doFullReplace(context.Background(), mc, "doc123", entries)
	if err != nil {
		t.Fatalf("doFullReplace() error = %v", err)
	}
}

func TestCountEmbedImages(t *testing.T) {
	t.Run("no images", func(t *testing.T) {
		data := map[string]any{
			"valueRange": map[string]any{
				"values": []any{
					[]any{"hello", "world"},
				},
			},
		}
		count := countEmbedImages(data)
		if count != 0 {
			t.Fatalf("expected 0, got %d", count)
		}
	})

	t.Run("with embed images", func(t *testing.T) {
		data := map[string]any{
			"valueRange": map[string]any{
				"values": []any{
					[]any{
						[]any{
							map[string]any{"type": "embed-image", "url": "http://example.com/img.png"},
						},
						"text",
					},
					[]any{
						[]any{
							map[string]any{"type": "embed-image", "url": "http://example.com/img2.png"},
							map[string]any{"type": "text", "text": "hello"},
						},
					},
				},
			},
		}
		count := countEmbedImages(data)
		if count != 2 {
			t.Fatalf("expected 2, got %d", count)
		}
	})

	t.Run("nil data", func(t *testing.T) {
		count := countEmbedImages(nil)
		if count != 0 {
			t.Fatalf("expected 0, got %d", count)
		}
	})

	t.Run("malformed data", func(t *testing.T) {
		count := countEmbedImages("not a map")
		if count != 0 {
			t.Fatalf("expected 0, got %d", count)
		}
	})
}

func TestDownloadImages(t *testing.T) {
	t.Run("downloads images", func(t *testing.T) {
		tmp := t.TempDir()
		mc := &mockClient{
			downloadMediaCT: "image/png",
		}

		images := []imageEntry{
			{token: "tok1", name: "img1", width: 100, height: 200},
			{token: "tok2", name: "img2", width: 0, height: 0},
		}

		err := downloadImages(context.Background(), mc, images, tmp)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(mc.downloadMediaCalls) != 2 {
			t.Fatalf("expected 2 download calls, got %d", len(mc.downloadMediaCalls))
		}

		// Check files exist
		entries, _ := os.ReadDir(tmp)
		if len(entries) != 2 {
			t.Fatalf("expected 2 files, got %d", len(entries))
		}
	})

	t.Run("jpeg content type", func(t *testing.T) {
		tmp := t.TempDir()
		mc := &mockClient{
			downloadMediaCT: "image/jpeg",
		}

		images := []imageEntry{
			{token: "tok_jpg", name: "img"},
		}

		err := downloadImages(context.Background(), mc, images, tmp)
		if err != nil {
			t.Fatalf("error = %v", err)
		}

		// Should have .jpg extension
		entries, _ := os.ReadDir(tmp)
		found := false
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".jpg") {
				found = true
			}
		}
		if !found {
			t.Fatal("expected .jpg file")
		}
	})

	t.Run("download error continues", func(t *testing.T) {
		tmp := t.TempDir()
		mc := &mockClient{
			downloadMediaErr: fmt.Errorf("download failed"),
		}

		images := []imageEntry{
			{token: "fail_tok", name: "fail"},
		}

		err := downloadImages(context.Background(), mc, images, tmp)
		if err != nil {
			t.Fatalf("error = %v (should not fail on individual download error)", err)
		}

		// File should be cleaned up
		entries, _ := os.ReadDir(tmp)
		if len(entries) != 0 {
			t.Fatalf("expected 0 files after download failure, got %d", len(entries))
		}
	})

	t.Run("empty images", func(t *testing.T) {
		tmp := t.TempDir()
		mc := &mockClient{}

		err := downloadImages(context.Background(), mc, nil, tmp)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestBuildDefaultScopes(t *testing.T) {
	scopes := buildDefaultScopes()

	// Should include base scopes
	for _, s := range strings.Fields(baseScopes) {
		if !strings.Contains(scopes, s) {
			t.Fatalf("missing base scope: %s", s)
		}
	}

	// Should include scopes from all groups
	for name, group := range scopeGroups {
		for _, s := range strings.Fields(group.Scopes) {
			if !strings.Contains(scopes, s) {
				t.Fatalf("missing scope %s from group %s", s, name)
			}
		}
	}

	// No duplicates
	fields := strings.Fields(scopes)
	seen := map[string]bool{}
	for _, f := range fields {
		if seen[f] {
			t.Fatalf("duplicate scope: %s", f)
		}
		seen[f] = true
	}
}

func TestPrintJSON_CompactVsPretty(t *testing.T) {
	origCompact := compactOutput
	defer func() { compactOutput = origCompact }()

	data := map[string]any{"a": 1, "b": "two"}

	t.Run("compact has no newlines in object", func(t *testing.T) {
		compactOutput = true
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		printJSON(data)
		w.Close()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		os.Stdout = old

		out := strings.TrimSpace(buf.String())
		var m map[string]any
		if err := json.Unmarshal([]byte(out), &m); err != nil {
			t.Fatalf("compact output not valid JSON: %s", out)
		}
		// Compact should be a single line
		if strings.Count(out, "\n") > 0 {
			t.Fatalf("compact output should be single line: %s", out)
		}
	})

	t.Run("pretty has indentation", func(t *testing.T) {
		compactOutput = false
		old := os.Stdout
		r, w, _ := os.Pipe()
		os.Stdout = w
		printJSON(data)
		w.Close()
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		os.Stdout = old

		out := buf.String()
		if !strings.Contains(out, "  ") {
			t.Fatalf("pretty output should have indentation: %s", out)
		}
	})
}

// A test must never launch the developer's browser: the scope-upgrade flow calls
// tryOpenBrowser with no way to turn it off from the command line.
func TestBrowserLaunchSuppressedInTests(t *testing.T) {
	if !browserLaunchSuppressed() {
		t.Fatal("browserLaunchSuppressed() = false inside a test binary; tryOpenBrowser would spawn a real browser")
	}
}

// A failed auto-child update must not make the caller skip that entry: the content
// would be dropped from the document while the import still reported success.
func TestUpdateAutoChildReportsFailureInsteadOfDroppingContent(t *testing.T) {
	entry := mdBlockEntry{
		BlockType: btText,
		Body: map[string]any{
			"block_type": btText,
			"text": map[string]any{
				"elements": []any{map[string]any{"text_run": map[string]any{"content": "first line"}}},
			},
		},
	}

	failing := &mockClient{updateBlocksErr: fmt.Errorf("feishu 1770006 schema mismatch")}
	consumed, err := updateAutoChild(context.Background(), failing, "doc1", "child1", entry)
	if err == nil {
		t.Fatal("want the upstream error to propagate, got nil")
	}
	if consumed {
		t.Fatal("a failed update must not report the entry as consumed")
	}

	ok := &mockClient{updateBlocksResp: map[string]any{}}
	consumed, err = updateAutoChild(context.Background(), ok, "doc1", "child1", entry)
	if err != nil || !consumed {
		t.Fatalf("successful update: consumed=%v err=%v", consumed, err)
	}
}

func TestMediaBlockIDFromCreate(t *testing.T) {
	tests := []struct {
		name        string
		resp        any
		wantType    int
		want        string
		wantCreated string
		wantErr     bool
	}{
		{
			name:        "image block carries the token itself",
			resp:        map[string]any{"children": []any{map[string]any{"block_id": "img1", "block_type": 27}}},
			wantType:    27,
			want:        "img1",
			wantCreated: "img1",
		},
		{
			// Feishu answers a file-block request with a view block wrapping it.
			// The token goes to the inner block, the wrapper is what gets removed.
			name: "file block is wrapped in a view block",
			resp: map[string]any{"children": []any{map[string]any{
				"block_id": "view1", "block_type": 33, "children": []any{"file1"},
			}}},
			wantType:    23,
			want:        "file1",
			wantCreated: "view1",
		},
		{
			name:     "no children",
			resp:     map[string]any{"children": []any{}},
			wantType: 27,
			wantErr:  true,
		},
		{
			name:     "unexpected block type without children",
			resp:     map[string]any{"children": []any{map[string]any{"block_id": "x", "block_type": 2}}},
			wantType: 27,
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotCreated, gotMedia, err := mediaBlockIDFromCreate(tt.resp, tt.wantType)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q/%q", gotCreated, gotMedia)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMedia != tt.want {
				t.Fatalf("media id = %q, want %q", gotMedia, tt.want)
			}
			if gotCreated != tt.wantCreated {
				t.Fatalf("created id = %q, want %q", gotCreated, tt.wantCreated)
			}
		})
	}
}

func TestDocMentionObjTypeFromURL(t *testing.T) {
	tests := []struct {
		target string
		want   int
		isURL  bool
	}{
		{"https://xxx.feishu.cn/docx/DOCXTOKEN1234567890abcde", 22, true},
		{"https://xxx.feishu.cn/wiki/WIKITOKEN1234567890abcde", 16, true},
		{"https://xxx.feishu.cn/sheets/TOKEN", 3, true},
		{"https://xxx.feishu.cn/base/TOKEN", 8, true},
		{"https://xxx.feishu.cn/docs/TOKEN", 1, true},
		{"https://example.com/whatever", 22, true},
		{"DOCXTOKEN1234567890abcde", 0, false},
		{"张三", 0, false},
	}
	for _, tt := range tests {
		got, ok := docMentionObjTypeFromURL(tt.target)
		if ok != tt.isURL {
			t.Fatalf("%s: isURL = %v, want %v", tt.target, ok, tt.isURL)
		}
		if ok && got != tt.want {
			t.Fatalf("%s: obj_type = %d, want %d", tt.target, got, tt.want)
		}
	}
}

func TestLooksLikeDocToken(t *testing.T) {
	tests := map[string]bool{
		"DOCXTOKEN1234567890abcde":   true,
		"WIKITOKEN1234567890abcde":   true,
		"张三":                         false,
		"Zhang San":                  false,
		"short":                      false,
		"has-a-dash-but-long-enough": false,
	}
	for target, want := range tests {
		if got := looksLikeDocToken(target); got != want {
			t.Fatalf("looksLikeDocToken(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestDocsMentionElement(t *testing.T) {
	ctx := context.Background()

	t.Run("open_id becomes a user mention", func(t *testing.T) {
		el, err := docsMentionElement(ctx, &mockClient{}, "ou_abc123", 22)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		user, ok := el["mention_user"].(map[string]any)
		if !ok || user["user_id"] != "ou_abc123" {
			t.Fatalf("got %v", el)
		}
	})

	t.Run("wiki URL keeps obj_type 16", func(t *testing.T) {
		el, err := docsMentionElement(ctx, &mockClient{}, "https://xxx.feishu.cn/wiki/WIKITOKEN1234567890abcde", 22)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		doc, ok := el["mention_doc"].(map[string]any)
		if !ok || doc["obj_type"] != 16 || doc["token"] != "WIKITOKEN1234567890abcde" {
			t.Fatalf("got %v", el)
		}
	})

	t.Run("bare token uses the default obj_type", func(t *testing.T) {
		el, err := docsMentionElement(ctx, &mockClient{}, "DOCXTOKEN1234567890abcde", 22)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		doc := el["mention_doc"].(map[string]any)
		if doc["obj_type"] != 22 {
			t.Fatalf("got %v", el)
		}
	})

	t.Run("name resolves through the contact search", func(t *testing.T) {
		m := &mockClient{searchUsersResp: map[string]any{"users": []any{
			map[string]any{"name": "张三", "open_id": "ou_zhangsan"},
		}}}
		el, err := docsMentionElement(ctx, m, "张三", 22)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if el["mention_user"].(map[string]any)["user_id"] != "ou_zhangsan" {
			t.Fatalf("got %v", el)
		}
	})

	t.Run("ambiguous name is refused rather than guessed", func(t *testing.T) {
		m := &mockClient{searchUsersResp: map[string]any{"users": []any{
			map[string]any{"name": "张三", "open_id": "ou_a"},
			map[string]any{"name": "张三", "open_id": "ou_b"},
		}}}
		if _, err := docsMentionElement(ctx, m, "张三", 22); err == nil {
			t.Fatal("expected an error for an ambiguous name")
		}
	})

	t.Run("partial match is not accepted as the person", func(t *testing.T) {
		m := &mockClient{searchUsersResp: map[string]any{"users": []any{
			map[string]any{"name": "张三丰", "open_id": "ou_a"},
		}}}
		if _, err := docsMentionElement(ctx, m, "张三", 22); err == nil {
			t.Fatal("expected an error when no name matches exactly")
		}
	})
}

func TestResolveDocID(t *testing.T) {
	ctx := context.Background()

	t.Run("plain document id passes through", func(t *testing.T) {
		got, err := resolveDocID(ctx, &mockClient{}, "DOCXTOKEN1234567890abcde")
		if err != nil || got != "DOCXTOKEN1234567890abcde" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("wiki URL resolves to obj_token", func(t *testing.T) {
		m := &mockClient{getWikiNodeResp: map[string]any{"node": map[string]any{"obj_token": "docx123"}}}
		got, err := resolveDocID(ctx, m, "https://xxx.feishu.cn/wiki/WIKITOKEN")
		if err != nil || got != "docx123" {
			t.Fatalf("got %q, err %v", got, err)
		}
	})

	t.Run("wiki node without obj_token is an error", func(t *testing.T) {
		// Falling through with the wiki token would fail later inside the docx
		// API with a confusing "resource not found".
		m := &mockClient{getWikiNodeResp: map[string]any{"node": map[string]any{}}}
		if _, err := resolveDocID(ctx, m, "https://xxx.feishu.cn/wiki/WIKITOKEN"); err == nil {
			t.Fatal("expected an error")
		}
	})
}

func TestAddDocsMedia(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, []byte("imagebytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := &mockClient{
		createBlocksResp:     map[string]any{"children": []any{map[string]any{"block_id": "img1", "block_type": 27}}},
		uploadDocsMediaToken: "ft-xyz",
	}
	blockID, err := addDocsMedia(context.Background(), m, "doc1", "", -1, path, docsImageKind)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if blockID != "img1" {
		t.Fatalf("block id = %q, want img1", blockID)
	}

	if len(m.createBlocksCalls) != 1 {
		t.Fatalf("create calls = %d", len(m.createBlocksCalls))
	}
	created := m.createBlocksCalls[0].body.(map[string]any)
	child := created["children"].([]any)[0].(map[string]any)
	if child["block_type"] != 27 {
		t.Fatalf("created block_type = %v, want 27", child["block_type"])
	}
	if created["index"] != -1 {
		t.Fatalf("index = %v, want -1", created["index"])
	}

	if len(m.uploadedDocsMedia) != 1 {
		t.Fatalf("upload calls = %d", len(m.uploadedDocsMedia))
	}
	up := m.uploadedDocsMedia[0]
	if up.ParentType != "docx_image" || up.ParentNode != "img1" || up.FileName != "pic.png" || up.Size != int64(len("imagebytes")) {
		t.Fatalf("upload call = %+v", up)
	}

	if len(m.updateBlocksCalls) != 1 {
		t.Fatalf("update calls = %d", len(m.updateBlocksCalls))
	}
	req := m.updateBlocksCalls[0].(map[string]any)["requests"].([]any)[0].(map[string]any)
	if req["block_id"] != "img1" {
		t.Fatalf("update block_id = %v", req["block_id"])
	}
	if req["replace_image"].(map[string]any)["token"] != "ft-xyz" {
		t.Fatalf("update body = %v", req)
	}
}

func TestAddDocsMediaRejectsDirectory(t *testing.T) {
	if _, err := addDocsMedia(context.Background(), &mockClient{}, "doc1", "", -1, t.TempDir(), docsFileKind); err == nil {
		t.Fatal("expected an error for a directory")
	}
}

func TestDoDiffUpdateKeepsViewWrappedAttachment(t *testing.T) {
	// Feishu nests a file block inside a view block, and the markdown export
	// marks the inner file block. Without mapping it back to its wrapper the
	// attachment reads as a new block while the wrapper reads as removed.
	newDoc := func() *mockClient {
		return &mockClient{
			getDocBlocksResp: map[string]any{
				"items": []any{
					map[string]any{
						"block_id":   "doc123",
						"block_type": float64(btPage),
						"children":   []any{"view1", "txt1"},
					},
					map[string]any{
						"block_id":   "view1",
						"block_type": float64(btView),
						"children":   []any{"file1"},
					},
					map[string]any{
						"block_id":   "file1",
						"block_type": float64(btFile),
						"file":       map[string]any{"name": "report.pdf", "token": "ft1"},
					},
					map[string]any{
						"block_id":   "txt1",
						"block_type": float64(2),
						"text": map[string]any{"elements": []any{
							map[string]any{"text_run": map[string]any{"content": "hello"}},
						}},
					},
				},
			},
		}
	}

	t.Run("unchanged attachment is left alone", func(t *testing.T) {
		mc := newDoc()
		entries := []mdBlockEntry{
			{BlockID: "file1", BlockType: btFile, Comparable: "file:report.pdf\tft1"},
			{BlockID: "txt1", BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "hello"},
		}
		if err := doDiffUpdate(context.Background(), mc, "doc123", entries); err != nil {
			t.Fatalf("doDiffUpdate() error = %v", err)
		}
		if len(mc.deleteBlockCalls) != 0 {
			t.Fatalf("expected no deletes, got %v", mc.deleteBlockCalls)
		}
		if len(mc.createBlocksCalls) != 0 {
			t.Fatalf("expected no creates, got %d", len(mc.createBlocksCalls))
		}
	})

	t.Run("dropping the attachment deletes its wrapper", func(t *testing.T) {
		mc := newDoc()
		entries := []mdBlockEntry{
			{BlockID: "txt1", BlockType: 2, Body: map[string]any{"block_type": 2}, Comparable: "hello"},
		}
		if err := doDiffUpdate(context.Background(), mc, "doc123", entries); err != nil {
			t.Fatalf("doDiffUpdate() error = %v", err)
		}
		if len(mc.deleteBlockCalls) != 1 || mc.deleteBlockCalls[0].blockID != "view1" {
			t.Fatalf("expected view1 to be deleted, got %v", mc.deleteBlockCalls)
		}
	})
}

func TestAddDocsMediaRollsBackPlaceholder(t *testing.T) {
	// A tokenless image/file block renders in Feishu as a broken "failed to
	// load" placeholder, so a failed upload must not leave one behind.
	dir := t.TempDir()
	path := filepath.Join(dir, "pic.png")
	if err := os.WriteFile(path, []byte("imagebytes"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("upload failure removes the image block", func(t *testing.T) {
		mc := &mockClient{
			createBlocksResp:   map[string]any{"children": []any{map[string]any{"block_id": "img1", "block_type": 27}}},
			uploadDocsMediaErr: fmt.Errorf("upload failed (status=404): 404 page not found"),
		}
		_, err := addDocsMedia(context.Background(), mc, "doc1", "", -1, path, docsImageKind)
		if err == nil {
			t.Fatal("expected an error")
		}
		if len(mc.deleteBlockCalls) != 1 || mc.deleteBlockCalls[0].blockID != "img1" {
			t.Fatalf("expected img1 to be deleted, got %v", mc.deleteBlockCalls)
		}
	})

	t.Run("attachment rollback removes the view wrapper, not the inner block", func(t *testing.T) {
		mc := &mockClient{
			createBlocksResp: map[string]any{"children": []any{map[string]any{
				"block_id": "view1", "block_type": 33, "children": []any{"file1"},
			}}},
			uploadDocsMediaErr: fmt.Errorf("boom"),
		}
		_, err := addDocsMedia(context.Background(), mc, "doc1", "", -1, path, docsFileKind)
		if err == nil {
			t.Fatal("expected an error")
		}
		// Deleting file1 would leave an empty view block behind.
		if len(mc.deleteBlockCalls) != 1 || mc.deleteBlockCalls[0].blockID != "view1" {
			t.Fatalf("expected view1 to be deleted, got %v", mc.deleteBlockCalls)
		}
	})

	t.Run("patch failure removes the block too", func(t *testing.T) {
		mc := &mockClient{
			createBlocksResp: map[string]any{"children": []any{map[string]any{"block_id": "img1", "block_type": 27}}},
			updateBlocksErr:  fmt.Errorf("boom"),
		}
		if _, err := addDocsMedia(context.Background(), mc, "doc1", "", -1, path, docsImageKind); err == nil {
			t.Fatal("expected an error")
		}
		if len(mc.deleteBlockCalls) != 1 || mc.deleteBlockCalls[0].blockID != "img1" {
			t.Fatalf("expected img1 to be deleted, got %v", mc.deleteBlockCalls)
		}
	})
}
