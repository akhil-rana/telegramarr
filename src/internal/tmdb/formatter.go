package tmdb

import (
	"fmt"
	"strings"
)

// FormatMovieDetailsMessage formats movie details for a Telegram message caption
func FormatMovieDetailsMessage(movie *MovieDetails) string {
	var parts []string

	// Title (with original title if different)
	title := fmt.Sprintf("<b>%s</b>", escapeHTML(movie.Title))
	if movie.OriginalTitle != "" && movie.OriginalTitle != movie.Title {
		title += fmt.Sprintf(" <i>(%s)</i>", escapeHTML(movie.OriginalTitle))
	}
	// Add release year at the end with hyphen
	if movie.ReleaseDate != "" {
		releaseYear := strings.Split(movie.ReleaseDate, "-")[0]
		title += fmt.Sprintf(" - <b>%s</b>", releaseYear)
	}
	// Add IMDb link at the end if available
	if movie.ExternalIDs != nil && movie.ExternalIDs.IMDbID != "" {
		imdbLink := fmt.Sprintf("<a href=\"https://imdb.com/title/%s/\">IMDb</a>", movie.ExternalIDs.IMDbID)
		title += fmt.Sprintf(" - %s", imdbLink)
	}
	parts = append(parts, title)

	// Runtime with emoji and label
	if movie.Runtime > 0 {
		parts = append(parts, "")
		hours := movie.Runtime / 60
		minutes := movie.Runtime % 60
		parts = append(parts, fmt.Sprintf("⏱️ <b>Runtime:</b> %dh %dm", hours, minutes))
	}

	// Rating from TMDB (0-10 scale, displayed as 0-100 percentage)
	if movie.VoteAverage > 0 {
		parts = append(parts, "")
		rating := fmt.Sprintf("⭐ <b>TMDB Score:</b> %d%%", int(movie.VoteAverage*10))
		parts = append(parts, rating)
	}

	// Genres
	if len(movie.Genres) > 0 {
		parts = append(parts, "")
		genreNames := make([]string, len(movie.Genres))
		for i, g := range movie.Genres {
			genreNames[i] = escapeHTML(g.Name)
		}
		parts = append(parts, "<b>Genre:</b> "+strings.Join(genreNames, ", "))
	}

	// Tagline
	if movie.Tagline != "" {
		parts = append(parts, "")
		parts = append(parts, fmt.Sprintf("<i>\"%s\"</i>", escapeHTML(movie.Tagline)))
	}

	// Overview/Synopsis
	if movie.Overview != "" {
		parts = append(parts, "")
		parts = append(parts, escapeHTML(movie.Overview))
	}

	return strings.Join(parts, "\n")
}

// escapeHTML escapes special HTML characters for Telegram HTML entities
func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	s = strings.ReplaceAll(s, "'", "&#39;")
	return s
}
