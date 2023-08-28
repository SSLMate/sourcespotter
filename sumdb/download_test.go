package sumdb

import (
	"testing"
)

func TestFormatTileIndex(t *testing.T) {
	tests := []struct {
		in  uint64
		out string
	}{
		{0, "000"},
		{1, "001"},
		{12, "012"},
		{105, "105"},
		{1000, "x001/000"},
		{1050, "x001/050"},
		{52123, "x052/123"},
		{999001, "x999/001"},
		{1999001, "x001/x999/001"},
		{15999001, "x015/x999/001"},
	}
	for i, test := range tests {
		result := formatTileIndex(test.in)
		if result != test.out {
			t.Errorf("#%d: formatTileIndex(%q) = %q, want %q", i, test.in, result, test.out)
		}
	}
}
