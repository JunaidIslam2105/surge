package tui

import "testing"

func TestJoinVerticalFixedGolden(t *testing.T) {
	got := joinVerticalFixed(
		"╭──╮\n│A │\n╰──╯",
		"╭──╮\n│B │\n╰──╯",
	)
	want := "╭──╮\n│A │\n╰──╯\n╭──╮\n│B │\n╰──╯"
	if got != want {
		t.Fatalf("vertical join mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJoinHorizontalFixedGolden(t *testing.T) {
	got := joinHorizontalFixed(
		"L1  \nL2  ",
		"R1\nR2",
	)
	want := "L1  R1\nL2  R2"
	if got != want {
		t.Fatalf("horizontal join mismatch:\n got:\n%s\nwant:\n%s", got, want)
	}
}

func TestJoinHorizontalFixedNarrowLinesGolden(t *testing.T) {
	// A one-cell pane is a valid layout at the narrow boundary. The helper
	// must concatenate its exact lines without adding width or padding.
	got := joinHorizontalFixed("╭╮\n╰╯", "╭╮\n╰╯")
	want := "╭╮╭╮\n╰╯╰╯"
	if got != want {
		t.Fatalf("narrow horizontal join mismatch: got %q, want %q", got, want)
	}
}
