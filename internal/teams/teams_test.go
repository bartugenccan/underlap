package teams

import "testing"

func TestCanonical(t *testing.T) {
	for _, tc := range []struct{ source, alias, want string }{
		{"footballdata", "Buyuksehyr", "Başakşehir"},
		{"footballdata", "Goztep", "Göztepe"},
		{"footballdata", "Erzurum BB", "Erzurumspor FK"},
		{"footballdata", "Erzurumspor", "Erzurumspor FK"},
	} {
		got, err := Canonical(tc.source, tc.alias)
		if err != nil || got != tc.want {
			t.Errorf("Canonical(%q, %q) = %q, %v; want %q", tc.source, tc.alias, got, err, tc.want)
		}
	}
	if _, err := Canonical("footballdata", "Fenerbahce SK"); err == nil {
		t.Error("unlisted alias accepted")
	}
}
