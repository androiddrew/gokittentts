package phonemize_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/androiddrew/gokittentts/internal/phonemize"
)

// bracketing stands in for espeak: it wraps every text it's given in <...>,
// so the output shows exactly where the text was cut around punctuation.
type bracketing struct{}

func (bracketing) Phonemize(text string) (string, error) {
	return "<" + strings.ToUpper(strings.TrimSpace(text)) + "> ", nil
}

// The expected strings come from upstream phonemizer 3.4.0's
// Punctuation.preserve and restore (with the span-split fix that
// scripts/make_golden.py applies), run over the same bracketing backend.
func TestPreservePunctuation(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Hello, world!", "<HELLO>, <WORLD>! "},
		{"$3.5 million, or 1,500 dollars.", "<$3.5 MILLION>, <OR 1,500 DOLLARS>. "},
		{"Version 3.5 shipped on time.", "<VERSION 3.5 SHIPPED ON TIME>. "},
		{"Pi is 3.14.", "<PI IS 3.14>. "},
		{"Wait... what?!", "<WAIT>... <WHAT>?! "},
		{"(Parentheses) and more,", "(<PARENTHESES>) <AND MORE>, "},
		{"\"Quoted.\"", "\"<QUOTED>.\" "},
		{"No punctuation", "<NO PUNCTUATION> "},
		{"...", "..."},
		{"a , b.", "<A> , <B>. "},
		{"Version 1.5.2 is out.", "<VERSION 1.5.2 IS OUT>. "},
		{"Chapter 5. Next,", "<CHAPTER 5>. <NEXT>, "},
		{".5 of it,", ".<5 OF IT>, "},
		{"one;two:three,", "<ONE>;<TWO>:<THREE>, "},
		{"“Curly” quotes, «guillemets» and ¿inverted?", "“<CURLY>” <QUOTES>, «<GUILLEMETS>» <AND> ¿<INVERTED>? "},
		{"a — dash… ellipsis.", "<A> — <DASH>… <ELLIPSIS>. "},
		{"Tab\tthen, mark.", "<TAB\tTHEN>, <MARK>. "},
	}
	p := phonemize.PreservePunctuation(bracketing{})
	for _, c := range cases {
		got, err := p.Phonemize(c.in)
		if err != nil {
			t.Fatalf("%q: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("%q\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

type failing struct{}

func (failing) Phonemize(string) (string, error) { return "", errors.New("backend down") }

func TestPreservePunctuationReturnsBackendErrors(t *testing.T) {
	_, err := phonemize.PreservePunctuation(failing{}).Phonemize("Hello, world.")
	if err == nil || !strings.Contains(err.Error(), "backend down") {
		t.Fatalf("err = %v, want the backend's error", err)
	}
}
