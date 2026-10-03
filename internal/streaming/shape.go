package streaming

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// StreamingOption is one way to watch a movie, as the API describes it:
// one service, one kind of access, often one video quality. A service
// that streams a movie in HD and 4K is two of these.
type StreamingOption struct {
	Service Service `json:"service"`
	// Type is subscription, addon, free, rent or buy.
	Type string `json:"type"`
	// Addon is the channel an addon option is watched through, such as a
	// channel bought inside Prime Video. Only on Type addon.
	Addon *Service `json:"addon,omitempty"`
	// Link is the movie's own page on the service.
	Link  string `json:"link"`
	Price *Price `json:"price,omitempty"`
	// ExpiresOn is when the option leaves, in Unix seconds. Only an
	// option that is leaving has one.
	ExpiresOn   int64 `json:"expiresOn,omitempty"`
	ExpiresSoon bool  `json:"expiresSoon,omitempty"`
}

// Service is a streaming service, or an addon channel, with its logos.
type Service struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	ImageSet ImageSet `json:"imageSet"`
}

// ImageSet is a service's logos, one for each theme.
type ImageSet struct {
	LightThemeImage string `json:"lightThemeImage"`
	DarkThemeImage  string `json:"darkThemeImage"`
}

// Price is what renting or buying costs.
type Price struct {
	Amount    Amount `json:"amount"`
	Currency  string `json:"currency"`
	Formatted string `json:"formatted"`
}

// Amount is a price as a number. The API writes it as a string ("3.99");
// a bare number is read the same way, and anything else is no amount.
type Amount struct {
	Value float64
	OK    bool
}

// UnmarshalJSON reads "3.99" or 3.99.
func (a *Amount) UnmarshalJSON(b []byte) error {
	*a = Amount{}
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	var s string
	if b[0] == '"' {
		if err := json.Unmarshal(b, &s); err != nil {
			return nil
		}
	} else {
		s = string(b)
	}
	if v, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		*a = Amount{Value: v, OK: true}
	}
	return nil
}

// leftBy reports whether the option's day to leave has come. The API can
// go on listing an option for a while after it has gone, and a reader
// sent to it finds nothing there.
func (o StreamingOption) leftBy(now time.Time) bool {
	return o.ExpiresOn > 0 && !time.Unix(o.ExpiresOn, 0).After(now)
}

// Answer is where a movie can be watched in one country, as the page
// shows it. Every list is present, empty when there is nothing in it.
type Answer struct {
	// Stream is everything included with a subscription, the add-on
	// channels inside one among them.
	Stream []Offer `json:"stream"`
	Free   []Offer `json:"free"`
	Rent   []Offer `json:"rent"`
	Buy    []Offer `json:"buy"`
}

// Offer is one service in one of those lists.
type Offer struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Link opens the movie on the service.
	Link string `json:"link"`
	Logo Logo   `json:"logo"`
	// Price is what it costs to rent or buy, as the API writes it for
	// the country. Empty when there is no price.
	Price string `json:"price,omitempty"`
	// Via is the service an add-on channel is watched through.
	Via string `json:"via,omitempty"`
}

// Logo is a service's logo for each theme: Dark is drawn on a dark page,
// Light on a light one.
type Logo struct {
	Dark  string `json:"dark"`
	Light string `json:"light"`
}

// Empty reports whether the movie is on nothing at all.
func (a Answer) Empty() bool {
	return len(a.Stream) == 0 && len(a.Free) == 0 && len(a.Rent) == 0 && len(a.Buy) == 0
}

// Normalise gives every list a value, so an answer read back from
// storage is written to a reader with [] rather than null.
func (a *Answer) Normalise() {
	for _, list := range []*[]Offer{&a.Stream, &a.Free, &a.Rent, &a.Buy} {
		if *list == nil {
			*list = []Offer{}
		}
	}
}

// Shape turns the API's options into the page's four lists.
//
//   - Stream holds subscription and addon options. An add-on keeps its own
//     name and logo, and says which service it is watched through.
//   - Each list has one entry per service. The API lists a service once
//     for every quality it carries, and a reader has one choice to make.
//   - Rent and buy keep the lowest price a service asks, and the link
//     that goes with it.
//   - Everything stays in the order the API gave it.
//
// An option whose day to leave has come by now is left out.
func Shape(options []StreamingOption, now time.Time) Answer {
	stream, free := newGroup(), newGroup()
	rent, buy := newGroup(), newGroup()
	for _, o := range options {
		if o.leftBy(now) {
			continue
		}
		switch o.Type {
		case "subscription":
			stream.add(offerOf(o.Service, o.Link), nil)
		case "addon":
			if o.Addon == nil {
				stream.add(offerOf(o.Service, o.Link), nil)
				continue
			}
			offer := offerOf(*o.Addon, o.Link)
			offer.Via = o.Service.Name
			stream.add(offer, nil)
		case "free":
			free.add(offerOf(o.Service, o.Link), nil)
		case "rent":
			rent.add(offerOf(o.Service, o.Link), o.Price)
		case "buy":
			buy.add(offerOf(o.Service, o.Link), o.Price)
		}
	}
	return Answer{Stream: stream.offers, Free: free.offers, Rent: rent.offers, Buy: buy.offers}
}

// NextExpiry is when the first of these options leaves, or zero when none
// is leaving. Only a time after now counts: an option whose day has come
// has gone already, and asking again about it would find nothing new.
func NextExpiry(options []StreamingOption, now time.Time) time.Time {
	var next time.Time
	for _, o := range options {
		if o.ExpiresOn <= 0 {
			continue
		}
		at := time.Unix(o.ExpiresOn, 0).UTC()
		if !at.After(now) {
			continue
		}
		if next.IsZero() || at.Before(next) {
			next = at
		}
	}
	return next
}

func offerOf(s Service, link string) Offer {
	return Offer{
		ID:   s.ID,
		Name: s.Name,
		Link: link,
		Logo: Logo{Dark: s.ImageSet.DarkThemeImage, Light: s.ImageSet.LightThemeImage},
	}
}

// group is one list being built: an entry per service, in the order each
// service first appeared, with the price each entry stands at.
type group struct {
	offers []Offer
	at     map[string]int
	prices []*Price
}

func newGroup() *group { return &group{offers: []Offer{}, at: map[string]int{}} }

// add keeps the first entry for a service, and replaces its price and
// link with a later one's when that one is cheaper. Prices in two
// currencies are not compared: the first stands.
func (g *group) add(o Offer, price *Price) {
	key := o.ID
	if key == "" {
		key = o.Name
	}
	if price != nil {
		o.Price = strings.TrimSpace(price.Formatted)
	}
	i, seen := g.at[key]
	if !seen {
		g.at[key] = len(g.offers)
		g.offers = append(g.offers, o)
		g.prices = append(g.prices, price)
		return
	}
	if cheaper(price, g.prices[i]) {
		g.offers[i].Price = o.Price
		g.offers[i].Link = o.Link
		g.prices[i] = price
	}
}

// cheaper reports whether a is a lower price than b. A price with no
// amount is never cheaper than one with an amount, and anything with an
// amount is cheaper than no price at all.
func cheaper(a, b *Price) bool {
	if a == nil || !a.Amount.OK {
		return false
	}
	if b == nil || !b.Amount.OK {
		return true
	}
	if !strings.EqualFold(a.Currency, b.Currency) {
		return false
	}
	return a.Amount.Value < b.Amount.Value
}
