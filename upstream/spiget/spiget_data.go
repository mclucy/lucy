package spiget

// searchResponse mirrors Spiget's resource search payload.
type searchResponse []resourceResponse

// resourceResponse mirrors the subset of Spiget's Resource payload Lucy needs.
type resourceResponse struct {
	ID             int64                  `json:"id"`
	Name           string                 `json:"name"`
	Tag            string                 `json:"tag"`
	Contributors   string                 `json:"contributors"`
	TestedVersions []string               `json:"testedVersions"`
	Links          map[string]string      `json:"links"`
	External       bool                   `json:"external"`
	File           resourceFileResponse   `json:"file"`
	Author         idReference            `json:"author"`
	Category       idReference            `json:"category"`
	Version        resourceVersionPointer `json:"version"`
	Versions       []idReference          `json:"versions"`
	Description    string                 `json:"description"`
	Documentation  string                 `json:"documentation"`
	SourceCodeLink string                 `json:"sourceCodeLink"`
	DonationLink   string                 `json:"donationLink"`
	Premium        bool                   `json:"premium"`
	Downloads      int64                  `json:"downloads"`
}

type resourceFileResponse struct {
	Type        string  `json:"type"`
	Size        float64 `json:"size"`
	SizeUnit    string  `json:"sizeUnit"`
	URL         string  `json:"url"`
	ExternalURL string  `json:"externalUrl"`
}

type resourceVersionPointer struct {
	ID   int64  `json:"id"`
	UUID string `json:"uuid"`
}

type versionResponse struct {
	ID          int64          `json:"id"`
	UUID        string         `json:"uuid"`
	Name        string         `json:"name"`
	Resource    int64          `json:"resource"`
	Downloads   int64          `json:"downloads"`
	ReleaseDate int64          `json:"releaseDate"`
	Rating      ratingResponse `json:"rating"`
}

type idReference struct {
	ID int64 `json:"id"`
}

type ratingResponse struct {
	Count   int64   `json:"count"`
	Average float64 `json:"average"`
}
