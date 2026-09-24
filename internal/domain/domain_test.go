package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const browserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_0) AppleWebKit/537.36 Chrome/128.0 Safari/537.36"

func TestNewCode_Format(t *testing.T) {
	for i := 0; i < 10_000; i++ {
		code, err := NewCode()
		if err != nil {
			t.Fatal(err)
		}
		if !ValidCode(code) {
			t.Fatalf("invalid code %q", code)
		}
	}
}

func TestNewCode_Unique(t *testing.T) {
	const n = 100_000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		code, _ := NewCode()
		if _, dup := seen[code]; dup {
			t.Fatalf("duplicate code after %d generations: %s", i, code)
		}
		seen[code] = struct{}{}
	}
}

// Rejection sampling phải cho phân bố đều: mỗi ký tự lệch không quá ±5% so với kỳ vọng.
func TestNewCode_UniformDistribution(t *testing.T) {
	const codes = 80_000
	counts := make(map[rune]int, len(codeAlphabet))
	for i := 0; i < codes; i++ {
		code, _ := NewCode()
		for _, r := range code {
			counts[r]++
		}
	}
	expected := float64(codes*CodeLength) / float64(len(codeAlphabet))
	for _, r := range codeAlphabet {
		got := float64(counts[r])
		if got < expected*0.95 || got > expected*1.05 {
			t.Fatalf("char %q: got %.0f, expected ~%.0f", r, got, expected)
		}
	}
}

func TestValidCode(t *testing.T) {
	cases := map[string]bool{
		"Ab3xK9pQ":               true,
		"00000000":               true,
		"Ab3xK9p":                false,
		"Ab3xK9pQ1":              false,
		"Ab3x-9pQ":               false,
		"Ab3xK9p✓":               false,
		"":                       false,
		strings.Repeat("a", 100): false,
	}
	for in, want := range cases {
		if got := ValidCode(in); got != want {
			t.Errorf("ValidCode(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLink_Accessible(t *testing.T) {
	now := time.Now()
	past, future := now.Add(-time.Hour), now.Add(time.Hour)
	cases := []struct {
		name string
		link Link
		want error
	}{
		{"active", Link{Status: StatusActive}, nil},
		{"active, not expired", Link{Status: StatusActive, ExpiresAt: &future}, nil},
		{"disabled", Link{Status: StatusDisabled}, ErrLinkGone},
		{"deleted", Link{Status: StatusActive, DeletedAt: &past}, ErrLinkGone},
		{"expired", Link{Status: StatusActive, ExpiresAt: &past}, ErrLinkGone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.link.Accessible(now); !errors.Is(got, tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLink_CountsViewFrom(t *testing.T) {
	l := &Link{OwnerID: 1}
	cases := []struct {
		name   string
		viewer Viewer
		want   bool
	}{
		{"anonymous browser", Viewer{VisitorID: "x", UserAgent: browserUA}, true},
		{"other user", Viewer{UserID: 2, UserAgent: browserUA}, true},
		{"owner", Viewer{UserID: 1, UserAgent: browserUA}, false},
		{"facebook crawler", Viewer{UserAgent: "facebookexternalhit/1.1"}, false},
		{"zalo preview", Viewer{UserAgent: "Zalo-Preview/1.0"}, false},
		{"googlebot", Viewer{UserAgent: "Mozilla/5.0 (compatible; Googlebot/2.1)"}, false},
		{"curl", Viewer{UserAgent: "curl/8.4.0"}, false},
		{"empty UA", Viewer{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := l.CountsViewFrom(tc.viewer); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestViewer_Key(t *testing.T) {
	a := Viewer{VisitorID: "1.2.3.4|UA"}.Key()
	b := Viewer{VisitorID: "1.2.3.4|UA"}.Key()
	c := Viewer{VisitorID: "5.6.7.8|UA"}.Key()
	if a != b || a == c {
		t.Fatalf("key must be stable and distinct: %s %s %s", a, b, c)
	}
	if got := (Viewer{UserID: 42, VisitorID: "x"}).Key(); got != "u42" {
		t.Fatalf("logged-in viewer must be keyed by user id, got %s", got)
	}
}

func TestResourceTypeAndStatus_Valid(t *testing.T) {
	if !ResourceWriting.Valid() || ResourceType("admin_secret").Valid() {
		t.Fatal("ResourceType.Valid wrong")
	}
	if !StatusDisabled.Valid() || Status("deleted").Valid() {
		t.Fatal("Status.Valid wrong")
	}
}
