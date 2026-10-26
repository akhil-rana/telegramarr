package uploader

import (
	"fmt"
	"path/filepath"
	"strings"
)

// ShortenerConfig holds configuration for filename shortening
type ShortenerConfig struct {
	MovieTitle   string
	Quality      string // e.g., "1080p", "720p"
	Format       string // e.g., "x264", "x265", "h264"
	ReleaseGroup string
	PartNumber   int // 1, 2, 3, etc.
}

// ShortenRarBasename generates a Telegram-compatible RAR basename (max 60 chars when including .partXX.rar)
// RAR will automatically add .part1.rar, .part2.rar, etc.
// This function returns just the basename to use, and calculates length with .partXX.rar considered
// Strategy:
// 1. Try original filename if basename + .partXX.rar <= 60 chars
// 2. Use smart abbreviation of metadata
// 3. Always include first 5 letters of movie title as fallback
func ShortenRarBasename(originalFilename string, config ShortenerConfig) string {
	// RAR will add .partXX.rar automatically, we just need the base name
	// Calculate the part suffix length for longest possible part number
	// Assume max 99 parts (unlikely but safe): ".part99.rar" = 11 chars
	maxPartSuffixLen := len(fmt.Sprintf(".part99.rar"))
	maxBaseLen := 60 - maxPartSuffixLen // Leave room for .partXX.rar

	// First, try original filename without extension
	baseName := strings.TrimSuffix(originalFilename, filepath.Ext(originalFilename))
	if len(baseName) <= maxBaseLen {
		return baseName
	}

	// Build components with abbreviations
	titleAbbrev := abbreviateTitle(config.MovieTitle)
	qualityShort := abbreviateQuality(config.Quality)
	formatShort := config.Format
	groupShort := config.ReleaseGroup

	// Try: title_quality_format_group
	candidate := fmt.Sprintf("%s_%s_%s_%s", titleAbbrev, qualityShort, formatShort, groupShort)
	if len(candidate) <= maxBaseLen {
		return candidate
	}

	// Try: title_quality_format (drop release group)
	candidate = fmt.Sprintf("%s_%s_%s", titleAbbrev, qualityShort, formatShort)
	if len(candidate) <= maxBaseLen {
		return candidate
	}

	// Try: title_quality (drop format and group)
	candidate = fmt.Sprintf("%s_%s", titleAbbrev, qualityShort)
	if len(candidate) <= maxBaseLen {
		return candidate
	}

	// Last resort: Ensure at least first 5 chars of movie title
	minTitle := minStringLength(titleAbbrev, 5)

	// Try with shortened release group
	if len(groupShort) > 0 {
		shortGroup := minStringLength(groupShort, 10)
		candidate = fmt.Sprintf("%s_%s_%s", minTitle, qualityShort, shortGroup)
		if len(candidate) <= maxBaseLen {
			return candidate
		}
	}

	// Try just: title_quality_group
	candidate = fmt.Sprintf("%s_%s_%s", minTitle, qualityShort, groupShort)
	if len(candidate) <= maxBaseLen {
		return candidate
	}

	// Try just: title_quality
	candidate = fmt.Sprintf("%s_%s", minTitle, qualityShort)
	if len(candidate) <= maxBaseLen {
		return candidate
	}

	// Absolute last: Just minimum title and quality, trim quality if needed
	minQuality := minStringLength(qualityShort, 4) // Keep at least "1080" or "720p"
	candidate = fmt.Sprintf("%s_%s", minTitle, minQuality)
	if len(candidate) <= maxBaseLen {
		return candidate
	}

	// Final fallback: trim base name to fit
	return minStringLength(candidate, maxBaseLen)
}

// abbreviateTitle creates an abbreviation from the title
// Multi-word titles: first letters (e.g., "Lord of the Rings" -> "LotR")
// Single/two word: keep as is (e.g., "Avengers" -> "Avengers", "The Matrix" -> "TheMatrix")
func abbreviateTitle(title string) string {
	title = strings.TrimSpace(title)
	words := strings.Fields(title)

	if len(words) == 0 {
		return "Movie"
	}

	if len(words) <= 2 {
		// Single or two words: keep as is, remove spaces and special chars
		return sanitizeFilename(strings.Join(words, ""))
	}

	// Multi-word: take first letter of each word
	var abbrev strings.Builder
	for _, word := range words {
		if len(word) > 0 {
			abbrev.WriteString(strings.ToUpper(string(word[0])))
		}
	}
	return abbrev.String()
}

// abbreviateQuality shortens quality string
// 1080p -> 1080p, 720p -> 720p, BluRay -> BR, WEB-DL -> WEB
func abbreviateQuality(quality string) string {
	quality = strings.TrimSpace(quality)
	if quality == "" {
		return "UNK"
	}

	// Common abbreviations
	if strings.Contains(strings.ToLower(quality), "2160") {
		return "2160p"
	}
	if strings.Contains(strings.ToLower(quality), "1080") {
		return "1080p"
	}
	if strings.Contains(strings.ToLower(quality), "720") {
		return "720p"
	}
	if strings.Contains(strings.ToLower(quality), "480") {
		return "480p"
	}

	// Source-based abbreviations
	if strings.EqualFold(quality, "BluRay") || strings.EqualFold(quality, "Blu-ray") {
		return "BR"
	}
	if strings.Contains(strings.ToLower(quality), "web") {
		return "WEB"
	}
	if strings.EqualFold(quality, "HDTV") {
		return "HDTV"
	}

	// Return first 8 chars of quality if no match
	if len(quality) > 8 {
		return quality[:8]
	}
	return quality
}

// sanitizeFilename removes special characters from filename
func sanitizeFilename(name string) string {
	// Replace common special chars with nothing
	replacer := strings.NewReplacer(
		".", "",
		"-", "",
		" ", "",
		":", "",
		"/", "",
		"\\", "",
	)
	return replacer.Replace(name)
}

// minStringLength ensures string is at least minLen chars, otherwise returns original
// If string is longer than maxLen, trims to maxLen
func minStringLength(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s
}
