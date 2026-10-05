// Package glinettest runs a fake GL.iNet router for tests.
package glinettest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// Login values of the fake router. Salt and Nonce were captured from a router
// on firmware 4.9.0. loginHash is what a client must send for them: computed
// with `openssl passwd -1 -salt <Salt> <Password>`, then `openssl dgst
// -sha256` over "root:<cipher>:<Nonce>".
const (
	User     = "root"
	Password = "goodlife"
	Salt     = "k4NJToyX"
	Nonce    = "v0p5vSEoDZfWG8we1Hx0p6ee4zxThgkY"
	SID      = "test-sid"

	loginHash = "3ae7d3ba18572048dcc6160ebab78a8c90ed98d402027db4c8198788a0ff61df"
)

// Handler answers a "call" request given its arguments object.
type Handler func(args json.RawMessage) any

// RPCError returned by a Handler (as a value or non-nil pointer) makes the
// router answer with a JSON-RPC error instead of a result, so tests can
// exercise failing calls.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Task returned by a Handler (as a value or non-nil pointer; a nil *Task is
// an ordinary null result) makes the router answer the call with an async
// task handle {"id": N} and serve the "task" polls for it. Polls is the
// number of complete:false answers before the poll that completes with
// Result, or Error if set.
type Task struct {
	Result any       // final result when the task completes
	Error  *RPCError // if set, the completed task reports this error
	Polls  int
}

// taskEntry is one issued task awaiting its polls.
type taskEntry struct {
	remainingPolls int
	task           Task
}

// Router mimics firmware 4.9.0: it accepts User and Password and answers
// "call" requests from handlers keyed by "module.function". Other calls fail
// with "Method not found", and calls after logout with "Access denied".
type Router struct {
	URL       string
	loggedOut atomic.Bool

	mu     sync.Mutex // guards the task state below
	tasks  map[int]*taskEntry
	nextID int
}

// LoggedOut reports whether a client has ended its session.
func (r *Router) LoggedOut() bool {
	return r.loggedOut.Load()
}

// registerTask issues the next sequential id for t and remembers it.
func (r *Router) registerTask(t Task) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	id := r.nextID
	r.tasks[id] = &taskEntry{remainingPolls: t.Polls, task: t}
	return id
}

// NewRouter starts a fake router that stops when the test ends.
func NewRouter(t testing.TB, calls map[string]Handler) *Router {
	t.Helper()
	router := &Router{tasks: map[int]*taskEntry{}}
	decode := func(data json.RawMessage, v any) {
		if err := json.Unmarshal(data, v); err != nil {
			t.Errorf("decode params: %v", err)
		}
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			return
		}

		var result any
		var rpcErr *rpcError
		switch req.Method {
		case "challenge":
			result = map[string]any{"hash-method": "sha256", "alg": 1, "salt": Salt, "nonce": Nonce}
		case "login":
			var p struct {
				Hash string `json:"hash"`
			}
			decode(req.Params, &p)
			if p.Hash != loginHash {
				rpcErr = &rpcError{Code: -32000, Message: "Access denied"}
				break
			}
			result = map[string]string{"username": User, "sid": SID}
		case "logout":
			var p struct {
				SID string `json:"sid"`
			}
			decode(req.Params, &p)
			if p.SID != SID {
				rpcErr = &rpcError{Code: -32000, Message: "Access denied"}
				break
			}
			router.loggedOut.Store(true)
			result = map[string]any{}
		case "call":
			var p []json.RawMessage
			var gotSID, module, function string
			decode(req.Params, &p)
			decode(p[0], &gotSID)
			decode(p[1], &module)
			decode(p[2], &function)
			h, ok := calls[module+"."+function]
			switch {
			case gotSID != SID || router.loggedOut.Load():
				rpcErr = &rpcError{Code: -32000, Message: "Access denied"}
			case !ok:
				rpcErr = &rpcError{Code: -32601, Message: "Method not found"}
			default:
				result = h(p[3])
				switch e := result.(type) {
				case RPCError:
					rpcErr, result = &rpcError{Code: e.Code, Message: e.Message}, nil
				case *RPCError:
					if e != nil {
						rpcErr, result = &rpcError{Code: e.Code, Message: e.Message}, nil
					}
				case Task:
					result = map[string]any{"id": router.registerTask(e)}
				case *Task:
					if e != nil {
						result = map[string]any{"id": router.registerTask(*e)}
					}
				}
			}
		case "task":
			var p struct {
				ID int `json:"id"`
			}
			decode(req.Params, &p)
			var entry *taskEntry
			pending := false
			router.mu.Lock()
			if e, ok := router.tasks[p.ID]; ok {
				if e.remainingPolls > 0 {
					e.remainingPolls--
					pending = true
				} else {
					entry = e
				}
			}
			router.mu.Unlock()
			switch {
			case pending:
				result = map[string]any{"complete": false}
			case entry == nil:
				rpcErr = &rpcError{Code: -32000, Message: "Task not found"}
			case entry.task.Error != nil:
				result = map[string]any{
					"complete": true,
					"error": map[string]any{
						"code":    entry.task.Error.Code,
						"message": entry.task.Error.Message,
					},
				}
			default:
				result = map[string]any{"complete": true, "result": entry.task.Result}
			}
		default:
			rpcErr = &rpcError{Code: -32601, Message: "Method not found"}
		}

		resp := map[string]any{"jsonrpc": "2.0", "id": 1}
		if rpcErr != nil {
			resp["error"] = rpcErr
		} else {
			resp["result"] = result
		}
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	router.URL = srv.URL
	return router
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
