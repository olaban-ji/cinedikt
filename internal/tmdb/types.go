package tmdb

// Movie is one movie in a search result. Its id is what the catalog
// matched to an IMDb title when it was imported, so it is all a search
// fallback needs.
type Movie struct {
	ID int `json:"id"`
}

// SearchResults is the first page of a movie search.
type SearchResults struct {
	Results []Movie `json:"results"`
}
