package ampapi

// SearchResp represents the top-level response from the search API.
type SearchResp struct {
	Results SearchResults `json:"results"`
}

// SearchResults contains the different types of search results.
type SearchResults struct {
	Songs       *SongResults       `json:"songs,omitempty"`
	MusicVideos *MusicVideoResults `json:"music-videos,omitempty"`
	Albums      *AlbumResults      `json:"albums,omitempty"`
	Artists     *ArtistResults     `json:"artists,omitempty"`
}

// SongResults contains a list of song search results.
type SongResults struct {
	Href string         `json:"href"`
	Next string         `json:"next"`
	Data []SongRespData `json:"data"`
}

type MusicVideoResults struct {
	Href string                     `json:"href"`
	Next string                     `json:"next"`
	Data []SearchMusicVideoRespData `json:"data"`
}

type SearchMusicVideoRespData struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Href       string `json:"href"`
	Attributes struct {
		Name             string   `json:"name"`
		ArtistName       string   `json:"artistName"`
		URL              string   `json:"url"`
		GenreNames       []string `json:"genreNames"`
		DurationInMillis int      `json:"durationInMillis"`
	} `json:"attributes"`
}

// AlbumResults contains a list of album search results.
type AlbumResults struct {
	Href string          `json:"href"`
	Next string          `json:"next"`
	Data []AlbumRespData `json:"data"`
}

// ArtistResults contains a list of artist search results.
type ArtistResults struct {
	Href string `json:"href"`
	Next string `json:"next"`
	Data []struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Href       string `json:"href"`
		Attributes struct {
			Name       string   `json:"name"`
			GenreNames []string `json:"genreNames"`
			URL        string   `json:"url"`
		} `json:"attributes"`
	} `json:"data"`
}
