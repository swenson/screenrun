package main

import "testing"

func TestAttachSizes(t *testing.T) {
	l := len(makeAttachMessage(5, "", 0, 0))
	if l != 3372 {
		t.Errorf("Expected version 5 to be 3372, got %d", l)
	}

	tests := []struct {
		os      string
		version int
		size    int
	}{
		{"darwin", 0, 3372},
		{"darwin", 1, 3372},
		{"darwin", 2, 3372},
		{"darwin", 3, 3372},
		{"darwin", 4, 3372},
		{"darwin", 5, 3372},
		{"linux", 0, 12588},
		{"linux", 1, 12588},
		{"linux", 2, 12588},
		{"linux", 3, 12588},
		{"linux", 4, 12588},
		{"linux", 5, 12588},
	}

	for _, test := range tests {
		l = messageSize(test.version, FindMaxPathLen(test.os))
		if l != test.size {
			t.Errorf("Expected version %d on %s to be %d, got %d", test.version, test.os, test.size, l)
		}
	}
}
