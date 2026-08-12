package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ProtocolVersion must match the gateway's protocol version.
const ProtocolVersion = "v1"

type VersionResponse struct {
	ProtocolVersion  string `json:"protocol_version"`
	MaxSecurityLevel int    `json:"max_security_level"`
}

type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Code == "" {
		return fmt.Sprintf("gateway error (status=%d): %s", e.Status, e.Message)
	}
	return fmt.Sprintf("gateway error [%s] (status=%d): %s", e.Code, e.Status, e.Message)
}

type apiEnvelope struct {
	Code    string          `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type GatewayClient struct {
	baseURL    string
	httpClient *http.Client

	mu           sync.RWMutex
	sessionToken string
}

type DeviceStartResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	IntervalSeconds         int    `json:"interval_seconds"`
	ExpiresInSeconds        int    `json:"expires_in_seconds"`
}

type DevicePollResponse struct {
	Pending           bool         `json:"pending"`
	ClientToken       string       `json:"client_token,omitempty"`
	SessionExpiresAt  string       `json:"session_expires_at,omitempty"`
	SessionIdleAt     string       `json:"session_idle_at,omitempty"`
	User              UserIdentity `json:"user,omitempty"`
	Message           string       `json:"message,omitempty"`
	RecommendedWaitMS int          `json:"recommended_wait_ms,omitempty"`
}

type SessionRefreshResponse struct {
	Refreshed        bool   `json:"refreshed"`
	SessionExpiresAt string `json:"session_expires_at,omitempty"`
	SessionIdleAt    string `json:"session_idle_at,omitempty"`
}

func NewGatewayClient(baseURL string) *GatewayClient {
	return &GatewayClient{
		baseURL: normalizeBaseURL(baseURL),
		httpClient: &http.Client{
			Timeout: 45 * time.Second,
		},
	}
}

// CheckVersion verifies that the gateway supports this client's protocol version.
func (c *GatewayClient) CheckVersion(ctx context.Context) (VersionResponse, error) {
	var out VersionResponse
	err := c.callJSON(ctx, "GET", "/v1/version", nil, nil, false, &out)
	if err != nil {
		return VersionResponse{}, err
	}
	if out.ProtocolVersion != ProtocolVersion {
		return out, fmt.Errorf("protocol version mismatch: gateway=%s client=%s, please upgrade", out.ProtocolVersion, ProtocolVersion)
	}
	return out, nil
}

func (c *GatewayClient) SetSessionToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sessionToken = strings.TrimSpace(token)
}

func (c *GatewayClient) session() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.sessionToken
}

func (c *GatewayClient) StartDeviceLogin(ctx context.Context, scopes, token string) (DeviceStartResponse, error) {
	body := map[string]any{"client": "larkctl"}
	if scopes != "" {
		body["scopes"] = scopes
	}
	if token != "" {
		body["token"] = token
	}
	var out DeviceStartResponse
	err := c.callJSON(ctx, "POST", "/v1/device/start", nil, body, false, &out)
	return out, err
}

func (c *GatewayClient) PollDeviceLogin(ctx context.Context, deviceCode string) (DevicePollResponse, error) {
	status, env, err := c.requestEnvelope(ctx, "POST", "/v1/device/poll", nil, map[string]any{"device_code": deviceCode}, false)
	if err != nil {
		return DevicePollResponse{}, err
	}

	switch env.Code {
	case "ok":
		var out DevicePollResponse
		if len(env.Data) > 0 {
			if err := json.Unmarshal(env.Data, &out); err != nil {
				return DevicePollResponse{}, err
			}
		}
		return out, nil
	case "authorization_pending", "slow_down":
		out := DevicePollResponse{Pending: true, Message: env.Message}
		if env.Code == "slow_down" {
			out.RecommendedWaitMS = 5_000
		}
		return out, nil
	default:
		return DevicePollResponse{}, &APIError{Status: status, Code: env.Code, Message: env.Message}
	}
}

func (c *GatewayClient) WhoAmI(ctx context.Context) (map[string]any, error) {
	var out map[string]any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/whoami", nil, nil, &out)
	return out, err
}

func (c *GatewayClient) Logout(ctx context.Context) error {
	_, env, err := c.requestEnvelope(ctx, "DELETE", "/v1/session", nil, nil, true)
	if err != nil {
		return err
	}
	if env.Code != "ok" && env.Code != "session_not_found" {
		return &APIError{Status: http.StatusUnauthorized, Code: env.Code, Message: env.Message}
	}
	return nil
}

func (c *GatewayClient) RefreshSession(ctx context.Context) error {
	if strings.TrimSpace(c.session()) == "" {
		return errors.New("no client token to refresh")
	}

	var out SessionRefreshResponse
	if err := c.callJSON(ctx, "POST", "/v1/session/refresh", nil, nil, true, &out); err != nil {
		return err
	}
	// Client token stays the same — gateway rotates session internally
	return nil
}

func (c *GatewayClient) GetDocumentInfo(ctx context.Context, input, docType string) (any, error) {
	resolvedType, err := inferDocumentType(input, docType)
	if err != nil {
		return nil, err
	}

	query := url.Values{}
	query.Set("document_id", input)
	query.Set("document_type", resolvedType)

	var out any
	err = c.callJSONWithRefresh(ctx, "GET", "/v1/docs/info", query, nil, &out)
	return out, err
}

func (c *GatewayClient) GetDocumentBlocks(ctx context.Context, input string) (any, error) {
	query := url.Values{}
	query.Set("document_id", input)

	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/docs/blocks", query, nil, &out)
	return out, err
}

func (c *GatewayClient) GetWikiNode(ctx context.Context, input string) (any, error) {
	query := url.Values{}
	query.Set("token", input)

	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/wiki/node", query, nil, &out)
	return out, err
}

func (c *GatewayClient) GetSheetsMeta(ctx context.Context, spreadsheetToken string) (any, error) {
	query := url.Values{}
	query.Set("spreadsheet_token", spreadsheetToken)

	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/sheets/meta", query, nil, &out)
	return out, err
}

func (c *GatewayClient) GetSheetValues(ctx context.Context, spreadsheetToken, sheetID, rangeRef string, extra url.Values) (any, error) {
	query := url.Values{}
	for k, vs := range extra {
		for _, v := range vs {
			query.Add(k, v)
		}
	}
	query.Set("spreadsheet_token", spreadsheetToken)
	query.Set("sheet_id", sheetID)
	query.Set("range", rangeRef)

	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/sheets/values", query, nil, &out)
	return out, err
}

func (c *GatewayClient) GetDocumentComments(ctx context.Context, fileToken, fileType string) (any, error) {
	query := url.Values{}
	query.Set("file_token", fileToken)
	if fileType != "" {
		query.Set("file_type", fileType)
	}

	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/docs/comments", query, nil, &out)
	return out, err
}

func (c *GatewayClient) CreateDocumentBlocks(ctx context.Context, docID, blockID string, body any) (any, error) {
	query := url.Values{}
	query.Set("document_id", docID)
	if blockID != "" {
		query.Set("block_id", blockID)
	}

	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/blocks/create", query, body, &out)
	return out, err
}

func (c *GatewayClient) DeleteDocumentBlock(ctx context.Context, docID, blockID string) (any, error) {
	query := url.Values{}
	query.Set("document_id", docID)
	query.Set("block_id", blockID)

	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/blocks/delete", query, nil, &out)
	return out, err
}

func (c *GatewayClient) UpdateDocumentBlocks(ctx context.Context, docID string, body any) (any, error) {
	query := url.Values{"document_id": {docID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/blocks/update", query, body, &out)
	return out, err
}

func (c *GatewayClient) SearchDocs(ctx context.Context, body any) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/search", nil, body, &out)
	return out, err
}

func (c *GatewayClient) CreateDocument(ctx context.Context, body any) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/create", nil, body, &out)
	return out, err
}

func (c *GatewayClient) CreateDocumentComment(ctx context.Context, fileToken, fileType string, body any) (any, error) {
	query := url.Values{}
	query.Set("file_token", fileToken)
	if fileType != "" {
		query.Set("file_type", fileType)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/comments/create", query, body, &out)
	return out, err
}

func (c *GatewayClient) ReplyDocumentComment(ctx context.Context, fileToken, commentID, fileType string, body any) (any, error) {
	query := url.Values{}
	query.Set("file_token", fileToken)
	query.Set("comment_id", commentID)
	if fileType != "" {
		query.Set("file_type", fileType)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/comments/reply", query, body, &out)
	return out, err
}

func (c *GatewayClient) ResolveDocumentComment(ctx context.Context, fileToken, commentID, fileType string, resolved bool) (any, error) {
	query := url.Values{}
	query.Set("file_token", fileToken)
	query.Set("comment_id", commentID)
	if fileType != "" {
		query.Set("file_type", fileType)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/docs/comments/resolve", query, map[string]any{"is_solved": resolved}, &out)
	return out, err
}

func (c *GatewayClient) ListDocumentPermissions(ctx context.Context, token, fileType string) (any, error) {
	query := url.Values{}
	query.Set("token", token)
	query.Set("file_type", fileType)
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/docs/permissions", query, nil, &out)
	return out, err
}

func (c *GatewayClient) ListDriveFiles(ctx context.Context, folderToken string) (any, error) {
	query := url.Values{}
	if folderToken != "" {
		query.Set("folder_token", folderToken)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/drive/files", query, nil, &out)
	return out, err
}

func (c *GatewayClient) CreateFolder(ctx context.Context, body any) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/drive/folder/create", nil, body, &out)
	return out, err
}

func (c *GatewayClient) GetBitableMeta(ctx context.Context, appToken string) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/bitable/meta", query, nil, &out)
	return out, err
}

func (c *GatewayClient) ListBitableTables(ctx context.Context, appToken string) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/bitable/tables", query, nil, &out)
	return out, err
}

func (c *GatewayClient) ListBitableFields(ctx context.Context, appToken, tableID string) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	query.Set("table_id", tableID)
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/bitable/fields", query, nil, &out)
	return out, err
}

func (c *GatewayClient) ListBitableRecords(ctx context.Context, appToken, tableID string, extra url.Values) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	query.Set("table_id", tableID)
	for k, v := range extra {
		query[k] = v
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/bitable/records", query, nil, &out)
	return out, err
}

func (c *GatewayClient) CreateBitableRecord(ctx context.Context, appToken, tableID string, body any) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	query.Set("table_id", tableID)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/bitable/records/create", query, body, &out)
	return out, err
}

func (c *GatewayClient) UpdateBitableRecord(ctx context.Context, appToken, tableID, recordID string, body any) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	query.Set("table_id", tableID)
	query.Set("record_id", recordID)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/bitable/records/update", query, body, &out)
	return out, err
}

func (c *GatewayClient) CreateBitableField(ctx context.Context, appToken, tableID string, body any) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	query.Set("table_id", tableID)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/bitable/fields/create", query, body, &out)
	return out, err
}

func (c *GatewayClient) UpdateBitableField(ctx context.Context, appToken, tableID, fieldID string, body any) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	query.Set("table_id", tableID)
	query.Set("field_id", fieldID)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/bitable/fields/update", query, body, &out)
	return out, err
}

func (c *GatewayClient) DeleteBitableField(ctx context.Context, appToken, tableID, fieldID string) (any, error) {
	query := url.Values{}
	query.Set("app_token", appToken)
	query.Set("table_id", tableID)
	query.Set("field_id", fieldID)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/bitable/fields/delete", query, nil, &out)
	return out, err
}

func (c *GatewayClient) UpdateSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error) {
	query := url.Values{}
	query.Set("spreadsheet_token", spreadsheetToken)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/sheets/values/update", query, body, &out)
	return out, err
}

func (c *GatewayClient) AppendSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error) {
	query := url.Values{}
	query.Set("spreadsheet_token", spreadsheetToken)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/sheets/values/append", query, body, &out)
	return out, err
}

func (c *GatewayClient) ListWikiSpaces(ctx context.Context) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/wiki/spaces", nil, nil, &out)
	return out, err
}

func (c *GatewayClient) ListWikiNodes(ctx context.Context, spaceID, parentNodeToken string) (any, error) {
	query := url.Values{}
	query.Set("space_id", spaceID)
	if parentNodeToken != "" {
		query.Set("parent_node_token", parentNodeToken)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/wiki/nodes", query, nil, &out)
	return out, err
}

func (c *GatewayClient) CreateWikiNode(ctx context.Context, spaceID string, body any) (any, error) {
	query := url.Values{}
	query.Set("space_id", spaceID)
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/wiki/nodes/create", query, body, &out)
	return out, err
}

func (c *GatewayClient) GetBoardNodes(ctx context.Context, whiteboardID string) (any, error) {
	query := url.Values{}
	query.Set("whiteboard_id", whiteboardID)
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/board/nodes", query, nil, &out)
	return out, err
}

func (c *GatewayClient) CreateTask(ctx context.Context, body any) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/create", nil, body, &out)
	return out, err
}

func (c *GatewayClient) SearchUsers(ctx context.Context, query string, pageSize int) (any, error) {
	body := map[string]any{"query": query}
	if pageSize > 0 {
		body["page_size"] = pageSize
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/users/search", nil, body, &out)
	return out, err
}

func (c *GatewayClient) GetCalendarPrimary(ctx context.Context) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/calendar/primary", nil, nil, &out)
	return out, err
}

func (c *GatewayClient) ListCalendarEvents(ctx context.Context, calendarID, startTime, endTime string) (any, error) {
	query := url.Values{"calendar_id": {calendarID}}
	if startTime != "" {
		query.Set("start_time", startTime)
	}
	if endTime != "" {
		query.Set("end_time", endTime)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/calendar/events", query, nil, &out)
	return out, err
}

func (c *GatewayClient) CreateCalendarEvent(ctx context.Context, calendarID string, body any) (any, error) {
	query := url.Values{"calendar_id": {calendarID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/calendar/events/create", query, body, &out)
	return out, err
}

func (c *GatewayClient) AddCalendarAttendees(ctx context.Context, calendarID, eventID string, body any) (any, error) {
	query := url.Values{"calendar_id": {calendarID}, "event_id": {eventID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/calendar/events/attendees", query, body, &out)
	return out, err
}

func (c *GatewayClient) GetFreebusy(ctx context.Context, body any) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/calendar/freebusy", nil, body, &out)
	return out, err
}

func (c *GatewayClient) ListRooms(ctx context.Context, keyword string) (any, error) {
	query := url.Values{}
	if keyword != "" {
		query.Set("keyword", keyword)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/calendar/rooms", query, nil, &out)
	return out, err
}

func (c *GatewayClient) UpgradeScopes(ctx context.Context, scopes string) (map[string]any, error) {
	var out map[string]any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/session/upgrade", nil, map[string]any{"scopes": scopes}, &out)
	return out, err
}

func (c *GatewayClient) GetSessionScopes(ctx context.Context) (string, error) {
	var out map[string]any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/session/scopes", nil, nil, &out)
	if err != nil {
		return "", err
	}
	s, _ := out["scopes"].(string)
	return s, nil
}

func (c *GatewayClient) ExportCreate(ctx context.Context, token, fileType, fileExtension string) (string, error) {
	body := map[string]any{"token": token, "type": fileType, "file_extension": fileExtension}
	var out map[string]any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/export/create", nil, body, &out)
	if err != nil {
		return "", err
	}
	ticket, _ := out["ticket"].(string)
	return ticket, nil
}

func (c *GatewayClient) ExportStatus(ctx context.Context, ticket, token string) (exportResult, error) {
	query := url.Values{"ticket": {ticket}, "token": {token}}
	var out map[string]any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/export/status", query, nil, &out)
	if err != nil {
		return exportResult{}, err
	}
	result, _ := out["result"].(map[string]any)
	if result == nil {
		return exportResult{}, fmt.Errorf("no result in response")
	}
	return exportResult{
		JobStatus: int(intFromAny(result["job_status"])),
		FileToken: pickString(result, "file_token"),
		FileSize:  int64(intFromAny(result["file_size"])),
		FileName:  pickString(result, "file_name"),
		ErrMsg:    pickString(result, "job_error_msg"),
	}, nil
}

func (c *GatewayClient) ExportDownload(ctx context.Context, fileToken string, w io.Writer) error {
	targetURL := c.baseURL + "/v1/export/download?file_token=" + url.QueryEscape(fileToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(c.session())
	if token == "" {
		return errors.New("missing session token")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Client-Version", ProtocolVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed (status=%d): %s", resp.StatusCode, string(body))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (c *GatewayClient) GetSheetsFloatImages(ctx context.Context, spreadsheetToken, sheetID string) (any, error) {
	query := url.Values{"spreadsheet_token": {spreadsheetToken}, "sheet_id": {sheetID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/sheets/float_images", query, nil, &out)
	return out, err
}

// DownloadMedia downloads a drive media file (image, etc.) and writes it to w.
// Returns the Content-Type header from the upstream response.
func (c *GatewayClient) DownloadMedia(ctx context.Context, fileToken string, w io.Writer) (contentType string, err error) {
	targetURL := c.baseURL + "/v1/drive/media?file_token=" + url.QueryEscape(fileToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(c.session())
	if token == "" {
		return "", errors.New("missing session token, run `larkctl login`")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Client-Version", ProtocolVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download failed (status=%d): %s", resp.StatusCode, string(body))
	}

	contentType = resp.Header.Get("Content-Type")
	_, err = io.Copy(w, resp.Body)
	return contentType, err
}

// IM methods

func (c *GatewayClient) SendMessage(ctx context.Context, body any) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/im/messages/send", nil, body, &out)
	return out, err
}

func (c *GatewayClient) ListMessages(ctx context.Context, containerID string, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("container_id", containerID)
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/im/messages", query, nil, &out)
	return out, err
}

func (c *GatewayClient) SearchMessages(ctx context.Context, body any, query url.Values) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/im/messages/search", query, body, &out)
	return out, err
}

func (c *GatewayClient) ReplyMessage(ctx context.Context, messageID string, body any) (any, error) {
	m, ok := body.(map[string]any)
	if !ok {
		m = map[string]any{}
		if body != nil {
			b, _ := json.Marshal(body)
			_ = json.Unmarshal(b, &m)
		}
	}
	m["message_id"] = messageID
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/im/messages/reply", nil, m, &out)
	return out, err
}

func (c *GatewayClient) SearchChats(ctx context.Context, query string, pageSize int) (any, error) {
	q := url.Values{}
	if pageSize > 0 {
		q.Set("page_size", strconv.Itoa(pageSize))
	}
	body := map[string]any{"query": query}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/im/chats/search", q, body, &out)
	return out, err
}

func (c *GatewayClient) MGetMessages(ctx context.Context, messageIDs []string) (any, error) {
	body := map[string]any{"message_ids": messageIDs}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/im/messages/mget", nil, body, &out)
	return out, err
}

func (c *GatewayClient) DownloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string, w io.Writer) error {
	q := url.Values{
		"message_id": {messageID},
		"file_key":   {fileKey},
		"type":       {resourceType},
	}
	targetURL := c.baseURL + "/v1/im/messages/resource?" + q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(c.session())
	if token == "" {
		return errors.New("missing session token, run `larkctl login`")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Client-Version", ProtocolVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed (status=%d): %s", resp.StatusCode, string(body))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

func (c *GatewayClient) ListThreadMessages(ctx context.Context, threadID string, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("container_id", threadID)
	query.Set("container_id_type", "thread")
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/im/messages", query, nil, &out)
	return out, err
}

func (c *GatewayClient) ReactMessage(ctx context.Context, messageID, emojiType string) (any, error) {
	body := map[string]any{"message_id": messageID, "emoji_type": emojiType}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/im/messages/reactions", nil, body, &out)
	return out, err
}

func (c *GatewayClient) ListMessageReactions(ctx context.Context, messageID string, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("message_id", messageID)
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/im/messages/reactions", query, nil, &out)
	return out, err
}

func (c *GatewayClient) DeleteMessageReaction(ctx context.Context, messageID, reactionID string) (any, error) {
	query := url.Values{"message_id": {messageID}, "reaction_id": {reactionID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "DELETE", "/v1/im/messages/reactions", query, nil, &out)
	return out, err
}

// uploadIMResource posts a multipart form to a gateway IM upload endpoint and
// returns the named key ("image_key" / "file_key") from the response.
func (c *GatewayClient) uploadIMResource(ctx context.Context, path, keyField string, fill func(*multipart.Writer) error) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := fill(mw); err != nil {
		return "", fmt.Errorf("build multipart form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Client-Version", ProtocolVersion)
	token := strings.TrimSpace(c.session())
	if token == "" {
		return "", errors.New("missing session token, run `larkctl login`")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("upload failed (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if env.Code != "ok" {
		return "", &APIError{Status: resp.StatusCode, Code: env.Code, Message: env.Message}
	}
	var data map[string]any
	if len(env.Data) > 0 {
		_ = json.Unmarshal(env.Data, &data)
	}
	key, _ := data[keyField].(string)
	if key == "" {
		return "", fmt.Errorf("upload returned no %s: %s", keyField, strings.TrimSpace(string(raw)))
	}
	return key, nil
}

func (c *GatewayClient) UploadIMImage(ctx context.Context, fileName string, fileReader io.Reader) (string, error) {
	return c.uploadIMResource(ctx, "/v1/im/images/upload", "image_key", func(mw *multipart.Writer) error {
		if err := mw.WriteField("image_type", "message"); err != nil {
			return err
		}
		fw, err := mw.CreateFormFile("image", fileName)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, fileReader)
		return err
	})
}

func (c *GatewayClient) UploadIMFile(ctx context.Context, fileType, fileName string, fileReader io.Reader) (string, error) {
	return c.uploadIMResource(ctx, "/v1/im/files/upload", "file_key", func(mw *multipart.Writer) error {
		if err := mw.WriteField("file_type", fileType); err != nil {
			return err
		}
		if err := mw.WriteField("file_name", fileName); err != nil {
			return err
		}
		fw, err := mw.CreateFormFile("file", fileName)
		if err != nil {
			return err
		}
		_, err = io.Copy(fw, fileReader)
		return err
	})
}

// Task members, reminders, tasklists

func (c *GatewayClient) ManageTaskMembers(ctx context.Context, taskID, action string, members []map[string]any) (any, error) {
	body := map[string]any{"task_id": taskID, "action": action, "members": members}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/members", nil, body, &out)
	return out, err
}

func (c *GatewayClient) ManageTaskReminders(ctx context.Context, taskID, action string, reminders []map[string]any) (any, error) {
	body := map[string]any{"task_id": taskID, "action": action, "reminders": reminders}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/reminders", nil, body, &out)
	return out, err
}

func (c *GatewayClient) CreateTasklist(ctx context.Context, name string) (any, error) {
	body := map[string]any{"name": name}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/tasklist/create", nil, body, &out)
	return out, err
}

func (c *GatewayClient) AddTaskToTasklist(ctx context.Context, taskID, tasklistID string) (any, error) {
	body := map[string]any{"task_id": taskID, "tasklist_id": tasklistID}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/tasklist/add", nil, body, &out)
	return out, err
}

func (c *GatewayClient) ManageTasklistMembers(ctx context.Context, tasklistID, action string, members []map[string]any) (any, error) {
	body := map[string]any{"tasklist_id": tasklistID, "action": action, "members": members}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/tasklist/members", nil, body, &out)
	return out, err
}

// Task methods (extended)

func (c *GatewayClient) ListTasks(ctx context.Context, query url.Values) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/tasks/list", query, nil, &out)
	return out, err
}

func (c *GatewayClient) UpdateTask(ctx context.Context, taskID string, body any) (any, error) {
	m, ok := body.(map[string]any)
	if !ok {
		m = map[string]any{}
		if body != nil {
			b, _ := json.Marshal(body)
			_ = json.Unmarshal(b, &m)
		}
	}
	m["task_id"] = taskID
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/update", nil, m, &out)
	return out, err
}

func (c *GatewayClient) CompleteTask(ctx context.Context, taskID string, completed bool) (any, error) {
	body := map[string]any{"task_id": taskID, "completed": completed}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/complete", nil, body, &out)
	return out, err
}

func (c *GatewayClient) CommentTask(ctx context.Context, taskID, content string) (any, error) {
	body := map[string]any{"task_id": taskID, "content": content}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/tasks/comment", nil, body, &out)
	return out, err
}

// Drive methods (extended)

func (c *GatewayClient) UploadFile(ctx context.Context, parentToken, fileName string, fileReader io.Reader, fileSize int64) (any, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("parent_type", "explorer")
	_ = mw.WriteField("parent_node", parentToken)
	_ = mw.WriteField("file_name", fileName)
	_ = mw.WriteField("size", strconv.FormatInt(fileSize, 10))
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return nil, fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, fileReader); err != nil {
		return nil, fmt.Errorf("write file to form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close multipart writer: %w", err)
	}

	targetURL := c.baseURL + "/v1/drive/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Client-Version", ProtocolVersion)
	token := strings.TrimSpace(c.session())
	if token == "" {
		return nil, errors.New("missing session token, run `larkctl login`")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upload failed (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}
	if env.Code != "ok" {
		return nil, &APIError{Status: resp.StatusCode, Code: env.Code, Message: env.Message}
	}
	var out any
	if len(env.Data) > 0 {
		_ = json.Unmarshal(env.Data, &out)
	}
	return out, nil
}

func (c *GatewayClient) ImportUpload(ctx context.Context, fileName, docType, fileExt string, fileReader io.Reader, fileSize int64) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("file_name", fileName)
	_ = mw.WriteField("parent_type", "ccm_import_open")
	_ = mw.WriteField("size", strconv.FormatInt(fileSize, 10))
	extraJSON, _ := json.Marshal(map[string]string{"obj_type": docType, "file_extension": strings.TrimPrefix(fileExt, ".")})
	_ = mw.WriteField("extra", string(extraJSON))
	fw, err := mw.CreateFormFile("file", fileName)
	if err != nil {
		return "", fmt.Errorf("create form file: %w", err)
	}
	if _, err := io.Copy(fw, fileReader); err != nil {
		return "", fmt.Errorf("write file to form: %w", err)
	}
	if err := mw.Close(); err != nil {
		return "", fmt.Errorf("close multipart writer: %w", err)
	}

	targetURL := c.baseURL + "/v1/drive/import/upload"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("X-Client-Version", ProtocolVersion)
	token := strings.TrimSpace(c.session())
	if token == "" {
		return "", errors.New("missing session token, run `larkctl login`")
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("import upload failed (status=%d): %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return "", fmt.Errorf("decode import upload response: %w", err)
	}
	if env.Code != "ok" {
		return "", &APIError{Status: resp.StatusCode, Code: env.Code, Message: env.Message}
	}
	var data map[string]any
	if len(env.Data) > 0 {
		_ = json.Unmarshal(env.Data, &data)
	}
	fileToken, _ := data["file_token"].(string)
	if fileToken == "" {
		// Without this the caller proceeds to ImportCreate with an empty token and
		// fails there instead, hiding what actually went wrong.
		return "", fmt.Errorf("import upload returned no file_token: %s", strings.TrimSpace(string(raw)))
	}
	return fileToken, nil
}

func (c *GatewayClient) ImportCreate(ctx context.Context, fileToken, fileType, fileExt, fileName, folderToken string) (string, error) {
	body := map[string]any{
		"file_token":     fileToken,
		"type":           fileType,
		"file_extension": fileExt,
		"file_name":      fileName,
		"mount_key":      folderToken,
	}
	var out map[string]any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/drive/import/create", nil, body, &out)
	if err != nil {
		return "", err
	}
	ticket, _ := out["ticket"].(string)
	return ticket, nil
}

func (c *GatewayClient) ImportStatus(ctx context.Context, ticket string) (any, error) {
	query := url.Values{"ticket": {ticket}}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/drive/import/status", query, nil, &out)
	return out, err
}

func (c *GatewayClient) DownloadFile(ctx context.Context, fileToken string, w io.Writer) error {
	targetURL := c.baseURL + "/v1/drive/download?file_token=" + url.QueryEscape(fileToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return err
	}
	token := strings.TrimSpace(c.session())
	if token == "" {
		return errors.New("missing session token, run `larkctl login`")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Client-Version", ProtocolVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("download failed (status=%d): %s", resp.StatusCode, string(body))
	}
	_, err = io.Copy(w, resp.Body)
	return err
}

// Calendar method (extended)

func (c *GatewayClient) RSVPCalendarEvent(ctx context.Context, calendarID, eventID, rsvpStatus string) (any, error) {
	body := map[string]any{"calendar_id": calendarID, "event_id": eventID, "rsvp_status": rsvpStatus}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/calendar/events/rsvp", nil, body, &out)
	return out, err
}

// Mail.

func (c *GatewayClient) ListMailMessages(ctx context.Context, query url.Values) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/messages", query, nil, &out)
	return out, err
}

func (c *GatewayClient) GetMailMessage(ctx context.Context, messageID, format string) (any, error) {
	q := url.Values{"message_id": {messageID}}
	if format != "" {
		q.Set("format", format)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/messages/get", q, nil, &out)
	return out, err
}

func (c *GatewayClient) BatchGetMailMessages(ctx context.Context, body any) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/mail/messages/batch_get", nil, body, &out)
	return out, err
}

func (c *GatewayClient) GetMailThread(ctx context.Context, threadID, format string, includeSpamTrash bool) (any, error) {
	q := url.Values{"thread_id": {threadID}}
	if format != "" {
		q.Set("format", format)
	}
	if includeSpamTrash {
		q.Set("include_spam_trash", "true")
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/threads/get", q, nil, &out)
	return out, err
}

func (c *GatewayClient) SearchMail(ctx context.Context, body any, query url.Values) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/mail/search", query, body, &out)
	return out, err
}

func (c *GatewayClient) TrashMailMessage(ctx context.Context, messageID string) (any, error) {
	q := url.Values{"message_id": {messageID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/mail/messages/trash", q, nil, &out)
	return out, err
}

func (c *GatewayClient) GetMailSendStatus(ctx context.Context, messageID string) (any, error) {
	q := url.Values{"message_id": {messageID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/messages/send_status", q, nil, &out)
	return out, err
}

func (c *GatewayClient) GetMailAttachmentDownloadURL(ctx context.Context, messageID string, attachmentIDs []string) (any, error) {
	q := url.Values{"message_id": {messageID}}
	for _, id := range attachmentIDs {
		q.Add("attachment_ids", id)
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/messages/attachments/download_url", q, nil, &out)
	return out, err
}

func (c *GatewayClient) CreateMailDraft(ctx context.Context, rawEML string) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/mail/drafts/create", nil, map[string]any{"raw": rawEML}, &out)
	return out, err
}

func (c *GatewayClient) UpdateMailDraft(ctx context.Context, draftID, rawEML string) (any, error) {
	q := url.Values{"draft_id": {draftID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/mail/drafts/update", q, map[string]any{"raw": rawEML}, &out)
	return out, err
}

func (c *GatewayClient) SendMailDraft(ctx context.Context, draftID, sendTime string) (any, error) {
	q := url.Values{"draft_id": {draftID}}
	var body map[string]any
	if sendTime != "" {
		body = map[string]any{"send_time": sendTime}
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/mail/drafts/send", q, body, &out)
	return out, err
}

func (c *GatewayClient) DeleteMailDraft(ctx context.Context, draftID string) (any, error) {
	q := url.Values{"draft_id": {draftID}}
	var out any
	err := c.callJSONWithRefresh(ctx, "POST", "/v1/mail/drafts/delete", q, nil, &out)
	return out, err
}

func (c *GatewayClient) ListMailFolders(ctx context.Context, folderType int) (any, error) {
	q := url.Values{}
	if folderType > 0 {
		q.Set("folder_type", strconv.Itoa(folderType))
	}
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/folders", q, nil, &out)
	return out, err
}

func (c *GatewayClient) ListAccessibleMailboxes(ctx context.Context) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/accessible_mailboxes", nil, nil, &out)
	return out, err
}

func (c *GatewayClient) GetMailSendAs(ctx context.Context) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/send_as", nil, nil, &out)
	return out, err
}

func (c *GatewayClient) GetMailProfile(ctx context.Context) (any, error) {
	var out any
	err := c.callJSONWithRefresh(ctx, "GET", "/v1/mail/profile", nil, nil, &out)
	return out, err
}

func (c *GatewayClient) callJSONWithRefresh(ctx context.Context, method, path string, query url.Values, body any, out any) error {
	err := c.callJSON(ctx, method, path, query, body, true, out)
	if err == nil {
		return nil
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return err
	}
	if apiErr.Status != http.StatusUnauthorized {
		return err
	}
	if apiErr.Code != "session_expired" && apiErr.Code != "access_token_refresh_failed" {
		return err
	}

	if refreshErr := c.RefreshSession(ctx); refreshErr != nil {
		return err
	}
	return c.callJSON(ctx, method, path, query, body, true, out)
}

func (c *GatewayClient) callJSON(ctx context.Context, method, path string, query url.Values, body any, auth bool, out any) error {
	status, env, err := c.requestEnvelope(ctx, method, path, query, body, auth)
	if err != nil {
		return err
	}
	if env.Code != "ok" {
		return &APIError{Status: status, Code: env.Code, Message: env.Message}
	}
	if out == nil || len(env.Data) == 0 {
		return nil
	}
	if err := json.Unmarshal(env.Data, out); err != nil {
		return fmt.Errorf("decode response data: %w", err)
	}
	return nil
}

func (c *GatewayClient) requestEnvelope(ctx context.Context, method, path string, query url.Values, body any, auth bool) (int, apiEnvelope, error) {
	targetURL := c.baseURL + path
	if len(query) > 0 {
		targetURL += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return 0, apiEnvelope{}, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, targetURL, reqBody)
	if err != nil {
		return 0, apiEnvelope{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Client-Version", ProtocolVersion)

	if auth {
		token := strings.TrimSpace(c.session())
		if token == "" {
			return 0, apiEnvelope{}, errors.New("missing session token, run `larkctl login`")
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, apiEnvelope{}, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, apiEnvelope{}, err
	}

	var env apiEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp.StatusCode, apiEnvelope{Code: "ok", Data: json.RawMessage(raw)}, nil
		}
		return resp.StatusCode, apiEnvelope{}, &APIError{Status: resp.StatusCode, Code: "invalid_response", Message: string(raw)}
	}

	if env.Code == "" {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			env.Code = "ok"
		} else {
			env.Code = "http_error"
		}
	}

	if env.Message == "" && resp.StatusCode >= 400 {
		env.Message = http.StatusText(resp.StatusCode)
	}

	return resp.StatusCode, env, nil
}
