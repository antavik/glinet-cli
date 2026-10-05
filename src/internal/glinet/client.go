// Package glinet is a client for the JSON-RPC API of GL.iNet routers on
// firmware 4.x. client.go has the transport and login. Every other file
// covers one router API module and is named after it, e.g. vpnclient.go for
// "vpn-client".
package glinet

import (
	"bytes"
	"context"
	"crypto/md5" //nolint:gosec // The router protocol mandates MD5 when hash-method is absent.
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/GehirnInc/crypt"
	_ "github.com/GehirnInc/crypt/md5_crypt"
	_ "github.com/GehirnInc/crypt/sha256_crypt"
	_ "github.com/GehirnInc/crypt/sha512_crypt"
)

// Client talks to one router over the GL.iNet firmware 4.x JSON-RPC API.
// Call Login before any other method.
type Client struct {
	url  string
	http *http.Client
	sid  string
	wait bool
}

// NewClient returns a client for the router at baseURL, e.g. "http://192.168.8.1".
// When wait is true, calls wait for async operations to finish.
func NewClient(baseURL string, wait bool) *Client {
	return &Client{
		url:  strings.TrimRight(baseURL, "/") + "/rpc",
		wait: wait,
		http: &http.Client{
			// A followed 307/308 redirect would resend the login hash or
			// session ID to whatever host the redirect names.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Login runs the challenge-response handshake and keeps the session ID.
func (c *Client) Login(ctx context.Context, username, password string) error {
	var ch struct {
		Alg        int    `json:"alg"`
		Salt       string `json:"salt"`
		Nonce      string `json:"nonce"`
		HashMethod string `json:"hash-method"`
	}
	if err := c.rpc(ctx, "challenge", map[string]string{"username": username}, &ch); err != nil {
		return fmt.Errorf("challenge: %w", err)
	}

	h, err := loginHash(username, password, ch.Alg, ch.Salt, ch.Nonce, ch.HashMethod)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	var res struct {
		SID string `json:"sid"`
	}
	if err := c.rpc(ctx, "login", map[string]string{"username": username, "hash": h}, &res); err != nil {
		return fmt.Errorf("login: %w", err)
	}
	if res.SID == "" {
		return errors.New("login: router returned no session ID")
	}
	c.sid = res.SID
	return nil
}

// logoutTimeout bounds Logout. Routers answer in milliseconds on a LAN.
const logoutTimeout = 3 * time.Second

// taskPollInterval is how long call waits between polls of an unfinished task.
const taskPollInterval = 500 * time.Millisecond

// Logout ends the session so a captured session ID stops working. It is best
// effort: the router drops a session after 5 idle minutes anyway. Like Close,
// it takes no context, so a deferred Logout still runs after Ctrl+C or the
// command timeout has cancelled the command's context.
func (c *Client) Logout() {
	if c.sid == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), logoutTimeout)
	defer cancel()
	_ = c.rpc(ctx, "logout", map[string]string{"sid": c.sid}, nil)
	c.sid = ""
}

// loginHash returns hashMethod("username:crypt(password):nonce") as hex.
// alg picks the crypt(3) scheme; hashMethod is missing on older firmware,
// which always uses MD5.
func loginHash(username, password string, alg int, salt, nonce, hashMethod string) (string, error) {
	// A "$" would let the router prepend "rounds=N$" to the salt and make
	// SHA-crypt run up to a billion rounds, which ctx cannot interrupt.
	// Real salts never contain one.
	if strings.Contains(salt, "$") {
		return "", errors.New("invalid salt")
	}
	var scheme crypt.Crypt
	switch alg {
	case 1:
		scheme = crypt.MD5
	case 5:
		scheme = crypt.SHA256
	case 6:
		scheme = crypt.SHA512
	default:
		return "", fmt.Errorf("unsupported password algorithm %d", alg)
	}
	cipher, err := scheme.New().Generate([]byte(password), fmt.Appendf(nil, "$%d$%s", alg, salt))
	if err != nil {
		return "", fmt.Errorf("crypt password: %w", err)
	}

	var h hash.Hash
	switch hashMethod {
	case "", "md5":
		h = md5.New() //nolint:gosec // See the crypto/md5 import.
	case "sha256":
		h = sha256.New()
	case "sha512":
		h = sha512.New()
	default:
		return "", fmt.Errorf("unsupported hash method %q", hashMethod)
	}
	h.Write([]byte(username + ":" + cipher + ":" + nonce))
	return hex.EncodeToString(h.Sum(nil)), nil
}

// call invokes module.function using the session from Login. If the router
// answers asynchronously with a task handle and c.wait is set, it polls the
// task until it finishes and decodes its result.
func (c *Client) call(ctx context.Context, module, function string, args, result any) error {
	if args == nil {
		args = struct{}{}
	}
	var raw json.RawMessage
	if err := c.rpc(ctx, "call", []any{c.sid, module, function, args}, &raw); err != nil {
		return fmt.Errorf("%s.%s: %w", module, function, err)
	}
	if c.wait {
		if id, ok := taskID(raw); ok {
			var err error
			if raw, err = c.awaitTask(ctx, module, function, id); err != nil {
				return err // awaitTask returns module.function-wrapped errors
			}
		}
	}
	if result == nil {
		return nil
	}
	if len(raw) == 0 {
		raw = json.RawMessage("null")
	}
	if err := json.Unmarshal(raw, result); err != nil {
		return fmt.Errorf("%s.%s: decode result: %w", module, function, err)
	}
	return nil
}

// taskID reports whether raw is an async task handle: a JSON object whose
// only key is "id" and whose value is an integer. Anything else, including
// non-integer numbers like 1.5, is a plain result, not a task.
func taskID(raw json.RawMessage) (int, bool) {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil || len(obj) != 1 {
		return 0, false
	}
	idRaw, ok := obj["id"]
	if !ok {
		return 0, false
	}
	id, err := json.Number(idRaw.String()).Int64()
	if err != nil {
		return 0, false
	}
	return int(id), true
}

// awaitTask polls the "task" method until it completes and returns the
// result it carries. The router answers an async call with {"id": N} and
// runs it in the background; polling "task" with that id replies
// {"complete": false} until done, then {"complete": true} with either a
// "result" or an "error".
func (c *Client) awaitTask(ctx context.Context, module, function string, id int) (json.RawMessage, error) {
	for {
		var reply struct {
			Complete bool            `json:"complete"`
			Result   json.RawMessage `json:"result"`
			Error    *rpcError       `json:"error"`
		}
		if err := c.rpc(ctx, "task", map[string]any{"id": id}, &reply); err != nil {
			return nil, fmt.Errorf("%s.%s: task %d: %w", module, function, id, err)
		}
		if reply.Complete {
			if reply.Error != nil {
				return nil, fmt.Errorf("%s.%s: %w", module, function, reply.Error)
			}
			return reply.Result, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%s.%s: %w", module, function, ctx.Err())
		case <-time.After(taskPollInterval):
		}
	}
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("%s (code %d)", printable(e.Message), e.Code)
}

// printable replaces control, format and other non-graphic runes in s with
// '?'. Text from the router goes through it before reaching the terminal, so
// a hostile router cannot inject escape sequences or bidi overrides. Spaces
// such as U+3000 in CJK names are kept.
func printable(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsGraphic(r) {
			return r
		}
		return '?'
	}, s)
}

// maxResponseSize caps how much of a reply is read. Real replies are a few KB.
const maxResponseSize = 1 << 20

// rpc sends one JSON-RPC 2.0 request and decodes its result into result, if not nil.
func (c *Client) rpc(ctx context.Context, method string, params, result any) error {
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	var r struct {
		Result json.RawMessage `json:"result"`
		Error  *rpcError       `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseSize)).Decode(&r); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	if r.Error != nil {
		return r.Error
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(r.Result, result); err != nil {
		return fmt.Errorf("decode result: %w", err)
	}
	return nil
}
