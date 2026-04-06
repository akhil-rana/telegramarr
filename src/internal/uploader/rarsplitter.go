package uploader

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"
)

// RarSplitter handles splitting files into RAR archives
type RarSplitter struct {
	logger *zap.Logger
}

// NewRarSplitter creates a new RAR splitter
func NewRarSplitter(logger *zap.Logger) *RarSplitter {
	return &RarSplitter{
		logger: logger,
	}
}

// SplitFile splits a file into multiple RAR parts using WinRAR command line
// Each RAR part will contain a portion of the original file without compression
// isPremium determines the max file size (2GB for free, 4GB for premium)
// progressCallback is called with (bytesProcessed, totalBytes, percentage)
// If RAR files already exist with correct sizes, they will be reused (no re-splitting)
// Creates a subfolder named after the original file in outputDir
// rarNameConfig: optional configuration for shortened RAR filename (nil = use original)
func (rs *RarSplitter) SplitFile(inputPath string, outputDir string, partSize int64, isPremium bool, progressCallback func(bytesProcessed, totalBytes int64, percent int), rarNameConfig *ShortenerConfig) ([]string, error) {
	fileInfo, err := os.Stat(inputPath)
	if err != nil {
		rs.logger.Error("Failed to stat file", zap.String("file", inputPath), zap.Error(err))
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	totalSize := fileInfo.Size()
	fileName := filepath.Base(inputPath)
	fileNameWithoutExt := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	// Create subfolder in output directory for this file
	fileSubDir := filepath.Join(outputDir, fileNameWithoutExt)

	// RAR file split sizes
	// RAR split: 4000 MiB for premium, 2000 MiB for free
	// Effective size: slightly less for Telegram compatibility (4096 part limit × 512KB parts)
	var rarVolumeMB int64
	if isPremium {
		rarVolumeMB = 4000 // 4GB for premium
	} else {
		rarVolumeMB = 2000 // 2GB for free
	}

	// Also track the effective part size for progress/logging
	// This is what Telegram can actually receive (4096 parts × 512KB)
	var effectivePartSize int64
	if isPremium {
		effectivePartSize = int64(4193452032) // 4GB limit shown to Telegram (3999 MiB)
	} else {
		effectivePartSize = int64(2097152000) // 2GB limit shown to Telegram (2000 MiB)
	}

	// Calculate number of parts needed
	numParts := (totalSize + effectivePartSize - 1) / effectivePartSize

	rs.logger.Info("Starting file split with RAR",
		zap.String("file", fileName),
		zap.Int64("total_size", totalSize),
		zap.Int64("effective_part_size", effectivePartSize),
		zap.Bool("premium_user", isPremium),
		zap.Int64("num_parts", numParts))

	// Determine RAR output basename BEFORE checking for existing files
	// This ensures we check for the correct filenames
	var rarBaseName string
	if rarNameConfig != nil {
		// Use shortened basename - RAR will add .part1.rar, .part2.rar, etc. automatically
		rarBaseName = ShortenRarBasename(fileName, *rarNameConfig)
		rs.logger.Info("Using shortened RAR basename", zap.String("base", rarBaseName), zap.String("config", fmt.Sprintf("%+v", rarNameConfig)))
	} else {
		// Use original filename without extension
		rarBaseName = fileNameWithoutExt
	}

	// Check if RAR parts already exist with correct sizes
	existingParts := rs.checkExistingRarParts(fileSubDir, rarBaseName, numParts)
	if len(existingParts) == int(numParts) {
		rs.logger.Info("Reusing existing RAR parts", zap.Int("count", len(existingParts)))
		// Still call progress callback to show 100% completion
		if progressCallback != nil {
			progressCallback(totalSize, totalSize, 100)
		}
		return existingParts, nil
	}

	// If some parts exist but not all, or folder has other files - clean it up and start fresh
	if len(existingParts) > 0 || rs.dirHasFiles(fileSubDir) {
		rs.logger.Info("Incomplete or corrupted RAR parts found, cleaning up and recreating",
			zap.Int("existing", len(existingParts)),
			zap.Int("expected", int(numParts)))
		if err := os.RemoveAll(fileSubDir); err != nil {
			rs.logger.Warn("Failed to remove existing RAR directory", zap.String("dir", fileSubDir), zap.Error(err))
			// Continue anyway - MkdirAll will handle existing dir
		}
	}

	if err := os.MkdirAll(fileSubDir, 0755); err != nil {
		rs.logger.Error("Failed to create file subdirectory", zap.String("dir", fileSubDir), zap.Error(err))
		return nil, fmt.Errorf("failed to create file subdirectory: %w", err)
	}

	rs.logger.Info("Created RAR subdirectory", zap.String("dir", fileSubDir))

	// Create RAR output filename (will be part1.rar, part2.rar, etc.)
	rarOutputBase := filepath.Join(fileSubDir, rarBaseName)

	// Build RAR command
	// rar a -ep1 -v<size>m -m0 -y <output.rar> <input>
	// a: Add files to archive
	// -ep1: Exclude base folder from paths
	// -v<size>m: Create volumes (split) with specified size in MiB
	// -m0: Store (no compression)
	// -y: Assume Yes on all queries
	// RAR output will be named: basename.part1.rar, basename.part2.rar, etc.
	rarCmd := exec.Command("rar", "a",
		"-ep1",
		fmt.Sprintf("-v%dm", rarVolumeMB), // Use MiB for RAR volume size
		"-m0",
		"-y",
		fmt.Sprintf("%s.rar", rarOutputBase),
		inputPath)

	rs.logger.Info("Executing RAR command",
		zap.String("command", strings.Join(rarCmd.Args, " ")))

	// Create pipes for command output
	stdout, err := rarCmd.StdoutPipe()
	if err != nil {
		rs.logger.Error("Failed to create stdout pipe", zap.Error(err))
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := rarCmd.StderrPipe()
	if err != nil {
		rs.logger.Error("Failed to create stderr pipe", zap.Error(err))
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start RAR command
	if err := rarCmd.Start(); err != nil {
		rs.logger.Error("Failed to start RAR command", zap.Error(err))
		return nil, fmt.Errorf("failed to start RAR command: %w", err)
	}

	// Use channels to sync goroutines
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	var errorOutput strings.Builder

	// Scan stdout for progress in goroutine
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Text()
			rs.logger.Debug("RAR output", zap.String("line", line))
		}
		stdoutDone <- struct{}{}
	}()

	// Scan stderr for any errors in goroutine
	go func() {
		stderrScanner := bufio.NewScanner(stderr)
		for stderrScanner.Scan() {
			errorOutput.WriteString(stderrScanner.Text() + "\n")
		}
		stderrDone <- struct{}{}
	}()

	// Monitor file system for progress in real-time
	// This goroutine tracks file sizes being created
	progressTicker := time.NewTicker(500 * time.Millisecond)
	defer progressTicker.Stop()
	lastReportedPercent := 0

	go func() {
		for range progressTicker.C {
			// Calculate total bytes written in output directory
			totalBytesWritten := rs.getTotalDirSize(fileSubDir)
			if totalBytesWritten > 0 && totalSize > 0 && progressCallback != nil {
				percent := int((totalBytesWritten * 100) / totalSize)
				if percent > lastReportedPercent && percent <= 100 {
					progressCallback(totalBytesWritten, totalSize, percent)
					lastReportedPercent = percent
					rs.logger.Debug("RAR progress", zap.Int64("bytes", totalBytesWritten), zap.Int64("total", totalSize), zap.Int("percent", percent))
				}
			}
		}
	}()

	// Wait for command to complete
	if err := rarCmd.Wait(); err != nil {
		rs.logger.Error("RAR split failed",
			zap.Error(err),
			zap.String("stderr", errorOutput.String()))
		return nil, fmt.Errorf("rar split failed: %w, stderr: %s", err, errorOutput.String())
	}

	// Wait for goroutines to finish reading output
	<-stdoutDone
	<-stderrDone

	rs.logger.Info("RAR split completed successfully")

	// Collect the generated RAR parts
	// RAR names parts as: basename.part1.rar, basename.part2.rar, etc.
	// For single-part archives (no split), it may be: basename.rar
	var rarParts []string
	for partNum := int64(1); partNum <= numParts; partNum++ {
		var rarPath string
		if numParts == 1 {
			// Single file, just .rar
			rarPath = filepath.Join(fileSubDir, fmt.Sprintf("%s.rar", rarBaseName))
		} else {
			// Multiple parts: .part1.rar, .part2.rar, etc.
			rarPath = filepath.Join(fileSubDir, fmt.Sprintf("%s.part%d.rar", rarBaseName, partNum))
		}

		// Check if file exists
		if _, err := os.Stat(rarPath); err == nil {
			rarParts = append(rarParts, rarPath)

			fileInfo, _ := os.Stat(rarPath)
			rs.logger.Info("RAR part created",
				zap.String("file", filepath.Base(rarPath)),
				zap.Int64("size", fileInfo.Size()),
				zap.Int64("part_number", partNum))
		} else {
			rs.logger.Warn("Expected RAR part not found",
				zap.String("file", rarPath),
				zap.Int64("part_number", partNum))
		}
	}

	if progressCallback != nil {
		progressCallback(totalSize, totalSize, 100)
	}

	rs.logger.Info("File split complete with RAR",
		zap.String("file", fileName),
		zap.Int("num_parts_created", len(rarParts)))

	return rarParts, nil
}

// checkExistingRarParts checks if RAR parts already exist with the expected count
func (rs *RarSplitter) checkExistingRarParts(outputDir string, baseName string, expectedCount int64) []string {
	var existingParts []string

	// For single file, check if .rar exists
	if expectedCount == 1 {
		rarPath := filepath.Join(outputDir, fmt.Sprintf("%s.rar", baseName))
		if _, err := os.Stat(rarPath); err == nil {
			existingParts = append(existingParts, rarPath)
		}
		return existingParts
	}

	// For multiple parts, check .part1.rar, .part2.rar, etc.
	for i := int64(1); i <= expectedCount; i++ {
		rarPath := filepath.Join(outputDir, fmt.Sprintf("%s.part%d.rar", baseName, i))
		if _, err := os.Stat(rarPath); err == nil {
			existingParts = append(existingParts, rarPath)
		} else {
			break // Stop if we hit a missing part
		}
	}

	return existingParts
}

// CleanupRarFiles removes all RAR part files and their parent folder
func (rs *RarSplitter) CleanupRarFiles(rarParts []string) {
	if len(rarParts) == 0 {
		return
	}

	// Get the parent directory of the first RAR file (all should be in same folder)
	parentDir := filepath.Dir(rarParts[0])

	// Remove individual RAR files first
	for _, rarFile := range rarParts {
		if err := os.Remove(rarFile); err != nil {
			rs.logger.Warn("Failed to remove RAR file", zap.String("file", rarFile), zap.Error(err))
		} else {
			rs.logger.Debug("Removed RAR file", zap.String("file", rarFile))
		}
	}

	// Remove the parent folder
	if err := os.RemoveAll(parentDir); err != nil {
		rs.logger.Warn("Failed to remove RAR folder", zap.String("folder", parentDir), zap.Error(err))
	} else {
		rs.logger.Debug("Removed RAR folder", zap.String("folder", parentDir))
	}
}

// CleanupFiles removes all RAR part files and their parent folder (implements Archiver interface)
func (rs *RarSplitter) CleanupFiles(rarParts []string) {
	rs.CleanupRarFiles(rarParts)
}

// getTotalDirSize calculates the total size of all files in a directory
func (rs *RarSplitter) getTotalDirSize(dirPath string) int64 {
	var totalSize int64
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return 0
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			info, err := entry.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
	}
	return totalSize
}

// dirHasFiles checks if a directory exists and has any files in it
func (rs *RarSplitter) dirHasFiles(dirPath string) bool {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		// Directory doesn't exist or can't be read
		return false
	}
	return len(entries) > 0
}
