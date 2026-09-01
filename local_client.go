package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultFeishuBaseURL = "https://open.feishu.cn/open-apis"
	docxPath             = "/docx/v1/documents/"
)

type feishuTokenResponse struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

// LocalClient calls Feishu Open APIs directly using user OAuth tokens.
type LocalClient struct {
	appID     string
	appSecret string
	baseURL   string

	mu             sync.Mutex
	accessToken    string
	refreshToken   string
	accessExpireAt time.Time

	httpClient *http.Client
}

func NewLocalClient(appID, appSecret string) *LocalClient {
	return &LocalClient{
		appID:     appID,
		appSecret: appSecret,
		baseURL:   defaultFeishuBaseURL,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

func (c *LocalClient) SetTokens(access, refresh string, expireAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.accessToken = access
	c.refreshToken = refresh
	c.accessExpireAt = expireAt
}

func (c *LocalClient) ensureToken(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.accessToken == "" {
		return fmt.Errorf("not logged in, run `larkctl login`")
	}

	// Refresh 5 minutes before expiry
	if time.Now().Before(c.accessExpireAt.Add(-5 * time.Minute)) {
		return nil
	}

	if c.refreshToken == "" {
		return fmt.Errorf("access token expired and no refresh token, run `larkctl login`")
	}

	tok, err := c.doRefreshToken(ctx, c.refreshToken)
	if err != nil {
		return fmt.Errorf("token refresh failed: %w, run `larkctl login`", err)
	}

	c.accessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		c.refreshToken = tok.RefreshToken
	}
	c.accessExpireAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)

	// Persist refreshed tokens
	if err := SaveLocalTokens(c.accessToken, c.refreshToken, c.accessExpireAt); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to save refreshed tokens: %v\n", err)
	}
	return nil
}

func (c *LocalClient) token() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.accessToken
}

func (c *LocalClient) feishuRequest(ctx context.Context, method, path string, query url.Values, body any) (any, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return string(raw), nil
		}
		return nil, fmt.Errorf("upstream status=%d body=%s", resp.StatusCode, string(raw))
	}

	if codeVal, ok := payload["code"]; ok {
		codeNum := intFromAny(codeVal)
		if codeNum != 0 {
			msg := pickString(payload, "msg", "message")
			if msg == "" {
				msg = "unknown Feishu error"
			}
			return nil, fmt.Errorf("feishu code=%d msg=%s", codeNum, msg)
		}
	}

	if resp.StatusCode >= 400 {
		msg := pickString(payload, "msg", "message")
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("feishu HTTP %d: %s", resp.StatusCode, msg)
	}

	if data, ok := payload["data"]; ok {
		return data, nil
	}
	return payload, nil
}

func (c *LocalClient) exchangeAuthCode(ctx context.Context, code, redirectURI string) (feishuTokenResponse, error) {
	payload := map[string]any{
		"grant_type":    "authorization_code",
		"code":          code,
		"client_id":     c.appID,
		"client_secret": c.appSecret,
		"redirect_uri":  redirectURI,
	}
	data, err := c.feishuRequestNoAuth(ctx, http.MethodPost, "/authen/v2/oauth/token", nil, payload)
	if err != nil {
		return feishuTokenResponse{}, err
	}
	return parseTokenResponse(data)
}

func (c *LocalClient) doRefreshToken(ctx context.Context, refreshToken string) (feishuTokenResponse, error) {
	payload := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.appID,
		"client_secret": c.appSecret,
	}
	data, err := c.feishuRequestNoAuth(ctx, http.MethodPost, "/authen/v2/oauth/token", nil, payload)
	if err == nil {
		return parseTokenResponse(data)
	}

	// Fallback to v1 endpoint
	fallback := map[string]any{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
	}
	data, err = c.feishuRequestNoAuth(ctx, http.MethodPost, "/authen/v1/refresh_access_token", nil, fallback)
	if err != nil {
		return feishuTokenResponse{}, err
	}
	return parseTokenResponse(data)
}

// feishuRequestNoAuth makes a request without the Authorization header (for token exchange).
func (c *LocalClient) feishuRequestNoAuth(ctx context.Context, method, path string, query url.Values, body any) (any, error) {
	target := c.baseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	var reqBody io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("upstream status=%d body=%s", resp.StatusCode, string(raw))
	}

	if codeVal, ok := payload["code"]; ok {
		codeNum := intFromAny(codeVal)
		if codeNum != 0 {
			msg := pickString(payload, "msg", "message")
			return nil, fmt.Errorf("feishu code=%d msg=%s", codeNum, msg)
		}
	}

	if data, ok := payload["data"]; ok {
		return data, nil
	}
	return payload, nil
}

func (c *LocalClient) fetchUser(ctx context.Context) (UserIdentity, error) {
	data, err := c.feishuRequest(ctx, http.MethodGet, "/authen/v1/user_info", nil, nil)
	if err != nil {
		return UserIdentity{}, err
	}
	payload, ok := data.(map[string]any)
	if !ok {
		return UserIdentity{}, fmt.Errorf("unexpected user info response: %T", data)
	}
	user := UserIdentity{
		UserID:    pickString(payload, "user_id"),
		Name:      pickString(payload, "name", "en_name"),
		OpenID:    pickString(payload, "open_id"),
		UnionID:   pickString(payload, "union_id"),
		Email:     pickString(payload, "email"),
		TenantKey: pickString(payload, "tenant_key"),
	}
	if user.UserID == "" {
		user.UserID = user.OpenID
	}
	if user.Name == "" {
		user.Name = "unknown"
	}
	return user, nil
}

func parseTokenResponse(data any) (feishuTokenResponse, error) {
	payload, ok := data.(map[string]any)
	if !ok {
		return feishuTokenResponse{}, fmt.Errorf("unexpected token response: %T", data)
	}

	resp := feishuTokenResponse{
		AccessToken:  pickString(payload, "access_token", "user_access_token", "token"),
		RefreshToken: pickString(payload, "refresh_token"),
		ExpiresIn:    pickInt(payload, "expires_in", "expire"),
	}
	if resp.AccessToken == "" {
		if nested, ok := payload["data"].(map[string]any); ok {
			resp.AccessToken = pickString(nested, "access_token", "user_access_token", "token")
			if resp.RefreshToken == "" {
				resp.RefreshToken = pickString(nested, "refresh_token")
			}
			if resp.ExpiresIn == 0 {
				resp.ExpiresIn = pickInt(nested, "expires_in", "expire")
			}
		}
	}
	if resp.AccessToken == "" {
		return feishuTokenResponse{}, fmt.Errorf("no access token in response")
	}
	if resp.ExpiresIn <= 0 {
		resp.ExpiresIn = 2 * 60 * 60
	}
	return resp, nil
}

// ---- FeishuClient interface implementation ----

func (c *LocalClient) WhoAmI(ctx context.Context) (map[string]any, error) {
	user, err := c.fetchUser(ctx)
	if err != nil {
		return nil, err
	}
	b, _ := json.Marshal(user)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m, nil
}

func (c *LocalClient) GetDocumentInfo(ctx context.Context, input, docType string) (any, error) {
	token := extractToken(input)
	if docType == "wiki" || strings.Contains(input, "/wiki/") {
		return c.feishuRequest(ctx, http.MethodGet, "/wiki/v2/spaces/get_node", url.Values{"token": {token}}, nil)
	}
	return c.feishuRequest(ctx, http.MethodGet, docxPath+mustPathEscape(token), nil, nil)
}

func (c *LocalClient) GetDocumentBlocks(ctx context.Context, input string) (any, error) {
	docID := extractToken(input)
	// Resolve wiki token if needed
	if strings.Contains(input, "/wiki/") {
		resolved, err := c.resolveWikiDocToken(ctx, docID)
		if err == nil && resolved != "" {
			docID = resolved
		}
	}

	var allBlocks []any
	pageToken := ""
	for {
		query := url.Values{"page_size": {"500"}, "document_revision_id": {"-1"}}
		if pageToken != "" {
			query.Set("page_token", pageToken)
		}
		data, err := c.feishuRequest(ctx, http.MethodGet, docxPath+mustPathEscape(docID)+"/blocks", query, nil)
		if err != nil {
			return nil, err
		}
		dataMap, ok := data.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("unexpected block response")
		}
		if items, ok := dataMap["items"].([]any); ok {
			allBlocks = append(allBlocks, items...)
		}
		next, _ := dataMap["page_token"].(string)
		if strings.TrimSpace(next) == "" {
			break
		}
		pageToken = next
	}
	return map[string]any{"items": allBlocks, "total": len(allBlocks)}, nil
}

func (c *LocalClient) GetWikiNode(ctx context.Context, input string) (any, error) {
	token := extractToken(input)
	return c.feishuRequest(ctx, http.MethodGet, "/wiki/v2/spaces/get_node", url.Values{"token": {token}}, nil)
}

func (c *LocalClient) GetSheetsMeta(ctx context.Context, spreadsheetToken string) (any, error) {
	token := extractToken(spreadsheetToken)
	return c.feishuRequest(ctx, http.MethodGet, "/sheets/v3/spreadsheets/"+mustPathEscape(token), nil, nil)
}

func (c *LocalClient) GetSheetValues(ctx context.Context, spreadsheetToken, sheetID, rangeRef string, query url.Values) (any, error) {
	token := extractToken(spreadsheetToken)
	rangeStr := sheetID
	if rangeRef != "" {
		rangeStr += "!" + rangeRef
	}
	return c.feishuRequest(ctx, http.MethodGet,
		"/sheets/v2/spreadsheets/"+mustPathEscape(token)+"/values/"+mustPathEscape(rangeStr), query, nil)
}

func (c *LocalClient) GetDocumentComments(ctx context.Context, fileToken, fileType string) (any, error) {
	token := extractToken(fileToken)
	query := url.Values{"file_type": {fileType}}
	return c.feishuRequest(ctx, http.MethodGet,
		"/drive/v1/files/"+mustPathEscape(token)+"/comments", query, nil)
}

func (c *LocalClient) CreateDocumentBlocks(ctx context.Context, docID, blockID string, body any) (any, error) {
	if blockID == "" {
		blockID = docID
	}
	path := docxPath + mustPathEscape(docID) + "/blocks/" + mustPathEscape(blockID) + "/children"
	return c.feishuRequest(ctx, http.MethodPost, path, url.Values{"document_revision_id": {"-1"}}, body)
}

func (c *LocalClient) DeleteDocumentBlock(ctx context.Context, docID, blockID string) (any, error) {
	// Get block to find parent
	getPath := docxPath + mustPathEscape(docID) + "/blocks/" + mustPathEscape(blockID)
	data, err := c.feishuRequest(ctx, http.MethodGet, getPath, url.Values{"document_revision_id": {"-1"}}, nil)
	if err != nil {
		return nil, fmt.Errorf("get block: %w", err)
	}
	dataMap, _ := data.(map[string]any)
	block, _ := dataMap["block"].(map[string]any)
	parentID, _ := block["parent_id"].(string)
	if parentID == "" {
		return nil, fmt.Errorf("cannot determine parent block")
	}

	deletePath := docxPath + mustPathEscape(docID) + "/blocks/" + mustPathEscape(parentID) + "/children/batch_delete"
	return c.feishuRequest(ctx, http.MethodDelete, deletePath,
		url.Values{"document_revision_id": {"-1"}},
		map[string]any{"start_index": -1, "end_index": -1, "block_ids": []string{blockID}})
}

func (c *LocalClient) UpdateDocumentBlocks(ctx context.Context, docID string, body any) (any, error) {
	path := docxPath + mustPathEscape(docID) + "/blocks/batch_update"
	return c.feishuRequest(ctx, http.MethodPatch, path, url.Values{"document_revision_id": {"-1"}}, body)
}

func (c *LocalClient) SearchDocs(ctx context.Context, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, "/suite/docs-api/search/object", nil, body)
}

func (c *LocalClient) CreateDocument(ctx context.Context, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, docxPath[:len(docxPath)-1], nil, body)
}

func (c *LocalClient) CreateDocumentComment(ctx context.Context, fileToken, fileType string, body any) (any, error) {
	token := extractToken(fileToken)
	query := url.Values{"file_type": {fileType}}
	return c.feishuRequest(ctx, http.MethodPost,
		"/drive/v1/files/"+mustPathEscape(token)+"/comments", query, body)
}

func (c *LocalClient) ReplyDocumentComment(ctx context.Context, fileToken, commentID, fileType string, body any) (any, error) {
	token := extractToken(fileToken)
	query := url.Values{"file_type": {fileType}}
	return c.feishuRequest(ctx, http.MethodPost,
		"/drive/v1/files/"+mustPathEscape(token)+"/comments/"+mustPathEscape(commentID)+"/replies", query, body)
}

func (c *LocalClient) ResolveDocumentComment(ctx context.Context, fileToken, commentID, fileType string, resolved bool) (any, error) {
	token := extractToken(fileToken)
	query := url.Values{"file_type": {fileType}}
	body := map[string]any{"is_solved": resolved}
	return c.feishuRequest(ctx, http.MethodPatch,
		"/drive/v1/files/"+mustPathEscape(token)+"/comments/"+mustPathEscape(commentID), query, body)
}

func (c *LocalClient) ListDocumentPermissions(ctx context.Context, token, fileType string) (any, error) {
	t := extractToken(token)
	query := url.Values{"type": {fileType}}
	return c.feishuRequest(ctx, http.MethodGet,
		"/drive/v1/permissions/"+mustPathEscape(t)+"/members", query, nil)
}

func (c *LocalClient) ListDriveFiles(ctx context.Context, folderToken string) (any, error) {
	query := url.Values{}
	if folderToken != "" {
		query.Set("folder_token", extractToken(folderToken))
	}
	return c.feishuRequest(ctx, http.MethodGet, "/drive/v1/files", query, nil)
}

func (c *LocalClient) CreateFolder(ctx context.Context, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, "/drive/v1/files/create_folder", nil, body)
}

func (c *LocalClient) GetBitableMeta(ctx context.Context, appToken string) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodGet, "/bitable/v1/apps/"+mustPathEscape(token), nil, nil)
}

func (c *LocalClient) ListBitableTables(ctx context.Context, appToken string) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodGet, "/bitable/v1/apps/"+mustPathEscape(token)+"/tables", nil, nil)
}

func (c *LocalClient) ListBitableFields(ctx context.Context, appToken, tableID string) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodGet,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/fields", nil, nil)
}

func (c *LocalClient) ListBitableRecords(ctx context.Context, appToken, tableID string, extra url.Values) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodGet,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/records", extra, nil)
}

func (c *LocalClient) CreateBitableRecord(ctx context.Context, appToken, tableID string, body any) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodPost,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/records", nil, body)
}

func (c *LocalClient) UpdateBitableRecord(ctx context.Context, appToken, tableID, recordID string, body any) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodPut,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/records/"+mustPathEscape(recordID), nil, body)
}

func (c *LocalClient) CreateBitableField(ctx context.Context, appToken, tableID string, body any) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodPost,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/fields", nil, body)
}

func (c *LocalClient) UpdateBitableField(ctx context.Context, appToken, tableID, fieldID string, body any) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodPut,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/fields/"+mustPathEscape(fieldID), nil, body)
}

func (c *LocalClient) DeleteBitableField(ctx context.Context, appToken, tableID, fieldID string) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodDelete,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/fields/"+mustPathEscape(fieldID), nil, nil)
}

func (c *LocalClient) ListBitableViews(ctx context.Context, appToken, tableID string) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodGet,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/views", nil, nil)
}

func (c *LocalClient) GetBitableView(ctx context.Context, appToken, tableID, viewID string) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodGet,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/views/"+mustPathEscape(viewID), nil, nil)
}

func (c *LocalClient) CreateBitableView(ctx context.Context, appToken, tableID string, body any) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodPost,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/views", nil, body)
}

func (c *LocalClient) UpdateBitableView(ctx context.Context, appToken, tableID, viewID string, body any) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodPatch,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/views/"+mustPathEscape(viewID), nil, body)
}

func (c *LocalClient) DeleteBitableView(ctx context.Context, appToken, tableID, viewID string) (any, error) {
	token := extractToken(appToken)
	return c.feishuRequest(ctx, http.MethodDelete,
		"/bitable/v1/apps/"+mustPathEscape(token)+"/tables/"+mustPathEscape(tableID)+"/views/"+mustPathEscape(viewID), nil, nil)
}

func (c *LocalClient) UpdateSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error) {
	token := extractToken(spreadsheetToken)
	return c.feishuRequest(ctx, http.MethodPut,
		"/sheets/v2/spreadsheets/"+mustPathEscape(token)+"/values", nil, body)
}

func (c *LocalClient) AppendSheetValues(ctx context.Context, spreadsheetToken string, body any) (any, error) {
	token := extractToken(spreadsheetToken)
	return c.feishuRequest(ctx, http.MethodPost,
		"/sheets/v2/spreadsheets/"+mustPathEscape(token)+"/values_append", nil, body)
}

func (c *LocalClient) ListWikiSpaces(ctx context.Context) (any, error) {
	return c.feishuRequest(ctx, http.MethodGet, "/wiki/v2/spaces", nil, nil)
}

func (c *LocalClient) ListWikiNodes(ctx context.Context, spaceID, parentNodeToken string) (any, error) {
	query := url.Values{}
	if parentNodeToken != "" {
		query.Set("parent_node_token", parentNodeToken)
	}
	return c.feishuRequest(ctx, http.MethodGet,
		"/wiki/v2/spaces/"+mustPathEscape(spaceID)+"/nodes", query, nil)
}

func (c *LocalClient) CreateWikiNode(ctx context.Context, spaceID string, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost,
		"/wiki/v2/spaces/"+mustPathEscape(spaceID)+"/nodes", nil, body)
}

func (c *LocalClient) GetBoardNodes(ctx context.Context, whiteboardID string) (any, error) {
	token := extractToken(whiteboardID)
	return c.feishuRequest(ctx, http.MethodGet,
		"/board/v1/whiteboards/"+mustPathEscape(token)+"/nodes", nil, nil)
}

func (c *LocalClient) CreateBoardNodes(ctx context.Context, whiteboardID string, body any, clientToken string) (any, error) {
	token := extractToken(whiteboardID)
	var query url.Values
	if clientToken != "" {
		query = url.Values{"client_token": {clientToken}}
	}
	return c.feishuRequest(ctx, http.MethodPost,
		"/board/v1/whiteboards/"+mustPathEscape(token)+"/nodes", query, body)
}

func (c *LocalClient) DeleteBoardNodes(ctx context.Context, whiteboardID string, nodeIDs []string) (any, error) {
	token := extractToken(whiteboardID)
	return c.feishuRequest(ctx, http.MethodDelete,
		"/board/v1/whiteboards/"+mustPathEscape(token)+"/nodes/batch_delete", nil,
		map[string]any{"ids": nodeIDs})
}

func (c *LocalClient) CreateTask(ctx context.Context, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, "/task/v2/tasks", nil, body)
}

func (c *LocalClient) GetCalendarPrimary(ctx context.Context) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, "/calendar/v4/calendars/primary", nil, nil)
}

func (c *LocalClient) ListCalendarEvents(ctx context.Context, calendarID, startTime, endTime string) (any, error) {
	query := url.Values{}
	if startTime != "" {
		query.Set("start_time", startTime)
	}
	if endTime != "" {
		query.Set("end_time", endTime)
	}
	return c.feishuRequest(ctx, http.MethodGet, "/calendar/v4/calendars/"+url.PathEscape(calendarID)+"/events", query, nil)
}

func (c *LocalClient) CreateCalendarEvent(ctx context.Context, calendarID string, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, "/calendar/v4/calendars/"+url.PathEscape(calendarID)+"/events",
		url.Values{"user_id_type": {"user_id"}}, body)
}

func (c *LocalClient) AddCalendarAttendees(ctx context.Context, calendarID, eventID string, body any) (any, error) {
	path := "/calendar/v4/calendars/" + url.PathEscape(calendarID) + "/events/" + url.PathEscape(eventID) + "/attendees"
	return c.feishuRequest(ctx, http.MethodPost, path, url.Values{"user_id_type": {"user_id"}}, body)
}

func (c *LocalClient) GetFreebusy(ctx context.Context, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, "/calendar/v4/freebusy/list",
		url.Values{"user_id_type": {"user_id"}}, body)
}

func (c *LocalClient) ListRooms(ctx context.Context, keyword string) (any, error) {
	body := map[string]any{"page_size": 100}
	if keyword != "" {
		body = map[string]any{"keyword": keyword, "page_size": 20}
	}
	return c.feishuRequest(ctx, http.MethodPost, "/vc/v1/rooms/search", nil, body)
}

func (c *LocalClient) SearchUsers(ctx context.Context, query string, pageSize int) (any, error) {
	if pageSize <= 0 {
		pageSize = 5
	}
	return c.feishuRequest(ctx, http.MethodPost, "/search/v1/user",
		url.Values{"user_id_type": {"user_id"}, "page_size": {strconv.Itoa(pageSize)}},
		map[string]any{"query": query})
}

func (c *LocalClient) ExportCreate(ctx context.Context, token, fileType, fileExtension string) (string, error) {
	body := map[string]any{"token": token, "type": fileType, "file_extension": fileExtension}
	data, err := c.feishuRequest(ctx, http.MethodPost, "/drive/v1/export_tasks", nil, body)
	if err != nil {
		return "", err
	}
	m, _ := data.(map[string]any)
	ticket, _ := m["ticket"].(string)
	return ticket, nil
}

func (c *LocalClient) ExportStatus(ctx context.Context, ticket, token string) (exportResult, error) {
	query := url.Values{"token": {token}}
	data, err := c.feishuRequest(ctx, http.MethodGet, "/drive/v1/export_tasks/"+mustPathEscape(ticket), query, nil)
	if err != nil {
		return exportResult{}, err
	}
	m, _ := data.(map[string]any)
	result, _ := m["result"].(map[string]any)
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

func (c *LocalClient) ExportDownload(ctx context.Context, fileToken string, w io.Writer) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}
	target := c.baseURL + "/drive/v1/export_tasks/file/" + mustPathEscape(fileToken) + "/download"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token())
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

func (c *LocalClient) GetSheetsFloatImages(ctx context.Context, spreadsheetToken, sheetID string) (any, error) {
	token := extractToken(spreadsheetToken)
	return c.feishuRequest(ctx, http.MethodGet,
		"/sheets/v3/spreadsheets/"+mustPathEscape(token)+"/sheets/"+mustPathEscape(sheetID)+"/float_images/query", nil, nil)
}

func (c *LocalClient) DownloadMedia(ctx context.Context, fileToken string, w io.Writer) (string, error) {
	if err := c.ensureToken(ctx); err != nil {
		return "", err
	}

	target := c.baseURL + "/drive/v1/medias/" + mustPathEscape(fileToken) + "/download"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("download failed (status=%d): %s", resp.StatusCode, string(body))
	}

	ct := resp.Header.Get("Content-Type")
	_, err = io.Copy(w, resp.Body)
	return ct, err
}

// IM methods

func (c *LocalClient) SendMessage(ctx context.Context, body any) (any, error) {
	m, _ := body.(map[string]any)
	if m == nil {
		m = map[string]any{}
		if body != nil {
			b, _ := json.Marshal(body)
			_ = json.Unmarshal(b, &m)
		}
	}
	receiveIDType, _ := m["receive_id_type"].(string)
	if receiveIDType == "" {
		receiveIDType = "open_id"
	}
	query := url.Values{"receive_id_type": {receiveIDType}}
	return c.feishuRequest(ctx, http.MethodPost, "/im/v1/messages", query, body)
}

func (c *LocalClient) ListMessages(ctx context.Context, containerID string, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("container_id_type", "chat")
	query.Set("container_id", containerID)
	return c.feishuRequest(ctx, http.MethodGet, "/im/v1/messages", query, nil)
}

func (c *LocalClient) SearchMessages(ctx context.Context, body any, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("user_id_type", "user_id")
	return c.feishuRequest(ctx, http.MethodPost, "/im/v1/messages/search", query, body)
}

func (c *LocalClient) ReplyMessage(ctx context.Context, messageID string, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost,
		"/im/v1/messages/"+url.PathEscape(messageID)+"/reply", nil, body)
}

func (c *LocalClient) SearchChats(ctx context.Context, query string, pageSize int) (any, error) {
	q := url.Values{"user_id_type": {"user_id"}}
	if pageSize > 0 {
		q.Set("page_size", strconv.Itoa(pageSize))
	}
	return c.feishuRequest(ctx, http.MethodPost, "/im/v2/chats/search", q, map[string]any{"query": query})
}

func (c *LocalClient) MGetMessages(ctx context.Context, messageIDs []string) (any, error) {
	query := url.Values{"user_id_type": {"user_id"}}
	for _, id := range messageIDs {
		query.Add("message_id", id)
	}
	return c.feishuRequest(ctx, http.MethodGet, "/im/v1/messages/mget", query, nil)
}

func (c *LocalClient) DownloadMessageResource(ctx context.Context, messageID, fileKey, resourceType string, w io.Writer) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}
	target := c.baseURL + "/im/v1/messages/" + url.PathEscape(messageID) +
		"/resources/" + url.PathEscape(fileKey) + "?type=" + url.QueryEscape(resourceType)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token())

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

func (c *LocalClient) ListThreadMessages(ctx context.Context, threadID string, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("container_id_type", "thread")
	query.Set("container_id", threadID)
	return c.feishuRequest(ctx, http.MethodGet, "/im/v1/messages", query, nil)
}

func (c *LocalClient) ReactMessage(ctx context.Context, messageID, emojiType string) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost,
		"/im/v1/messages/"+mustPathEscape(messageID)+"/reactions", nil,
		map[string]any{"reaction_type": map[string]any{"emoji_type": emojiType}})
}

func (c *LocalClient) ListMessageReactions(ctx context.Context, messageID string, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("user_id_type", "user_id")
	return c.feishuRequest(ctx, http.MethodGet,
		"/im/v1/messages/"+mustPathEscape(messageID)+"/reactions", query, nil)
}

func (c *LocalClient) DeleteMessageReaction(ctx context.Context, messageID, reactionID string) (any, error) {
	return c.feishuRequest(ctx, http.MethodDelete,
		"/im/v1/messages/"+mustPathEscape(messageID)+"/reactions/"+mustPathEscape(reactionID), nil, nil)
}

// uploadMultipartResource posts a multipart form to a Feishu upload endpoint and
// returns the named key ("image_key" / "file_key" / "file_token") from the response.
func (c *LocalClient) uploadMultipartResource(ctx context.Context, path, keyField string, fill func(*multipart.Writer) error) (string, error) {
	if err := c.ensureToken(ctx); err != nil {
		return "", err
	}

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
	req.Header.Set("Authorization", "Bearer "+c.token())

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
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("decode upload response: %w", err)
	}
	if intFromAny(payload["code"]) != 0 {
		msg := pickString(payload, "msg", "message")
		return "", fmt.Errorf("feishu code=%d msg=%s", int(intFromAny(payload["code"])), msg)
	}
	data, _ := payload["data"].(map[string]any)
	key, _ := data[keyField].(string)
	if key == "" {
		return "", fmt.Errorf("upload returned no %s: %s", keyField, strings.TrimSpace(string(raw)))
	}
	return key, nil
}

func (c *LocalClient) UploadIMImage(ctx context.Context, fileName string, fileReader io.Reader) (string, error) {
	return c.uploadMultipartResource(ctx, "/im/v1/images", "image_key", func(mw *multipart.Writer) error {
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

// UploadDocsMedia uploads a local file as document media bound to an existing
// image or file block. The returned token is written into that block by the
// caller via a replace_image / replace_file batch update.
func (c *LocalClient) UploadDocsMedia(ctx context.Context, parentType, parentNode, fileName string, fileReader io.Reader, fileSize int64) (string, error) {
	return c.uploadMultipartResource(ctx, "/drive/v1/medias/upload_all", "file_token", func(mw *multipart.Writer) error {
		return fillDocsMediaForm(mw, parentType, parentNode, fileName, fileReader, fileSize)
	})
}

func (c *LocalClient) UploadIMFile(ctx context.Context, fileType, fileName string, fileReader io.Reader) (string, error) {
	return c.uploadMultipartResource(ctx, "/im/v1/files", "file_key", func(mw *multipart.Writer) error {
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

// Task methods (extended)

func (c *LocalClient) ListTasks(ctx context.Context, query url.Values) (any, error) {
	if query == nil {
		query = url.Values{}
	}
	query.Set("user_id_type", "user_id")
	return c.feishuRequest(ctx, http.MethodGet, "/task/v2/tasks", query, nil)
}

func (c *LocalClient) UpdateTask(ctx context.Context, taskID string, body any) (any, error) {
	m, _ := body.(map[string]any)
	if m == nil {
		m = map[string]any{}
		if body != nil {
			b, _ := json.Marshal(body)
			_ = json.Unmarshal(b, &m)
		}
	}
	// Build update_fields from keys in body
	var fields []string
	for k := range m {
		fields = append(fields, k)
	}
	wrapped := map[string]any{"task": m, "update_fields": fields}
	return c.feishuRequest(ctx, http.MethodPatch,
		"/task/v2/tasks/"+url.PathEscape(taskID),
		url.Values{"user_id_type": {"user_id"}}, wrapped)
}

func (c *LocalClient) CompleteTask(ctx context.Context, taskID string, completed bool) (any, error) {
	var completedAt string
	if completed {
		completedAt = strconv.FormatInt(time.Now().Unix(), 10)
	}
	body := map[string]any{"completed_at": completedAt}
	return c.feishuRequest(ctx, http.MethodPatch,
		"/task/v2/tasks/"+url.PathEscape(taskID), nil, body)
}

func (c *LocalClient) CommentTask(ctx context.Context, taskID, content string) (any, error) {
	body := map[string]any{
		"content":       content,
		"resource_type": "task",
		"resource_id":   taskID,
	}
	return c.feishuRequest(ctx, http.MethodPost, "/task/v2/comments", nil, body)
}

func (c *LocalClient) ManageTaskMembers(ctx context.Context, taskID, action string, members []map[string]any) (any, error) {
	var apiPath string
	if action == "add" {
		apiPath = "/task/v2/tasks/" + url.PathEscape(taskID) + "/add_members"
	} else {
		apiPath = "/task/v2/tasks/" + url.PathEscape(taskID) + "/remove_members"
	}
	query := url.Values{"user_id_type": {"user_id"}}
	return c.feishuRequest(ctx, http.MethodPost, apiPath, query, map[string]any{"members": members})
}

func (c *LocalClient) ManageTaskReminders(ctx context.Context, taskID, action string, reminders []map[string]any) (any, error) {
	var apiPath string
	if action == "add" {
		apiPath = "/task/v2/tasks/" + url.PathEscape(taskID) + "/add_reminders"
	} else {
		apiPath = "/task/v2/tasks/" + url.PathEscape(taskID) + "/remove_reminders"
	}
	return c.feishuRequest(ctx, http.MethodPost, apiPath, nil, map[string]any{"reminders": reminders})
}

func (c *LocalClient) CreateTasklist(ctx context.Context, name string) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, "/task/v2/tasklists", nil, map[string]any{"name": name})
}

func (c *LocalClient) AddTaskToTasklist(ctx context.Context, taskID, tasklistID string) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost,
		"/task/v2/tasks/"+url.PathEscape(taskID)+"/add_tasklist", nil,
		map[string]any{"tasklist": map[string]any{"id": tasklistID}})
}

func (c *LocalClient) ManageTasklistMembers(ctx context.Context, tasklistID, action string, members []map[string]any) (any, error) {
	var apiPath string
	if action == "add" {
		apiPath = "/task/v2/tasklists/" + url.PathEscape(tasklistID) + "/add_members"
	} else {
		apiPath = "/task/v2/tasklists/" + url.PathEscape(tasklistID) + "/remove_members"
	}
	query := url.Values{"user_id_type": {"user_id"}}
	return c.feishuRequest(ctx, http.MethodPost, apiPath, query, map[string]any{"members": members})
}

// Drive methods (extended)

func (c *LocalClient) UploadFile(ctx context.Context, parentToken, fileName string, fileReader io.Reader, fileSize int64) (any, error) {
	if err := c.ensureToken(ctx); err != nil {
		return nil, err
	}

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

	target := c.baseURL + "/drive/v1/files/upload_all"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &buf)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token())

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
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode upload response: %w", err)
	}
	if intFromAny(payload["code"]) != 0 {
		msg := pickString(payload, "msg", "message")
		return nil, fmt.Errorf("feishu code=%d msg=%s", int(intFromAny(payload["code"])), msg)
	}
	if data, ok := payload["data"]; ok {
		return data, nil
	}
	return payload, nil
}

func (c *LocalClient) ImportUpload(ctx context.Context, fileName, docType, fileExt string, fileReader io.Reader, fileSize int64) (string, error) {
	if err := c.ensureToken(ctx); err != nil {
		return "", err
	}

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

	target := c.baseURL + "/drive/v1/medias/upload_all"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.token())

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
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("decode import upload response: %w", err)
	}
	if intFromAny(payload["code"]) != 0 {
		msg := pickString(payload, "msg", "message")
		return "", fmt.Errorf("feishu code=%d msg=%s", int(intFromAny(payload["code"])), msg)
	}
	data, _ := payload["data"].(map[string]any)
	fileToken, _ := data["file_token"].(string)
	if fileToken == "" {
		// Otherwise ImportCreate is called with an empty token and fails there instead.
		return "", fmt.Errorf("import upload returned no file_token: %s", strings.TrimSpace(string(raw)))
	}
	return fileToken, nil
}

func (c *LocalClient) ImportCreate(ctx context.Context, fileToken, fileType, fileExt, fileName, folderToken string) (string, error) {
	body := map[string]any{
		"file_extension": fileExt,
		"file_token":     fileToken,
		"type":           fileType,
		"file_name":      fileName,
		"point": map[string]any{
			"mount_type": 1,
			"mount_key":  folderToken,
		},
	}
	data, err := c.feishuRequest(ctx, http.MethodPost, "/drive/v1/import_tasks", nil, body)
	if err != nil {
		return "", err
	}
	m, _ := data.(map[string]any)
	ticket, _ := m["ticket"].(string)
	return ticket, nil
}

func (c *LocalClient) ImportStatus(ctx context.Context, ticket string) (any, error) {
	return c.feishuRequest(ctx, http.MethodGet, "/drive/v1/import_tasks/"+mustPathEscape(ticket), nil, nil)
}

func (c *LocalClient) DownloadFile(ctx context.Context, fileToken string, w io.Writer) error {
	if err := c.ensureToken(ctx); err != nil {
		return err
	}
	target := c.baseURL + "/drive/v1/files/" + url.PathEscape(fileToken) + "/download"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token())

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

func (c *LocalClient) RSVPCalendarEvent(ctx context.Context, calendarID, eventID, rsvpStatus string) (any, error) {
	path := "/calendar/v4/calendars/" + url.PathEscape(calendarID) +
		"/events/" + url.PathEscape(eventID) + "/reply"
	return c.feishuRequest(ctx, http.MethodPost, path, nil, map[string]any{"rsvp_status": rsvpStatus})
}

// Mail methods. user_mailbox_id is always "me" — gateway/local share the same
// caller (cobra commands) which never need cross-mailbox access.

const mailBase = "/mail/v1/user_mailboxes/me"

func (c *LocalClient) ListMailMessages(ctx context.Context, query url.Values) (any, error) {
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/messages", query, nil)
}

func (c *LocalClient) GetMailMessage(ctx context.Context, messageID, format string) (any, error) {
	var q url.Values
	if format != "" {
		q = url.Values{"format": {format}}
	}
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/messages/"+url.PathEscape(messageID), q, nil)
}

func (c *LocalClient) BatchGetMailMessages(ctx context.Context, body any) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, mailBase+"/messages/batch_get", nil, body)
}

func (c *LocalClient) GetMailThread(ctx context.Context, threadID, format string, includeSpamTrash bool) (any, error) {
	q := url.Values{}
	if format != "" {
		q.Set("format", format)
	}
	if includeSpamTrash {
		q.Set("include_spam_trash", "true")
	}
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/threads/"+url.PathEscape(threadID), q, nil)
}

func (c *LocalClient) SearchMail(ctx context.Context, body any, query url.Values) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, mailBase+"/search", query, body)
}

func (c *LocalClient) TrashMailMessage(ctx context.Context, messageID string) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, mailBase+"/messages/"+url.PathEscape(messageID)+"/trash", nil, nil)
}

func (c *LocalClient) GetMailSendStatus(ctx context.Context, messageID string) (any, error) {
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/messages/"+url.PathEscape(messageID)+"/send_status", nil, nil)
}

func (c *LocalClient) GetMailAttachmentDownloadURL(ctx context.Context, messageID string, attachmentIDs []string) (any, error) {
	q := url.Values{}
	for _, id := range attachmentIDs {
		q.Add("attachment_ids", id)
	}
	return c.feishuRequest(ctx, http.MethodGet,
		mailBase+"/messages/"+url.PathEscape(messageID)+"/attachments/download_url", q, nil)
}

func (c *LocalClient) CreateMailDraft(ctx context.Context, rawEML string) (any, error) {
	return c.feishuRequest(ctx, http.MethodPost, mailBase+"/drafts", nil, map[string]any{"raw": rawEML})
}

func (c *LocalClient) UpdateMailDraft(ctx context.Context, draftID, rawEML string) (any, error) {
	return c.feishuRequest(ctx, http.MethodPut, mailBase+"/drafts/"+url.PathEscape(draftID), nil, map[string]any{"raw": rawEML})
}

func (c *LocalClient) SendMailDraft(ctx context.Context, draftID, sendTime string) (any, error) {
	var body map[string]any
	if sendTime != "" {
		body = map[string]any{"send_time": sendTime}
	}
	return c.feishuRequest(ctx, http.MethodPost, mailBase+"/drafts/"+url.PathEscape(draftID)+"/send", nil, body)
}

func (c *LocalClient) DeleteMailDraft(ctx context.Context, draftID string) (any, error) {
	return c.feishuRequest(ctx, http.MethodDelete, mailBase+"/drafts/"+url.PathEscape(draftID), nil, nil)
}

func (c *LocalClient) ListMailFolders(ctx context.Context, folderType int) (any, error) {
	q := url.Values{}
	if folderType > 0 {
		q.Set("folder_type", strconv.Itoa(folderType))
	}
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/folders", q, nil)
}

func (c *LocalClient) ListAccessibleMailboxes(ctx context.Context) (any, error) {
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/accessible_mailboxes", nil, nil)
}

func (c *LocalClient) GetMailSendAs(ctx context.Context) (any, error) {
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/settings/send_as", nil, nil)
}

func (c *LocalClient) GetMailProfile(ctx context.Context) (any, error) {
	return c.feishuRequest(ctx, http.MethodGet, mailBase+"/profile", nil, nil)
}

func (c *LocalClient) resolveWikiDocToken(ctx context.Context, wikiToken string) (string, error) {
	data, err := c.feishuRequest(ctx, http.MethodGet, "/wiki/v2/spaces/get_node",
		url.Values{"token": {wikiToken}}, nil)
	if err != nil {
		return "", err
	}
	node, ok := data.(map[string]any)
	if !ok {
		return "", fmt.Errorf("unexpected wiki node response")
	}
	if n, ok := node["node"].(map[string]any); ok {
		if objToken, ok := n["obj_token"].(string); ok && objToken != "" {
			return objToken, nil
		}
	}
	return "", fmt.Errorf("could not resolve wiki token")
}
