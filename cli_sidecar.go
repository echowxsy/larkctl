package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/spf13/cobra"
)

// `larkctl cli` — the full lark-cli command set, authenticated through the gateway.
//
// lark-cli built with its `authsidecar` tag sends every authenticated request to
// a loopback "auth sidecar" instead of Feishu: the request carries a placeholder
// token and is HMAC-signed with a key shared through the environment. larkctl is
// that sidecar. It verifies the signature, relays the request to the gateway's
// /v1/cli/proxy under the user's larkctl session, and the gateway injects the
// real user access token. lark-cli never holds a Feishu credential, and every
// call passes the gateway's audit log, security-level gate and IM whitelist.

const (
	// larkCLIVersion is the upstream lark-cli release the runtime is built from.
	// Bumping it changes the asset larkctl downloads; the release script reads it.
	larkCLIVersion = "v1.0.95"

	sidecarProtocolV1   = "v1"
	sidecarMaxDriftSecs = 60
	// sidecarMaxBody bounds a relayed request body. lark-cli buffers the whole
	// body to sign it, so the sidecar holds it in memory too.
	sidecarMaxBody = 100 << 20
	// sidecarInspectBody is how much of a JSON response is read to spot a
	// missing-scope error. Error bodies are tiny; larger responses stream through.
	sidecarInspectBody = 1 << 20

	// Feishu error codes the sidecar reads or produces.
	feishuCodeUserScopeInsufficient = 99991679
	feishuCodeInvalidAccessToken    = 99991663
)

// Sidecar wire headers (lark-cli's sidecar protocol v1).
const (
	sidecarHeaderVersion    = "X-Lark-Proxy-Version"
	sidecarHeaderTarget     = "X-Lark-Proxy-Target"
	sidecarHeaderIdentity   = "X-Lark-Proxy-Identity"
	sidecarHeaderSignature  = "X-Lark-Proxy-Signature"
	sidecarHeaderTimestamp  = "X-Lark-Proxy-Timestamp"
	sidecarHeaderBodySHA256 = "X-Lark-Body-SHA256"
	sidecarHeaderAuthHeader = "X-Lark-Proxy-Auth-Header"
)

// sidecarCanonical holds the fields covered by the request signature. The field
// order of canonicalString is part of lark-cli's protocol.
type sidecarCanonical struct {
	Version, Method, Host, PathAndQuery, BodySHA256, Timestamp, Identity, AuthHeader string
}

func (c sidecarCanonical) canonicalString() string {
	return strings.Join([]string{
		c.Version, c.Method, c.Host, c.PathAndQuery, c.BodySHA256, c.Timestamp, c.Identity, c.AuthHeader,
	}, "\n")
}

func (c sidecarCanonical) sign(key []byte) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(c.canonicalString()))
	return hex.EncodeToString(mac.Sum(nil))
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// cliSidecar is the loopback auth sidecar lark-cli talks to.
type cliSidecar struct {
	key     []byte
	gateway *GatewayClient
	http    *http.Client
	now     func() time.Time

	mu            sync.Mutex
	missingScopes map[string]bool
}

func newCLISidecar(key []byte, gateway *GatewayClient) *cliSidecar {
	return &cliSidecar{
		key:     key,
		gateway: gateway,
		// No overall timeout: downloads and uploads can run long. lark-cli's own
		// request timeout and the process lifetime bound each call.
		http:          &http.Client{},
		now:           time.Now,
		missingScopes: map[string]bool{},
	}
}

// MissingScopes returns the user scopes Feishu reported as not granted.
func (s *cliSidecar) MissingScopes() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.missingScopes))
	for scope := range s.missingScopes {
		out = append(out, scope)
	}
	sort.Strings(out)
	return out
}

func (s *cliSidecar) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, sidecarMaxBody+1))
	if err != nil {
		writeFeishuError(w, http.StatusBadRequest, 400, "read request body: "+err.Error())
		return
	}
	if len(body) > sidecarMaxBody {
		writeFeishuError(w, http.StatusRequestEntityTooLarge, 413, "request body exceeds 100MB")
		return
	}
	if status, msg := s.verify(r, body); msg != "" {
		writeFeishuError(w, status, status, msg)
		return
	}

	resp, err := s.relay(r, body)
	if err != nil {
		writeFeishuError(w, http.StatusBadGateway, 502, "larkctl gateway unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	s.writeResponse(w, resp)
}

// verify checks the request against lark-cli's sidecar protocol. It returns a
// status and message when the request is rejected.
func (s *cliSidecar) verify(r *http.Request, body []byte) (int, string) {
	if v := r.Header.Get(sidecarHeaderVersion); v != sidecarProtocolV1 {
		return http.StatusBadRequest, fmt.Sprintf("unsupported sidecar protocol %q; upgrade larkctl", v)
	}
	bodySHA := sha256Hex(body)
	if r.Header.Get(sidecarHeaderBodySHA256) != bodySHA {
		return http.StatusUnauthorized, "request body does not match its signed digest"
	}
	ts, err := strconv.ParseInt(r.Header.Get(sidecarHeaderTimestamp), 10, 64)
	if err != nil || math.Abs(float64(s.now().Unix()-ts)) > sidecarMaxDriftSecs {
		return http.StatusUnauthorized, "request timestamp missing or outside the allowed window"
	}
	target := r.Header.Get(sidecarHeaderTarget)
	host := target
	if i := strings.Index(host, "://"); i >= 0 {
		host = host[i+3:]
	}
	want := sidecarCanonical{
		Version:      sidecarProtocolV1,
		Method:       r.Method,
		Host:         host,
		PathAndQuery: r.URL.RequestURI(),
		BodySHA256:   bodySHA,
		Timestamp:    r.Header.Get(sidecarHeaderTimestamp),
		Identity:     r.Header.Get(sidecarHeaderIdentity),
		AuthHeader:   r.Header.Get(sidecarHeaderAuthHeader),
	}.sign(s.key)
	if !hmac.Equal([]byte(want), []byte(r.Header.Get(sidecarHeaderSignature))) {
		return http.StatusUnauthorized, "request signature does not verify"
	}
	if id := r.Header.Get(sidecarHeaderIdentity); id != "user" {
		return http.StatusForbidden, "larkctl cli runs lark-cli with user identity only; --as bot is not available"
	}
	return 0, ""
}

// relay forwards a verified request to the gateway. A request that fails only
// because the gateway session needs refreshing is retried once after a refresh.
func (s *cliSidecar) relay(r *http.Request, body []byte) (*http.Response, error) {
	send := func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(r.Context(), r.Method,
			s.gateway.baseURL+cliProxyPath+r.URL.RequestURI(), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for k, vs := range r.Header {
			if sidecarDroppedHeaders[http.CanonicalHeaderKey(k)] {
				continue
			}
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
		req.Header.Set("Authorization", "Bearer "+s.gateway.session())
		return s.http.Do(req)
	}

	resp, err := send()
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}
	head, _ := io.ReadAll(io.LimitReader(resp.Body, sidecarInspectBody))
	resp.Body.Close()
	if code := gatewayErrorCode(head); code != "session_expired" && code != "access_token_refresh_failed" {
		resp.Body = io.NopCloser(bytes.NewReader(head))
		return resp, nil
	}
	if err := s.gateway.RefreshSession(r.Context()); err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(head))
		return resp, nil
	}
	return send()
}

// writeResponse streams the gateway's answer back to lark-cli. Small JSON bodies
// are inspected first: a Feishu missing-scope error is recorded so larkctl can
// upgrade the session, and a gateway-level error is rewritten into Feishu's
// shape, which is the only error shape lark-cli knows how to report.
func (s *cliSidecar) writeResponse(w http.ResponseWriter, resp *http.Response) {
	var head []byte
	isJSON := strings.Contains(resp.Header.Get("Content-Type"), "json")
	if isJSON {
		head, _ = io.ReadAll(io.LimitReader(resp.Body, sidecarInspectBody+1))
	}
	if isJSON && len(head) <= sidecarInspectBody {
		s.recordMissingScopes(head)
		if code := gatewayErrorCode(head); code != "" {
			var env struct {
				Message string `json:"message"`
			}
			_ = json.Unmarshal(head, &env)
			feishuCode := resp.StatusCode
			if resp.StatusCode == http.StatusUnauthorized {
				feishuCode = feishuCodeInvalidAccessToken
			}
			writeFeishuError(w, resp.StatusCode, feishuCode,
				fmt.Sprintf("larkctl gateway: %s (%s); run `larkctl login` if your session has expired", env.Message, code))
			return
		}
	}

	for k, vs := range resp.Header {
		if sidecarDroppedResponseHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(head)
	_, _ = io.Copy(w, resp.Body)
}

// recordMissingScopes notes the scopes a Feishu 99991679 error names in
// error.permission_violations[].subject.
func (s *cliSidecar) recordMissingScopes(body []byte) {
	var resp struct {
		Code  int `json:"code"`
		Error struct {
			PermissionViolations []struct {
				Subject string `json:"subject"`
			} `json:"permission_violations"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &resp) != nil || resp.Code != feishuCodeUserScopeInsufficient {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range resp.Error.PermissionViolations {
		if scope := strings.TrimSpace(v.Subject); scope != "" {
			s.missingScopes[scope] = true
		}
	}
}

// gatewayErrorCode returns the code of a gateway error envelope. Gateway codes
// are strings ("session_expired"); Feishu's are numbers, so a Feishu response
// never matches.
func gatewayErrorCode(body []byte) string {
	var env struct {
		Code any `json:"code"`
	}
	if json.Unmarshal(body, &env) != nil {
		return ""
	}
	code, ok := env.Code.(string)
	if !ok || code == "ok" {
		return ""
	}
	return code
}

func writeFeishuError(w http.ResponseWriter, status, code int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "msg": msg})
}

const cliProxyPath = "/v1/cli/proxy"

// Headers consumed on the lark-cli → sidecar hop. The target, identity and
// token-header fields are kept: the gateway enforces them again.
var sidecarDroppedHeaders = map[string]bool{
	"Authorization": true, "Host": true, "Connection": true, "Keep-Alive": true,
	"Proxy-Connection": true, "Te": true, "Trailer": true, "Transfer-Encoding": true, "Upgrade": true,
	"X-Lark-Proxy-Version": true, "X-Lark-Proxy-Signature": true, "X-Lark-Proxy-Timestamp": true,
	"X-Lark-Body-Sha256": true, "Content-Length": true,
}

var sidecarDroppedResponseHeaders = map[string]bool{
	"Connection": true, "Keep-Alive": true, "Te": true, "Trailer": true,
	"Transfer-Encoding": true, "Upgrade": true, "Set-Cookie": true,
}

// ── command ──

// exitCodeError carries a child process's exit status out through cobra, so
// `larkctl cli` exits exactly as lark-cli did.
type exitCodeError struct{ code int }

func (e *exitCodeError) Error() string { return fmt.Sprintf("lark-cli exited with status %d", e.code) }

func newCLICmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cli [lark-cli arguments...]",
		Short: "Run any lark-cli command through the gateway (user identity)",
		Long: `Run the full lark-cli command set — every service, shortcut and the generic
` + "`api`" + ` command — authenticated through the larkctl gateway.

lark-cli runs without any Feishu credential of its own: larkctl signs you in with
your gateway session, and every call passes the gateway's audit log and access
gates. Only user identity is available; --as bot is refused.

The lark-cli runtime (` + larkCLIVersion + `, built with sidecar support) is downloaded
to ~/.lark/bin on first use. Set LARKCTL_LARK_CLI to use your own build.

If a command needs a scope your session lacks, larkctl opens the browser to grant
it and runs the command again.

Examples:
  larkctl cli --help
  larkctl cli calendar +agenda
  larkctl cli okr +cycle-list
  larkctl cli api GET /open-apis/im/v1/chats`,
		DisableFlagParsing: true,
		SilenceErrors:      true,
		SilenceUsage:       true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := runLarkCLI(cmd.Context(), args); err != nil {
				var exitErr *exitCodeError
				if !errors.As(err, &exitErr) {
					fmt.Fprintln(os.Stderr, "Error:", err)
				}
				return err
			}
			return nil
		},
	}
}

func runLarkCLI(ctx context.Context, args []string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	client, err := authedClient(gatewayURL)
	if err != nil {
		return err
	}
	gc, ok := client.(*GatewayClient)
	if !ok {
		return errors.New("larkctl cli runs through the gateway, but larkctl is in local mode (app credentials in ~/.lark/config.json)")
	}
	// Surfaces an expired login up front, and refreshes the gateway session so
	// the first relayed call does not have to.
	if _, err := gc.GetSessionScopes(ctx); err != nil {
		return fmt.Errorf("gateway session: %w (run `larkctl login`)", err)
	}

	bin, err := ensureLarkCLIRuntime(ctx)
	if err != nil {
		return err
	}
	configDir, err := resolveLarkHomeDir()
	if err != nil {
		return err
	}
	configDir = filepath.Join(configDir, "lark-cli")

	code, missing, err := runLarkCLIOnce(ctx, bin, args, gc, configDir)
	if err != nil {
		return err
	}
	if code != 0 && len(missing) > 0 {
		fmt.Fprintf(os.Stderr, "=> This command needs scope(s) your session has not granted: %s\n", strings.Join(missing, " "))
		if err := requireScopes(ctx, client, strings.Join(missing, " ")); err != nil {
			return err
		}
		if !stdinReplayable() {
			// Piped input was consumed by the first run and cannot be replayed.
			fmt.Fprintln(os.Stderr, "=> Scopes granted. Re-run the command to continue.")
			return &exitCodeError{code: code}
		}
		fmt.Fprintln(os.Stderr, "=> Retrying...")
		code, _, err = runLarkCLIOnce(ctx, bin, args, gc, configDir)
		if err != nil {
			return err
		}
	}
	if code != 0 {
		return &exitCodeError{code: code}
	}
	return nil
}

// runLarkCLIOnce runs lark-cli behind a fresh sidecar and returns its exit code
// and the scopes Feishu reported missing during the run.
func runLarkCLIOnce(ctx context.Context, bin string, args []string, gc *GatewayClient, configDir string) (int, []string, error) {
	keyBytes := make([]byte, 32)
	if _, err := rand.Read(keyBytes); err != nil {
		return 0, nil, fmt.Errorf("generate sidecar key: %w", err)
	}
	key := hex.EncodeToString(keyBytes)
	sidecar := newCLISidecar([]byte(key), gc)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, nil, fmt.Errorf("start sidecar: %w", err)
	}
	srv := &http.Server{Handler: sidecar, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	child := exec.CommandContext(ctx, bin, args...) //nolint:gosec // bin is the pinned runtime or an explicit LARKCTL_LARK_CLI override
	child.Env = larkCLIEnv(os.Environ(), "http://"+ln.Addr().String(), key, configDir)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr

	// Ctrl-C reaches lark-cli directly (same process group); larkctl must stay
	// alive to serve its last requests and report its exit status.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)

	err = child.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, sidecar.MissingScopes(), nil
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), sidecar.MissingScopes(), nil
	default:
		return 0, nil, fmt.Errorf("run lark-cli %s: %w", bin, err)
	}
}

// larkCLIEnv builds lark-cli's environment for sidecar mode. Every inherited
// LARKSUITE_CLI_* variable is dropped first: a user's own lark-cli settings
// (a profile, a raw access token, another app) must not leak into a run that is
// supposed to go through the gateway.
func larkCLIEnv(base []string, proxyAddr, key, configDir string) []string {
	env := make([]string, 0, len(base)+10)
	for _, kv := range base {
		if !strings.HasPrefix(kv, "LARKSUITE_CLI_") {
			env = append(env, kv)
		}
	}
	return append(env,
		"LARKSUITE_CLI_AUTH_PROXY="+proxyAddr,
		"LARKSUITE_CLI_PROXY_KEY="+key,
		// lark-cli requires an app id in sidecar mode; the gateway supplies the
		// real app, so this only labels lark-cli's local caches.
		"LARKSUITE_CLI_APP_ID=larkctl-gateway",
		"LARKSUITE_CLI_BRAND=feishu",
		"LARKSUITE_CLI_STRICT_MODE=user",
		"LARKSUITE_CLI_DEFAULT_AS=user",
		"LARKSUITE_CLI_CONFIG_DIR="+configDir,
		// Upgrades come with larkctl; lark-cli's own notices would point at npm.
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
		"LARKSUITE_CLI_NO_SKILLS_NOTIFIER=1",
	)
}

// stdinReplayable reports whether a failed run can be run again with the same
// input. Piped or redirected input has been consumed by the first run; a
// character device — a terminal, or /dev/null — gives the retry what the first
// run had. A variable so tests can exercise both paths.
var stdinReplayable = func() bool {
	fi, err := os.Stdin.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// ── runtime provisioning ──

// larkCLIAssetName is the release asset holding the sidecar-enabled lark-cli.
// The upstream version is in the name so a larkctl release that bumps it never
// reuses a cached runtime built from an older tag.
func larkCLIAssetName(goos, goarch string) string {
	name := fmt.Sprintf("lark-cli_%s_%s_%s", larkCLIVersion, goos, goarch)
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// ensureLarkCLIRuntime returns the lark-cli binary to run, downloading and
// verifying it on first use.
func ensureLarkCLIRuntime(ctx context.Context) (string, error) {
	if p := strings.TrimSpace(os.Getenv("LARKCTL_LARK_CLI")); p != "" {
		return p, nil
	}
	home, err := resolveLarkHomeDir()
	if err != nil {
		return "", err
	}
	name := larkCLIAssetName(runtime.GOOS, runtime.GOARCH)
	path := filepath.Join(home, "bin", name)
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		return path, nil
	}

	fmt.Fprintf(os.Stderr, "=> Downloading lark-cli %s runtime (first use)...\n", larkCLIVersion)
	if err := downloadVerifiedAsset(ctx, downloadBaseURL, name, path); err != nil {
		return "", fmt.Errorf("install lark-cli runtime: %w\n"+
			"  If this larkctl is older than the latest release, run `larkctl upgrade`.\n"+
			"  Or build it yourself: go install -tags authsidecar github.com/larksuite/cli@%s\n"+
			"  and point LARKCTL_LARK_CLI at the binary", err, larkCLIVersion)
	}
	return path, nil
}

// downloadVerifiedAsset fetches name from baseURL, checks it against the
// release's checksums.txt, and installs it at dest as an executable.
func downloadVerifiedAsset(ctx context.Context, baseURL, name, dest string) error {
	sums, err := fetchURL(ctx, baseURL+"/checksums.txt", 1<<20)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}
	want := ""
	scanner := bufio.NewScanner(bytes.NewReader(sums))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			want = strings.ToLower(fields[0])
			break
		}
	}
	if want == "" {
		return fmt.Errorf("%s is not listed in the release checksums", name)
	}

	data, err := fetchURL(ctx, baseURL+"/"+name, 200<<20)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", name, err)
	}
	if got := sha256Hex(data); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", name, got, want)
	}
	return writeFileAtomic(dest, data, 0o755)
}

func fetchURL(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return data, nil
}
