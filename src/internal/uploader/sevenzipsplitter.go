package uploader

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// SevenZipSplitter handles splitting files into 7z archives
type SevenZipSplitter struct {
	logger *zap.Logger
}

// NewSevenZipSplitter creates a new 7z splitter
func NewSevenZipSplitter(logger *zap.Logger) *SevenZipSplitter {
	return &SevenZipSplitter{
		logger: logger,
	}
}

// SplitFile splits a file into multiple 7z parts using 7z command line
// Each 7z part will contain a portion of the original file without compression
// isPremium determines the max file size (2GB for free, 4GB for premium)
// progressCallback is called with (bytesProcessed, totalBytes, percentage)
// Creates a subfolder named after the original file in outputDir
// archiveNameConfig: optional configuration for shortened 7z filename (nil = use original)
func (sz *SevenZipSplitter) SplitFile(inputPath string, outputDir string, partSize int64, isPremium bool, progressCallback func(bytesProcessed, totalBytes int64, percent int), archiveNameConfig *ShortenerConfig) ([]string, error) {
	fileInfo, err := os.Stat(inputPath)
	if err != nil {
		sz.logger.Error("Failed to stat file", zap.String("file", inputPath), zap.Error(err))
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	totalSize := fileInfo.Size()
	fileName := filepath.Base(inputPath)
	fileNameWithoutExt := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	// Create subfolder in output directory for this file
	fileSubDir := filepath.Join(outputDir, fileNameWithoutExt)

	// 7z split sizes (same as RAR)
	// 7z split: 4000 MiB for premium, 2000 MiB for free
	maxPartSize := int64(2000 * 1024 * 1024) // 2000 MiB for free
	if isPremium {
		maxPartSize = int64(4000 * 1024 * 1024) // 4000 MiB for premium
	}

	// Calculate the archive base name
	archiveBaseName := fileNameWithoutExt
	if archiveNameConfig != nil {
		// Use shortened basename for 7z - similar to RAR shortening
		archiveBaseName = ShortenRarBasename(fileName, *archiveNameConfig)
		sz.logger.Info("Using shortened 7z basename", zap.String("base", archiveBaseName), zap.String("config", fmt.Sprintf("%+v", archiveNameConfig)))
	}

	// Check if 7z parts already exist with correct sizes
	existingParts := sz.getExistingSevenZipParts(fileSubDir, archiveBaseName)
	if len(existingParts) > 0 && sz.verifySevenZipPartSizes(fileSubDir, archiveBaseName, totalSize, maxPartSize) {
		sz.logger.Info("7z parts already exist and are valid, reusing them", zap.String("file", inputPath), zap.Int("parts", len(existingParts)))
		// Report 100% progress
		progressCallback(totalSize, totalSize, 100)
		return existingParts, nil
	}

	// Create the output subdirectory for this file
	if err := os.MkdirAll(fileSubDir, 0755); err != nil {
		sz.logger.Error("Failed to create 7z output directory", zap.String("dir", fileSubDir), zap.Error(err))
		return nil, fmt.Errorf("failed to create 7z output directory: %w", err)
	}

	// Build the 7z command
	// Strategy: Change to the file's directory, archive only the filename
	// This prevents directory structure from being stored in the archive
	inputDir := filepath.Dir(inputPath)
	inputFileName := filepath.Base(inputPath)
	volumeSize := fmt.Sprintf("%dm", maxPartSize/(1024*1024))

	// Archive path - we need to make this relative to inputDir since that's where 7z will run from
	// Calculate relative path from inputDir to fileSubDir
	relativeArchivePath, err := filepath.Rel(inputDir, filepath.Join(fileSubDir, archiveBaseName))
	if err != nil {
		// If we can't calculate relative path, use absolute path
		relativeArchivePath = filepath.Join(fileSubDir, archiveBaseName)
		sz.logger.Warn("Failed to calculate relative path, using absolute", zap.Error(err), zap.String("absolute", relativeArchivePath))
	}

	cmd := exec.Command("7z", "a",
		"-tzip",                         // Use zip format (compatible with most systems)
		fmt.Sprintf("-v%s", volumeSize), // Volume size (split size)
		"-mx=0",                         // No compression (store only)
		relativeArchivePath,             // Output archive path (relative to inputDir)
		inputFileName)                   // Just the filename

	// Set working directory to the file's directory
	// So "inputFileName" resolves correctly and archiveOutputPath is relative from here
	cmd.Dir = inputDir

	archiveOutputPath := filepath.Join(fileSubDir, archiveBaseName)
	sz.logger.Info("Starting 7z split", zap.String("file", inputPath), zap.String("volume_size", volumeSize), zap.String("archive", archiveOutputPath), zap.String("cwd", inputDir), zap.String("relative_archive", relativeArchivePath))

	// Report initial progress
	progressCallback(0, totalSize, 0)

	// Capture stderr to log any errors
	var stderr strings.Builder
	cmd.Stderr = &stderr

	// Start the process
	if err := cmd.Start(); err != nil {
		sz.logger.Error("Failed to start 7z command", zap.Error(err))
		return nil, fmt.Errorf("failed to start 7z command: %w", err)
	}

	// Monitor progress while the command runs
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	lastProgressReport := time.Now()

	for {
		select {
		case err := <-done:
			if err != nil {
				stderrOutput := stderr.String()
				sz.logger.Error("7z command failed", zap.Error(err), zap.String("stderr", stderrOutput))
				return nil, fmt.Errorf("7z command failed: %w, stderr: %s", err, stderrOutput)
			}
			// Command completed successfully
			progressCallback(totalSize, totalSize, 100)
			sz.logger.Info("7z split finished", zap.String("archive_output_path", archiveOutputPath))
			goto completed

		case <-ticker.C:
			// Report progress based on created archive parts
			parts, _ := sz.getSevenZipPartFiles(fileSubDir, archiveBaseName)

			// Debug logging for progress
			if len(parts) == 0 {
				sz.logger.Debug("No archive parts found yet", zap.String("dir", fileSubDir), zap.String("basename", archiveBaseName))
			}

			var writtenSize int64
			for _, part := range parts {
				info, err := os.Stat(part)
				if err == nil {
					writtenSize += info.Size()
				}
			}
			if writtenSize > 0 {
				percent := int((writtenSize * 100) / totalSize)
				if percent > 100 {
					percent = 100
				}
				// Report progress every 2 seconds or when we reach 100%
				if time.Since(lastProgressReport) >= 2*time.Second || percent == 100 {
					progressCallback(writtenSize, totalSize, percent)
					lastProgressReport = time.Now()
					sz.logger.Info("7z progress", zap.Int("percent", percent), zap.Int64("bytes", writtenSize), zap.Int("parts", len(parts)))
				}
			}
		}
	}

completed:

	// Get list of created 7z parts
	sevenZipParts, err := sz.getSevenZipPartFiles(fileSubDir, archiveBaseName)
	if err != nil {
		sz.logger.Error("Failed to get 7z part files", zap.Error(err))
		return nil, fmt.Errorf("failed to get 7z part files: %w", err)
	}

	sz.logger.Info("7z split completed", zap.String("file", inputPath), zap.Int("parts", len(sevenZipParts)))
	return sevenZipParts, nil
}

// getSevenZipPartFiles returns list of 7z part files created
func (sz *SevenZipSplitter) getSevenZipPartFiles(dir, baseName string) ([]string, error) {
	var parts []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to read directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Match pattern: basename.001, basename.002, basename.zip.001, basename.zip.002, etc.
		// 7z creates numbered parts like: archive.001, archive.002
		if strings.HasPrefix(name, baseName) {
			parts = append(parts, filepath.Join(dir, name))
		}
	}

	return parts, nil
}

// getExistingSevenZipParts returns list of existing 7z part files
func (sz *SevenZipSplitter) getExistingSevenZipParts(dir, baseName string) []string {
	parts, err := sz.getSevenZipPartFiles(dir, baseName)
	if err != nil {
		return nil
	}
	return parts
}

// verifySevenZipPartSizes checks if existing 7z parts have correct sizes
func (sz *SevenZipSplitter) verifySevenZipPartSizes(dir, baseName string, totalSize, maxPartSize int64) bool {
	parts, err := sz.getSevenZipPartFiles(dir, baseName)
	if err != nil || len(parts) == 0 {
		return false
	}

	var totalPartSize int64
	for _, part := range parts {
		info, err := os.Stat(part)
		if err != nil {
			return false
		}
		totalPartSize += info.Size()
	}

	// Check if total matches original file size (within reasonable margin)
	// 7z adds headers, so total might be slightly larger
	return totalPartSize >= totalSize && totalPartSize <= totalSize+int64(100*1024) // Allow 100KB overhead per part
}

// CleanupFiles removes all 7z part files and their parent folder
func (sz *SevenZipSplitter) CleanupFiles(filePaths []string) {
	if len(filePaths) == 0 {
		return
	}

	// Get the parent directory of the first 7z file (all should be in same folder)
	parentDir := filepath.Dir(filePaths[0])

	// Remove individual 7z files first
	for _, file := range filePaths {
		if err := os.Remove(file); err != nil {
			sz.logger.Warn("Failed to remove 7z file", zap.String("file", file), zap.Error(err))
		} else {
			sz.logger.Debug("Removed 7z file", zap.String("file", file))
		}
	}

	// Remove the parent folder
	if err := os.RemoveAll(parentDir); err != nil {
		sz.logger.Warn("Failed to remove 7z folder", zap.String("folder", parentDir), zap.Error(err))
	} else {
		sz.logger.Debug("Removed 7z folder", zap.String("folder", parentDir))
	}
}
