// Package mcp is the Model Context Protocol adapter over the HTTP API
// (ADR-0012).
//
// The adapter holds no state and calls no internal function: every tool is
// one request to the public API, made with the credential of whoever called
// the tool. Permissions, validation and the single writer are therefore
// enforced in exactly one place, and every change an agent makes is
// attributed to the person or token behind it.
//
// The protocol is JSON-RPC 2.0 with the methods initialize, ping,
// tools/list and tools/call, written by hand. [Handle] answers one message;
// the transports are [Serve] (one message per line, for a local agent
// process) and the server's POST /mcp route.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/dbuchs7/wikiwikibangbang/internal/budget"
	"github.com/dbuchs7/wikiwikibangbang/internal/buildinfo"
)

// API makes one request to the HTTP API with the caller's credential and
// returns the status and body of the answer.
type API interface {
	Call(ctx context.Context, method, path string, body []byte) (status int, answer []byte, err error)
}

// Protocol versions this adapter speaks, newest first.
var versions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

var latestVersion = versions[0]

// JSON-RPC error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
)

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Handle answers one JSON-RPC message. It reports false for a notification,
// which gets no answer.
func Handle(ctx context.Context, api API, msg []byte) ([]byte, bool) {
	var req request
	if err := json.Unmarshal(msg, &req); err != nil {
		var batch []json.RawMessage
		if json.Unmarshal(msg, &batch) == nil {
			return reply(nil, nil, &rpcError{codeInvalidRequest, "batches are not supported"}), true
		}
		return reply(nil, nil, &rpcError{codeParse, "parse error"}), true
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		return reply(req.ID, nil, &rpcError{codeInvalidRequest, "not a JSON-RPC 2.0 request"}), true
	}
	notification := len(req.ID) == 0 || string(req.ID) == "null"
	result, rerr := dispatch(ctx, api, req)
	if notification {
		return nil, false
	}
	return reply(req.ID, result, rerr), true
}

func dispatch(ctx context.Context, api API, req request) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := latestVersion
		for _, v := range versions {
			if v == p.ProtocolVersion {
				version = v
			}
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "wikiwikibangbang", "version": buildinfo.Get().Version},
			"instructions": "Tools of the wikiwikibangbang HTTP API. Every call acts with your caller's credential; " +
				"what they may not do, the API refuses.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		list := make([]map[string]any, len(tools))
		for i, t := range tools {
			list[i] = t.describe()
		}
		return map[string]any{"tools": list}, nil
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil || p.Name == "" {
			return nil, &rpcError{codeInvalidParams, "tools/call needs a tool name"}
		}
		t := find(p.Name)
		if t == nil {
			return nil, &rpcError{codeInvalidParams, fmt.Sprintf("no tool %q", p.Name)}
		}
		method, path, body, err := t.request(p.Arguments)
		if err != nil {
			return nil, &rpcError{codeInvalidParams, fmt.Sprintf("%s: %v", t.Name, err)}
		}
		status, answer, err := api.Call(ctx, method, path, body)
		switch {
		case err != nil:
			return toolResult("the API could not be reached: "+err.Error(), true), nil
		case status >= 400:
			return toolResult(fmt.Sprintf("the API refused (%d): %s", status, answer), true), nil
		}
		return toolResult(string(answer), false), nil
	}
	return nil, &rpcError{codeMethodNotFound, fmt.Sprintf("no method %q", req.Method)}
}

func toolResult(text string, isError bool) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isError}
}

func reply(id json.RawMessage, result any, rerr *rpcError) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	resp := response{JSONRPC: "2.0", ID: id, Error: rerr}
	if rerr == nil {
		resp.Result = result
	}
	out, err := json.Marshal(resp)
	if err != nil {
		out, _ = json.Marshal(response{JSONRPC: "2.0", ID: id, Error: &rpcError{-32603, "internal error"}})
	}
	return out
}

// Serve answers one message per line from in on out until in ends. A line
// longer than the request budget (ADR-0015) stops it with an error.
func Serve(ctx context.Context, api API, in io.Reader, out io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64<<10), int(budget.Get().RequestBody))
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		answer, ok := Handle(ctx, api, line)
		if !ok {
			continue
		}
		if _, err := out.Write(append(answer, '\n')); err != nil {
			return err
		}
	}
	return sc.Err()
}
