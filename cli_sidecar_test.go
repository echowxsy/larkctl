package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// The signature vector below was produced with lark-cli's own
// sidecar.Sign (github.com/larksuite/cli/sidecar, v1.0.95). If lark-cli
// changes its canonical string without bumping the protocol version, this is
// the test that notices.
func TestSidecarSignatureMatchesUpstreamVector(t *testing.T) {
	key := []byte("3f1c9e2a7b6d4c8e0f5a1b2c3d4e5f60718293a4b5c6d7e8f90a1b2c3d4e5f6a")
	body := []byte(`{"receive_id":"oc_x","content":"{\"text\":\"中文\"}"}`)
	c := sidecarCanonical{
		Version:      "v1",
		Method:       "POST",
		Host:         "open.feishu.cn",
		PathAndQuery: "/open-apis/im/v1/messages?receive_id_type=chat_id",
		BodySHA256:   sha256Hex(body),
		Timestamp:    "1789000000",
		Identity:     "user",
		AuthHeader:   "Authorization",
	}
	if c.BodySHA256 != "8bfefb1ad53e1a95e617fb2019cee06770aea1e972a558d35ceda267623f9492" {
		t.Fatalf("body digest = %s", c.BodySHA256)
	}
	if got := c.sign(key); got != "ce5a9288ff8d8185ad8ae358176cc633d9292959c3770f0e94d4f9b68e2b07b1" {
		t.Fatalf("signature = %s, want upstream vector", got)
	}
	if got := sha256Hex(nil); got != "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855" {
		t.Fatalf("empty body digest = %s", got)
	}
}

// signedSidecarRequest builds a request exactly as lark-cli's sidecar
// interceptor sends it.
func signedSidecarRequest(t *testing.T, key []byte, method, pathAndQuery, body, identity string, ts time.Time) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, pathAndQuery, strings.NewReader(body))
	digest := sha256Hex([]byte(body))
	stamp := strconv.FormatInt(ts.Unix(), 10)
	req.Header.Set(sidecarHeaderVersion, "v1")
	req.Header.Set(sidecarHeaderTarget, "https://open.feishu.cn")
	req.Header.Set(sidecarHeaderIdentity, identity)
	req.Header.Set(sidecarHeaderAuthHeader, "Authorization")
	req.Header.Set(sidecarHeaderBodySHA256, digest)
	req.Header.Set(sidecarHeaderTimestamp, stamp)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(sidecarHeaderSignature, sidecarCanonical{
		Version: "v1", Method: method, Host: "open.feishu.cn", PathAndQuery: pathAndQuery,
		BodySHA256: digest, Timestamp: stamp, Identity: identity, AuthHeader: "Authorization",
	}.sign(key))
	return req
}

// fakeCLIGateway records what the sidecar relays to /v1/cli/proxy.
type fakeCLIGateway struct {
	mu       sync.Mutex
	proxied  []*http.Request
	bodies   []string
	upgrades []string
	refresh  int
	// proxy answers /v1/cli/proxy; n is the 1-based call count.
	proxy func(w http.ResponseWriter, r *http.Request, n int)
}

func (g *fakeCLIGateway) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(cliProxyPath+"/", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		g.mu.Lock()
		g.proxied = append(g.proxied, r.Clone(r.Context()))
		g.bodies = append(g.bodies, string(body))
		n := len(g.proxied)
		g.mu.Unlock()
		if g.proxy != nil {
			g.proxy(w, r, n)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"success","data":{"ok":true}}`))
	})
	mux.HandleFunc("/v1/session/refresh", func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.refresh++
		g.mu.Unlock()
		_, _ = w.Write([]byte(`{"code":"ok","data":{}}`))
	})
	mux.HandleFunc("/v1/session/scopes", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"ok","data":{"scopes":"offline_access"}}`))
	})
	mux.HandleFunc("/v1/session/upgrade", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Scopes string `json:"scopes"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		g.mu.Lock()
		g.upgrades = append(g.upgrades, req.Scopes)
		g.mu.Unlock()
		// Already granted by the time the client polls: no browser round-trip.
		_, _ = w.Write([]byte(`{"code":"ok","data":{"upgrade_needed":false}}`))
	})
	return mux
}

func newTestSidecar(t *testing.T, gw *fakeCLIGateway) (*cliSidecar, []byte) {
	t.Helper()
	srv := httptest.NewServer(gw.handler())
	t.Cleanup(srv.Close)
	client := NewGatewayClient(srv.URL)
	client.SetSessionToken("client-token")
	key := []byte("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	return newCLISidecar(key, client), key
}

func serveSidecar(s *cliSidecar, req *http.Request) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	s.ServeHTTP(rr, req)
	return rr
}

func TestSidecarRelaysVerifiedRequest(t *testing.T) {
	gw := &fakeCLIGateway{}
	sc, key := newTestSidecar(t, gw)
	body := `{"receive_id":"oc_x","msg_type":"text","content":"{\"text\":\"中文\"}"}`
	rr := serveSidecar(sc, signedSidecarRequest(t, key, "POST", "/open-apis/im/v1/messages?receive_id_type=chat_id", body, "user", time.Now()))

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), `"ok":true`) {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if len(gw.proxied) != 1 {
		t.Fatalf("gateway calls = %d", len(gw.proxied))
	}
	got := gw.proxied[0]
	if got.URL.RequestURI() != "/v1/cli/proxy/open-apis/im/v1/messages?receive_id_type=chat_id" {
		t.Errorf("relayed to %s", got.URL.RequestURI())
	}
	if got.Header.Get("Authorization") != "Bearer client-token" {
		t.Errorf("Authorization = %q, want the larkctl session", got.Header.Get("Authorization"))
	}
	if gw.bodies[0] != body {
		t.Errorf("body altered: %s", gw.bodies[0])
	}
	// The gateway re-checks these, so they travel on.
	for _, h := range []string{sidecarHeaderTarget, sidecarHeaderIdentity, sidecarHeaderAuthHeader} {
		if got.Header.Get(h) == "" {
			t.Errorf("header %s not relayed", h)
		}
	}
	// Signing material is spent on this hop.
	for _, h := range []string{sidecarHeaderSignature, sidecarHeaderTimestamp, sidecarHeaderVersion, sidecarHeaderBodySHA256} {
		if v := got.Header.Get(h); v != "" {
			t.Errorf("header %s leaked to the gateway: %q", h, v)
		}
	}
}

func TestSidecarRejectsUnverifiableRequests(t *testing.T) {
	gw := &fakeCLIGateway{}
	sc, key := newTestSidecar(t, gw)

	cases := map[string]func() *http.Request{
		"wrong key": func() *http.Request {
			return signedSidecarRequest(t, []byte("not-the-key"), "GET", "/open-apis/im/v1/chats", "", "user", time.Now())
		},
		"tampered body": func() *http.Request {
			req := signedSidecarRequest(t, key, "POST", "/open-apis/im/v1/messages", `{"a":1}`, "user", time.Now())
			req.Body = io.NopCloser(strings.NewReader(`{"a":2}`))
			return req
		},
		"tampered path": func() *http.Request {
			req := signedSidecarRequest(t, key, "GET", "/open-apis/im/v1/chats", "", "user", time.Now())
			req.URL.Path = "/open-apis/im/v1/messages"
			return req
		},
		"stale timestamp": func() *http.Request {
			return signedSidecarRequest(t, key, "GET", "/open-apis/im/v1/chats", "", "user", time.Now().Add(-5*time.Minute))
		},
		"unknown protocol version": func() *http.Request {
			req := signedSidecarRequest(t, key, "GET", "/open-apis/im/v1/chats", "", "user", time.Now())
			req.Header.Set(sidecarHeaderVersion, "v2")
			return req
		},
		"bot identity": func() *http.Request {
			return signedSidecarRequest(t, key, "GET", "/open-apis/bot/v3/info", "", "bot", time.Now())
		},
	}
	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			rr := serveSidecar(sc, build())
			if rr.Code < 400 {
				t.Fatalf("status %d, want rejection", rr.Code)
			}
			var feishu struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &feishu); err != nil || feishu.Code == 0 || feishu.Msg == "" {
				t.Fatalf("rejection is not Feishu-shaped: %s", rr.Body.String())
			}
		})
	}
	if len(gw.proxied) != 0 {
		t.Fatalf("rejected requests reached the gateway %d times", len(gw.proxied))
	}
}

func TestSidecarRecordsMissingScopes(t *testing.T) {
	scopeErr := `{"code":99991679,"msg":"user not authorized","error":{"permission_violations":[{"subject":"okr:okr.period:readonly"},{"subject":"okr:okr:readonly"}]}}`
	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(scopeErr))
	}}
	sc, key := newTestSidecar(t, gw)
	rr := serveSidecar(sc, signedSidecarRequest(t, key, "GET", "/open-apis/okr/v1/periods", "", "user", time.Now()))

	// lark-cli still sees Feishu's own error.
	if rr.Code != http.StatusBadRequest || rr.Body.String() != scopeErr {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if got := strings.Join(sc.MissingScopes(), " "); got != "okr:okr.period:readonly okr:okr:readonly" {
		t.Fatalf("missing scopes = %q", got)
	}
}

func TestSidecarIgnoresOtherPermissionErrors(t *testing.T) {
	// 99991672: the app itself lacks the scope — re-authorizing cannot fix it.
	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":99991672,"error":{"permission_violations":[{"subject":"hire:job"}]}}`))
	}}
	sc, key := newTestSidecar(t, gw)
	serveSidecar(sc, signedSidecarRequest(t, key, "GET", "/open-apis/hire/v1/jobs", "", "user", time.Now()))
	if got := sc.MissingScopes(); len(got) != 0 {
		t.Fatalf("missing scopes = %v, want none", got)
	}
}

func TestSidecarTranslatesGatewayErrors(t *testing.T) {
	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":"session_not_found","message":"invalid client token"}`))
	}}
	sc, key := newTestSidecar(t, gw)
	rr := serveSidecar(sc, signedSidecarRequest(t, key, "GET", "/open-apis/im/v1/chats", "", "user", time.Now()))

	var feishu struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &feishu); err != nil {
		t.Fatalf("not JSON: %s", rr.Body.String())
	}
	if rr.Code != http.StatusUnauthorized || feishu.Code != feishuCodeInvalidAccessToken {
		t.Fatalf("status %d code %d", rr.Code, feishu.Code)
	}
	if !strings.Contains(feishu.Msg, "larkctl login") || !strings.Contains(feishu.Msg, "session_not_found") {
		t.Errorf("msg = %q", feishu.Msg)
	}
	if gw.refresh != 0 {
		t.Errorf("refreshed a session that does not exist")
	}
}

func TestSidecarRefreshesExpiredSessionOnce(t *testing.T) {
	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"session_expired","message":"session expired"}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{"retried":true}}`))
	}}
	sc, key := newTestSidecar(t, gw)
	body := `{"x":"y"}`
	rr := serveSidecar(sc, signedSidecarRequest(t, key, "POST", "/open-apis/task/v2/tasks", body, "user", time.Now()))

	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "retried") {
		t.Fatalf("status %d body %s", rr.Code, rr.Body.String())
	}
	if gw.refresh != 1 || len(gw.proxied) != 2 {
		t.Fatalf("refresh=%d proxied=%d, want 1 and 2", gw.refresh, len(gw.proxied))
	}
	if gw.bodies[1] != body {
		t.Errorf("retry lost the body: %q", gw.bodies[1])
	}
}

func TestSidecarStreamsLargeResponsesIntact(t *testing.T) {
	payload := bytes.Repeat([]byte("0123456789"), (sidecarInspectBody/10)+50_000)
	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(payload)
	}}
	sc, key := newTestSidecar(t, gw)
	rr := serveSidecar(sc, signedSidecarRequest(t, key, "GET", "/open-apis/drive/v1/files", "", "user", time.Now()))
	if !bytes.Equal(rr.Body.Bytes(), payload) {
		t.Fatalf("large body altered: got %d bytes, want %d", rr.Body.Len(), len(payload))
	}
}

func TestLarkCLIEnvScrubsInheritedSettings(t *testing.T) {
	base := []string{
		"PATH=/usr/bin",
		"HOME=/home/me",
		"LARKSUITE_CLI_USER_ACCESS_TOKEN=u-real-token",
		"LARKSUITE_CLI_PROFILE=personal",
		"LARKSUITE_CLI_APP_SECRET=s3cret",
		"LARKSUITE_CLI_STRICT_MODE=off",
	}
	env := larkCLIEnv(base, "http://127.0.0.1:5555", "k3y", "/home/me/.lark/lark-cli")
	joined := strings.Join(env, "\n")
	for _, leaked := range []string{"u-real-token", "PROFILE=personal", "s3cret", "STRICT_MODE=off"} {
		if strings.Contains(joined, leaked) {
			t.Errorf("inherited %s survived", leaked)
		}
	}
	for _, want := range []string{
		"PATH=/usr/bin",
		"LARKSUITE_CLI_AUTH_PROXY=http://127.0.0.1:5555",
		"LARKSUITE_CLI_PROXY_KEY=k3y",
		"LARKSUITE_CLI_STRICT_MODE=user",
		"LARKSUITE_CLI_CONFIG_DIR=/home/me/.lark/lark-cli",
		"LARKSUITE_CLI_NO_UPDATE_NOTIFIER=1",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("env missing %s", want)
		}
	}
}

func TestLarkCLIAssetName(t *testing.T) {
	if got := larkCLIAssetName("darwin", "arm64"); got != "lark-cli_"+larkCLIVersion+"_darwin_arm64" {
		t.Errorf("got %s", got)
	}
	if got := larkCLIAssetName("windows", "amd64"); !strings.HasSuffix(got, "_windows_amd64.exe") {
		t.Errorf("got %s", got)
	}
}

func TestDownloadVerifiedAsset(t *testing.T) {
	asset := []byte("#!/bin/sh\necho lark-cli\n")
	name := larkCLIAssetName("linux", "amd64")
	checksums := sha256Hex(asset) + "  " + name + "\n" + sha256Hex([]byte("other")) + "  other-asset\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch strings.TrimPrefix(r.URL.Path, "/") {
		case "checksums.txt":
			_, _ = w.Write([]byte(checksums))
		case name:
			_, _ = w.Write(asset)
		case "tampered":
			_, _ = w.Write([]byte("evil"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	dir := t.TempDir()

	dest := filepath.Join(dir, "bin", name)
	if err := downloadVerifiedAsset(t.Context(), srv.URL, name, dest); err != nil {
		t.Fatalf("download: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, asset) {
		t.Fatalf("installed content differs")
	}
	if fi, _ := os.Stat(dest); runtime.GOOS != "windows" && fi.Mode().Perm()&0o100 == 0 {
		t.Errorf("installed without execute permission: %v", fi.Mode())
	}

	if err := downloadVerifiedAsset(t.Context(), srv.URL, "unlisted", filepath.Join(dir, "unlisted")); err == nil {
		t.Error("unlisted asset installed")
	}
	// Listed under a checksum that the served bytes do not match.
	checksums += sha256Hex([]byte("the real thing")) + "  tampered\n"
	if err := downloadVerifiedAsset(t.Context(), srv.URL, "tampered", filepath.Join(dir, "tampered")); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Errorf("tampered asset: err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "tampered")); err == nil {
		t.Error("tampered asset left on disk")
	}
}

// ── end to end through `larkctl cli`, with this test binary standing in for lark-cli ──

// TestFakeLarkCLIProcess is not a test: it is the fake lark-cli that
// installFakeLarkCLI points LARKCTL_LARK_CLI at. It signs one request to the
// sidecar named in its environment, prints the response, and exits 0 on a 2xx
// response and 7 otherwise.
func TestFakeLarkCLIProcess(t *testing.T) {
	if os.Getenv("LARKCTL_FAKE_LARK_CLI") != "1" {
		t.Skip("helper process")
	}
	args := os.Args
	for i, a := range args {
		if a == "--" {
			args = args[i+1:]
			break
		}
	}
	method, pathAndQuery := args[0], args[1]
	body := ""
	if len(args) > 2 {
		body = args[2]
	}
	proxy := os.Getenv("LARKSUITE_CLI_AUTH_PROXY")
	key := []byte(os.Getenv("LARKSUITE_CLI_PROXY_KEY"))
	if os.Getenv("LARKSUITE_CLI_STRICT_MODE") != "user" || os.Getenv("LARKSUITE_CLI_USER_ACCESS_TOKEN") != "" {
		fmt.Println("unexpected environment")
		os.Exit(9)
	}

	digest := sha256Hex([]byte(body))
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, _ := http.NewRequest(method, proxy+pathAndQuery, strings.NewReader(body))
	req.Header.Set(sidecarHeaderVersion, "v1")
	req.Header.Set(sidecarHeaderTarget, "https://open.feishu.cn")
	req.Header.Set(sidecarHeaderIdentity, "user")
	req.Header.Set(sidecarHeaderAuthHeader, "Authorization")
	req.Header.Set(sidecarHeaderBodySHA256, digest)
	req.Header.Set(sidecarHeaderTimestamp, stamp)
	req.Header.Set(sidecarHeaderSignature, sidecarCanonical{
		Version: "v1", Method: method, Host: "open.feishu.cn", PathAndQuery: pathAndQuery,
		BodySHA256: digest, Timestamp: stamp, Identity: "user", AuthHeader: "Authorization",
	}.sign(key))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Println("request failed:", err)
		os.Exit(8)
	}
	out, _ := io.ReadAll(resp.Body)
	fmt.Println(string(out))
	if resp.StatusCode >= 300 {
		os.Exit(7)
	}
	os.Exit(0)
}

func installFakeLarkCLI(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake lark-cli wrapper is a shell script")
	}
	script := filepath.Join(t.TempDir(), "lark-cli")
	content := fmt.Sprintf("#!/bin/sh\nLARKCTL_FAKE_LARK_CLI=1 exec %q -test.run='^TestFakeLarkCLIProcess$' -- \"$@\"\n", os.Args[0])
	if err := os.WriteFile(script, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LARKCTL_LARK_CLI", script)
	t.Setenv("LARKSUITE_CLI_USER_ACCESS_TOKEN", "must-not-reach-lark-cli")
}

func TestLarkctlCLIRunsThroughSidecar(t *testing.T) {
	gw := &fakeCLIGateway{}
	setupGatewayEnv(t, gw.handler())
	installFakeLarkCLI(t)

	_, err := execCmd(t, "cli", "GET", "/open-apis/calendar/v4/calendars?page_size=1")
	if err != nil {
		t.Fatalf("larkctl cli: %v", err)
	}
	if len(gw.proxied) != 1 || gw.proxied[0].URL.RequestURI() != "/v1/cli/proxy/open-apis/calendar/v4/calendars?page_size=1" {
		t.Fatalf("gateway saw %d calls", len(gw.proxied))
	}
	if gw.proxied[0].Header.Get("Authorization") != "Bearer test-token" {
		t.Errorf("relayed with %q", gw.proxied[0].Header.Get("Authorization"))
	}
}

func TestLarkctlCLIPassesThroughExitStatus(t *testing.T) {
	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":403,"msg":"larkctl gateway: sender not in IM whitelist"}`))
	}}
	setupGatewayEnv(t, gw.handler())
	installFakeLarkCLI(t)

	_, err := execCmd(t, "cli", "POST", "/open-apis/im/v1/messages", `{"receive_id":"oc_x"}`)
	var exitErr *exitCodeError
	if !asExitCode(err, &exitErr) || exitErr.code != 7 {
		t.Fatalf("err = %v, want lark-cli's exit status 7", err)
	}
	if len(gw.upgrades) != 0 {
		t.Errorf("a non-scope failure triggered a scope upgrade")
	}
}

func TestLarkctlCLIUpgradesScopesAndRetries(t *testing.T) {
	prev := stdinReplayable
	stdinReplayable = func() bool { return true }
	t.Cleanup(func() { stdinReplayable = prev })

	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":99991679,"error":{"permission_violations":[{"subject":"okr:okr:readonly"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"code":0,"data":{}}`))
	}}
	setupGatewayEnv(t, gw.handler())
	installFakeLarkCLI(t)

	if _, err := execCmd(t, "cli", "GET", "/open-apis/okr/v1/periods"); err != nil {
		t.Fatalf("larkctl cli: %v", err)
	}
	if len(gw.upgrades) != 1 || gw.upgrades[0] != "okr:okr:readonly" {
		t.Fatalf("upgrades = %v", gw.upgrades)
	}
	if len(gw.proxied) != 2 {
		t.Fatalf("lark-cli ran %d requests, want the failed one and the retry", len(gw.proxied))
	}
}

func TestLarkctlCLIDoesNotReplayPipedInput(t *testing.T) {
	prev := stdinReplayable
	stdinReplayable = func() bool { return false }
	t.Cleanup(func() { stdinReplayable = prev })

	gw := &fakeCLIGateway{proxy: func(w http.ResponseWriter, r *http.Request, n int) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":99991679,"error":{"permission_violations":[{"subject":"okr:okr:readonly"}]}}`))
	}}
	setupGatewayEnv(t, gw.handler())
	installFakeLarkCLI(t)

	_, err := execCmd(t, "cli", "GET", "/open-apis/okr/v1/periods")
	var exitErr *exitCodeError
	if !asExitCode(err, &exitErr) {
		t.Fatalf("err = %v, want the first run's exit status", err)
	}
	if len(gw.upgrades) != 1 || len(gw.proxied) != 1 {
		t.Fatalf("upgrades=%d proxied=%d, want 1 and 1", len(gw.upgrades), len(gw.proxied))
	}
}

func TestLarkctlCLIRefusesLocalMode(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	t.Setenv("LARKCTL_SESSION_TOKEN", "")
	cfg := `{"app_id":"cli_local","app_secret":"secret","access_token":"a","refresh_token":"r"}`
	if err := os.MkdirAll(filepath.Join(tmp, ".lark"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".lark", "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	gatewayURL = ""
	_, err := execCmd(t, "cli", "GET", "/open-apis/im/v1/chats")
	if err == nil || !strings.Contains(err.Error(), "local mode") {
		t.Fatalf("err = %v", err)
	}
}

func asExitCode(err error, target **exitCodeError) bool {
	if err == nil {
		return false
	}
	e, ok := err.(*exitCodeError)
	if ok {
		*target = e
	}
	return ok
}
