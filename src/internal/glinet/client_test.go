package glinet

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/antavik/glinet-cli/src/internal/glinet/glinettest"
)

const (
	testSalt     = glinettest.Salt
	testNonce    = glinettest.Nonce
	testPassword = glinettest.Password
)

// Expected hashes were computed with `openssl passwd -<alg> -salt <salt>
// <password>` and `openssl dgst -<hash>` over "root:<cipher>:<nonce>".
func TestLoginHash(t *testing.T) {
	tests := []struct {
		alg        int
		hashMethod string
		want       string
	}{
		{1, "", "d360140442c69f51eed8d9efb8336fbc"},
		{1, "md5", "d360140442c69f51eed8d9efb8336fbc"},
		{1, "sha256", "3ae7d3ba18572048dcc6160ebab78a8c90ed98d402027db4c8198788a0ff61df"},
		{5, "sha256", "24a6194482c4064b3b61b35c8166a37029605ccd1005692266d13e46ab527b9c"},
		{6, "sha512", "b93e695714bf9ea0b3ca5a94a4610a3d535fe2e647db282b90d405da5a234f4d6402dc5615245f14a4b7ddc1321e8b67396372618f072b8bc3398bf3292eda9a"},
	}
	for _, tt := range tests {
		got, err := loginHash("root", testPassword, tt.alg, testSalt, testNonce, tt.hashMethod)
		if err != nil {
			t.Errorf("loginHash(alg=%d, %q): %v", tt.alg, tt.hashMethod, err)
			continue
		}
		if got != tt.want {
			t.Errorf("loginHash(alg=%d, %q) = %s, want %s", tt.alg, tt.hashMethod, got, tt.want)
		}
	}
}

func TestLoginHashUnsupported(t *testing.T) {
	if _, err := loginHash("root", testPassword, 2, testSalt, testNonce, "md5"); err == nil {
		t.Error("want error for unsupported alg")
	}
	if _, err := loginHash("root", testPassword, 1, testSalt, testNonce, "sha1"); err == nil {
		t.Error("want error for unsupported hash method")
	}
	if _, err := loginHash("root", testPassword, 6, "rounds=1000000$"+testSalt, testNonce, "sha512"); err == nil {
		t.Error("want error for salt that sets crypt rounds")
	}
}

func TestPrintable(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Home/WG", "Home/WG"},
		{"東京\u3000VPN", "東京\u3000VPN"},
		{"a\x1b[2Jb", "a?[2Jb"}, // ESC
		{"a\u009b2Jb", "a?2Jb"}, // C1 CSI
		{"a\u202eb", "a?b"},     // bidi override
		{"a\tb\nc", "a?b?c"},
	}
	for _, tt := range tests {
		if got := printable(tt.in); got != tt.want {
			t.Errorf("printable(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLoginWrongPassword(t *testing.T) {
	c := NewClient(glinettest.NewRouter(t, nil).URL)
	err := c.Login(t.Context(), "root", "wrong")
	if err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("Login() error = %v, want Access denied", err)
	}
}

func TestStatus(t *testing.T) {
	c := loggedIn(t, map[string]glinettest.Handler{
		"system.get_status": func(json.RawMessage) any {
			return map[string]any{"system": map[string]any{
				"uptime":       90061.5,
				"load_average": [3]float64{2.01, 0.89, 0.33},
				"memory_total": 126943232,
				"memory_free":  78471168,
				"flash_total":  106278912,
				"flash_free":   105918464,
			}}
		},
	})
	got, err := c.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := SystemStatus{
		Uptime:      25*time.Hour + time.Minute + 1500*time.Millisecond,
		LoadAverage: [3]float64{2.01, 0.89, 0.33},
		MemoryTotal: 126943232,
		MemoryFree:  78471168,
		FlashTotal:  106278912,
		FlashFree:   105918464,
	}
	if got != want {
		t.Errorf("Status() = %+v, want %+v", got, want)
	}
}

func TestInfo(t *testing.T) {
	c := loggedIn(t, map[string]glinettest.Handler{
		"system.get_info": func(json.RawMessage) any {
			return map[string]any{
				"model":            "xe300",
				"mac":              "94:83:C4:0C:74:9A",
				"firmware_version": "4.9.0",
				"board_info":       map[string]any{"hostname": "GL\u001b[2J-AXT1800"},
			}
		},
	})
	got, err := c.Info(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := DeviceInfo{
		Model:           "xe300",
		MAC:             "94:83:C4:0C:74:9A",
		FirmwareVersion: "4.9.0",
		Hostname:        "GL?[2J-AXT1800", // ESC sanitized by printable()
	}
	if got != want {
		t.Errorf("Info() = %+v, want %+v", got, want)
	}
}

func TestWanStatus(t *testing.T) {
	tests := []struct {
		name    string
		result  string
		want    WanStatus
		wantErr bool
	}{
		{
			name:    "static",
			result:  `{"mode":0,"status":1,"protocol":"static","ipv4":{"ip":"192.168.113.137/24","gateway":"192.168.113.1","dns":["8.8.8.8","8.8.4.4"]}}`,
			want:    WanStatus{Protocol: "static", Status: 1, IPv4: WanIPv4{IP: "192.168.113.137/24", Gateway: "192.168.113.1", DNS: []string{"8.8.8.8", "8.8.4.4"}}},
			wantErr: false,
		},
		{
			name:    "negative err_code",
			result:  `{"mode":0,"status":1,"protocol":"static","ipv4":{"ip":"192.168.113.137/24","gateway":"192.168.113.1","dns":["8.8.8.8","8.8.4.4"]},"err_code":-4}`,
			wantErr: true,
		},
		{
			name:    "empty protocol",
			result:  `{"mode":0,"status":1,"ipv4":{"ip":"192.168.113.137/24","gateway":"192.168.113.1","dns":["8.8.8.8"]}}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := loggedIn(t, map[string]glinettest.Handler{
				"cable.get_status": func(json.RawMessage) any { return json.RawMessage(tt.result) },
			})
			got, err := c.WanStatus(t.Context())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("WanStatus() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("WanStatus() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCheckFirmware(t *testing.T) {
	tests := []struct {
		name    string
		result  string
		want    FirmwareUpdate
		wantErr bool
	}{
		{
			name:    "update available",
			result:  `{"current_version":"4.9.0","version_new":"4.10.0"}`,
			want:    FirmwareUpdate{CurrentVersion: "4.9.0", NewVersion: "4.10.0"},
			wantErr: false,
		},
		{
			name:    "up to date",
			result:  `{"current_version":"4.9.0"}`,
			want:    FirmwareUpdate{CurrentVersion: "4.9.0"},
			wantErr: false,
		},
		{
			name:    "empty current version",
			result:  `{"current_version":""}`,
			wantErr: true,
		},
		{
			name:    "negative err_code",
			result:  `{"current_version":"4.9.0","err_code":-2}`,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := loggedIn(t, map[string]glinettest.Handler{
				"upgrade.check_firmware_online": func(json.RawMessage) any { return json.RawMessage(tt.result) },
			})
			got, err := c.CheckFirmware(t.Context())
			if tt.wantErr {
				if err == nil {
					t.Fatalf("CheckFirmware() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("CheckFirmware() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTunnels(t *testing.T) {
	c := loggedIn(t, map[string]glinettest.Handler{
		"vpn-client.get_status": func(json.RawMessage) any {
			return json.RawMessage(`{"status_list":[
				{"tunnel_id":2001,"name":"Home/WG","enabled":true,"status":1,"group_id":1001,"peer_id":2001},
				{"tunnel_id":2002,"name":"Work/OVPN\u001b]52;c;cm0gLXJmIH4=\u0007","enabled":false,"status":0}
			]}`)
		},
	})
	got, err := c.Tunnels(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []Tunnel{
		{ID: 2001, Name: "Home/WG", Enabled: true, Status: 1},
		{ID: 2002, Name: "Work/OVPN?]52;c;cm0gLXJmIH4=?", Enabled: false, Status: 0},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Tunnels() = %+v, want %+v", got, want)
	}
}

func TestSetTunnel(t *testing.T) {
	var gotArgs string
	c := loggedIn(t, map[string]glinettest.Handler{
		"vpn-client.set_tunnel": func(args json.RawMessage) any {
			gotArgs = string(args)
			return map[string]any{"tunnel_id": 2001}
		},
	})
	if err := c.SetTunnel(t.Context(), 2001, true); err != nil {
		t.Fatal(err)
	}
	if want := `{"enabled":true,"tunnel_id":2001}`; gotArgs != want {
		t.Errorf("set_tunnel args = %s, want %s", gotArgs, want)
	}
}

func TestCallError(t *testing.T) {
	c := loggedIn(t, nil)
	_, err := c.Tunnels(t.Context())
	if err == nil || !strings.Contains(err.Error(), "vpn-client.get_status: Method not found") {
		t.Fatalf("Tunnels() error = %v, want method not found", err)
	}
}

func TestLogout(t *testing.T) {
	c := loggedIn(t, map[string]glinettest.Handler{
		"vpn-client.get_status": func(json.RawMessage) any { return map[string]any{"status_list": []any{}} },
	})
	old := c.sid
	c.Logout()
	if c.sid != "" {
		t.Errorf("sid = %q after Logout, want empty", c.sid)
	}
	c.sid = old // replay the old session ID
	if _, err := c.Tunnels(t.Context()); err == nil || !strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("Tunnels() after Logout error = %v, want Access denied", err)
	}
}

func TestRedirectNotFollowed(t *testing.T) {
	var hit atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit.Store(true) }))
	t.Cleanup(other.Close)
	redirect := httptest.NewServer(http.RedirectHandler(other.URL+"/rpc", http.StatusTemporaryRedirect))
	t.Cleanup(redirect.Close)

	err := NewClient(redirect.URL).Login(t.Context(), "root", testPassword)
	if err == nil || !strings.Contains(err.Error(), "307") {
		t.Fatalf("Login() error = %v, want unexpected HTTP status 307", err)
	}
	if hit.Load() {
		t.Error("client followed the redirect to another host")
	}
}

// loggedIn returns a client logged in to a fake router with the given handlers.
func loggedIn(t *testing.T, calls map[string]glinettest.Handler) *Client {
	t.Helper()
	c := NewClient(glinettest.NewRouter(t, calls).URL)
	if err := c.Login(t.Context(), "root", testPassword); err != nil {
		t.Fatalf("Login(): %v", err)
	}
	return c
}

// rawRouter answers each JSON-RPC method with the body reply returns.
func rawRouter(t *testing.T, reply func(method string) string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(reply(req.Method)))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// challenge is a valid challenge reply; alg is the crypt scheme it names.
func challenge(alg int) string {
	return `{"jsonrpc":"2.0","id":1,"result":{"alg":` + strconv.Itoa(alg) +
		`,"salt":"` + testSalt + `","nonce":"` + testNonce + `","hash-method":"sha256"}}`
}

func TestLoginErrors(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr string
	}{
		{"malformed response", rawRouter(t, func(string) string { return "<html>not json" }), "challenge: decode response"},
		{"unsupported algorithm", rawRouter(t, func(string) string { return challenge(2) }), "login: unsupported password algorithm 2"},
		{"no session ID", rawRouter(t, func(m string) string {
			if m == "challenge" {
				return challenge(1)
			}
			return `{"jsonrpc":"2.0","id":1,"result":{"sid":""}}`
		}), "router returned no session ID"},
		{"result of the wrong type", rawRouter(t, func(m string) string {
			if m == "challenge" {
				return `{"jsonrpc":"2.0","id":1,"result":"challenge"}`
			}
			return "{}"
		}), "challenge: decode result"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.url)
			err := c.Login(t.Context(), "root", testPassword)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Login() error = %v, want it to contain %q", err, tt.wantErr)
			}
			if c.sid != "" {
				t.Errorf("sid = %q after a failed login, want empty", c.sid)
			}
		})
	}
}
