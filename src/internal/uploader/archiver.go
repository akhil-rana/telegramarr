package uploader

import "go.uber.org/zap"

// Archiver defines the interface for file splitting archives (RAR or 7z)
type Archiver interface {
	// SplitFile splits a file into multiple archive parts
	// inputPath: path to the file to split
	// outputDir: directory to write archive parts
	// partSize: max size per part (0 = auto)
	// isPremium: determines max file size (2GB for free, 4GB for premium)
	// progressCallback: called with (bytesProcessed, totalBytes, percentage)
	// archiveNameConfig: optional configuration for shortened archive filename (nil = use original)
	// Returns: list of created archive part file paths
	SplitFile(inputPath string, outputDir string, partSize int64, isPremium bool, progressCallback func(bytesProcessed, totalBytes int64, percent int), archiveNameConfig *ShortenerConfig) ([]string, error)

	// CleanupFiles removes the specified archive part files
	CleanupFiles(filePaths []string)
}

// NewArchiver creates a new Archiver instance based on format
// format should be "rar" or "7z"
func NewArchiver(format string, logger *zap.Logger) Archiver {
	switch format {
	case "7z":
		return NewSevenZipSplitter(logger)
	case "rar":
		fallthrough
	default:
		return NewRarSplitter(logger)
	}
}
