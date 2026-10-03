package streaming

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"
)

func svc(id, name string) Service {
	return Service{ID: id, Name: name, ImageSet: ImageSet{
		LightThemeImage: "https://img/" + id + "-light.svg",
		DarkThemeImage:  "https://img/" + id + "-dark.svg",
	}}
}

func price(amount, currency, formatted string) *Price {
	p := &Price{Currency: currency, Formatted: formatted}
	_ = p.Amount.UnmarshalJSON([]byte(`"` + amount + `"`))
	return p
}

func names(offers []Offer) []string {
	out := make([]string, len(offers))
	for i, o := range offers {
		out[i] = o.Name
	}
	return out
}

// TestAddOnsStreamWithTheServiceTheyComeThrough: an add-on is in the
// stream list under its own name and logo, and says what it is watched
// through.
func TestAddOnsStreamWithTheServiceTheyComeThrough(t *testing.T) {
	prime := svc("prime", "Prime Video")
	starz := svc("starz", "Starz")
	got := Shape([]StreamingOption{
		{Service: svc("netflix", "Netflix"), Type: "subscription", Link: "https://netflix/title/1"},
		{Service: prime, Type: "addon", Addon: &starz, Link: "https://prime/starz/1"},
	}, time.Now())
	want := []Offer{
		{ID: "netflix", Name: "Netflix", Link: "https://netflix/title/1",
			Logo: Logo{Dark: "https://img/netflix-dark.svg", Light: "https://img/netflix-light.svg"}},
		{ID: "starz", Name: "Starz", Link: "https://prime/starz/1", Via: "Prime Video",
			Logo: Logo{Dark: "https://img/starz-dark.svg", Light: "https://img/starz-light.svg"}},
	}
	if !reflect.DeepEqual(got.Stream, want) {
		t.Errorf("stream = %+v\nwant     %+v", got.Stream, want)
	}
	if len(got.Free) != 0 || len(got.Rent) != 0 || len(got.Buy) != 0 {
		t.Errorf("the other lists = %+v", got)
	}
}

// TestEachListHasOneEntryPerServiceInTheAPIsOrder: a service listed once
// for each quality is one entry, where it first appeared.
func TestEachListHasOneEntryPerServiceInTheAPIsOrder(t *testing.T) {
	got := Shape([]StreamingOption{
		{Service: svc("hulu", "Hulu"), Type: "subscription", Link: "h-sd"},
		{Service: svc("netflix", "Netflix"), Type: "subscription", Link: "n-hd"},
		{Service: svc("hulu", "Hulu"), Type: "subscription", Link: "h-hd"},
		{Service: svc("tubi", "Tubi"), Type: "free", Link: "t"},
		{Service: svc("netflix", "Netflix"), Type: "subscription", Link: "n-uhd"},
		{Service: svc("pluto", "Pluto TV"), Type: "free", Link: "p"},
		{Service: svc("tubi", "Tubi"), Type: "free", Link: "t2"},
	}, time.Now())
	if n := names(got.Stream); !reflect.DeepEqual(n, []string{"Hulu", "Netflix"}) {
		t.Errorf("stream = %v", n)
	}
	if got.Stream[0].Link != "h-sd" || got.Stream[1].Link != "n-hd" {
		t.Errorf("an entry took a later option's link: %+v", got.Stream)
	}
	if n := names(got.Free); !reflect.DeepEqual(n, []string{"Tubi", "Pluto TV"}) {
		t.Errorf("free = %v", n)
	}
}

// TestRentAndBuyKeepTheLowestPrice: the cheapest option of a service is
// its entry's price and link, at the place the service first appeared.
func TestRentAndBuyKeepTheLowestPrice(t *testing.T) {
	got := Shape([]StreamingOption{
		{Service: svc("apple", "Apple TV"), Type: "rent", Link: "a-4k", Price: price("5.99", "USD", "5.99 USD")},
		{Service: svc("prime", "Prime Video"), Type: "rent", Link: "p-hd", Price: price("3.99", "USD", "3.99 USD")},
		{Service: svc("apple", "Apple TV"), Type: "rent", Link: "a-hd", Price: price("3.49", "USD", "3.49 USD")},
		{Service: svc("prime", "Prime Video"), Type: "rent", Link: "p-4k", Price: price("4.99", "USD", "4.99 USD")},
		{Service: svc("prime", "Prime Video"), Type: "buy", Link: "pb-4k", Price: price("19.99", "USD", "19.99 USD")},
		{Service: svc("prime", "Prime Video"), Type: "buy", Link: "pb-hd", Price: price("14.99", "USD", "14.99 USD")},
	}, time.Now())
	wantRent := []struct{ name, link, price string }{
		{"Apple TV", "a-hd", "3.49 USD"},
		{"Prime Video", "p-hd", "3.99 USD"},
	}
	if len(got.Rent) != len(wantRent) {
		t.Fatalf("rent = %+v", got.Rent)
	}
	for i, w := range wantRent {
		if o := got.Rent[i]; o.Name != w.name || o.Link != w.link || o.Price != w.price {
			t.Errorf("rent[%d] = %+v, want %+v", i, o, w)
		}
	}
	if len(got.Buy) != 1 || got.Buy[0].Price != "14.99 USD" || got.Buy[0].Link != "pb-hd" {
		t.Errorf("buy = %+v", got.Buy)
	}
	if len(got.Stream) != 0 {
		t.Errorf("a store was put in stream: %+v", got.Stream)
	}
}

// TestPricesAreComparedOnlyInOneCurrency: an amount the API could not
// write, or one in another currency, never displaces the first price.
func TestPricesAreComparedOnlyInOneCurrency(t *testing.T) {
	got := Shape([]StreamingOption{
		{Service: svc("apple", "Apple TV"), Type: "buy", Link: "a1", Price: price("9.99", "GBP", "£9.99")},
		{Service: svc("apple", "Apple TV"), Type: "buy", Link: "a2", Price: price("1.00", "USD", "1.00 USD")},
		{Service: svc("apple", "Apple TV"), Type: "buy", Link: "a3", Price: price("n/a", "GBP", "?")},
		{Service: svc("google", "Google Play"), Type: "rent", Link: "g1"},
		{Service: svc("google", "Google Play"), Type: "rent", Link: "g2", Price: price("2.49", "GBP", "£2.49")},
	}, time.Now())
	if len(got.Buy) != 1 || got.Buy[0].Link != "a1" || got.Buy[0].Price != "£9.99" {
		t.Errorf("buy = %+v", got.Buy)
	}
	// An option with no price at all gives way to one with a price.
	if len(got.Rent) != 1 || got.Rent[0].Link != "g2" || got.Rent[0].Price != "£2.49" {
		t.Errorf("rent = %+v", got.Rent)
	}
}

// TestAnOptionThatHasLeftIsLeftOut: the API can go on listing an option
// after its day; the reader would find nothing at the link.
func TestAnOptionThatHasLeftIsLeftOut(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	got := Shape([]StreamingOption{
		{Service: svc("netflix", "Netflix"), Type: "subscription", Link: "n", ExpiresOn: now.Add(-time.Hour).Unix()},
		{Service: svc("max", "Max"), Type: "subscription", Link: "m", ExpiresOn: now.Unix()},
		{Service: svc("hulu", "Hulu"), Type: "subscription", Link: "h", ExpiresOn: now.Add(time.Hour).Unix()},
	}, now)
	if n := names(got.Stream); !reflect.DeepEqual(n, []string{"Hulu"}) {
		t.Errorf("stream = %v, want only the one still there", n)
	}
}

func TestNextExpiryIsTheEarliestStillToCome(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	options := []StreamingOption{
		{Type: "subscription", ExpiresOn: now.Add(-time.Hour).Unix()},
		{Type: "subscription", ExpiresOn: now.Add(72 * time.Hour).Unix()},
		{Type: "subscription"},
		{Type: "rent", ExpiresOn: now.Add(24 * time.Hour).Unix()},
		{Type: "free", ExpiresOn: now.Unix()},
	}
	if got, want := NextExpiry(options, now), now.Add(24*time.Hour); !got.Equal(want) {
		t.Errorf("next = %v, want %v", got, want)
	}
	if got := NextExpiry([]StreamingOption{{Type: "subscription"}}, now); !got.IsZero() {
		t.Errorf("nothing leaving gave %v", got)
	}
}

// TestAnAnswerAlwaysHasItsFourLists: the page reads [] for nothing, never
// null, whether the answer was just shaped or read back.
func TestAnAnswerAlwaysHasItsFourLists(t *testing.T) {
	raw, err := json.Marshal(Shape(nil, time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"stream":[],"free":[],"rent":[],"buy":[]}` {
		t.Errorf("empty answer = %s", raw)
	}
	var back Answer
	if err := json.Unmarshal([]byte(`{"stream":null}`), &back); err != nil {
		t.Fatal(err)
	}
	back.Normalise()
	if raw, _ := json.Marshal(back); string(raw) != `{"stream":[],"free":[],"rent":[],"buy":[]}` {
		t.Errorf("normalised = %s", raw)
	}
	if !back.Empty() {
		t.Error("an empty answer is not Empty")
	}
}

func TestAmountsReadAsStringsOrNumbers(t *testing.T) {
	for raw, want := range map[string]Amount{
		`"3.99"`: {3.99, true},
		`3.99`:   {3.99, true},
		`"free"`: {},
		`null`:   {},
	} {
		var a Amount
		if err := json.Unmarshal([]byte(raw), &a); err != nil {
			t.Errorf("%s: %v", raw, err)
		}
		if a != want {
			t.Errorf("%s = %+v, want %+v", raw, a, want)
		}
	}
}
