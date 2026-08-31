package main

import (
	"testing"
)

func TestNormalizeAlbumTitle(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"Kind of Blue", "kind of blue"},
		// remaster/deluxe/etc. noise in parentheses or brackets is stripped
		{"Kind of Blue (2020 Remaster)", "kind of blue"},
		{"In Rainbows [Deluxe Edition]", "in rainbows"},
		{"Pet Sounds (The Stereo Mix)", "pet sounds (the stereo mix)"},
		{"The Dark Side of the Moon (2011 Remastered Version)", "the dark side of the moon"},
		{"  Marble Machine  ", "marble machine"},
		{"Blood Sugar Sex Magik (Deluxe) [Bonus Tracks]", "blood sugar sex magik"},
	}
	for _, c := range cases {
		got := NormalizeAlbumTitle(c.in)
		if got != c.want {
			t.Errorf("NormalizeAlbumTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormalizeAlbumTitlePureAndDeterministic(t *testing.T) {
	// Guard the property that makes dedupe safe: same input → same output,
	// every time, and no mutation of the input string.
	a := "Random Access Memories (Deluxe Edition)"
	b := "Random Access Memories (Deluxe Edition)"
	if NormalizeAlbumTitle(a) != NormalizeAlbumTitle(b) {
		t.Fatal("normalization not deterministic")
	}
	if a != b {
		t.Fatal("normalization mutated its input")
	}
}