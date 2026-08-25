package main

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

// version is injected at build time via -ldflags "-X main.version=<tag>".
// Builds without the ldflag (e.g. `go install ...@latest`) fall back to the
// module version Go stamps into the binary.
var version = "dev"

func effectiveVersion() string {
	if version != "dev" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return version
}

var (
	compactOutput bool
	gatewayURL    string
)

const (
	defaultGatewayURL = "http://127.0.0.1:8787"
)

// baseScopes are always included in every login.
const baseScopes = "offline_access base:app:read drive:file.meta.sec_label.read_only"

// scopeGroups maps login subcommand names to their OAuth scopes.
// Each group's scopes are merged with baseScopes at login time.
var scopeGroups = map[string]struct {
	Description string
	Scopes      string
}{
	"docs": {
		Description: "Documents, comments, permissions, search, and drive",
		Scopes: "docx:document:readonly docx:document docx:document:create docx:document.block:convert " +
			"docs:doc docs:doc:readonly " +
			"docs:document.comment:create docs:document.comment:read docs:document.comment:update docs:document.comment:write_only " +
			"docs:document.content:read docs:document.media:download docs:document.media:upload " +
			"docs:document.subscription docs:document.subscription:read " +
			"docs:document:copy docs:document:export docs:document:import " +
			"docs:permission.member docs:permission.member:auth docs:permission.member:create docs:permission.member:delete " +
			"docs:permission.member:readonly docs:permission.member:retrieve docs:permission.member:transfer docs:permission.member:update " +
			"docs:permission.setting docs:permission.setting:read docs:permission.setting:readonly docs:permission.setting:write_only " +
			"drive:drive drive:drive.metadata:readonly drive:drive.search:readonly drive:drive:readonly drive:drive:version drive:drive:version:readonly " +
			"drive:export:readonly drive:file drive:file.like:readonly drive:file:download drive:file:readonly drive:file:upload drive:file:view_record:readonly " +
			"space:document:retrieve space:folder:create " +
			"search:docs:read",
	},
	"wiki": {
		Description: "Wiki spaces and nodes",
		Scopes: "wiki:member:create wiki:member:retrieve wiki:member:update " +
			"wiki:node:copy wiki:node:create wiki:node:move wiki:node:read wiki:node:retrieve wiki:node:update " +
			"wiki:setting:read wiki:setting:write_only " +
			"wiki:space:read wiki:space:retrieve wiki:space:write_only " +
			"wiki:wiki wiki:wiki:readonly",
	},
	"sheets": {
		Description: "Spreadsheets read and write",
		Scopes:      "sheets:spreadsheet sheets:spreadsheet:readonly",
	},
	"bitable": {
		Description: "Multi-dimensional tables",
		Scopes:      "bitable:app bitable:app:readonly",
	},
	"im": {
		Description: "Messages and chats",
		Scopes: "im:chat im:chat:read im:chat:readonly " +
			"im:message im:message:readonly " +
			"im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user " +
			"im:resource " +
			"search:message " +
			"contact:user.base:readonly contact:user.basic_profile:readonly",
	},
	"board": {
		Description: "Whiteboards",
		Scopes:      "board:whiteboard:node:create board:whiteboard:node:read",
	},
	"calendar": {
		Description: "Calendar events",
		Scopes:      "calendar:calendar",
	},
	"contact": {
		Description: "Contacts and user info",
		Scopes:      "contact:contact.base:readonly contact:user.employee_id:readonly",
	},
	"task": {
		Description: "Task management",
		Scopes:      "task:task:write task:comment:write",
	},
	"mail": {
		Description: "Mailbox: read, send, drafts, folders, search",
		Scopes: "mail:user_mailbox:readonly mail:user_mailbox.folder:read " +
			"mail:user_mailbox.message:readonly mail:user_mailbox.message:modify mail:user_mailbox.message:send " +
			"mail:user_mailbox.message.subject:read mail:user_mailbox.message.address:read mail:user_mailbox.message.body:read",
	},
}

// buildScopes merges baseScopes with the specified groups.
// If groups is empty, returns empty string (use gateway default).
func buildScopes(groups ...string) string {
	seen := map[string]bool{}
	var parts []string
	for _, s := range strings.Fields(baseScopes) {
		if !seen[s] {
			seen[s] = true
			parts = append(parts, s)
		}
	}
	for _, g := range groups {
		if sg, ok := scopeGroups[g]; ok {
			for _, s := range strings.Fields(sg.Scopes) {
				if !seen[s] {
					seen[s] = true
					parts = append(parts, s)
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

// allScopeGroups returns all group names sorted.
func allScopeGroupNames() []string {
	names := make([]string, 0, len(scopeGroups))
	for k := range scopeGroups {
		names = append(names, k)
	}
	// simple sort
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[i] > names[j] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:     "larkctl",
		Short:   "Enterprise Feishu CLI with per-user auth gateway",
		Version: effectiveVersion(),
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return resolveGatewayURL(cmd)
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&compactOutput, "compact", "c", false, "output compact JSON")
	rootCmd.PersistentFlags().StringVar(&gatewayURL, "gateway-url", "", "auth gateway base URL (default: config/env/local)")
	rootCmd.PersistentFlags().StringVar(&outputFormat, "format", "json", "output format: json, table, csv")

	rootCmd.AddCommand(
		newInitCmd(),
		newLoginCmd(),
		newLogoutCmd(),
		newWhoAmICmd(),
		newImagesCmd(),
		newDocsCmd(),
		newWikiCmd(),
		newSheetsCmd(),
		newDriveCmd(),
		newBitableCmd(),
		newTasksCmd(),
		newCalendarCmd(),
		newBoardCmd(),
		newIMCmd(),
		newAgentCmd(),
		newMailCmd(),
		newMCPCmd(),
		newUpgradeCmd(),
		newSchemaCmd(rootCmd),
	)

	return rootCmd
}

func newInitCmd() *cobra.Command {
	var appID, appSecret string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Configure local mode with Feishu app credentials",
		Long: `Configure larkctl to work directly with Feishu APIs (without a gateway server).

You need a Feishu Open Platform app with OAuth credentials.
Register redirect_uri http://127.0.0.1:19876/callback in your app settings.

Credentials are saved to ~/.lark/config.json.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if appID == "" {
				appID = strings.TrimSpace(os.Getenv("FEISHU_APP_ID"))
			}
			if appSecret == "" {
				appSecret = strings.TrimSpace(os.Getenv("FEISHU_APP_SECRET"))
			}
			if appID == "" || appSecret == "" {
				return fmt.Errorf("--app-id and --app-secret are required (or set FEISHU_APP_ID / FEISHU_APP_SECRET)")
			}

			if err := SaveLocalApp(appID, appSecret); err != nil {
				return fmt.Errorf("save app credentials: %w", err)
			}
			fmt.Println("Local mode configured.")
			fmt.Printf("  App ID: %s\n", appID)
			fmt.Println("Run `larkctl login` to authenticate.")
			return nil
		},
	}
	cmd.Flags().StringVar(&appID, "app-id", "", "Feishu app ID")
	cmd.Flags().StringVar(&appSecret, "app-secret", "", "Feishu app secret")
	return cmd
}

func newLoginCmd() *cobra.Command {
	var timeout time.Duration
	var openBrowser bool

	// Build the long description with available groups
	var groupHelp strings.Builder
	groupHelp.WriteString("Login via device code flow.\n\nAvailable scope groups:\n")
	for _, name := range allScopeGroupNames() {
		g := scopeGroups[name]
		groupHelp.WriteString(fmt.Sprintf("  %-12s %s\n", name, g.Description))
	}
	groupHelp.WriteString(fmt.Sprintf("  %-12s All scope groups combined\n", "all"))
	groupHelp.WriteString("\nExamples:\n")
	groupHelp.WriteString("  larkctl login              # default scopes (docs, wiki, sheets, bitable, etc.)\n")
	groupHelp.WriteString("  larkctl login docs          # base + all document scopes\n")
	groupHelp.WriteString("  larkctl login docs wiki     # base + document + wiki scopes\n")
	groupHelp.WriteString("  larkctl login all           # all available scopes\n")

	cmd := &cobra.Command{
		Use:   "login [scope_groups...]",
		Short: "Login to Feishu (local OAuth or gateway device flow)",
		Long:  groupHelp.String(),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			// Determine scopes based on arguments
			var scopes string
			if len(args) > 0 {
				groups := args
				if len(groups) == 1 && groups[0] == "all" {
					groups = allScopeGroupNames()
				} else {
					for _, g := range groups {
						if _, ok := scopeGroups[g]; !ok {
							return fmt.Errorf("unknown scope group %q, run 'larkctl login --help' to see available groups", g)
						}
					}
				}
				scopes = buildScopes(groups...)
				fmt.Fprintf(os.Stderr, "scope groups: %s\n", strings.Join(groups, ", "))
			}

			// Local mode: OAuth authorization code flow
			if IsLocalMode() {
				appID, appSecret, err := LoadLocalApp()
				if err != nil {
					return err
				}
				if scopes == "" {
					scopes = buildDefaultScopes()
				}
				client := NewLocalClient(appID, appSecret)
				return localLogin(ctx, client, scopes, openBrowser)
			}

			// Gateway mode: device code flow
			client := NewGatewayClient(gatewayURL)

			ver, err := client.CheckVersion(ctx)
			if err != nil {
				return fmt.Errorf("gateway version check: %w", err)
			}
			fmt.Fprintf(os.Stderr, "gateway: protocol=%s max_security_level=L%d\n", ver.ProtocolVersion, ver.MaxSecurityLevel)

			// Reuse existing token so MCP config doesn't need to change after re-login
			existingToken, _ := LoadSessionToken(gatewayURL)
			start, err := client.StartDeviceLogin(ctx, scopes, existingToken)
			if err != nil {
				return err
			}

			fmt.Printf("Open this URL to authorize: %s\n", start.VerificationURIComplete)
			fmt.Printf("User code: %s\n", start.UserCode)

			if openBrowser {
				_ = tryOpenBrowser(start.VerificationURIComplete)
			}

			deadline := time.Now().Add(timeout)
			interval := time.Duration(start.IntervalSeconds) * time.Second
			if interval <= 0 {
				interval = 3 * time.Second
			}

			for {
				if time.Now().After(deadline) {
					return fmt.Errorf("login timeout after %s", timeout)
				}

				poll, err := client.PollDeviceLogin(ctx, start.DeviceCode)
				if err == nil {
					if poll.Pending {
						// The gateway answers slow_down with a longer wait; ignoring it
						// keeps hammering at the rate it just rejected.
						wait := interval
						if poll.RecommendedWaitMS > 0 {
							wait = time.Duration(poll.RecommendedWaitMS) * time.Millisecond
						}
						time.Sleep(wait)
						continue
					}

					if poll.ClientToken == "" {
						return errors.New("authorization completed but client token is empty")
					}
					if err := SaveSessionToken(gatewayURL, poll.ClientToken); err != nil {
						return fmt.Errorf("save session token: %w", err)
					}
					if err := SaveGatewayURL(gatewayURL); err != nil {
						return fmt.Errorf("save gateway url: %w", err)
					}

					printJSON(poll)
					return nil
				}

				var apiErr *APIError
				if errors.As(err, &apiErr) {
					switch apiErr.Code {
					case "authorization_pending", "slow_down":
						time.Sleep(interval)
						continue
					case "expired_token", "access_denied":
						return err
					}
				}

				return err
			}
		},
	}

	cmd.Flags().DurationVar(&timeout, "timeout", 5*time.Minute, "maximum time waiting for authorization")
	cmd.Flags().BoolVar(&openBrowser, "open-browser", true, "open browser automatically")
	return cmd
}

func newLogoutCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Logout and clear local session",
		RunE: func(cmd *cobra.Command, args []string) error {
			if IsLocalMode() {
				if err := DeleteLocalTokens(); err != nil {
					return err
				}
				fmt.Println("logged out (local mode)")
				return nil
			}

			client := NewGatewayClient(gatewayURL)
			token, err := LoadSessionToken(gatewayURL)
			if err == nil {
				client.SetSessionToken(token)
				_ = client.Logout(cmd.Context())
			}

			if err := DeleteSessionToken(gatewayURL); err != nil {
				return fmt.Errorf("delete local session token: %w", err)
			}
			fmt.Println("logged out")
			return nil
		},
	}
}

func newWhoAmICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show current authenticated user",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.WhoAmI(cmd.Context())
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
}

func newImagesCmd() *cobra.Command {
	var outputDir string
	cmd := &cobra.Command{
		Use:   "images [url]",
		Short: "Download images from a document, wiki, or spreadsheet",
		Long: `Download all images from a Feishu URL.

Supports:
  - Document: https://xxx.feishu.cn/docx/TOKEN
  - Wiki:     https://xxx.feishu.cn/wiki/TOKEN
  - Sheet:    https://xxx.feishu.cn/wiki/TOKEN?sheet=SHEET_ID
              https://xxx.feishu.cn/sheets/TOKEN?sheet=SHEET_ID

Examples:
  larkctl images https://xxx.feishu.cn/wiki/TOKEN
  larkctl images https://xxx.feishu.cn/wiki/TOKEN?sheet=SHEET_ID -o ./out`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}

			rawURL := args[0]
			dir := outputDir
			if dir == "" {
				dir = "."
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}

			// Parse URL to detect sheet parameter
			u, _ := url.Parse(rawURL)
			sheetID := ""
			if u != nil {
				sheetID = u.Query().Get("sheet")
			}

			if sheetID != "" {
				return downloadSheetImages(cmd.Context(), client, rawURL, sheetID, dir)
			}
			return downloadDocImages(cmd.Context(), client, rawURL, dir)
		},
	}
	cmd.Flags().StringVarP(&outputDir, "output-dir", "o", ".", "output directory for images")
	return cmd
}

type imageEntry struct {
	token  string
	name   string
	width  int
	height int
}

func downloadDocImages(ctx context.Context, client FeishuClient, input, dir string) error {
	var blocksResp struct {
		Items []struct {
			BlockType int    `json:"block_type"`
			BlockID   string `json:"block_id"`
			Image     *struct {
				Token  string `json:"token"`
				Width  int    `json:"width"`
				Height int    `json:"height"`
			} `json:"image,omitempty"`
		} `json:"items"`
	}
	raw, err := client.GetDocumentBlocks(ctx, input)
	if err != nil {
		return err
	}
	b, _ := json.Marshal(raw)
	if err := json.Unmarshal(b, &blocksResp); err != nil {
		return fmt.Errorf("parse blocks: %w", err)
	}

	var images []imageEntry
	for _, item := range blocksResp.Items {
		if item.BlockType == 27 && item.Image != nil && item.Image.Token != "" {
			images = append(images, imageEntry{
				token:  item.Image.Token,
				name:   item.BlockID,
				width:  item.Image.Width,
				height: item.Image.Height,
			})
		}
	}

	if len(images) == 0 {
		fmt.Println("No images found in document.")
		return nil
	}
	return downloadImages(ctx, client, images, dir)
}

func downloadSheetImages(ctx context.Context, client FeishuClient, input, sheetID, dir string) error {
	spreadsheetToken := extractToken(input)

	// For wiki URLs, resolve the actual spreadsheet token
	if strings.Contains(input, "/wiki/") {
		wikiData, err := client.GetWikiNode(ctx, input)
		if err == nil {
			if m, ok := wikiData.(map[string]any); ok {
				if node, ok := m["node"].(map[string]any); ok {
					if objToken, ok := node["obj_token"].(string); ok && objToken != "" {
						spreadsheetToken = objToken
					}
				}
			}
		}
	}

	// Count embed-images (not downloadable via API)
	embedCount := 0
	valData, err := client.GetSheetValues(ctx, spreadsheetToken, sheetID, "A1:ZZ999", nil)
	if err == nil {
		embedCount = countEmbedImages(valData)
	}

	raw, err := client.GetSheetsFloatImages(ctx, spreadsheetToken, sheetID)
	if err != nil {
		return fmt.Errorf("get float images: %w", err)
	}

	b, _ := json.Marshal(raw)
	var resp struct {
		Items []struct {
			FloatImageID    string  `json:"float_image_id"`
			FloatImageToken string  `json:"float_image_token"`
			Range           string  `json:"range"`
			Width           float64 `json:"width"`
			Height          float64 `json:"height"`
		} `json:"items"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return fmt.Errorf("parse float images: %w", err)
	}

	var images []imageEntry
	for _, item := range resp.Items {
		if item.FloatImageToken == "" {
			continue
		}
		images = append(images, imageEntry{
			token:  item.FloatImageToken,
			name:   item.FloatImageID,
			width:  int(item.Width),
			height: int(item.Height),
		})
	}

	if len(images) == 0 && embedCount == 0 {
		fmt.Println("No images found in sheet.")
		return nil
	}

	// Export xlsx to extract embed-images
	if embedCount > 0 {
		fmt.Printf("Found %d cell-embedded image(s), exporting xlsx to extract...\n", embedCount)
		extracted, err := extractImagesFromExport(ctx, client, spreadsheetToken, dir)
		if err != nil {
			fmt.Printf("Warning: xlsx export failed: %v\n", err)
		} else if extracted > 0 {
			fmt.Printf("Extracted %d image(s) from xlsx export.\n", extracted)
		}
	}

	if len(images) == 0 {
		return nil
	}
	fmt.Printf("Downloading %d float image(s)...\n", len(images))
	return downloadImages(ctx, client, images, dir)
}

func countEmbedImages(data any) int {
	b, _ := json.Marshal(data)
	var resp struct {
		ValueRange struct {
			Values [][]any `json:"values"`
		} `json:"valueRange"`
	}
	if json.Unmarshal(b, &resp) != nil {
		return 0
	}
	count := 0
	for _, row := range resp.ValueRange.Values {
		for _, cell := range row {
			cellBytes, _ := json.Marshal(cell)
			var items []map[string]any
			if json.Unmarshal(cellBytes, &items) == nil {
				for _, item := range items {
					if t, ok := item["type"].(string); ok && t == "embed-image" {
						count++
					}
				}
			}
		}
	}
	return count
}

func downloadImages(ctx context.Context, client FeishuClient, images []imageEntry, dir string) error {
	fmt.Printf("Found %d image(s), downloading...\n", len(images))

	for i, img := range images {
		ext := ".png"
		filename := fmt.Sprintf("%03d_%s%s", i+1, img.token, ext)
		filepath := dir + "/" + filename

		f, err := os.Create(filepath)
		if err != nil {
			return fmt.Errorf("create file %s: %w", filepath, err)
		}

		ct, err := client.DownloadMedia(ctx, img.token, f)
		f.Close()
		if err != nil {
			fmt.Printf("  [%d/%d] FAILED %s: %v\n", i+1, len(images), img.token, err)
			os.Remove(filepath)
			continue
		}

		// Rename with correct extension based on content-type
		if strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
			ext = ".jpg"
		} else if strings.Contains(ct, "gif") {
			ext = ".gif"
		} else if strings.Contains(ct, "webp") {
			ext = ".webp"
		} else if strings.Contains(ct, "svg") {
			ext = ".svg"
		}
		newFilename := fmt.Sprintf("%03d_%s%s", i+1, img.token, ext)
		newFilepath := dir + "/" + newFilename
		if newFilepath != filepath {
			if err := os.Rename(filepath, newFilepath); err != nil {
				fmt.Fprintf(os.Stderr, "warning: rename %s: %v\n", filepath, err)
			}
			filepath = newFilepath
		}

		sizeStr := ""
		if img.width > 0 && img.height > 0 {
			sizeStr = fmt.Sprintf(" (%dx%d)", img.width, img.height)
		}
		fmt.Printf("  [%d/%d] %s%s\n", i+1, len(images), filepath, sizeStr)
	}
	return nil
}

func extractImagesFromExport(ctx context.Context, client FeishuClient, spreadsheetToken, dir string) (int, error) {
	// 1. Create export task
	ticket, err := client.ExportCreate(ctx, spreadsheetToken, "sheet", "xlsx")
	if err != nil {
		return 0, fmt.Errorf("create export: %w", err)
	}
	if ticket == "" {
		return 0, fmt.Errorf("empty export ticket")
	}

	// 2. Poll until done (max 60s)
	var result exportResult
	deadline := time.Now().Add(60 * time.Second)
	for {
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("export timeout after 60s")
		}
		time.Sleep(2 * time.Second)

		result, err = client.ExportStatus(ctx, ticket, spreadsheetToken)
		if err != nil {
			return 0, fmt.Errorf("poll export: %w", err)
		}
		switch result.JobStatus {
		case 0: // success
			goto done
		case 1, 2: // initializing, processing
			continue
		default:
			msg := result.ErrMsg
			if msg == "" {
				msg = fmt.Sprintf("job_status=%d", result.JobStatus)
			}
			return 0, fmt.Errorf("export failed: %s", msg)
		}
	}
done:

	if result.FileToken == "" {
		return 0, fmt.Errorf("export succeeded but no file_token")
	}

	// 3. Download xlsx to temp file
	tmpFile, err := os.CreateTemp("", "larkctl-export-*.xlsx")
	if err != nil {
		return 0, err
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	if err := client.ExportDownload(ctx, result.FileToken, tmpFile); err != nil {
		tmpFile.Close()
		return 0, fmt.Errorf("download export: %w", err)
	}
	tmpFile.Close()

	// 4. Extract images from xlsx (it's a zip)
	zr, err := zip.OpenReader(tmpPath)
	if err != nil {
		return 0, fmt.Errorf("open xlsx: %w", err)
	}
	defer zr.Close()

	count := 0
	for _, f := range zr.File {
		// xlsx stores images in xl/media/
		if !strings.HasPrefix(f.Name, "xl/media/") {
			continue
		}
		name := filepath.Base(f.Name)
		outPath := filepath.Join(dir, name)

		rc, err := f.Open()
		if err != nil {
			continue
		}
		out, err := os.Create(outPath)
		if err != nil {
			rc.Close()
			continue
		}
		_, copyErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if copyErr != nil {
			os.Remove(outPath)
			continue
		}
		count++
		fmt.Printf("  [xlsx] %s\n", outPath)
	}
	return count, nil
}

func newDocsCmd() *cobra.Command {
	docsCmd := &cobra.Command{
		Use:   "docs",
		Short: "Feishu document commands",
	}

	var documentType string
	infoCmd := &cobra.Command{
		Use:   "info [document_id_or_url]",
		Short: "Get document or wiki info",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetDocumentInfo(cmd.Context(), args[0], documentType)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	infoCmd.Flags().StringVar(&documentType, "type", "auto", "document type: auto|document|wiki")

	blocksCmd := &cobra.Command{
		Use:   "blocks [document_id_or_url]",
		Short: "Get all blocks of a document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetDocumentBlocks(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	var createBlockID string
	createBlocksCmd := &cobra.Command{
		Use:   "create-blocks [document_id_or_url] [json_file_or_-]",
		Short: "Create blocks in a document (reads JSON from file or stdin)",
		Long: `Append children blocks to the specified block (default: document root).
The JSON body is the Feishu create-blocks request body, e.g.:
  {"children":[{"block_type":2,"text":{"elements":[{"text_run":{"content":"Hello"}}]}}],"index":-1}`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}

			var body any
			if err := readJSONInput(args, 1, &body); err != nil {
				return err
			}

			data, err := client.CreateDocumentBlocks(cmd.Context(), args[0], createBlockID, body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	createBlocksCmd.Flags().StringVar(&createBlockID, "block-id", "", "parent block ID (default: document root)")

	deleteBlockCmd := &cobra.Command{
		Use:   "delete-block [document_id_or_url] [block_id]",
		Short: "Delete a block from a document",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.DeleteDocumentBlock(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	var commentFileType string
	commentsCmd := &cobra.Command{
		Use:   "comments [document_id_or_url]",
		Short: "Get all comments on a document",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "contact:user.employee_id:readonly"); err != nil {
				return err
			}
			data, err := client.GetDocumentComments(cmd.Context(), args[0], commentFileType)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	commentsCmd.Flags().StringVar(&commentFileType, "file-type", "docx", "file type: doc|docx|sheet|bitable|file")

	searchCmd := &cobra.Command{
		Use:   "search [query]",
		Short: "Search documents by keyword",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{"search_key": args[0], "count": 20}
			data, err := client.SearchDocs(cmd.Context(), body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	createCmd := &cobra.Command{
		Use:   "create [title]",
		Short: "Create a new empty document",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{}
			if len(args) > 0 {
				body["title"] = args[0]
			}
			if v, _ := cmd.Flags().GetString("folder-token"); v != "" {
				body["folder_token"] = v
			}
			data, err := client.CreateDocument(cmd.Context(), body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	createCmd.Flags().String("folder-token", "", "parent folder token")

	var addCommentFileType string
	addCommentCmd := &cobra.Command{
		Use:   "add-comment [document_id_or_url] [text]",
		Short: "Add a comment to a document",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{
				"reply_list": map[string]any{
					"replies": []any{
						map[string]any{
							"content": map[string]any{
								"elements": []any{
									map[string]any{
										"type":     "text_run",
										"text_run": map[string]any{"text": args[1]},
									},
								},
							},
						},
					},
				},
			}
			data, err := client.CreateDocumentComment(cmd.Context(), args[0], addCommentFileType, body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	addCommentCmd.Flags().StringVar(&addCommentFileType, "file-type", "docx", "file type: doc|docx|sheet|bitable|file")

	var replyCommentFileType string
	replyCommentCmd := &cobra.Command{
		Use:   "reply-comment [document_id_or_url] [comment_id] [text]",
		Short: "Reply to a comment on a document",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{
				"content": map[string]any{
					"elements": []any{
						map[string]any{
							"type":     "text_run",
							"text_run": map[string]any{"text": args[2]},
						},
					},
				},
			}
			data, err := client.ReplyDocumentComment(cmd.Context(), args[0], args[1], replyCommentFileType, body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	replyCommentCmd.Flags().StringVar(&replyCommentFileType, "file-type", "docx", "file type")

	var resolveCommentFileType string
	resolveCommentCmd := &cobra.Command{
		Use:   "resolve-comment [document_id_or_url] [comment_id]",
		Short: "Resolve a document comment",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.ResolveDocumentComment(cmd.Context(), args[0], args[1], resolveCommentFileType, true)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	resolveCommentCmd.Flags().StringVar(&resolveCommentFileType, "file-type", "docx", "file type")

	var permFileType string
	permCmd := &cobra.Command{
		Use:   "permissions [document_id_or_url]",
		Short: "List document permissions",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.ListDocumentPermissions(cmd.Context(), args[0], permFileType)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	permCmd.Flags().StringVar(&permFileType, "file-type", "docx", "type: doc|docx|sheet|bitable|folder|wiki")

	var exportFormat, exportOutput, exportImageDir string
	exportCmd := &cobra.Command{
		Use:   "export [document_url_or_token]",
		Short: "Export document as pdf, docx, or markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}

			// Markdown: convert blocks locally
			if exportFormat == "md" || exportFormat == "markdown" {
				raw, err := client.GetDocumentBlocks(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				md := blocksToMarkdown(raw, exportImageDir)
				outPath := exportOutput
				if outPath == "" {
					outPath = extractToken(args[0]) + ".md"
				}
				if outPath == "-" {
					fmt.Print(md)
					return nil
				}
				if err := os.WriteFile(outPath, []byte(md), 0o644); err != nil {
					return err
				}
				fmt.Printf("Saved: %s\n", outPath)
				return nil
			}

			token := extractToken(args[0])

			// Resolve wiki token
			if strings.Contains(args[0], "/wiki/") {
				wikiData, err := client.GetWikiNode(cmd.Context(), args[0])
				if err == nil {
					if m, ok := wikiData.(map[string]any); ok {
						if node, ok := m["node"].(map[string]any); ok {
							if objToken, ok := node["obj_token"].(string); ok && objToken != "" {
								token = objToken
							}
						}
					}
				}
			}

			fmt.Println("Creating export task...")
			ticket, err := client.ExportCreate(cmd.Context(), token, "docx", exportFormat)
			if err != nil {
				return err
			}

			fmt.Println("Waiting for export...")
			deadline := time.Now().Add(120 * time.Second)
			var result exportResult
			for {
				if time.Now().After(deadline) {
					return fmt.Errorf("export timeout")
				}
				time.Sleep(2 * time.Second)
				result, err = client.ExportStatus(cmd.Context(), ticket, token)
				if err != nil {
					return err
				}
				switch result.JobStatus {
				case 0:
					goto done
				case 1, 2:
					continue
				default:
					msg := result.ErrMsg
					if msg == "" {
						msg = fmt.Sprintf("job_status=%d", result.JobStatus)
					}
					return fmt.Errorf("export failed: %s", msg)
				}
			}
		done:
			outPath := exportOutput
			if outPath == "" {
				name := result.FileName
				if name == "" {
					name = token
				}
				outPath = name + "." + exportFormat
			}
			f, err := os.Create(outPath)
			if err != nil {
				return err
			}
			defer f.Close()
			fmt.Printf("Downloading %s (%d bytes)...\n", outPath, result.FileSize)
			if err := client.ExportDownload(cmd.Context(), result.FileToken, f); err != nil {
				return err
			}
			fmt.Printf("Saved: %s\n", outPath)
			return nil
		},
	}
	exportCmd.Flags().StringVarP(&exportFormat, "format", "f", "pdf", "export format: pdf, docx, or md")
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "output file path (default: <name>.<format>, use - for stdout)")
	exportCmd.Flags().StringVar(&exportImageDir, "image-dir", "", "image path prefix in markdown (default: feishu-media:// URIs)")

	updateCmd := &cobra.Command{
		Use:   "update [document_id_or_url] [markdown_file]",
		Short: "Update document content from a markdown file (diff-based, preserves comments)",
		Long: `Update document content using diff-based approach.

If the markdown contains <!-- bid:xxx --> markers (from docs export --markdown),
only changed blocks are updated, new blocks are created, and removed blocks are
deleted. Blocks with no changes are left untouched, preserving their comments.

If no markers are present, falls back to full replace (delete all + create all).

Examples:
  larkctl docs export <url> --format md -o doc.md   # markdown export embeds block IDs
  # edit doc.md...
  larkctl docs update <url> doc.md                  # diff update
  cat doc.md | larkctl docs update <url> -          # pipe from stdin`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}

			input := args[0]
			mdFile := args[1]

			var mdContent []byte
			if mdFile == "-" {
				mdContent, err = io.ReadAll(os.Stdin)
			} else {
				mdContent, err = os.ReadFile(mdFile)
			}
			if err != nil {
				return fmt.Errorf("read markdown: %w", err)
			}

			// Resolve document ID
			docID := extractToken(input)
			if strings.Contains(input, "/wiki/") {
				data, err := client.GetWikiNode(cmd.Context(), input)
				if err != nil {
					return fmt.Errorf("resolve wiki token: %w", err)
				}
				if m, ok := data.(map[string]any); ok {
					if node, ok := m["node"].(map[string]any); ok {
						if objToken, ok := node["obj_token"].(string); ok {
							docID = objToken
						}
					}
				}
			}

			// Parse markdown with block ID markers
			entries, err := parseMarkdownWithIDs(string(mdContent))
			if err != nil {
				return fmt.Errorf("parse markdown: %w", err)
			}
			if len(entries) == 0 {
				return fmt.Errorf("no blocks generated from markdown")
			}

			hasBIDs := false
			for _, e := range entries {
				if e.BlockID != "" {
					hasBIDs = true
					break
				}
			}

			if !hasBIDs {
				// No block IDs — full replace mode
				return doFullReplace(cmd.Context(), client, docID, entries)
			}

			// Diff-based update
			return doDiffUpdate(cmd.Context(), client, docID, entries)
		},
	}

	var filesOutputDir string
	filesCmd := &cobra.Command{
		Use:   "files [document_url_or_token]",
		Short: "Download file attachments from a document or wiki page",
		Long: `List and download file attachments (block_type=23) from a Feishu document or wiki page.

Supports:
  - Document: https://xxx.feishu.cn/docx/TOKEN
  - Wiki:     https://xxx.feishu.cn/wiki/TOKEN

Examples:
  larkctl docs files https://xxx.feishu.cn/wiki/TOKEN
  larkctl docs files https://xxx.feishu.cn/wiki/TOKEN -o ./downloads`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			rawURL := args[0]

			var blocksResp struct {
				Items []struct {
					BlockType int    `json:"block_type"`
					BlockID   string `json:"block_id"`
					File      *struct {
						Token string `json:"token"`
						Name  string `json:"name"`
					} `json:"file,omitempty"`
				} `json:"items"`
			}
			raw, err := client.GetDocumentBlocks(cmd.Context(), rawURL)
			if err != nil {
				return err
			}
			b, _ := json.Marshal(raw)
			if err := json.Unmarshal(b, &blocksResp); err != nil {
				return fmt.Errorf("parse blocks: %w", err)
			}

			type fileInfo struct {
				token string
				name  string
			}
			var files []fileInfo
			for _, item := range blocksResp.Items {
				if item.BlockType == 23 && item.File != nil && item.File.Token != "" {
					files = append(files, fileInfo{token: item.File.Token, name: item.File.Name})
				}
			}

			if len(files) == 0 {
				fmt.Println("No file attachments found in document.")
				return nil
			}

			dir := filesOutputDir
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create output dir: %w", err)
			}

			fmt.Printf("Found %d file(s), downloading...\n", len(files))
			for i, file := range files {
				outPath := filepath.Join(dir, file.name)

				f, err := os.Create(outPath)
				if err != nil {
					return fmt.Errorf("create file %s: %w", outPath, err)
				}

				_, err = client.DownloadMedia(cmd.Context(), file.token, f)
				f.Close()
				if err != nil {
					fmt.Printf("  [%d/%d] FAILED %s (%s): %v\n", i+1, len(files), file.name, file.token, err)
					os.Remove(outPath)
					continue
				}

				fi, _ := os.Stat(outPath)
				sizeStr := ""
				if fi != nil {
					sizeStr = fmt.Sprintf(" (%d bytes)", fi.Size())
				}
				fmt.Printf("  [%d/%d] %s%s\n", i+1, len(files), outPath, sizeStr)
			}
			return nil
		},
	}
	filesCmd.Flags().StringVarP(&filesOutputDir, "output-dir", "o", ".", "output directory for downloaded files")

	docsCmd.AddCommand(infoCmd, blocksCmd, createBlocksCmd, deleteBlockCmd, commentsCmd,
		searchCmd, createCmd, addCommentCmd, replyCommentCmd, resolveCommentCmd, permCmd, exportCmd, updateCmd, filesCmd)
	return docsCmd
}

func doFullReplace(ctx context.Context, client FeishuClient, docID string, entries []mdBlockEntry) error {
	// Get existing children
	blocksData, err := client.GetDocumentBlocks(ctx, docID)
	if err != nil {
		return fmt.Errorf("get blocks: %w", err)
	}
	bBytes, _ := json.Marshal(blocksData)
	var blocksResp struct {
		Items []struct {
			BlockID   string   `json:"block_id"`
			BlockType int      `json:"block_type"`
			Children  []string `json:"children"`
		} `json:"items"`
	}
	_ = json.Unmarshal(bBytes, &blocksResp)

	var childIDs []string
	for _, item := range blocksResp.Items {
		if item.BlockType == btPage {
			childIDs = item.Children
			break
		}
	}

	// Delete existing
	if len(childIDs) > 0 {
		fmt.Fprintf(os.Stderr, "=> Deleting %d existing blocks...\n", len(childIDs))
		for i, cid := range childIDs {
			_, _ = client.DeleteDocumentBlock(ctx, docID, cid)
			if (i+1)%20 == 0 {
				time.Sleep(500 * time.Millisecond)
			}
		}
		time.Sleep(1 * time.Second)
	}

	// Create new (with nested children support)
	return createEntriesRecursive(ctx, client, docID, "", entries)
}

func doDiffUpdate(ctx context.Context, client FeishuClient, docID string, entries []mdBlockEntry) error {
	// Get existing blocks for comparison
	blocksData, err := client.GetDocumentBlocks(ctx, docID)
	if err != nil {
		return fmt.Errorf("get blocks: %w", err)
	}
	bBytes, _ := json.Marshal(blocksData)
	var blocksResp struct {
		Items []blockData `json:"items"`
	}
	_ = json.Unmarshal(bBytes, &blocksResp)

	blockMap := map[string]*blockData{}
	for i := range blocksResp.Items {
		blockMap[blocksResp.Items[i].BlockID] = &blocksResp.Items[i]
	}

	// Build existing block metadata maps for comparison
	existingContent := map[string]string{}
	existingTypes := map[string]int{}
	for _, item := range blocksResp.Items {
		existingTypes[item.BlockID] = item.BlockType
		existingContent[item.BlockID] = blockComparableContent(blockMap[item.BlockID], blockMap)
	}

	// Find page block children order
	var pageChildren []string
	for _, item := range blocksResp.Items {
		if item.BlockType == btPage {
			pageChildren = item.Children
			break
		}
	}
	existingSet := map[string]bool{}
	for _, id := range pageChildren {
		existingSet[id] = true
	}

	// Classify entries: update, create, or skip
	var updateReqs []map[string]any
	var toCreate []mdBlockEntry
	keepSet := map[string]bool{}
	var unsupported []string

	for _, entry := range entries {
		if entry.BlockID != "" && existingSet[entry.BlockID] {
			keepSet[entry.BlockID] = true
			existingType := existingTypes[entry.BlockID]
			if existingType != entry.BlockType {
				unsupported = append(unsupported, fmt.Sprintf(
					"line %d: cannot change block %s from %s to %s in diff mode",
					entry.SourceLine, entry.BlockID, blockTypeName(existingType), blockTypeName(entry.BlockType)))
				continue
			}
			newContent := entryComparableContent(entry)
			if existingContent[entry.BlockID] != newContent {
				if entry.Body == nil {
					unsupported = append(unsupported, fmt.Sprintf(
						"line %d: %s block %s changed, but this block type can only be preserved or deleted",
						entry.SourceLine, blockTypeName(entry.BlockType), entry.BlockID))
					continue
				}
				updateReqs = append(updateReqs, buildUpdateRequest(entry.BlockID, entry))
			}
		} else {
			if entry.Body == nil {
				unsupported = append(unsupported, fmt.Sprintf(
					"line %d: cannot create %s blocks in diff mode",
					entry.SourceLine, blockTypeName(entry.BlockType)))
				continue
			}
			toCreate = append(toCreate, entry)
		}
	}

	if len(unsupported) > 0 {
		return fmt.Errorf("unsupported markdown changes:\n  - %s", strings.Join(unsupported, "\n  - "))
	}

	// Find blocks to delete (in existing but not in keep set)
	var toDelete []string
	for _, id := range pageChildren {
		if !keepSet[id] {
			toDelete = append(toDelete, id)
		}
	}

	fmt.Fprintf(os.Stderr, "=> Diff: %d update, %d create, %d delete, %d unchanged\n",
		len(updateReqs), len(toCreate), len(toDelete),
		len(keepSet)-len(updateReqs))

	// 1. Batch update changed blocks
	if len(updateReqs) > 0 {
		fmt.Fprintf(os.Stderr, "=> Updating %d blocks...\n", len(updateReqs))
		_, err := client.UpdateDocumentBlocks(ctx, docID, map[string]any{"requests": updateReqs})
		if err != nil {
			return fmt.Errorf("batch update failed: %w", err)
		}
	}

	// 2. Delete removed blocks
	if len(toDelete) > 0 {
		fmt.Fprintf(os.Stderr, "=> Deleting %d blocks...\n", len(toDelete))
		for i, id := range toDelete {
			_, _ = client.DeleteDocumentBlock(ctx, docID, id)
			if (i+1)%20 == 0 {
				time.Sleep(500 * time.Millisecond)
			}
		}
	}

	// 3. Create new blocks (append at end for simplicity)
	if len(toCreate) > 0 {
		var newBlocks []map[string]any
		for _, e := range toCreate {
			newBlocks = append(newBlocks, e.Body)
		}
		fmt.Fprintf(os.Stderr, "=> Creating %d new blocks...\n", len(newBlocks))
		if err := batchCreateBlocks(ctx, client, docID, "", newBlocks); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "=> Done\n")
	return nil
}

// createEntriesRecursive creates blocks from entries, handling nested children.
// For entries with children, it creates the parent first, then recursively creates children under it.
func createEntriesRecursive(ctx context.Context, client FeishuClient, docID, parentBlockID string, entries []mdBlockEntry) error {
	// Separate entries with children from flat entries
	var flatBlocks []map[string]any

	for _, e := range entries {
		if len(e.Children) > 0 {
			// Flush flat blocks first
			if len(flatBlocks) > 0 {
				if err := batchCreateBlocks(ctx, client, docID, parentBlockID, flatBlocks); err != nil {
					return err
				}
				flatBlocks = nil
			}
			// Create parent one-by-one to get its block ID
			parentBody := []map[string]any{e.Body}
			createdIDs, err := batchCreateBlocksReturnIDs(ctx, client, docID, parentBlockID, parentBody)
			if err != nil {
				return err
			}
			if len(createdIDs) > 0 {
				newParentID := createdIDs[0]
				children := e.Children
				// Container blocks (quote_container, callout) auto-create an empty text child.
				// Update it with first child's content, then create the rest.
				if isContainerBlock(e.BlockType) && len(children) > 0 {
					autoChildID := findAutoChild(ctx, client, docID, newParentID)
					if autoChildID != "" {
						consumed, err := updateAutoChild(ctx, client, docID, autoChildID, children[0])
						if err != nil {
							return err
						}
						if consumed {
							children = children[1:]
						}
					}
				}
				// Recursively create remaining children under this parent
				if len(children) > 0 {
					if err := createEntriesRecursive(ctx, client, docID, newParentID, children); err != nil {
						return err
					}
				}
			}
		} else {
			flatBlocks = append(flatBlocks, e.Body)
		}
	}

	// Flush remaining flat blocks
	if len(flatBlocks) > 0 {
		return batchCreateBlocks(ctx, client, docID, parentBlockID, flatBlocks)
	}
	return nil
}

// isContainerBlock returns true for block types where the Feishu API auto-creates an empty child.
func isContainerBlock(blockType int) bool {
	return blockType == btQuoteContainer || blockType == btCallout
}

// findAutoChild returns the block ID of the first auto-created child of a container block.
func findAutoChild(ctx context.Context, client FeishuClient, docID, blockID string) string {
	data, err := client.GetDocumentBlocks(ctx, docID)
	if err != nil {
		return ""
	}
	bBytes, _ := json.Marshal(data)
	var resp struct {
		Items []struct {
			BlockID  string `json:"block_id"`
			ParentID string `json:"parent_id"`
		} `json:"items"`
	}
	if json.Unmarshal(bBytes, &resp) != nil {
		return ""
	}
	for _, item := range resp.Items {
		if item.ParentID == blockID {
			return item.BlockID
		}
	}
	return ""
}

// updateAutoChild updates the auto-created empty text child with actual content.
// updateAutoChild writes entry into the empty text child a container block created
// for itself. It reports whether the content landed there: the caller may only skip
// the entry when it did, otherwise that line would be dropped from the document.
func updateAutoChild(ctx context.Context, client FeishuClient, docID, autoChildID string, entry mdBlockEntry) (bool, error) {
	elements := entryElements(entry)
	if elements == nil {
		return false, nil
	}
	req := map[string]any{
		"requests": []any{
			map[string]any{
				"block_id":             autoChildID,
				"update_text_elements": map[string]any{"elements": elements},
			},
		},
	}
	if _, err := client.UpdateDocumentBlocks(ctx, docID, req); err != nil {
		return false, err
	}
	return true, nil
}

// batchCreateBlocksReturnIDs creates blocks and returns the created block IDs.
func batchCreateBlocksReturnIDs(ctx context.Context, client FeishuClient, docID, parentBlockID string, blocks []map[string]any) ([]string, error) {
	body := map[string]any{"children": blocks, "index": -1}
	data, err := client.CreateDocumentBlocks(ctx, docID, parentBlockID, body)
	if err != nil && strings.Contains(err.Error(), "429") {
		time.Sleep(2 * time.Second)
		data, err = client.CreateDocumentBlocks(ctx, docID, parentBlockID, body)
	}
	if err != nil {
		return nil, err
	}

	var ids []string
	if m, ok := data.(map[string]any); ok {
		if children, ok := m["children"].([]any); ok {
			for _, child := range children {
				if cm, ok := child.(map[string]any); ok {
					if id, ok := cm["block_id"].(string); ok {
						ids = append(ids, id)
					}
				}
			}
		}
	}
	return ids, nil
}

func batchCreateBlocks(ctx context.Context, client FeishuClient, docID, parentBlockID string, blocks []map[string]any) error {
	batchSize := 10
	total := 0
	var lastErr error
	for i := 0; i < len(blocks); i += batchSize {
		end := i + batchSize
		if end > len(blocks) {
			end = len(blocks)
		}
		batch := blocks[i:end]
		body := map[string]any{"children": batch, "index": -1}
		data, err := client.CreateDocumentBlocks(ctx, docID, parentBlockID, body)
		if err != nil && strings.Contains(err.Error(), "429") {
			time.Sleep(2 * time.Second)
			data, err = client.CreateDocumentBlocks(ctx, docID, parentBlockID, body)
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "  batch %d: FAILED: %v\n", i/batchSize+1, err)
			lastErr = err
			continue
		}
		if m, ok := data.(map[string]any); ok {
			if children, ok := m["children"].([]any); ok {
				total += len(children)
			}
		}
	}
	fmt.Fprintf(os.Stderr, "=> Created %d blocks\n", total)
	if lastErr != nil {
		return fmt.Errorf("some batches failed, last error: %w", lastErr)
	}
	return nil
}

func entryComparableContent(entry mdBlockEntry) string {
	return entry.Comparable
}

func newWikiCmd() *cobra.Command {
	wikiCmd := &cobra.Command{
		Use:   "wiki",
		Short: "Feishu wiki read-only commands",
	}

	nodeCmd := &cobra.Command{
		Use:   "node [wiki_token_or_url]",
		Short: "Get wiki node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetWikiNode(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	spacesCmd := &cobra.Command{
		Use:   "spaces",
		Short: "List wiki spaces",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.ListWikiSpaces(cmd.Context())
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}

	nodesCmd := &cobra.Command{
		Use:   "nodes [space_id]",
		Short: "List wiki nodes in a space",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			parentToken, _ := cmd.Flags().GetString("parent")
			data, err := client.ListWikiNodes(cmd.Context(), args[0], parentToken)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	nodesCmd.Flags().String("parent", "", "parent node token (default: space root)")

	createNodeCmd := &cobra.Command{
		Use:   "create-node [space_id]",
		Short: "Create a wiki node",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			objType, _ := cmd.Flags().GetString("type")
			title, _ := cmd.Flags().GetString("title")
			parent, _ := cmd.Flags().GetString("parent")
			body := map[string]any{"obj_type": objType}
			if title != "" {
				body["title"] = title
			}
			if parent != "" {
				body["parent_node_token"] = parent
			}
			data, err := client.CreateWikiNode(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	createNodeCmd.Flags().String("type", "docx", "node type: doc|docx|sheet|bitable")
	createNodeCmd.Flags().String("title", "", "node title")
	createNodeCmd.Flags().String("parent", "", "parent node token")

	wikiCmd.AddCommand(nodeCmd, spacesCmd, nodesCmd, createNodeCmd)
	return wikiCmd
}

func newSheetsCmd() *cobra.Command {
	sheetsCmd := &cobra.Command{
		Use:   "sheets",
		Short: "Feishu sheets read-only commands",
	}

	metaCmd := &cobra.Command{
		Use:   "meta [spreadsheet_token]",
		Short: "Get spreadsheet sheet metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetSheetsMeta(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	valuesCmd := &cobra.Command{
		Use:   "values [spreadsheet_token] [sheet_id] [range]",
		Short: "Get sheet cell values",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			render, _ := cmd.Flags().GetString("render")
			dateRender, _ := cmd.Flags().GetString("date-render")
			var query url.Values
			if render != "" || dateRender != "" {
				query = url.Values{}
				if render != "" {
					query.Set("valueRenderOption", render)
				}
				if dateRender != "" {
					query.Set("dateTimeRenderOption", dateRender)
				}
			}
			data, err := client.GetSheetValues(cmd.Context(), args[0], args[1], args[2], query)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	valuesCmd.Flags().String("render", "",
		"valueRenderOption: ToString | FormattedValue | UnformattedValue | Formula. "+
			"Default (unset): rich-text cells (mentions/links) come back as structured arrays.")
	valuesCmd.Flags().String("date-render", "",
		"dateTimeRenderOption: FormattedString | UnformattedValue.")

	updateCmd := &cobra.Command{
		Use:   "update [spreadsheet_token] [json_file_or_-]",
		Short: "Write values to a spreadsheet range (JSON from file or stdin)",
		Long:  `JSON body: {"valueRange":{"range":"sheetId!A1:C3","values":[["a","b"],["c","d"]]}}`,
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			var body any
			if err := readJSONInput(args, 1, &body); err != nil {
				return err
			}
			data, err := client.UpdateSheetValues(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	appendCmd := &cobra.Command{
		Use:   "append [spreadsheet_token] [json_file_or_-]",
		Short: "Append rows to a spreadsheet (JSON from file or stdin)",
		Long:  `JSON body: {"valueRange":{"range":"sheetId!A1:C1","values":[["a","b","c"]]}}`,
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			var body any
			if err := readJSONInput(args, 1, &body); err != nil {
				return err
			}
			data, err := client.AppendSheetValues(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	var exportOutput string
	exportCmd := &cobra.Command{
		Use:   "export [spreadsheet_url_or_token]",
		Short: "Export spreadsheet as xlsx file",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}

			token := extractToken(args[0])

			// Resolve wiki token
			if strings.Contains(args[0], "/wiki/") {
				wikiData, err := client.GetWikiNode(cmd.Context(), args[0])
				if err == nil {
					if m, ok := wikiData.(map[string]any); ok {
						if node, ok := m["node"].(map[string]any); ok {
							if objToken, ok := node["obj_token"].(string); ok && objToken != "" {
								token = objToken
							}
						}
					}
				}
			}

			fmt.Println("Creating export task...")
			ticket, err := client.ExportCreate(cmd.Context(), token, "sheet", "xlsx")
			if err != nil {
				return err
			}

			fmt.Println("Waiting for export...")
			deadline := time.Now().Add(120 * time.Second)
			var result exportResult
			for {
				if time.Now().After(deadline) {
					return fmt.Errorf("export timeout")
				}
				time.Sleep(2 * time.Second)

				result, err = client.ExportStatus(cmd.Context(), ticket, token)
				if err != nil {
					return err
				}
				switch result.JobStatus {
				case 0:
					goto done
				case 1, 2:
					continue
				default:
					msg := result.ErrMsg
					if msg == "" {
						msg = fmt.Sprintf("job_status=%d", result.JobStatus)
					}
					return fmt.Errorf("export failed: %s", msg)
				}
			}
		done:

			outPath := exportOutput
			if outPath == "" {
				name := result.FileName
				if name == "" {
					name = token
				}
				outPath = name + ".xlsx"
			}

			f, err := os.Create(outPath)
			if err != nil {
				return err
			}
			defer f.Close()

			fmt.Printf("Downloading %s (%d bytes)...\n", outPath, result.FileSize)
			if err := client.ExportDownload(cmd.Context(), result.FileToken, f); err != nil {
				return err
			}
			fmt.Printf("Saved: %s\n", outPath)
			return nil
		},
	}
	exportCmd.Flags().StringVarP(&exportOutput, "output", "o", "", "output file path (default: <name>.xlsx)")

	sheetsCmd.AddCommand(metaCmd, valuesCmd, updateCmd, appendCmd, exportCmd)
	return sheetsCmd
}

func newDriveCmd() *cobra.Command {
	driveCmd := &cobra.Command{
		Use:   "drive",
		Short: "Feishu drive file commands",
	}

	listCmd := &cobra.Command{
		Use:   "list [folder_token]",
		Short: "List files in a folder (default: root)",
		Args:  cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			folderToken := ""
			if len(args) > 0 {
				folderToken = args[0]
			}
			data, err := client.ListDriveFiles(cmd.Context(), folderToken)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}

	mkdirCmd := &cobra.Command{
		Use:   "mkdir [parent_folder_token] [name]",
		Short: "Create a folder",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{"folder_token": args[0], "name": args[1]}
			data, err := client.CreateFolder(cmd.Context(), body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	uploadCmd := &cobra.Command{
		Use:   "upload [file_path]",
		Short: "Upload a file to drive",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			folderToken, _ := cmd.Flags().GetString("folder")
			filePath := args[0]
			f, err := os.Open(filePath)
			if err != nil {
				return fmt.Errorf("open file: %w", err)
			}
			defer f.Close()
			fi, err := f.Stat()
			if err != nil {
				return fmt.Errorf("stat file: %w", err)
			}
			data, err := client.UploadFile(cmd.Context(), folderToken, filepath.Base(filePath), f, fi.Size())
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	uploadCmd.Flags().String("folder", "", "parent folder token (required)")
	_ = uploadCmd.MarkFlagRequired("folder")

	downloadCmd := &cobra.Command{
		Use:   "download [file_token] [output_path]",
		Short: "Download a file from drive",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			fileToken := args[0]
			outPath := args[1]

			// If output_path is a directory, use file_token as filename
			if fi, err := os.Stat(outPath); err == nil && fi.IsDir() {
				outPath = filepath.Join(outPath, fileToken)
			}

			f, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()

			if err := client.DownloadFile(cmd.Context(), fileToken, f); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Downloaded to %s\n", outPath)
			return nil
		},
	}

	mediaCmd := &cobra.Command{
		Use:   "media [file_token] [output_path]",
		Short: "Download a media file (bitable attachment, doc image) from drive",
		Long: `Download a media file from drive.

Media are attachments living inside a document, bitable or sheet — they use a
different API than standalone drive files, so ` + "`drive download`" + ` (which hits
/drive/v1/files) returns 403 for them. Use this command instead.

Get bitable attachment tokens from the record fields:
  larkctl bitable records APP_TOKEN TABLE_ID -c | jq -r '[.. | objects | select(has("file_token"))][] | "\(.file_token)\t\(.name)"'

Examples:
  larkctl drive media FILE_TOKEN ./attachment.stl
  larkctl drive media FILE_TOKEN ./downloads/`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			fileToken := args[0]
			outPath := args[1]

			// If output_path is a directory, use file_token as filename
			if fi, err := os.Stat(outPath); err == nil && fi.IsDir() {
				outPath = filepath.Join(outPath, fileToken)
			}

			f, err := os.Create(outPath)
			if err != nil {
				return fmt.Errorf("create output file: %w", err)
			}
			defer f.Close()

			if _, err := client.DownloadMedia(cmd.Context(), fileToken, f); err != nil {
				return err
			}
			fmt.Fprintf(os.Stderr, "Downloaded to %s\n", outPath)
			return nil
		},
	}

	importCmd := &cobra.Command{
		Use:   "import [file_path]",
		Short: "Import a local file as a Feishu cloud document",
		Long: `Import a local file as a Feishu cloud document.

Supported formats:
  .docx, .doc, .txt, .md, .html → Feishu Document (docx)
  .xlsx, .xls, .csv → Feishu Spreadsheet (sheet)

Examples:
  larkctl drive import report.docx --folder FOLDER_TOKEN
  larkctl drive import data.xlsx --folder FOLDER_TOKEN --type sheet
  larkctl drive import notes.md --folder FOLDER_TOKEN`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			filePath := args[0]
			folderToken, _ := cmd.Flags().GetString("folder")
			if folderToken == "" {
				return fmt.Errorf("--folder is required")
			}
			targetType, _ := cmd.Flags().GetString("type")

			ext := strings.ToLower(filepath.Ext(filePath))
			if targetType == "" {
				switch ext {
				case ".docx", ".doc", ".txt", ".md", ".markdown", ".html":
					targetType = "docx"
				case ".xlsx", ".xls", ".csv":
					targetType = "sheet"
				default:
					return fmt.Errorf("unsupported file type %q, use --type to specify (docx or sheet)", ext)
				}
			}

			f, err := os.Open(filePath)
			if err != nil {
				return err
			}
			defer f.Close()
			info, err := f.Stat()
			if err != nil {
				return err
			}
			fileName := filepath.Base(filePath)
			// file_extension without dot for Feishu API
			fileExt := strings.TrimPrefix(ext, ".")
			// target name without extension (Feishu uses this as cloud document title)
			targetName := strings.TrimSuffix(fileName, ext)
			if targetName == "" {
				targetName = fileName
			}

			// Step 1: Upload media
			fmt.Fprintf(os.Stderr, "Uploading %s...\n", fileName)
			fileToken, err := client.ImportUpload(cmd.Context(), fileName, targetType, fileExt, f, info.Size())
			if err != nil {
				return fmt.Errorf("upload failed: %w", err)
			}

			// Step 2: Create import task
			fmt.Fprintf(os.Stderr, "Creating import task...\n")
			ticket, err := client.ImportCreate(cmd.Context(), fileToken, targetType, fileExt, targetName, folderToken)
			if err != nil {
				return fmt.Errorf("import create failed: %w", err)
			}

			// Step 3: Poll until done
			fmt.Fprintf(os.Stderr, "Importing...")
			for i := 0; i < 30; i++ {
				time.Sleep(2 * time.Second)
				fmt.Fprintf(os.Stderr, ".")
				data, err := client.ImportStatus(cmd.Context(), ticket)
				if err != nil {
					continue
				}
				m, _ := data.(map[string]any)
				result, _ := m["result"].(map[string]any)
				if result == nil {
					continue
				}
				status := intFromAny(result["job_status"])
				if status == 0 {
					fmt.Fprintln(os.Stderr, " done!")
					printJSON(result)
					return nil
				}
				if status > 2 {
					errMsg, _ := result["job_error_msg"].(string)
					return fmt.Errorf("\nimport failed: %s", errMsg)
				}
			}
			return fmt.Errorf("\nimport timeout, check manually with ticket: %s", ticket)
		},
	}
	importCmd.Flags().String("folder", "", "destination folder token (required)")
	importCmd.Flags().String("type", "", "target type: docx or sheet (auto-detected)")

	driveCmd.AddCommand(listCmd, mkdirCmd, uploadCmd, downloadCmd, mediaCmd, importCmd)
	return driveCmd
}

func newBitableCmd() *cobra.Command {
	bitableCmd := &cobra.Command{
		Use:   "bitable",
		Short: "Feishu bitable (multi-dimensional table) commands",
	}

	metaCmd := &cobra.Command{
		Use:   "meta [app_token]",
		Short: "Get bitable app metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetBitableMeta(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	tablesCmd := &cobra.Command{
		Use:   "tables [app_token]",
		Short: "List tables in a bitable app",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.ListBitableTables(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}

	fieldsCmd := &cobra.Command{
		Use:   "fields [app_token] [table_id]",
		Short: "List fields of a bitable table",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.ListBitableFields(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}

	recordsCmd := &cobra.Command{
		Use:   "records [app_token] [table_id]",
		Short: "List records of a bitable table",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			extra := url.Values{}
			if v, _ := cmd.Flags().GetString("filter"); v != "" {
				extra.Set("filter", v)
			}
			if v, _ := cmd.Flags().GetString("view-id"); v != "" {
				extra.Set("view_id", v)
			}
			data, err := client.ListBitableRecords(cmd.Context(), args[0], args[1], extra)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	recordsCmd.Flags().String("filter", "", "filter expression")
	recordsCmd.Flags().String("view-id", "", "view ID")

	createRecordCmd := &cobra.Command{
		Use:   "create-record [app_token] [table_id] [json_file_or_-]",
		Short: "Create a record (reads JSON fields from file or stdin)",
		Long:  `JSON body: {"fields":{"Name":"Alice","Status":"Done"}}`,
		Args:  cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			var body any
			if err := readJSONInput(args, 2, &body); err != nil {
				return err
			}
			data, err := client.CreateBitableRecord(cmd.Context(), args[0], args[1], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	updateRecordCmd := &cobra.Command{
		Use:   "update-record [app_token] [table_id] [record_id] [json_file_or_-]",
		Short: "Update a record (reads JSON fields from file or stdin)",
		Long:  `JSON body: {"fields":{"Status":"Done"}}`,
		Args:  cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			var body any
			if err := readJSONInput(args, 3, &body); err != nil {
				return err
			}
			data, err := client.UpdateBitableRecord(cmd.Context(), args[0], args[1], args[2], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	createFieldCmd := &cobra.Command{
		Use:   "create-field [app_token] [table_id] [json_file_or_-]",
		Short: "Create a field (reads JSON from file or stdin)",
		Long: `JSON body: {"field_name":"Status","type":3,"property":{"options":[{"name":"Done"}]}}
Common type codes: 1 text, 2 number, 3 single-select, 4 multi-select, 5 date,
7 checkbox, 11 person, 13 phone, 15 url, 17 attachment, 1001 created time`,
		Args: cobra.RangeArgs(2, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			var body any
			if err := readJSONInput(args, 2, &body); err != nil {
				return err
			}
			data, err := client.CreateBitableField(cmd.Context(), args[0], args[1], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	updateFieldCmd := &cobra.Command{
		Use:   "update-field [app_token] [table_id] [field_id] [json_file_or_-]",
		Short: "Update a field (reads JSON from file or stdin)",
		Long:  `JSON body: {"field_name":"New name","type":3,"property":{"options":[{"name":"Done"}]}}`,
		Args:  cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			var body any
			if err := readJSONInput(args, 3, &body); err != nil {
				return err
			}
			data, err := client.UpdateBitableField(cmd.Context(), args[0], args[1], args[2], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	deleteFieldCmd := &cobra.Command{
		Use:   "delete-field [app_token] [table_id] [field_id]",
		Short: "Delete a field (the column and all its values)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.DeleteBitableField(cmd.Context(), args[0], args[1], args[2])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	viewsCmd := &cobra.Command{
		Use:   "views [app_token] [table_id]",
		Short: "List views of a bitable table",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.ListBitableViews(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}

	viewCmd := &cobra.Command{
		Use:   "view [app_token] [table_id] [view_id]",
		Short: "Get a single view (including its property: filters, sorts, groups)",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetBitableView(cmd.Context(), args[0], args[1], args[2])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	createViewCmd := &cobra.Command{
		Use:   "create-view [app_token] [table_id] [view_name]",
		Short: "Create a view",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			viewType, _ := cmd.Flags().GetString("type")
			body := map[string]any{"view_name": args[2], "view_type": viewType}
			data, err := client.CreateBitableView(cmd.Context(), args[0], args[1], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	createViewCmd.Flags().String("type", "grid", "view type: grid|kanban|gallery|gantt|form")

	updateViewCmd := &cobra.Command{
		Use:   "update-view [app_token] [table_id] [view_id] [json_file_or_-]",
		Short: "Update a view (reads JSON from file or stdin)",
		Long: `JSON body: {"view_name":"New name","property":{...}}
property (grid views only) can set filter_info, hidden_fields, etc.`,
		Args: cobra.RangeArgs(3, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			var body any
			if err := readJSONInput(args, 3, &body); err != nil {
				return err
			}
			data, err := client.UpdateBitableView(cmd.Context(), args[0], args[1], args[2], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	deleteViewCmd := &cobra.Command{
		Use:   "delete-view [app_token] [table_id] [view_id]",
		Short: "Delete a view",
		Args:  cobra.ExactArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.DeleteBitableView(cmd.Context(), args[0], args[1], args[2])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	bitableCmd.AddCommand(metaCmd, tablesCmd, fieldsCmd, recordsCmd, createRecordCmd, updateRecordCmd,
		createFieldCmd, updateFieldCmd, deleteFieldCmd,
		viewsCmd, viewCmd, createViewCmd, updateViewCmd, deleteViewCmd)
	return bitableCmd
}

// emojiAliases maps common lowercase/unicode shorthands to Feishu reaction
// emoji_type values (case-sensitive enum, e.g. OK/Yes/No/THUMBSUP/LGTM).
// Unknown input passes through verbatim so any of the ~185 official types work.
var emojiAliases = map[string]string{
	"ok": "OK", "yes": "Yes", "no": "No",
	"thumbsup": "THUMBSUP", "+1": "THUMBSUP", "👍": "THUMBSUP",
	"thumbsdown": "ThumbsDown", "-1": "ThumbsDown", "👎": "ThumbsDown",
	"done": "DONE", "✅": "DONE", "checkmark": "CheckMark", "crossmark": "CrossMark",
	"lgtm": "LGTM", "onit": "OnIt", "thanks": "THANKS",
	"smile": "SMILE", "laugh": "LAUGH", "😄": "LAUGH",
	"heart": "HEART", "❤️": "HEART", "party": "PARTY", "🎉": "PARTY",
	"fire": "Fire", "🔥": "Fire", "clap": "CLAP", "applause": "APPLAUSE",
	"salute": "SALUTE", "muscle": "MUSCLE", "💪": "MUSCLE",
}

func canonicalEmojiType(s string) string {
	if v, ok := emojiAliases[strings.ToLower(strings.TrimSpace(s))]; ok {
		return v
	}
	return strings.TrimSpace(s)
}

// imFileType maps a file extension to the file_type field of /im/v1/files.
// Feishu only recognizes a fixed set; everything else uploads as "stream".
func imFileType(ext string) string {
	switch ext {
	case "opus", "mp4", "pdf", "doc", "xls", "ppt":
		return ext
	case "docx":
		return "doc"
	case "xlsx":
		return "xls"
	case "pptx":
		return "ppt"
	default:
		return "stream"
	}
}

func newIMCmd() *cobra.Command {
	imCmd := &cobra.Command{
		Use:   "im",
		Short: "Feishu IM message commands",
	}

	sendCmd := &cobra.Command{
		Use:   "send [message]",
		Short: "Send a message to a chat or user",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message"); err != nil {
				return err
			}

			to, _ := cmd.Flags().GetString("to")
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			idType, _ := cmd.Flags().GetString("type")
			if idType == "" {
				if strings.HasPrefix(to, "oc_") {
					idType = "chat_id"
				} else {
					idType = "open_id"
				}
			}
			msgType, _ := cmd.Flags().GetString("msg-type")
			fileFlag, _ := cmd.Flags().GetString("file")
			textFileFlag, _ := cmd.Flags().GetString("text-file")

			var content any
			if textFileFlag != "" {
				raw, err := os.ReadFile(textFileFlag)
				if err != nil {
					return fmt.Errorf("read text file: %w", err)
				}
				content = map[string]any{"text": string(raw)}
			} else if fileFlag != "" {
				f, err := os.Open(fileFlag)
				if err != nil {
					return fmt.Errorf("open file: %w", err)
				}
				defer f.Close()
				var raw any
				if err := json.NewDecoder(f).Decode(&raw); err != nil {
					return fmt.Errorf("parse JSON file: %w", err)
				}
				content = raw
			} else {
				text := ""
				if len(args) > 0 {
					text = args[0]
				}
				content = map[string]any{"text": text}
			}

			body := map[string]any{
				"receive_id":      to,
				"receive_id_type": idType,
				"msg_type":        msgType,
				"content":         mustJSONString(content),
			}

			data, err := client.SendMessage(cmd.Context(), body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	sendCmd.Flags().String("to", "", "recipient: chat_id or user open_id (required)")
	sendCmd.Flags().String("type", "", "receive_id_type: chat_id or open_id (auto-detected from --to)")
	sendCmd.Flags().String("msg-type", "text", "message type: text, post, image, etc.")
	sendCmd.Flags().String("file", "", "JSON file for complex message content")
	sendCmd.Flags().String("text-file", "", "plain-text file to send as a text message (avoids shell quoting)")

	findCmd := &cobra.Command{
		Use:   "find [name]",
		Short: "Search users by name; the returned open_id feeds `im send --to` directly",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, contactSearchScopes); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			data, err := client.SearchUsers(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	findCmd.Flags().Int("limit", 5, "max results (1-20)")

	sendFileCmd := &cobra.Command{
		Use:   "send-file [path]",
		Short: "Upload a local file or image and send it as a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message im:resource"); err != nil {
				return err
			}
			to, _ := cmd.Flags().GetString("to")
			if to == "" {
				return fmt.Errorf("--to is required")
			}
			idType, _ := cmd.Flags().GetString("type")
			if idType == "" {
				if strings.HasPrefix(to, "oc_") {
					idType = "chat_id"
				} else {
					idType = "open_id"
				}
			}
			asType, _ := cmd.Flags().GetString("as")
			fileName, _ := cmd.Flags().GetString("name")

			f, err := os.Open(args[0])
			if err != nil {
				return fmt.Errorf("open file: %w", err)
			}
			defer f.Close()
			if fileName == "" {
				fileName = filepath.Base(args[0])
			}
			ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
			if asType == "" || asType == "auto" {
				asType = "file"
				switch ext {
				case "jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff":
					asType = "image"
				}
			}

			var msgType string
			var content map[string]any
			switch asType {
			case "image":
				key, err := client.UploadIMImage(cmd.Context(), fileName, f)
				if err != nil {
					return fmt.Errorf("upload image: %w", err)
				}
				msgType, content = "image", map[string]any{"image_key": key}
			case "file":
				key, err := client.UploadIMFile(cmd.Context(), imFileType(ext), fileName, f)
				if err != nil {
					return fmt.Errorf("upload file: %w", err)
				}
				msgType, content = "file", map[string]any{"file_key": key}
			default:
				return fmt.Errorf("invalid --as %q, expected auto|image|file", asType)
			}

			body := map[string]any{
				"receive_id":      to,
				"receive_id_type": idType,
				"msg_type":        msgType,
				"content":         mustJSONString(content),
			}
			data, err := client.SendMessage(cmd.Context(), body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	sendFileCmd.Flags().String("to", "", "recipient: chat_id (oc_*) or user open_id (required)")
	sendFileCmd.Flags().String("type", "", "receive_id_type: chat_id, open_id or user_id (auto-detected from --to)")
	sendFileCmd.Flags().String("as", "auto", "send as: auto (by extension), image, or file")
	sendFileCmd.Flags().String("name", "", "file name shown to the recipient (default: base name of path)")

	reactCmd := &cobra.Command{
		Use:   "react [message_id] [emoji]",
		Short: "Add an emoji reaction to a message (OK/Yes/No/THUMBSUP/LGTM/... or aliases like ok,+1,done)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message"); err != nil {
				return err
			}
			data, err := client.ReactMessage(cmd.Context(), args[0], canonicalEmojiType(args[1]))
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	reactionsCmd := &cobra.Command{
		Use:   "reactions [message_id]",
		Short: "List emoji reactions on a message",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user"); err != nil {
				return err
			}
			query := url.Values{}
			if v, _ := cmd.Flags().GetString("type"); v != "" {
				query.Set("reaction_type", canonicalEmojiType(v))
			}
			if v, _ := cmd.Flags().GetInt("limit"); v > 0 {
				query.Set("page_size", strconv.Itoa(v))
			}
			data, err := client.ListMessageReactions(cmd.Context(), args[0], query)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	reactionsCmd.Flags().String("type", "", "filter by emoji type (e.g. OK, THUMBSUP)")
	reactionsCmd.Flags().Int("limit", 20, "number of reaction records to return")

	unreactCmd := &cobra.Command{
		Use:   "unreact [message_id] [reaction_id]",
		Short: "Remove a reaction you added (reaction_id from `im react` or `im reactions`)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message"); err != nil {
				return err
			}
			data, err := client.DeleteMessageReaction(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	listMsgCmd := &cobra.Command{
		Use:   "list [chat_id]",
		Short: "List messages in a chat",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user"); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			sort, _ := cmd.Flags().GetString("sort")
			pageToken, _ := cmd.Flags().GetString("page-token")
			pageAll, _ := cmd.Flags().GetBool("page-all")
			pageLimit, _ := cmd.Flags().GetInt("page-limit")
			pageDelayMs, _ := cmd.Flags().GetInt("page-delay-ms")

			buildQuery := func(tok string) url.Values {
				q := url.Values{
					"page_size": {strconv.Itoa(limit)},
					"sort_type": {sort},
				}
				if tok != "" {
					q.Set("page_token", tok)
				}
				return q
			}

			if !pageAll {
				data, err := client.ListMessages(cmd.Context(), args[0], buildQuery(pageToken))
				if err != nil {
					return err
				}
				printOutput(data)
				return nil
			}

			var collected []any
			next := pageToken
			for pages := 0; pageLimit <= 0 || pages < pageLimit; pages++ {
				raw, err := client.ListMessages(cmd.Context(), args[0], buildQuery(next))
				if err != nil {
					return err
				}
				m, ok := raw.(map[string]any)
				if !ok {
					return fmt.Errorf("unexpected im list response type %T", raw)
				}
				if items, ok := m["items"].([]any); ok {
					collected = append(collected, items...)
				}
				more, _ := m["has_more"].(bool)
				nextTok, _ := m["page_token"].(string)
				if !more || nextTok == "" {
					next = ""
					break
				}
				next = nextTok
				if pageDelayMs > 0 {
					time.Sleep(time.Duration(pageDelayMs) * time.Millisecond)
				}
			}
			out := map[string]any{
				"items":    collected,
				"has_more": next != "",
			}
			if next != "" {
				out["page_token"] = next
			}
			printOutput(out)
			return nil
		},
	}
	listMsgCmd.Flags().Int("limit", 20, "messages per page (server max: 50)")
	listMsgCmd.Flags().String("sort", "ByCreateTimeDesc", "sort order")
	listMsgCmd.Flags().String("page-token", "", "pagination token from a previous response")
	listMsgCmd.Flags().Bool("page-all", false, "auto-paginate through all pages")
	listMsgCmd.Flags().Int("page-limit", 0, "max pages to fetch with --page-all (0 = unlimited)")
	listMsgCmd.Flags().Int("page-delay-ms", 200, "delay between pages with --page-all")

	searchCmd := &cobra.Command{
		Use:   "search [keyword]",
		Short: "Search messages by keyword",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "search:message"); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			// page_size is a query parameter for /im/v1/messages/search; in the body
			// Feishu ignores it and silently returns its own default page.
			query := url.Values{}
			if limit > 0 {
				query.Set("page_size", strconv.Itoa(limit))
			}
			body := map[string]any{"query": args[0]}
			if chat, _ := cmd.Flags().GetString("chat"); chat != "" {
				body["chat_id"] = chat
			}
			data, err := client.SearchMessages(cmd.Context(), body, query)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	searchCmd.Flags().String("chat", "", "limit search to a specific chat_id")
	searchCmd.Flags().Int("limit", 20, "number of results")

	replyCmd := &cobra.Command{
		Use:   "reply [message_id] [text]",
		Short: "Reply to a message",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message"); err != nil {
				return err
			}
			body := map[string]any{
				"msg_type": "text",
				"content":  mustJSONString(map[string]any{"text": args[1]}),
			}
			data, err := client.ReplyMessage(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	chatsCmd := &cobra.Command{
		Use:   "chats [keyword]",
		Short: "Search chats/groups by keyword",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:chat:readonly"); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			data, err := client.SearchChats(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	chatsCmd.Flags().Int("limit", 20, "number of results")

	mgetCmd := &cobra.Command{
		Use:   "mget [message_id...]",
		Short: "Batch get messages by IDs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user"); err != nil {
				return err
			}
			data, err := client.MGetMessages(cmd.Context(), args)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}

	downloadCmd := &cobra.Command{
		Use:   "download [message_id] [file_key]",
		Short: "Download a message resource (image or file)",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user"); err != nil {
				return err
			}
			resourceType, _ := cmd.Flags().GetString("type")
			outputPath, _ := cmd.Flags().GetString("output")
			var w io.Writer
			if outputPath != "" {
				f, err := os.Create(outputPath)
				if err != nil {
					return fmt.Errorf("create output file: %w", err)
				}
				defer f.Close()
				w = f
			} else {
				w = os.Stdout
			}
			return client.DownloadMessageResource(cmd.Context(), args[0], args[1], resourceType, w)
		},
	}
	downloadCmd.Flags().String("type", "image", "resource type: image or file")
	downloadCmd.Flags().String("output", "", "output file path (default: stdout)")

	threadCmd := &cobra.Command{
		Use:   "thread [thread_id]",
		Short: "List messages in a thread",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "im:message.group_msg:get_as_user im:message.p2p_msg:get_as_user"); err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			query := url.Values{
				"page_size": {strconv.Itoa(limit)},
			}
			data, err := client.ListThreadMessages(cmd.Context(), args[0], query)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	threadCmd.Flags().Int("limit", 20, "number of messages to return")

	listenCmd := &cobra.Command{
		Use:   "listen",
		Short: "Stream your Feishu events from the gateway (one JSON line per event)",
		Long: "Holds the gateway's per-user event stream open and prints each Feishu event\n" +
			"addressed to you (DMs to the bot, group @-mentions you send) as one JSON line\n" +
			"on stdout. Status messages go to stderr. Gateway mode only.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			gc, ok := client.(*GatewayClient)
			if !ok {
				return fmt.Errorf("im listen requires gateway mode (local mode has no event stream)")
			}
			once, _ := cmd.Flags().GetBool("once")
			for {
				superseded := false
				err := gc.StreamEvents(cmd.Context(), func(event, data string) {
					switch event {
					case "ready":
						fmt.Fprintf(os.Stderr, "listening: %s\n", data)
					case "superseded":
						superseded = true
					default:
						fmt.Println(data)
					}
				})
				if cmd.Context().Err() != nil {
					return nil
				}
				if superseded {
					return fmt.Errorf("stream taken over by a newer 'im listen' for this account")
				}
				if once {
					return err
				}
				fmt.Fprintf(os.Stderr, "stream ended (%v), reconnecting in 5s...\n", err)
				select {
				case <-cmd.Context().Done():
					return nil
				case <-time.After(5 * time.Second):
				}
			}
		},
	}
	listenCmd.Flags().Bool("once", false, "exit when the stream ends instead of reconnecting")

	imCmd.AddCommand(sendCmd, sendFileCmd, findCmd, reactCmd, reactionsCmd, unreactCmd, listMsgCmd, searchCmd, replyCmd, chatsCmd, mgetCmd, downloadCmd, threadCmd, listenCmd)
	return imCmd
}

// mustJSONString marshals v to a JSON string, returning "{}" on error.
func mustJSONString(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func newTasksCmd() *cobra.Command {
	tasksCmd := &cobra.Command{
		Use:   "tasks",
		Short: "Feishu task commands",
	}

	createCmd := &cobra.Command{
		Use:   "create [summary]",
		Short: "Create a task with optional members",
		Long: `Create a Feishu task. Use --members to assign collaborators.

Members can be specified by name or user_id. Names are resolved automatically.
Prefix with "follower:" to add as follower instead of assignee.

Examples:
  larkctl tasks create "Fix bug" --members 张三
  larkctl tasks create "Review doc" --members 张三,follower:李四
  larkctl tasks create "Deploy" --members 12345,67890`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{"summary": args[0]}
			if v, _ := cmd.Flags().GetString("description"); v != "" {
				body["description"] = v
			}
			if v, _ := cmd.Flags().GetString("due"); v != "" {
				body["due"] = map[string]any{"timestamp": v}
			}
			if v, _ := cmd.Flags().GetStringSlice("members"); len(v) > 0 {
				members, err := resolveTaskMembers(cmd.Context(), client, v)
				if err != nil {
					return err
				}
				if len(members) > 0 {
					body["members"] = members
				}
			}
			data, err := client.CreateTask(cmd.Context(), body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	createCmd.Flags().String("description", "", "task description")
	createCmd.Flags().String("due", "", "due date in RFC3339 format")
	createCmd.Flags().StringSlice("members", nil, "task members: [role:]id_type:id (comma-separated)")

	listTasksCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			query := url.Values{"page_size": {strconv.Itoa(limit)}}
			data, err := client.ListTasks(cmd.Context(), query)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	listTasksCmd.Flags().Int("limit", 50, "maximum number of tasks to return")

	updateTaskCmd := &cobra.Command{
		Use:   "update [task_id]",
		Short: "Update a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			body := map[string]any{}
			if v, _ := cmd.Flags().GetString("summary"); v != "" {
				body["summary"] = v
			}
			if v, _ := cmd.Flags().GetString("description"); v != "" {
				body["description"] = v
			}
			if v, _ := cmd.Flags().GetString("due"); v != "" {
				body["due"] = map[string]any{"timestamp": parseToUnix(v)}
			}
			if len(body) == 0 {
				return fmt.Errorf("at least one of --summary, --description, or --due is required")
			}
			data, err := client.UpdateTask(cmd.Context(), args[0], body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	updateTaskCmd.Flags().String("summary", "", "task summary/title")
	updateTaskCmd.Flags().String("description", "", "task description")
	updateTaskCmd.Flags().String("due", "", "due date (YYYY-MM-DD or unix timestamp)")

	completeTaskCmd := &cobra.Command{
		Use:   "complete [task_id]",
		Short: "Mark a task as complete",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.CompleteTask(cmd.Context(), args[0], true)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	reopenTaskCmd := &cobra.Command{
		Use:   "reopen [task_id]",
		Short: "Reopen a completed task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.CompleteTask(cmd.Context(), args[0], false)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	commentTaskCmd := &cobra.Command{
		Use:   "comment [task_id] [text]",
		Short: "Add a comment to a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.CommentTask(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	assignCmd := &cobra.Command{
		Use:   "assign [task_id] [member_names...]",
		Short: "Add assignees to a task",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			members, err := resolveTaskMembers(cmd.Context(), client, args[1:])
			if err != nil {
				return err
			}
			data, err := client.ManageTaskMembers(cmd.Context(), args[0], "add", members)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	unassignCmd := &cobra.Command{
		Use:   "unassign [task_id] [member_names...]",
		Short: "Remove assignees from a task",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			members, err := resolveTaskMembers(cmd.Context(), client, args[1:])
			if err != nil {
				return err
			}
			data, err := client.ManageTaskMembers(cmd.Context(), args[0], "remove", members)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	followCmd := &cobra.Command{
		Use:   "follow [task_id] [member_names...]",
		Short: "Add followers to a task",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			rawMembers := make([]string, len(args[1:]))
			for i, m := range args[1:] {
				rawMembers[i] = "follower:" + m
			}
			members, err := resolveTaskMembers(cmd.Context(), client, rawMembers)
			if err != nil {
				return err
			}
			data, err := client.ManageTaskMembers(cmd.Context(), args[0], "add", members)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	unfollowCmd := &cobra.Command{
		Use:   "unfollow [task_id] [member_names...]",
		Short: "Remove followers from a task",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			rawMembers := make([]string, len(args[1:]))
			for i, m := range args[1:] {
				rawMembers[i] = "follower:" + m
			}
			members, err := resolveTaskMembers(cmd.Context(), client, rawMembers)
			if err != nil {
				return err
			}
			data, err := client.ManageTaskMembers(cmd.Context(), args[0], "remove", members)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	remindCmd := &cobra.Command{
		Use:   "remind [task_id] [minutes_before]",
		Short: "Add a reminder to a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			minutes, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("minutes_before must be a number: %w", err)
			}
			reminders := []map[string]any{{"relative_fire_minute": minutes}}
			data, err := client.ManageTaskReminders(cmd.Context(), args[0], "add", reminders)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	tasklistCreateCmd := &cobra.Command{
		Use:   "tasklist-create [name]",
		Short: "Create a new tasklist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.CreateTasklist(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	tasklistAddCmd := &cobra.Command{
		Use:   "tasklist-add [task_id] [tasklist_id]",
		Short: "Add a task to a tasklist",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.AddTaskToTasklist(cmd.Context(), args[0], args[1])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	tasklistMembersCmd := &cobra.Command{
		Use:   "tasklist-members [tasklist_id]",
		Short: "Add or remove members from a tasklist",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			addNames, _ := cmd.Flags().GetStringSlice("add")
			removeNames, _ := cmd.Flags().GetStringSlice("remove")
			role, _ := cmd.Flags().GetString("role")
			if role == "" {
				role = "editor"
			}
			if len(addNames) == 0 && len(removeNames) == 0 {
				return fmt.Errorf("at least one of --add or --remove is required")
			}
			if len(addNames) > 0 {
				var members []map[string]any
				for _, name := range addNames {
					ids, err := resolveUserIDs(cmd.Context(), client, []string{name})
					if err != nil {
						return err
					}
					for _, id := range ids {
						members = append(members, map[string]any{"id": id, "type": "user", "role": role})
					}
				}
				data, err := client.ManageTasklistMembers(cmd.Context(), args[0], "add", members)
				if err != nil {
					return err
				}
				printJSON(data)
			}
			if len(removeNames) > 0 {
				var members []map[string]any
				for _, name := range removeNames {
					ids, err := resolveUserIDs(cmd.Context(), client, []string{name})
					if err != nil {
						return err
					}
					for _, id := range ids {
						members = append(members, map[string]any{"id": id, "type": "user", "role": role})
					}
				}
				data, err := client.ManageTasklistMembers(cmd.Context(), args[0], "remove", members)
				if err != nil {
					return err
				}
				printJSON(data)
			}
			return nil
		},
	}
	tasklistMembersCmd.Flags().StringSlice("add", nil, "member names or IDs to add")
	tasklistMembersCmd.Flags().StringSlice("remove", nil, "member names or IDs to remove")
	tasklistMembersCmd.Flags().String("role", "editor", "role for added members: editor or viewer")

	tasksCmd.AddCommand(createCmd, listTasksCmd, updateTaskCmd, completeTaskCmd, reopenTaskCmd, commentTaskCmd,
		assignCmd, unassignCmd, followCmd, unfollowCmd, remindCmd,
		tasklistCreateCmd, tasklistAddCmd, tasklistMembersCmd)
	return tasksCmd
}

const (
	calendarScopes      = "calendar:calendar calendar:calendar:readonly"
	contactSearchScopes = "contact:user:search"
)

func newCalendarCmd() *cobra.Command {
	calendarCmd := &cobra.Command{
		Use:   "calendar",
		Short: "Feishu calendar commands (requires calendar scope, auto-authorized on first use)",
	}

	primaryCmd := &cobra.Command{
		Use:   "primary",
		Short: "Get primary calendar info",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, calendarScopes); err != nil {
				return err
			}
			data, err := client.GetCalendarPrimary(cmd.Context())
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List calendar events",
		Long: `List events from your primary calendar within a time range.

Examples:
  larkctl calendar list                                    # today's events
  larkctl calendar list --start 2026-03-20 --end 2026-03-21
  larkctl calendar list --days 7                           # next 7 days`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, calendarScopes); err != nil {
				return err
			}

			// Get primary calendar ID
			calID, err := getPrimaryCalendarID(cmd.Context(), client)
			if err != nil {
				return err
			}

			days, _ := cmd.Flags().GetInt("days")
			startStr, _ := cmd.Flags().GetString("start")
			endStr, _ := cmd.Flags().GetString("end")

			start, end := resolveTimeRange(startStr, endStr, days)

			data, err := client.ListCalendarEvents(cmd.Context(), calID, start, end)
			if err != nil {
				return err
			}
			printOutput(data)
			return nil
		},
	}
	listCmd.Flags().Int("days", 1, "number of days to list (from today)")
	listCmd.Flags().String("start", "", "start date (YYYY-MM-DD or unix timestamp)")
	listCmd.Flags().String("end", "", "end date (YYYY-MM-DD or unix timestamp)")

	createCmd := &cobra.Command{
		Use:   "create [summary]",
		Short: "Create a calendar event",
		Long: `Create a calendar event on your primary calendar.

Examples:
  larkctl calendar create "Team sync" --start "2026-03-20 14:00" --end "2026-03-20 15:00"
  larkctl calendar create "Lunch" --start "2026-03-20 12:00" --end "2026-03-20 13:00" --attendees 12345,67890
  larkctl calendar create "Meeting" --start "2026-03-20 10:00" --end "2026-03-20 11:00" --room omm_xxx`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, calendarScopes); err != nil {
				return err
			}

			calID, err := getPrimaryCalendarID(cmd.Context(), client)
			if err != nil {
				return err
			}

			startStr, _ := cmd.Flags().GetString("start")
			endStr, _ := cmd.Flags().GetString("end")
			if startStr == "" || endStr == "" {
				return fmt.Errorf("--start and --end are required")
			}

			body := map[string]any{
				"summary":    args[0],
				"start_time": map[string]any{"timestamp": parseToUnix(startStr)},
				"end_time":   map[string]any{"timestamp": parseToUnix(endStr)},
			}
			if v, _ := cmd.Flags().GetString("description"); v != "" {
				body["description"] = v
			}

			data, err := client.CreateCalendarEvent(cmd.Context(), calID, body)
			if err != nil {
				return err
			}

			// Add attendees and rooms if specified
			eventID := extractEventID(data)
			attendees, _ := cmd.Flags().GetStringSlice("attendees")
			room, _ := cmd.Flags().GetString("room")

			if eventID != "" && (len(attendees) > 0 || room != "") {
				// Resolve attendee names to user_ids
				resolved, err := resolveUserIDs(cmd.Context(), client, attendees)
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: failed to resolve attendees: %v\n", err)
				}
				var atts []map[string]any
				for _, uid := range resolved {
					atts = append(atts, map[string]any{"type": "user", "user_id": uid})
				}
				if room != "" {
					if err := requireScopes(cmd.Context(), client, "vc:room:readonly"); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: room scope upgrade failed: %v\n", err)
					}
					roomID, err := resolveRoomID(cmd.Context(), client, room)
					if err != nil {
						fmt.Fprintf(os.Stderr, "Warning: %v\n", err)
					} else {
						// Check room availability before booking
						startTS := parseToUnix(startStr)
						endTS := parseToUnix(endStr)
						busy, err := checkRoomBusy(cmd.Context(), client, roomID, startTS, endTS)
						if err != nil {
							fmt.Fprintf(os.Stderr, "Warning: unable to check room availability: %v\n", err)
						}
						if busy {
							return fmt.Errorf("meeting room is occupied during this time, use `larkctl calendar rooms` to find another room")
						}
						atts = append(atts, map[string]any{"type": "resource", "room_id": roomID})
					}
				}
				attData, err := client.AddCalendarAttendees(cmd.Context(), calID, eventID, map[string]any{"attendees": atts})
				if err != nil {
					fmt.Fprintf(os.Stderr, "Warning: event created but failed to add attendees: %v\n", err)
				} else {
					// Merge attendee info into output
					if m, ok := data.(map[string]any); ok {
						if am, ok := attData.(map[string]any); ok {
							m["attendees"] = am
						}
					}
				}
			}

			printJSON(data)
			return nil
		},
	}
	createCmd.Flags().String("start", "", "start time (YYYY-MM-DD HH:MM or unix timestamp)")
	createCmd.Flags().String("end", "", "end time (YYYY-MM-DD HH:MM or unix timestamp)")
	createCmd.Flags().String("description", "", "event description")
	createCmd.Flags().StringSlice("attendees", nil, "attendee user_ids (comma-separated)")
	createCmd.Flags().String("room", "", "meeting room name or ID (e.g. '1604' or 'omm_xxx')")

	freebusyCmd := &cobra.Command{
		Use:   "freebusy",
		Short: "Query free/busy status",
		Long: `Query free/busy status for a user or room.

Examples:
  larkctl calendar freebusy --user 12345 --start 2026-03-20 --end 2026-03-21
  larkctl calendar freebusy --room omm_xxx --start "2026-03-20 09:00" --end "2026-03-20 18:00"`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, calendarScopes); err != nil {
				return err
			}

			startStr, _ := cmd.Flags().GetString("start")
			endStr, _ := cmd.Flags().GetString("end")
			if startStr == "" || endStr == "" {
				return fmt.Errorf("--start and --end are required")
			}

			body := map[string]any{
				"time_min": parseToRFC3339(startStr),
				"time_max": parseToRFC3339(endStr),
			}
			if v, _ := cmd.Flags().GetString("user"); v != "" {
				body["user_id"] = v
			}
			if v, _ := cmd.Flags().GetString("room"); v != "" {
				body["room_id"] = v
			}

			data, err := client.GetFreebusy(cmd.Context(), body)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}
	freebusyCmd.Flags().String("start", "", "start time")
	freebusyCmd.Flags().String("end", "", "end time")
	freebusyCmd.Flags().String("user", "", "user_id to query")
	freebusyCmd.Flags().String("room", "", "room_id to query (omm_xxx)")

	roomsCmd := &cobra.Command{
		Use:   "rooms [keyword]",
		Short: "Search meeting rooms",
		Long: `Search available meeting rooms by keyword.

Examples:
  larkctl calendar rooms
  larkctl calendar rooms 1604
  larkctl calendar rooms C3`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, "vc:room:readonly"); err != nil {
				return err
			}
			keyword := ""
			if len(args) > 0 {
				keyword = args[0]
			}
			data, err := client.ListRooms(cmd.Context(), keyword)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	rsvpCmd := &cobra.Command{
		Use:   "rsvp [event_id] [accept|decline|tentative]",
		Short: "RSVP to a calendar event",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			eventID := args[0]
			rsvpStatus := args[1]
			switch rsvpStatus {
			case "accept", "decline", "tentative":
				// valid
			default:
				return fmt.Errorf("rsvp status must be one of: accept, decline, tentative")
			}

			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			if err := requireScopes(cmd.Context(), client, calendarScopes); err != nil {
				return err
			}

			calID, err := getPrimaryCalendarID(cmd.Context(), client)
			if err != nil {
				return err
			}

			data, err := client.RSVPCalendarEvent(cmd.Context(), calID, eventID, rsvpStatus)
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	calendarCmd.AddCommand(primaryCmd, listCmd, createCmd, freebusyCmd, roomsCmd, rsvpCmd)
	return calendarCmd
}

// getPrimaryCalendarID fetches and returns the user's primary calendar ID.
func getPrimaryCalendarID(ctx context.Context, client FeishuClient) (string, error) {
	data, err := client.GetCalendarPrimary(ctx)
	if err != nil {
		return "", fmt.Errorf("get primary calendar: %w", err)
	}
	m, _ := data.(map[string]any)
	calendars, _ := m["calendars"].([]any)
	for _, c := range calendars {
		cm, _ := c.(map[string]any)
		cal, _ := cm["calendar"].(map[string]any)
		if cal != nil {
			if id, _ := cal["calendar_id"].(string); id != "" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("primary calendar not found")
}

// resolveRoomID resolves a room name or ID to a room_id.
// If input starts with "omm_", it's used as-is. Otherwise search by keyword.
func resolveRoomID(ctx context.Context, client FeishuClient, input string) (string, error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "omm_") {
		return input, nil
	}
	data, err := client.ListRooms(ctx, input)
	if err != nil {
		return "", fmt.Errorf("search rooms: %w", err)
	}
	m, _ := data.(map[string]any)
	rooms, _ := m["rooms"].([]any)
	// Filter out disabled rooms
	for _, r := range rooms {
		rm, _ := r.(map[string]any)
		roomID, _ := rm["room_id"].(string)
		name, _ := rm["name"].(string)
		if roomID == "" {
			continue
		}
		// Check room_status.status (true = enabled)
		if status, ok := rm["room_status"].(map[string]any); ok {
			if enabled, ok := status["status"].(bool); ok && !enabled {
				fmt.Fprintf(os.Stderr, "=> Skipping disabled room: %s\n", name)
				continue
			}
		}
		fmt.Fprintf(os.Stderr, "=> Resolved room %q → %s (%s)\n", input, name, roomID)
		return roomID, nil
	}
	return "", fmt.Errorf("no available room found matching %q (all disabled or none found)", input)
}

// checkRoomBusy checks if a meeting room is occupied during the given time range.
func checkRoomBusy(ctx context.Context, client FeishuClient, roomID, startTS, endTS string) (bool, error) {
	data, err := client.GetFreebusy(ctx, map[string]any{
		"time_min": unixToRFC3339(startTS),
		"time_max": unixToRFC3339(endTS),
		"room_id":  roomID,
	})
	if err != nil {
		return false, err
	}
	m, _ := data.(map[string]any)
	list, _ := m["freebusy_list"].([]any)
	if len(list) > 0 {
		fmt.Fprintf(os.Stderr, "=> Room is busy during this time (%d conflicts)\n", len(list))
		return true, nil
	}
	fmt.Fprintf(os.Stderr, "=> Room is available\n")
	return false, nil
}

func extractEventID(data any) string {
	m, _ := data.(map[string]any)
	event, _ := m["event"].(map[string]any)
	if event == nil {
		return ""
	}
	id, _ := event["event_id"].(string)
	return id
}

// parseToUnix converts "YYYY-MM-DD", "YYYY-MM-DD HH:MM", or a raw unix timestamp string.
func parseToUnix(s string) string {
	s = strings.TrimSpace(s)
	// Already unix timestamp
	if len(s) > 0 && s[0] >= '0' && s[0] <= '9' && !strings.Contains(s, "-") {
		return s
	}
	for _, layout := range []string{
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return fmt.Sprintf("%d", t.Unix())
		}
	}
	return s
}

// parseToRFC3339 converts a date/time string to RFC3339 format.
func parseToRFC3339(s string) string {
	s = strings.TrimSpace(s)
	// Already RFC3339
	if strings.Contains(s, "T") && (strings.Contains(s, "+") || strings.HasSuffix(s, "Z")) {
		return s
	}
	// Try parsing as human-readable then format as RFC3339
	for _, layout := range []string{
		"2006-01-02 15:04",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t.Format(time.RFC3339)
		}
	}
	return s
}

// unixToRFC3339 converts a unix timestamp string to RFC3339 format.
func unixToRFC3339(ts string) string {
	var sec int64
	if _, err := fmt.Sscanf(ts, "%d", &sec); err != nil {
		return ts
	}
	return time.Unix(sec, 0).Format(time.RFC3339)
}

// resolveTimeRange resolves start/end strings and days flag into unix timestamps.
func resolveTimeRange(startStr, endStr string, days int) (string, string) {
	if startStr != "" && endStr != "" {
		return parseToUnix(startStr), parseToUnix(endStr)
	}
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	end := start.AddDate(0, 0, days)
	if startStr != "" {
		return parseToUnix(startStr), fmt.Sprintf("%d", end.Unix())
	}
	if endStr != "" {
		return fmt.Sprintf("%d", start.Unix()), parseToUnix(endStr)
	}
	return fmt.Sprintf("%d", start.Unix()), fmt.Sprintf("%d", end.Unix())
}

func newBoardCmd() *cobra.Command {
	boardCmd := &cobra.Command{
		Use:   "board",
		Short: "Feishu whiteboard/board commands",
	}

	nodesCmd := &cobra.Command{
		Use:   "nodes [whiteboard_id_or_url]",
		Short: "Get all nodes from a whiteboard",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := authedClient(gatewayURL)
			if err != nil {
				return err
			}
			data, err := client.GetBoardNodes(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			printJSON(data)
			return nil
		},
	}

	boardCmd.AddCommand(nodesCmd)
	return boardCmd
}

func newMCPCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp",
		Short: "Show MCP SSE endpoint on the gateway",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := LoadSessionToken(gatewayURL)
			if err != nil {
				return fmt.Errorf("not logged in, run 'larkctl login' first: %w", err)
			}
			base := strings.TrimRight(gatewayURL, "/")
			sseURL := base + "/mcp/sse?token=" + token
			fmt.Println("MCP SSE endpoint:")
			fmt.Println(sseURL)
			fmt.Println()
			fmt.Println("Configure your MCP client (e.g. Claude Desktop) with this URL.")
			return nil
		},
	}
}

// requireScopes ensures the current session has the requested scopes.
// If not, opens a browser for the user to authorize, then polls until complete.
func requireScopes(ctx context.Context, client FeishuClient, scopes string) error {
	gc, ok := client.(*GatewayClient)
	if !ok {
		return nil // local mode: scopes managed by app config
	}

	resp, err := gc.UpgradeScopes(ctx, scopes)
	if err != nil {
		return fmt.Errorf("check scopes: %w", err)
	}

	needed, _ := resp["upgrade_needed"].(bool)
	if !needed {
		return nil
	}

	upgradeURL, _ := resp["upgrade_url"].(string)
	if upgradeURL == "" {
		return fmt.Errorf("scope upgrade required but no URL returned")
	}

	fmt.Fprintf(os.Stderr, "=> Additional authorization required, opening browser...\n")
	_ = tryOpenBrowser(upgradeURL)
	fmt.Fprintf(os.Stderr, "=> Waiting for authorization (or open manually): %s\n", upgradeURL)

	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		current, err := gc.GetSessionScopes(ctx)
		if err != nil {
			continue
		}
		if hasAllScopes(current, scopes) {
			fmt.Fprintf(os.Stderr, "=> Scopes upgraded successfully\n")
			return nil
		}
	}
	return fmt.Errorf("scope upgrade timed out, please try again")
}

// hasAllScopes returns true if `have` contains all scopes in `need`.
func hasAllScopes(have, need string) bool {
	haveSet := map[string]bool{}
	for _, s := range strings.Fields(have) {
		haveSet[s] = true
	}
	for _, s := range strings.Fields(need) {
		if !haveSet[s] {
			return false
		}
	}
	return true
}

func authedClient(baseURL string) (FeishuClient, error) {
	if IsLocalMode() {
		return loadLocalClient()
	}
	token, err := LoadSessionToken(baseURL)
	if err != nil {
		return nil, err
	}
	client := NewGatewayClient(baseURL)
	client.SetSessionToken(token)
	return client, nil
}

func loadLocalClient() (*LocalClient, error) {
	appID, appSecret, err := LoadLocalApp()
	if err != nil {
		return nil, err
	}
	client := NewLocalClient(appID, appSecret)
	access, refresh, expireAt, err := LoadLocalTokens()
	if err != nil {
		return nil, err
	}
	client.SetTokens(access, refresh, expireAt)
	return client, nil
}

// browserLaunchSuppressed reports whether launching a browser must be skipped.
// The scope-upgrade flow opens URLs with no flag to turn it off, so a test that
// exercises it would otherwise hijack the developer's browser.
func browserLaunchSuppressed() bool {
	return flag.Lookup("test.v") != nil
}

func tryOpenBrowser(rawURL string) error {
	if browserLaunchSuppressed() {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "open", rawURL)
	case "linux":
		cmd = exec.CommandContext(ctx, "xdg-open", rawURL)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		return nil
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, string(out))
	}
	return nil
}

func printJSON(v any) {
	var b []byte
	if compactOutput {
		b, _ = json.Marshal(v)
	} else {
		b, _ = json.MarshalIndent(v, "", "  ")
	}
	fmt.Println(string(b))
}

func envOrDefault(key, defaultVal string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return defaultVal
}

// Base URL `larkctl upgrade` downloads binaries from. Defaults to the GitHub
// Release assets of this repo (raw per-platform binaries published by
// .github/workflows/release.yml). Deployments that serve their own binaries
// (e.g. via the gateway's /install flow) override it with LARKCTL_DOWNLOAD_URL.
var downloadBaseURL = envOrDefault("LARKCTL_DOWNLOAD_URL", "https://github.com/echowxsy/larkctl/releases/latest/download")

func newUpgradeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade larkctl to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			goos := runtime.GOOS
			goarch := runtime.GOARCH

			binary := fmt.Sprintf("larkctl-%s-%s", goos, goarch)
			if goos == "windows" {
				binary += ".exe"
			}
			dlURL := downloadBaseURL + "/" + binary

			self, err := os.Executable()
			if err != nil {
				return fmt.Errorf("resolve executable path: %w", err)
			}
			self, err = resolveSymlink(self)
			if err != nil {
				return fmt.Errorf("resolve symlink: %w", err)
			}

			tmpFile := self + ".tmp"
			fmt.Fprintf(os.Stderr, "Downloading %s ...\n", dlURL)

			resp, err := http.Get(dlURL)
			if err != nil {
				return fmt.Errorf("download: %w", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
			}

			f, err := os.OpenFile(tmpFile, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0755)
			if err != nil {
				return fmt.Errorf("create temp file: %w", err)
			}

			n, err := io.Copy(f, resp.Body)
			f.Close()
			if err != nil {
				os.Remove(tmpFile)
				return fmt.Errorf("write: %w", err)
			}

			if err := os.Rename(tmpFile, self); err != nil {
				os.Remove(tmpFile)
				return fmt.Errorf("replace binary: %w", err)
			}

			fmt.Fprintf(os.Stderr, "Upgraded %s (%d bytes)\n", self, n)
			return nil
		},
	}
}

func resolveSymlink(path string) (string, error) {
	for i := 0; i < 10; i++ {
		fi, err := os.Lstat(path)
		if err != nil {
			return path, err
		}
		if fi.Mode()&os.ModeSymlink == 0 {
			return path, nil
		}
		target, err := os.Readlink(path)
		if err != nil {
			return path, err
		}
		if !strings.HasPrefix(target, "/") {
			target = strings.TrimRight(path[:strings.LastIndex(path, "/")+1], "/") + "/" + target
		}
		path = target
	}
	return path, nil
}

// readJSONInput reads JSON from args[argIndex] (file path or "-" for stdin).
// If argIndex is out of range, reads from stdin.
func readJSONInput(args []string, argIndex int, out any) error {
	var r io.Reader
	if argIndex < len(args) && args[argIndex] != "-" {
		f, err := os.Open(args[argIndex])
		if err != nil {
			return fmt.Errorf("open input file: %w", err)
		}
		defer f.Close()
		r = f
	} else {
		r = os.Stdin
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("read input: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("parse JSON input: %w", err)
	}
	return nil
}

func resolveGatewayURL(cmd *cobra.Command) error {
	if cmd != nil {
		flag := cmd.Flags().Lookup("gateway-url")
		if flag != nil && flag.Changed {
			gatewayURL = normalizeBaseURL(gatewayURL)
			return nil
		}
	}

	if envURL := strings.TrimSpace(os.Getenv("FEISHU_GATEWAY_URL")); envURL != "" {
		gatewayURL = normalizeBaseURL(envURL)
		return nil
	}

	cfgURL, err := LoadGatewayURL()
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfgURL) != "" {
		gatewayURL = normalizeBaseURL(cfgURL)
		return nil
	}

	gatewayURL = normalizeBaseURL(defaultGatewayURL)
	return nil
}
