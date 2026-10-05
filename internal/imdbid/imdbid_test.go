package imdbid

import "testing"

func TestTitle(t *testing.T) {
	for _, c := range []struct {
		id   string
		want bool
	}{
		{"tt0133093", true},
		{"tt0000001", true},
		{"tt12345678901", true}, // ids have grown over the years
		{"tt", false},
		{"", false},
		{"nm0000206", false}, // a person is not a title
		{"603", false},       // the old TMDb id
		{"tt00x1", false},
		{"tt0133093; DROP TABLE titles", false},
		{"TT0133093", false},
		{"tt" + "1234567890123456789012", false},
	} {
		if got := Title(c.id); got != c.want {
			t.Errorf("Title(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}

func TestName(t *testing.T) {
	for _, c := range []struct {
		id   string
		want bool
	}{
		{"nm0000206", true},
		{"nm12345678901", true},
		{"nm", false},
		{"", false},
		{"tt0133093", false}, // a title is not a person
		{"nm00x1", false},
		{"NM0000206", false},
		{"nm" + "1234567890123456789012", false},
	} {
		if got := Name(c.id); got != c.want {
			t.Errorf("Name(%q) = %v, want %v", c.id, got, c.want)
		}
	}
}
