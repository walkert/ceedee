package server

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/walkert/ceedee/client"
)

func TestHappyPath(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)
	os.MkdirAll(filepath.Join(dir, "top", "next", "last"), 0700)
	os.MkdirAll(filepath.Join(dir, "foo"), 0700)
	s, err := New(
		WithRoot(dir),
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
			want:      fmt.Sprintf("e;%s/top/next/last", dir),
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

func TestRemovals(t *testing.T) {
	dir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatalf("unable to create temporary directory: %v", err)
	}
	defer os.RemoveAll(dir)
	os.MkdirAll(filepath.Join(dir, "rand", "next", "last"), 0700)
	os.MkdirAll(filepath.Join(dir, "next"), 0700)
	os.MkdirAll(filepath.Join(dir, "deleted"), 0700)
	s, err := New(
		WithRoot(dir),
		WithPort(9910),
		WithSkipList([]string{"ignore"}),
		WithHome("/this/home"),
		WithHistFile("../testdata/histfile"),
		WithMonitorInterval(1),
		WithDirInterval(1),
	)
	if err != nil {
		t.Fatalf("Unexpected error creating server: %v\n", err)
	}
	defer s.Stop()
	go func() {
		s.Start()
	}()
	c, err := client.New(9910)
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
			name:      "RemoveLoner",
			search:    "deleted",
			wantCount: 0,
			wantErr:   false,
		},
		{
			name:      "RemoveOneofTwo",
			search:    "next",
			want:      fmt.Sprintf("e;%s/rand/next", dir),
			wantCount: 1,
			wantErr:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.RemoveAll(filepath.Join(dir, tt.search))
			time.Sleep(2 * time.Second)
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
				t.Fatalf("Expected %d values but got %d: %s", tt.wantCount, len(vals), strings.Join(vals, ";"))
			}
			if tt.wantCount > 0 {
				if vals[0] != tt.want {
					t.Fatalf("Wanted '%s', got: '%s'", tt.want, vals[0])
				}
			}
		})
	}
}
