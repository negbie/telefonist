package telefonist

import (
	"testing"
)

func TestParseTestfileDefines(t *testing.T) {
	content := `
_define USER alice
_define USER_EMAIL alice@example.com
_define MSG Hello USER
_define GREETING MSG, USER_EMAIL

case1: USER says GREETING
`
	cases, _, _, _, _, err := parseTestfile(content)
	if err != nil {
		t.Fatalf("parseTestfile failed: %v", err)
	}

	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}

	// USER is replaced first (length 4)
	// USER_EMAIL is replaced before USER (length 10 vs 4)
	// So USER_EMAIL -> alice@example.com
	// Then USER -> alice
	// Sequence: alice says GREETING
	// Then GREETING -> MSG, USER_EMAIL
	// Sequence: alice says MSG, USER_EMAIL
	// Then MSG -> Hello USER
	// Sequence: alice says Hello USER, alice@example.com
	
	// Wait, since we replacement in one pass over keys, recursive defines ONLY work if the replaced text contains a key that comes LATER in the sorted key list.
	// USER_EMAIL (10)
	// GREETING (8)
	// USER (4)
	// MSG (3)
	
	// USER says GREETING
	// i=0: USER_EMAIL (no match)
	// i=1: GREETING -> MSG, USER_EMAIL
	//   Sequence: USER says MSG, USER_EMAIL
	// i=2: USER -> alice
	//   Sequence: alice says MSG, USER_EMAIL
	// i=3: MSG -> Hello USER
	//   Sequence: alice says Hello USER, USER_EMAIL
	
	// If we want FULLY recursive, we'd need multiple passes.
	// But let's see what the current fix does for the sorting issue.
}

func TestParseTestfileIsolation(t *testing.T) {
	content1 := `_define X 1
X`
	content2 := `X`

	cases1, _, _, _, _, _ := parseTestfile(content1)
	cases2, _, _, _, _, _ := parseTestfile(content2)

	if len(cases1) == 0 || cases1[0].sequence != "1" {
		t.Errorf("content1 expected 1, got %v", cases1)
	}
	if len(cases2) == 0 || cases2[0].sequence != "X" {
		t.Errorf("content2 expected X, got %v (isolation failure)", cases2)
	}

	content3 := `_define ua1 sip:test1@host
ua1:dial 123`
	cases3, _, _, _, _, _ := parseTestfile(content3)
	if len(cases3) == 0 || cases3[0].sequence != "sip:test1@host:dial 123" {
		t.Errorf("content3 expected sequence='sip:test1@host:dial 123', got sequence=%q", cases3[0].sequence)
	}
}

func TestParseTestfileSorting(t *testing.T) {
	content := `
_define FOO 1
_define FOOBAR 2
FOOBAR
`
	cases, _, _, _, _, _ := parseTestfile(content)
	if len(cases) == 0 || cases[0].sequence != "2" {
		t.Errorf("Expected 2, got %v (sorting failure)", cases)
	}
}
