package uploader

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
)

const (
	// Split sizes - using binary units (GiB) so they display correctly everywhere
	// These are legacy constants; RAR splitting is now used instead
	PremiumMaxSize    int64 = 4 * 1024 * 1024 * 1024 // 4 GiB (4,294,967,296 bytes)
	NonPremiumMaxSize int64 = 2 * 1024 * 1024 * 1024 // 2 GiB (2,147,483,648 bytes)
	ChunkSize         int64 = 32 * 1024 * 1024       // 32 MiB chunks for reading
)

// UploadProgress tracks upload progress
type UploadProgress struct {
	TotalFiles      int64
	ProcessedFiles  int64
	CurrentFile     string
	TotalBytes      int64
	ProcessedBytes  int64
	PercentComplete int
}

// Uploader handles file uploads to Telegram
type Uploader struct {
	logger    *zap.Logger
	isPremium bool
	tempDir   string
}

// NewUploader creates a new uploader
func NewUploader(logger *zap.Logger, isPremium bool, tempDir string) *Uploader {
	return &Uploader{
		logger:    logger,
		isPremium: isPremium,
		tempDir:   tempDir,
	}
}

// ExtractIMDbID extracts IMDb ID from filename
// Example: "Akira (1988) [...] [imdbid-tt0094625].mkv" -> "tt0094625"
func ExtractIMDbID(filename string) string {
	// Look for [imdbid-ttXXXXXXX] pattern
	start := strings.Index(filename, "[imdbid-")
	if start == -1 {
		return ""
	}
	start += 8 // len("[imdbid-")
	end := strings.Index(filename[start:], "]")
	if end == -1 {
		return ""
	}
	return filename[start : start+end]
}

// SplitFilePath represents a split file part
type SplitFilePath struct {
	Path      string
	Number    int
	TotalSize int64
	FileSize  int64
}

// SplitFile splits a file into chunks based on user's premium status
func (u *Uploader) SplitFile(ctx context.Context, filePath string, progressChan chan<- UploadProgress) ([]SplitFilePath, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	totalSize := fileInfo.Size()
	maxSize := NonPremiumMaxSize
	if u.isPremium {
		maxSize = PremiumMaxSize
	}

	// If file is smaller than max size, no need to split
	if totalSize <= maxSize {
		u.logger.Info("File doesn't need splitting", zap.String("file", filePath), zap.Int64("size", totalSize))
		return []SplitFilePath{
			{
				Path:      filePath,
				Number:    1,
				TotalSize: totalSize,
				FileSize:  totalSize,
			},
		}, nil
	}

	// Calculate number of parts needed
	numParts := (totalSize + maxSize - 1) / maxSize
	u.logger.Info("Splitting file", zap.String("file", filePath), zap.Int64("size", totalSize), zap.Int64("parts", numParts))

	var splitFiles []SplitFilePath
	filename := filepath.Base(filePath)
	baseDir := filepath.Dir(filePath)

	for part := 1; part <= int(numParts); part++ {
		partFilePath := filepath.Join(baseDir, fmt.Sprintf("%s.part%d", filename, part))

		// Create part file
		partFile, err := os.Create(partFilePath)
		if err != nil {
			u.logger.Error("Failed to create part file", zap.Error(err), zap.String("path", partFilePath))
			return nil, fmt.Errorf("failed to create part file: %w", err)
		}

		// Copy data
		written, err := io.CopyN(partFile, file, maxSize)
		partFile.Close()

		if err != nil && err != io.EOF {
			u.logger.Error("Failed to copy part", zap.Error(err))
			os.Remove(partFilePath)
			return nil, fmt.Errorf("failed to copy part %d: %w", part, err)
		}

		splitFiles = append(splitFiles, SplitFilePath{
			Path:      partFilePath,
			Number:    part,
			TotalSize: totalSize,
			FileSize:  written,
		})

		u.logger.Info("Created split part", zap.Int("part", part), zap.Int64("size", written))

		// Send progress
		if progressChan != nil {
			select {
			case progressChan <- UploadProgress{
				TotalFiles:     int64(numParts),
				ProcessedFiles: int64(part),
				CurrentFile:    partFilePath,
			}:
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		if err == io.EOF {
			break
		}
	}

	return splitFiles, nil
}

// CleanupSplitFiles removes split file parts
func (u *Uploader) CleanupSplitFiles(splitFiles []SplitFilePath) error {
	for _, sf := range splitFiles {
		// Only cleanup if it's a split part (has .partN suffix)
		if strings.Contains(sf.Path, ".part") {
			if err := os.Remove(sf.Path); err != nil && !os.IsNotExist(err) {
				u.logger.Error("Failed to cleanup split file", zap.Error(err), zap.String("path", sf.Path))
			}
		}
	}
	return nil
}

// GetTotalSize calculates total size of split files
func (u *Uploader) GetTotalSize(files []SplitFilePath) int64 {
	total := int64(0)
	for _, f := range files {
		total += f.FileSize
	}
	return total
}
