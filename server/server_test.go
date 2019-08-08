package server

import (
	"strings"
	"testing"
	"time"

	"github.com/walkert/ceedee/client"
)

func TestHappyPath(t *testing.T) {
	s, err := New(
		WithRoot("../testdata"),
		WithPort(9909),
		WithSkipList([]string{"ignore"}),
		WithHome("/this/home"),
		WithHistFile("../testdata/histfile"),
		WithMonitorInterval(1),
	)
	if err != nil {
		t.Fatalf("Unexpected error creating server: %v\n", err)
	}
	defer s.Stop()
	go func() {
		s.Start()
	}()
	// Give the hist monitor time to kick in
	time.Sleep(time.Second * 2)
	c, err := client.New(9909)
	if err != nil {
		t.Fatalf("Unexpected error creating client: %v\n", err)
	}
	tests := []struct {
		name, search, want string
		wantCount          int
		wantErr            bool
		errMatch           string
	}{
		{
			name:      "GetExactMatch",
			search:    "last",
			want:      "e;../testdata/top/next/last",
			wantCount: 1,
			wantErr:   false,
			errMatch:  "",
		},
		{
			name:      "ExpectNone",
			search:    "badname",
			want:      "",
			wantCount: 0,
			wantErr:   false,
			errMatch:  "",
		},
		{
			name:      "SearchSkipped",
			search:    "ignore",
			want:      "",
			wantCount: 0,
			wantErr:   false,
			errMatch:  "",
		},
		{
			name:      "SearchPartial",
			search:    "nex",
			want:      "p;next",
			wantCount: 1,
			wantErr:   false,
			errMatch:  "",
		},
		{
			name:      "CheckExpandedHistory",
			search:    "foo",
			want:      "e;/this/home/testdata/foo",
			wantCount: 2,
			wantErr:   false,
			errMatch:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vals, err := c.Get(tt.search)
			if err != nil {
				if !tt.wantErr {
					t.Errorf("Unexpected error getting %s: %v\n", tt.search, err)
				}
				if !strings.Contains(err.Error(), tt.errMatch) {
					t.Errorf("Expected error to contain '%s' but got: %v\n", tt.errMatch, err)
				}
			}
			if len(vals) != tt.wantCount {
				t.Fatalf("Expected %d values but got %d: %s", tt.wantCount, len(vals), strings.Join(vals, ","))
			}
			if tt.wantCount > 0 {
				if vals[0] != tt.want {
					t.Fatalf("Wanted '%s', got: '%s'", tt.want, vals[0])
				}
			}
		})
	}
}
