package main

import (
	"net/url"
	"testing"
)

func TestPageParams(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		query         string
		limit, offset int
	}{
		{"", 50, 0},
		{"limit=10&offset=20", 10, 20},
		{"limit=500", 500, 0},
		{"limit=501", 50, 0},
		{"limit=0&offset=-1", 50, 0},
		{"limit=x&offset=y", 50, 0},
	} {
		q, _ := url.ParseQuery(tc.query)
		limit, offset := pageParams(q, 50, 500)
		if limit != tc.limit || offset != tc.offset {
			t.Errorf("pageParams(%q) = %d, %d; want %d, %d", tc.query, limit, offset, tc.limit, tc.offset)
		}
	}
}
