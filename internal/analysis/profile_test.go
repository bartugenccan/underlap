package analysis

import "testing"

func TestWindow(t *testing.T) {
	// API-Football reports stoppage time as the half's last minute plus extra,
	// so 45+3' arrives as minute 45 and 90+5' as minute 90.
	for minute, want := range map[int]int{1: 0, 15: 0, 16: 1, 30: 1, 31: 2, 45: 2, 46: 3, 60: 3, 61: 4, 75: 4, 76: 5, 90: 5, 120: 5} {
		if got := window(minute); got != want {
			t.Errorf("window(%d) = %d (%s), want %d (%s)", minute, got, Windows[got], want, Windows[want])
		}
	}
}
