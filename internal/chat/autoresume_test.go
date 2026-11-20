package chat

import (
	"context"
	"errors"
	"testing"

	"github.com/yangk/kshell/internal/acp"
)

func TestShouldAutoResume(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		stop   string
		stderr string
		want   bool
	}{
		{name: "429 error", err: errors.New("acp rpc error -32603: 429 Requests are too frequent"), want: true},
		{name: "503 error", err: errors.New("upstream 503 Service Unavailable"), want: true},
		{name: "timeout", err: errors.New("context deadline exceeded"), want: true},
		{name: "connection reset", err: errors.New("read tcp: connection reset by peer"), want: true},
		{name: "refusal with 429 stderr", stop: "refusal", stderr: "error: 429 too many requests", want: true},
		{name: "refusal without transient", stop: "refusal", stderr: "model refused", want: false},
		{name: "auth required", err: &acp.RPCError{Code: acp.ErrCodeAuthRequired, Message: "authRequired"}, want: false},
		{name: "invalid params", err: &acp.RPCError{Code: -32602, Message: "invalid params"}, want: false},
		{name: "model not found", err: errors.New("model not found"), want: false},
		{name: "quota", err: errors.New("quota exceeded"), want: false},
		{name: "canceled", err: context.Canceled, want: false},
		{name: "max tokens", stop: "max_tokens", want: false},
		{name: "end turn", stop: "end_turn", want: false},
		{name: "unknown internal", err: &acp.RPCError{Code: -32603, Message: "Internal error"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldAutoResume(tc.err, tc.stop, tc.stderr); got != tc.want {
				t.Fatalf("shouldAutoResume(%v, %q, %q) = %v, want %v", tc.err, tc.stop, tc.stderr, got, tc.want)
			}
		})
	}
}
