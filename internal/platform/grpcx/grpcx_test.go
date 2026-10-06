package grpcx_test

import (
	"errors"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/raulalmeidatarazona/go-sdd/internal/platform/apperr"
	"github.com/raulalmeidatarazona/go-sdd/internal/platform/grpcx"
)

func TestToStatus(t *testing.T) {
	tests := map[string]struct {
		err        error
		code       codes.Code
		wantReason string
	}{
		"invalid":   {apperr.Invalid("bad_input", "bad"), codes.InvalidArgument, "bad_input"},
		"not found": {apperr.New(apperr.KindNotFound, "missing", "missing"), codes.NotFound, "missing"},
		"conflict":  {apperr.New(apperr.KindConflict, "dup", "dup"), codes.AlreadyExists, "dup"},
		"aborted":   {apperr.New(apperr.KindAborted, "race", "race"), codes.Aborted, "race"},
		"unknown":   {errors.New("db exploded: password=secret"), codes.Internal, ""},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			st := status.Convert(grpcx.ToStatus(tc.err))
			if st.Code() != tc.code {
				t.Fatalf("code = %s, want %s", st.Code(), tc.code)
			}
			if tc.code == codes.Internal && st.Message() != "internal error" {
				t.Fatalf("internal error leaked: %q", st.Message())
			}
			if tc.wantReason == "" {
				return
			}
			info, ok := st.Details()[0].(*errdetails.ErrorInfo)
			if !ok || info.GetReason() != tc.wantReason || info.GetDomain() != grpcx.ErrorDomain {
				t.Fatalf("details = %#v", st.Details())
			}
		})
	}
}
