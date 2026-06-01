package main

import (
	"errors"
	"fmt"
	"testing"

	polipage "github.com/poli-page/sdk-go"
	"github.com/stretchr/testify/assert"
)

// TestExitCodeFor is the table-driven version the plan recommends as the
// "easier option" (Step 8.2): drive the pure exit-code function with
// synthetic *polipage.Error values, no RoundTripper plumbing.
func TestExitCodeFor(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{
			name: "401 Unauthorized → 1 (auth error)",
			err:  &polipage.Error{StatusCode: 401, Code: polipage.ErrCodeInvalidAPIKey, Message: "invalid api key"},
			want: 1,
		},
		{
			name: "403 Forbidden → 1 (auth error)",
			err:  &polipage.Error{StatusCode: 403, Code: polipage.ErrCodeForbidden, Message: "forbidden"},
			want: 1,
		},
		{
			name: "400 Bad Request → 2 (validation)",
			err:  &polipage.Error{StatusCode: 400, Code: polipage.ErrCodeValidationError, Message: "data.email required"},
			want: 2,
		},
		{
			name: "network_error → 3",
			err:  &polipage.Error{Code: polipage.ErrCodeNetworkError, Message: "connection refused"},
			want: 3,
		},
		{
			name: "timeout → 3 (also IsNetworkError)",
			err:  &polipage.Error{Code: polipage.ErrCodeTimeout, Message: "deadline exceeded"},
			want: 3,
		},
		{
			name: "429 rate limit → 4 (anything else)",
			err:  &polipage.Error{StatusCode: 429, Code: polipage.ErrCodeQuotaExceeded, Message: "quota exceeded"},
			want: 4,
		},
		{
			name: "500 Internal → 4 (5xx)",
			err:  &polipage.Error{StatusCode: 500, Code: "INTERNAL_ERROR", Message: "server error"},
			want: 4,
		},
		{
			name: "non-polipage error → 4 (SDK-internal / unknown)",
			err:  errors.New("an unrelated failure"),
			want: 4,
		},
		{
			name: "*polipage.Error wrapped → still mapped via errors.As chain",
			err:  fmt.Errorf("call failed: %w", &polipage.Error{StatusCode: 401, Code: polipage.ErrCodeInvalidAPIKey}),
			want: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, exitCodeFor(tc.err))
		})
	}
}
