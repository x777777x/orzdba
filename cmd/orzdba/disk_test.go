package main

import "testing"

// A realistic /proc/diskstats excerpt: a whole disk (sda) with 14 fields, a
// partition (sda1), and a device-mapper node (dm-0).
const sampleDiskstats = `   8       0 sda 100 5 10000 1000 50 2 5000 500 0 1000 500 0 0
   8       1 sda1 50 2 5000 500 25 1 2500 250 0 500 250
 253       0 dm-0 10 0 1000 100 5 0 500 50 0 100 50 0 0
`

func TestFindDiskDevice(t *testing.T) {
	cases := []struct {
		dev  string
		want bool
	}{
		{"sda", true},      // whole disk
		{"sda1", true},     // partition (exact match, not a prefix of sda)
		{"dm-0", true},     // device-mapper
		{"sdb", false},     // not present
		{"nvme0n1", false}, // not present
		{"sd", false},      // prefix is not a whole-word match
		{"", false},        // empty
	}
	for _, c := range cases {
		if got := findDiskDevice([]byte(sampleDiskstats), c.dev); got != c.want {
			t.Errorf("findDiskDevice(%q) = %v, want %v", c.dev, got, c.want)
		}
	}
}
