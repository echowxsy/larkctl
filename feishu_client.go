package main

import (
	"context"
	"io"
	"mime/multipart"
	"net/url"
	"strconv"
)

type exportResult struct {
	JobStatus int    `json:"job_status"`
	FileToken string `json:"file_token"`
	FileSize  int64  `json:"file_size"`
	FileName  string `json:"file_name"`
	ErrMsg    string `json:"job_error_msg"`
}

// FeishuClient abstracts Feishu API operations.
// Implemented by GatewayClient (proxied via gateway) and LocalClient (direct API calls).
type FeishuClient interface {
	WhoAmI(ctx context.Context) (map[string]any, error)
	GetDocumentInfo(ctx context.Context, input, docType string) (any, error)
	GetDocumentBlocks(ctx context.Context, input string) (any, error)
	GetWikiNode(ctx context.Context, input string) (any, error)
	GetSheetsMeta(ctx context.Context, spreadsheetToken string) (any, error)
	GetSheetValues(ctx context.Context, spreadsheetToken, sheetID, rangeRef string, query url.Values) (any, error)
	GetDocumentComments(ctx context.Context, fileToken, fileType string) (any, error)
	CreateDocumentBlocks(ctx context.Context, docID, blockID string, body any) (any, error)
	DeleteDocumentBlock(ctx context.Context, docID, blockID string) (any, error)
	UpdateDocumentBlocks(ctx context.Context, docID string, body any) (any, error)
	SearchDocs(ctx context.Context, body any) (any, error)
	CreateDocument(ctx context.Context, body any) (any, error)
	CreateDocumentComment(ctx context.Context, fileToken, fileType string, body any) (any, error)
	ReplyDocumentComment(ctx context.Context, fileToken, commentID, fileType string, body any) (any, error)
	ResolveDocumentComment(ctx context.Context, fileToken, commentID, fileType string, resolved bool) (any, error)
	ListDocumentPermissions(ctx context.Context, token, fileType string) (any, error)
	ListDriveFiles(ctx context.Context, folderToken string) (any, error)
	CreateFolder(ctx context.Context, body any) (any, error)
	GetBitableMeta(ctx context.Context, appToken string) (any, error)
	ListBitableTables(ctx context.Context, appToken string) (any, error)
	ListBitableFields(ctx context.Context, appToken, tableID string) (any, error)
	ListBitableRecords(ctx context.Context, appToken, tableID string, extra url.Values) (any, error)
	CreateBitableRecord(ctx context.Context, appToken, tableID string, body any) (any, error)
	UpdateBitableRecord(ctx context.Context, appToken, tableID, recordID string, body any) (any, error)
	CreateBitableField(ctx context.Context, appToken, tableID string, body any) (any, error)
	UpdateBitableField(ctx context.Context, appToken, tableID, fieldID string, body any) (any, error)
	DeleteBitableField(ctx context.Context, appToken, tableID, fieldID string) (any, error)
	ListBitableViews(ctx context.Context, appToken, tableID string) (any, error)
	GetBitableView(ctx context.Context, appToken, tableID, viewID string) (any, error)
	CreateBitableView(ctx context.Context, appToken, tableID string, body any) (any, error)
	UpdateBitableView(ctx context.Context, appToken, tableID, viewID string, body any) (any, error)
	DeleteBitableView(ctx context.Context, appToken, tableID, viewID string) (any, error)
	UpdateSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error)
	AppendSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error)
	ListWikiSpaces(ctx context.Context) (any, error)
	ListWikiNodes(ctx context.Context, spaceID, parentNodeToken string) (any, error)
	CreateWikiNode(ctx context.Context, spaceID string, body any) (any, error)
	GetBoardNodes(ctx context.Context, whiteboardID string) (any, error)
	CreateBoardNodes(ctx context.Context, whiteboardID string, body any, clientToken string) (any, error)
	DeleteBoardNodes(ctx context.Context, whiteboardID string, nodeIDs []string) (any, error)
	CreateTask(ctx context.Context, body any) (any, error)
	SearchUsers(ctx context.Context, query string, pageSize int) (any, error)
	GetCalendarPrimary(ctx context.Context) (any, error)
	ListCalendarEvents(ctx context.Context, calendarID, startTime, endTime string) (any, error)
	CreateCalendarEvent(ctx context.Context, calendarID string, body any) (any, error)
	AddCalendarAttendees(ctx context.Context, calendarID, eventID string, body any) (any, error)
	GetFreebusy(ctx context.Context, body any) (any, error)
	ListRooms(ctx context.Context, keyword string) (any, error)
	GetSheetsFloatImages(ctx context.Context, spreadsheetToken, sheetID string) (any, error)
	ExportCreate(ctx context.Context, token, fileType, fileExtension string) (string, error) // returns ticket
	ExportStatus(ctx context.Context, ticket, token string) (exportResult, error)
	ExportDownload(ctx context.Context, fileToken string, w io.Writer) error
	DownloadMedia(ctx context.Context, fileToken string, w io.Writer) (string, error)

	// IM
	SendMessage(ctx context.Context, body any) (any, error)
	ListMessages(ctx context.Context, containerID string, query url.Values) (any, error)
	SearchMessages(ctx context.Context, body any, query url.Values) (any, error)
	ReplyMessage(ctx context.Context, messageID string, body any) (any, error)
	SearchChats(ctx context.Context, query string, pageSize int) (any, error)
	MGetMessages(ctx context.Context, messageIDs []string) (any, error)
	DownloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string, w io.Writer) error
	ListThreadMessages(ctx context.Context, threadID string, query url.Values) (any, error)
	UploadIMImage(ctx context.Context, fileName string, fileReader io.Reader) (string, error)          // returns image_key
	UploadIMFile(ctx context.Context, fileType, fileName string, fileReader io.Reader) (string, error) // returns file_key
	ReactMessage(ctx context.Context, messageID, emojiType string) (any, error)
	ListMessageReactions(ctx context.Context, messageID string, query url.Values) (any, error)
	DeleteMessageReaction(ctx context.Context, messageID, reactionID string) (any, error)
	// Task (extended)
	ListTasks(ctx context.Context, query url.Values) (any, error)
	UpdateTask(ctx context.Context, taskID string, body any) (any, error)
	CompleteTask(ctx context.Context, taskID string, completed bool) (any, error)
	CommentTask(ctx context.Context, taskID, content string) (any, error)
	ManageTaskMembers(ctx context.Context, taskID, action string, members []map[string]any) (any, error)
	ManageTaskReminders(ctx context.Context, taskID, action string, reminders []map[string]any) (any, error)
	CreateTasklist(ctx context.Context, name string) (any, error)
	AddTaskToTasklist(ctx context.Context, taskID, tasklistID string) (any, error)
	ManageTasklistMembers(ctx context.Context, tasklistID, action string, members []map[string]any) (any, error)
	// Docs media: uploads a local file as document media bound to an existing
	// image (docx_image) or file (docx_file) block; returns the media file_token.
	UploadDocsMedia(ctx context.Context, parentType, parentNode, fileName string, fileReader io.Reader, fileSize int64) (string, error)
	// Drive (extended)
	UploadFile(ctx context.Context, parentToken, fileName string, fileReader io.Reader, fileSize int64) (any, error)
	DownloadFile(ctx context.Context, fileToken string, w io.Writer) error
	// Drive import
	ImportUpload(ctx context.Context, fileName, docType, fileExt string, fileReader io.Reader, fileSize int64) (string, error)
	ImportCreate(ctx context.Context, fileToken, fileType, fileExt, fileName, folderToken string) (string, error)
	ImportStatus(ctx context.Context, ticket string) (any, error)
	// Calendar (extended)
	RSVPCalendarEvent(ctx context.Context, calendarID, eventID, rsvpStatus string) (any, error)

	// Mail
	ListMailMessages(ctx context.Context, query url.Values) (any, error)
	GetMailMessage(ctx context.Context, messageID, format string) (any, error)
	BatchGetMailMessages(ctx context.Context, body any) (any, error)
	GetMailThread(ctx context.Context, threadID, format string, includeSpamTrash bool) (any, error)
	SearchMail(ctx context.Context, body any, query url.Values) (any, error)
	TrashMailMessage(ctx context.Context, messageID string) (any, error)
	GetMailSendStatus(ctx context.Context, messageID string) (any, error)
	GetMailAttachmentDownloadURL(ctx context.Context, messageID string, attachmentIDs []string) (any, error)
	CreateMailDraft(ctx context.Context, rawEML string) (any, error)
	UpdateMailDraft(ctx context.Context, draftID, rawEML string) (any, error)
	SendMailDraft(ctx context.Context, draftID, sendTime string) (any, error)
	DeleteMailDraft(ctx context.Context, draftID string) (any, error)
	ListMailFolders(ctx context.Context, folderType int) (any, error)
	ListAccessibleMailboxes(ctx context.Context) (any, error)
	GetMailSendAs(ctx context.Context) (any, error)
	GetMailProfile(ctx context.Context) (any, error)
}

// fillDocsMediaForm writes the multipart form that /drive/v1/medias/upload_all
// expects for document media. Both clients post the same body — the gateway
// passes it through untouched — so the field contract lives here once.
func fillDocsMediaForm(mw *multipart.Writer, parentType, parentNode, fileName string, fileReader io.Reader, fileSize int64) error {
	fields := [][2]string{
		{"file_name", fileName},
		{"parent_type", parentType},
		{"parent_node", parentNode},
		{"size", strconv.FormatInt(fileSize, 10)},
	}
	for _, f := range fields {
		if err := mw.WriteField(f[0], f[1]); err != nil {
			return err
		}
	}
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return err
	}
	_, err = io.Copy(fw, fileReader)
	return err
}
