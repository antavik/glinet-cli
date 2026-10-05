package glinettest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/antavik/glinet-cli/src/internal/glinet"
)

// callTunnels logs in to a fake router whose vpn-client.get_status handler
// returns result, and makes one Tunnels call against it.
func callTunnels(t *testing.T, result any) ([]glinet.Tunnel, error) {
	t.Helper()
	router := NewRouter(t, map[string]Handler{
		"vpn-client.get_status": func(json.RawMessage) any { return result },
	})
	c := glinet.NewClient(router.URL, false)
	if err := c.Login(context.Background(), User, Password); err != nil {
		t.Fatal(err)
	}
	return c.Tunnels(context.Background())
}

func TestRPCError(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result any
	}{
		{"value", RPCError{Code: -32000, Message: "boom"}},
		{"pointer", &RPCError{Code: -32000, Message: "boom"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := callTunnels(t, tt.result)
			if err == nil || !strings.Contains(err.Error(), "boom") {
				t.Fatalf("Tunnels() error = %v, want it to contain %q", err, "boom")
			}
		})
	}
}

// A nil *RPCError is an ordinary result, not an error, so handlers can return
// one unconditionally. The null result decodes to no tunnels.
func TestNilRPCErrorPointerIsResult(t *testing.T) {
	tunnels, err := callTunnels(t, (*RPCError)(nil))
	if err != nil {
		t.Fatalf("Tunnels() error = %v, want nil", err)
	}
	if len(tunnels) != 0 {
		t.Errorf("Tunnels() = %+v, want no tunnels", tunnels)
	}
}

// rpc posts one JSON-RPC request to the router and returns the response.
func rpc(t *testing.T, url string, method string, params any) map[string]any {
	t.Helper()
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url+"/rpc", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return m
}

// A Handler may return a Task (value or non-nil pointer). The router answers
// the call with {"id": N} and serves "task" polls for that id.
func TestTask(t *testing.T) {
	tests := []struct {
		name   string
		result any
	}{
		{"value", Task{Result: map[string]any{"enabled": true}}},
		{"pointer", &Task{Result: map[string]any{"enabled": true}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := NewRouter(t, map[string]Handler{
				"mod.fn": func(json.RawMessage) any { return tt.result },
			})
			c := glinet.NewClient(router.URL, false)
			if err := c.Login(context.Background(), User, Password); err != nil {
				t.Fatal(err)
			}

			got := rpc(t, router.URL, "call", []any{SID, "mod", "fn", struct{}{}})
			want := map[string]any{"id": 1.0}
			if !jsonEqual(got["result"], want) {
				t.Fatalf("call result = %v, want %v", got["result"], want)
			}

			reply := rpc(t, router.URL, "task", map[string]any{"id": 1})
			if e := reply["error"]; e != nil {
				t.Fatalf("task error = %v", e)
			}
			res, ok := reply["result"].(map[string]any)
			if !ok {
				t.Fatalf("task result = %v, want an object", reply["result"])
			}
			if res["complete"] != true {
				t.Errorf("task complete = %v, want true", res["complete"])
			}
			if !jsonEqual(res["result"], map[string]any{"enabled": true}) {
				t.Errorf("task result.result = %v, want the handler result", res["result"])
			}
		})
	}
}

// A nil *Task is an ordinary result, like a nil *RPCError.
func TestNilTaskPointerIsResult(t *testing.T) {
	router := NewRouter(t, map[string]Handler{
		"mod.fn": func(json.RawMessage) any { return (*Task)(nil) },
	})
	c := glinet.NewClient(router.URL, false)
	if err := c.Login(context.Background(), User, Password); err != nil {
		t.Fatal(err)
	}
	if got := rpc(t, router.URL, "call", []any{SID, "mod", "fn", struct{}{}})["result"]; got != nil {
		t.Errorf("call result = %v, want null", got)
	}
}

// The first Polls task polls answer complete:false; the next one completes.
func TestTaskPolls(t *testing.T) {
	router := NewRouter(t, map[string]Handler{
		"mod.fn": func(json.RawMessage) any { return Task{Result: map[string]any{"ok": true}, Polls: 2} },
	})
	c := glinet.NewClient(router.URL, false)
	if err := c.Login(context.Background(), User, Password); err != nil {
		t.Fatal(err)
	}
	rpc(t, router.URL, "call", []any{SID, "mod", "fn", struct{}{}})

	for i := 1; i <= 2; i++ {
		reply := rpc(t, router.URL, "task", map[string]any{"id": 1})
		res, ok := reply["result"].(map[string]any)
		if !ok {
			t.Fatalf("poll %d: task result = %v, want an object", i, reply["result"])
		}
		if res["complete"] != false {
			t.Errorf("poll %d: complete = %v, want false", i, res["complete"])
		}
	}

	reply := rpc(t, router.URL, "task", map[string]any{"id": 1})
	res, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("task result = %v, want an object", reply["result"])
	}
	if res["complete"] != true {
		t.Fatalf("poll 3: complete = %v, want true", res["complete"])
	}
	if !jsonEqual(res["result"], map[string]any{"ok": true}) {
		t.Errorf("task result.result = %v, want the handler result", res["result"])
	}
}

// A Task with an Error completes with that error as an "error" member.
func TestTaskError(t *testing.T) {
	want := &RPCError{Code: -32000, Message: "tunnel failed"}
	router := NewRouter(t, map[string]Handler{
		"mod.fn": func(json.RawMessage) any { return Task{Error: want} },
	})
	c := glinet.NewClient(router.URL, false)
	if err := c.Login(context.Background(), User, Password); err != nil {
		t.Fatal(err)
	}
	rpc(t, router.URL, "call", []any{SID, "mod", "fn", struct{}{}})

	reply := rpc(t, router.URL, "task", map[string]any{"id": 1})
	res, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("task result = %v, want an object", reply["result"])
	}
	if res["complete"] != true {
		t.Fatalf("complete = %v, want true", res["complete"])
	}
	if !jsonEqual(res["error"], map[string]any{"code": float64(want.Code), "message": want.Message}) {
		t.Errorf("task error = %v, want %v", res["error"], want)
	}
}

// Polling a task id the router never issued is a JSON-RPC error.
func TestTaskUnknownID(t *testing.T) {
	router := NewRouter(t, nil)
	reply := rpc(t, router.URL, "task", map[string]any{"id": 1})
	e, ok := reply["error"].(map[string]any)
	if !ok {
		t.Fatalf("task error = %v, want a JSON-RPC error", reply["error"])
	}
	if e["code"] != float64(-32000) {
		t.Errorf("code = %v, want -32000", e["code"])
	}
	if !strings.Contains(e["message"].(string), "Task not found") {
		t.Errorf("message = %v, want it to contain %q", e["message"], "Task not found")
	}
}

// jsonEqual reports whether two decoded JSON values are deeply equal,
// comparing numbers across the float64/int boundary.
func jsonEqual(a, b any) bool {
	af, aok := normalizeNumber(a)
	bf, bok := normalizeNumber(b)
	if aok && bok {
		return af == bf
	}
	return deepEqual(a, b)
}

func normalizeNumber(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

func deepEqual(a, b any) bool {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			bv2, ok := bv[k]
			if !ok || !jsonEqual(v, bv2) {
				return false
			}
		}
		return true
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !jsonEqual(av[i], bv[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}
