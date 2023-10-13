package consistenthash

import (
	"strconv"
	"testing"
)

func TestHash(t *testing.T) {
	hash := New(3, func(data []byte) uint32 {
		i, _ := strconv.Atoi(string(data))
		return uint32(i)
	})

	// Given the above hash function, this will give replicas with hashes:
	// 2, 4, 6, 12, 14, 16, 22, 24, 26
	hash.Add("6", "4", "2")

	tests := map[string]string{
		"2":  "2",
		"11": "2",
		"23": "4",
		"27": "2",
	}

	for k, v := range tests {
		if got := hash.Get(k); got != v {
			t.Errorf("Get(%q) = %v, want: %v", k, got, v)
		}
	}

	// 8, 18, 28
	hash.Add("8")
	tests["27"] = "8"

	for k, v := range tests {
		if got := hash.Get(k); got != v {
			t.Errorf("Get(%q) = %v, want: %v", k, got, v)
		}
	}
}
