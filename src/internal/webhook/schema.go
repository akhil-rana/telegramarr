package webhook

// RadarrWebhookPayload represents the payload from Radarr webhook
type RadarrWebhookPayload struct {
	Movie struct {
		ID          int    `json:"id"`
		Title       string `json:"title"`
		Year        int    `json:"year"`
		ReleaseDate string `json:"releaseDate"`
		FolderPath  string `json:"folderPath"`
		TmdbID      int    `json:"tmdbId"`
		ImdbID      string `json:"imdbId"`
		Overview    string `json:"overview"`
	} `json:"movie"`
	RemoteMovie struct {
		TmdbID int    `json:"tmdbId"`
		ImdbID string `json:"imdbId"`
		Title  string `json:"title"`
		Year   int    `json:"year"`
	} `json:"remoteMovie"`
	MovieFile struct {
		ID             int    `json:"id"`
		RelativePath   string `json:"relativePath"`
		Path           string `json:"path"`
		Quality        string `json:"quality"`
		QualityVersion int    `json:"qualityVersion"`
		ReleaseGroup   string `json:"releaseGroup"`
		SceneName      string `json:"sceneName"`
		IndexerFlags   string `json:"indexerFlags"`
		Size           int64  `json:"size"`
		DateAdded      string `json:"dateAdded"`
		MediaInfo      struct {
			AudioChannels         float64  `json:"audioChannels"`
			AudioCodec            string   `json:"audioCodec"`
			AudioLanguages        []string `json:"audioLanguages"`
			Height                int      `json:"height"`
			Width                 int      `json:"width"`
			Subtitles             []string `json:"subtitles"`
			VideoCodec            string   `json:"videoCodec"`
			VideoDynamicRange     string   `json:"videoDynamicRange"`
			VideoDynamicRangeType string   `json:"videoDynamicRangeType"`
		} `json:"mediaInfo"`
	} `json:"movieFile"`
	IsUpgrade          bool          `json:"isUpgrade"`
	DownloadClient     string        `json:"downloadClient"`
	DownloadClientType string        `json:"downloadClientType"`
	DownloadID         string        `json:"downloadId"`
	DeletedFiles       []interface{} `json:"deletedFiles"`
	CustomFormatInfo   struct {
		CustomFormats []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"customFormats"`
		CustomFormatScore int `json:"customFormatScore"`
	} `json:"customFormatInfo"`
	Release struct {
		ReleaseTitle string `json:"releaseTitle"`
		Indexer      string `json:"indexer"`
		Size         int64  `json:"size"`
	} `json:"release"`
	EventType      string `json:"eventType"`
	InstanceName   string `json:"instanceName"`
	ApplicationURL string `json:"applicationUrl"`
}

// SonarrWebhookPayload represents the payload from Sonarr webhook
type SonarrWebhookPayload struct {
	Series struct {
		ID        int    `json:"id"`
		Title     string `json:"title"`
		TitleSlug string `json:"titleSlug"`
		Path      string `json:"path"`
		TvdbID    int    `json:"tvdbId"`
		TvMazeID  int    `json:"tvMazeId"`
		ImdbID    string `json:"imdbId"`
		Type      string `json:"type"`
		Year      int    `json:"year"`
	} `json:"series"`
	Episodes []struct {
		ID            int    `json:"id"`
		EpisodeNumber int    `json:"episodeNumber"`
		SeasonNumber  int    `json:"seasonNumber"`
		Title         string `json:"title"`
		Overview      string `json:"overview"`
		AirDate       string `json:"airDate"`
		AirDateUtc    string `json:"airDateUtc"`
		SeriesID      int    `json:"seriesId"`
		TvdbID        int    `json:"tvdbId"`
	} `json:"episodes"`
	EpisodeFile struct {
		ID             int    `json:"id"`
		RelativePath   string `json:"relativePath"`
		Path           string `json:"path"`
		Quality        string `json:"quality"`
		QualityVersion int    `json:"qualityVersion"`
		ReleaseGroup   string `json:"releaseGroup"`
		SceneName      string `json:"sceneName"`
		Size           int64  `json:"size"`
		DateAdded      string `json:"dateAdded"`
		MediaInfo      struct {
			AudioChannels         float64  `json:"audioChannels"`
			AudioCodec            string   `json:"audioCodec"`
			AudioLanguages        []string `json:"audioLanguages"`
			Height                int      `json:"height"`
			Width                 int      `json:"width"`
			Subtitles             []string `json:"subtitles"`
			VideoCodec            string   `json:"videoCodec"`
			VideoDynamicRange     string   `json:"videoDynamicRange"`
			VideoDynamicRangeType string   `json:"videoDynamicRangeType"`
		} `json:"mediaInfo"`
	} `json:"episodeFile"`
	IsUpgrade          bool          `json:"isUpgrade"`
	DownloadClient     string        `json:"downloadClient"`
	DownloadClientType string        `json:"downloadClientType"`
	DownloadID         string        `json:"downloadId"`
	DeletedFiles       []interface{} `json:"deletedFiles"`
	CustomFormatInfo   struct {
		CustomFormats []struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		} `json:"customFormats"`
		CustomFormatScore int `json:"customFormatScore"`
	} `json:"customFormatInfo"`
	Release struct {
		ReleaseTitle string `json:"releaseTitle"`
		Indexer      string `json:"indexer"`
		Size         int64  `json:"size"`
	} `json:"release"`
	EventType      string `json:"eventType"`
	InstanceName   string `json:"instanceName"`
	ApplicationURL string `json:"applicationUrl"`
}

// ValidateRadarrPayload validates the Radarr webhook payload
func ValidateRadarrPayload(payload *RadarrWebhookPayload) bool {
	if payload == nil {
		return false
	}
	// Check required fields
	if payload.Movie.Title == "" || payload.MovieFile.RelativePath == "" {
		return false
	}
	return true
}

// ValidateSonarrPayload validates the Sonarr webhook payload
func ValidateSonarrPayload(payload *SonarrWebhookPayload) bool {
	if payload == nil {
		return false
	}
	// Check required fields
	if payload.Series.Title == "" || payload.EpisodeFile.RelativePath == "" {
		return false
	}
	return true
}
