package uploader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"archive/zip"
	"go.uber.org/zap"
)

// ZipSplitter handles splitting files into ZIP archives
type ZipSplitter struct {
	logger *zap.Logger
}

// NewZipSplitter creates a new ZIP splitter
func NewZipSplitter(logger *zap.Logger) *ZipSplitter {
	return &ZipSplitter{
		logger: logger,
	}
}

// SplitFile splits a file into multiple ZIP parts
// Each ZIP part will contain a portion of the original file without compression
// isPremium determines the max file size (2GB for free, 4GB for premium)
// progressCallback is called with (bytesProcessed, totalBytes, percentage)
func (zs *ZipSplitter) SplitFile(inputPath string, outputDir string, partSize int64, isPremium bool, progressCallback func(bytesProcessed, totalBytes int64, percent int)) ([]string, error) {
	fileInfo, err := os.Stat(inputPath)
	if err != nil {
		zs.logger.Error("Failed to stat file", zap.String("file", inputPath), zap.Error(err))
		return nil, fmt.Errorf("failed to stat file: %w", err)
	}

	totalSize := fileInfo.Size()
	fileName := filepath.Base(inputPath)

	// Telegram file split sizes:
	// - Free users: 1.95 GB (2,094,104,576 bytes)
	// - Premium users: 3.9 GB (4,188,209,152 bytes)
	// These are safely under Telegram's actual limits to account for ZIP overhead

	var effectivePartSize int64
	if isPremium {
		effectivePartSize = int64(4188209152) // 3.9 GB for premium
	} else {
		effectivePartSize = int64(2094104576) // 1.95 GB for free
	}

	// Calculate number of parts needed based on effective size
	numParts := (totalSize + effectivePartSize - 1) / effectivePartSize

	zs.logger.Info("Starting file split",
		zap.String("file", fileName),
		zap.Int64("total_size", totalSize),
		zap.Int64("part_size", partSize),
		zap.Int64("effective_part_size", effectivePartSize),
		zap.Bool("premium_user", isPremium),
		zap.Int64("num_parts", numParts))

	var zipParts []string
	inputFile, err := os.Open(inputPath)
	if err != nil {
		zs.logger.Error("Failed to open input file", zap.String("file", inputPath), zap.Error(err))
		return nil, fmt.Errorf("failed to open input file: %w", err)
	}
	defer inputFile.Close()

	var totalBytesProcessed int64 = 0

	for partNum := int64(0); partNum < numParts; partNum++ {
		// Create ZIP file for this part
		zipFileName := fmt.Sprintf("Part_%d_of_%d_%s.zip", partNum+1, numParts, fileName)
		zipPath := filepath.Join(outputDir, zipFileName)

		zs.logger.Info("Creating ZIP part", zap.String("zip_file", zipFileName), zap.Int64("part_number", partNum+1))

		// Create ZIP file
		zipFile, err := os.Create(zipPath)
		if err != nil {
			zs.logger.Error("Failed to create ZIP file", zap.String("zip_file", zipPath), zap.Error(err))
			return nil, fmt.Errorf("failed to create ZIP file: %w", err)
		}

		zipWriter := zip.NewWriter(zipFile)
		// Set compression method to Store (no compression) for better performance
		// (already default, just documenting the intent)

		// Create file header for the original file in ZIP
		fileNameInZip := fileName
		header := &zip.FileHeader{
			Name:   fileNameInZip,
			Method: zip.Store, // No compression
		}

		// Calculate how much data to write in this part (using effective size, not raw part size)
		remainingSize := totalSize - (partNum * effectivePartSize)
		if remainingSize > effectivePartSize {
			remainingSize = effectivePartSize
		}

		header.UncompressedSize64 = uint64(remainingSize)

		// Write the file chunk to ZIP with progress tracking
		writer, err := zipWriter.CreateHeader(header)
		if err != nil {
			zipWriter.Close()
			zipFile.Close()
			zs.logger.Error("Failed to create ZIP header", zap.Error(err))
			return nil, fmt.Errorf("failed to create ZIP header: %w", err)
		}

		// Create a progress tracking writer wrapper
		bytesWritten, err := zs.copyWithProgress(writer, inputFile, remainingSize, totalSize, &totalBytesProcessed, progressCallback)
		if err != nil && err != io.EOF {
			zipWriter.Close()
			zipFile.Close()
			zs.logger.Error("Failed to write to ZIP", zap.Error(err))
			return nil, fmt.Errorf("failed to write to ZIP: %w", err)
		}

		zs.logger.Info("ZIP part created",
			zap.String("zip_file", zipFileName),
			zap.Int64("bytes_written", bytesWritten),
			zap.Int64("part_number", partNum+1))

		// Close ZIP writer
		if err := zipWriter.Close(); err != nil {
			zipFile.Close()
			zs.logger.Error("Failed to close ZIP writer", zap.Error(err))
			return nil, fmt.Errorf("failed to close ZIP writer: %w", err)
		}

		// Check final ZIP file size
		finalFileInfo, err := os.Stat(zipPath)
		if err != nil {
			zs.logger.Warn("Failed to check final ZIP file size", zap.String("file", zipPath), zap.Error(err))
		} else {
			finalSize := finalFileInfo.Size()
			if finalSize > partSize {
				zs.logger.Warn("ZIP part exceeds target size",
					zap.String("file", zipPath),
					zap.Int64("final_size", finalSize),
					zap.Int64("target_size", partSize),
					zap.Int64("excess_bytes", finalSize-partSize))
			}
		}

		zipFile.Close()

		zipParts = append(zipParts, zipPath)

		// Send final progress callback after part completion
		if progressCallback != nil {
			percent := int(totalBytesProcessed * 100 / totalSize)
			progressCallback(totalBytesProcessed, totalSize, percent)
		}
	}

	zs.logger.Info("File split complete",
		zap.String("file", fileName),
		zap.Int("num_parts_created", len(zipParts)))

	return zipParts, nil
}

// copyWithProgress copies data from src to dst and calls progressCallback periodically
// Tracks bytes written across all parts for overall progress calculation
// Uses a 16MB buffer for efficient large file handling
func (zs *ZipSplitter) copyWithProgress(dst io.Writer, src io.Reader, partSize, totalSize int64, totalBytesProcessed *int64, progressCallback func(bytesProcessed, totalBytes int64, percent int)) (int64, error) {
	buffer := make([]byte, 16*1024*1024) // 16MB buffer for efficient I/O
	var bytesWritten int64 = 0

	for {
		// Read up to partSize bytes
		readSize := int64(len(buffer))
		if bytesWritten+readSize > partSize {
			readSize = partSize - bytesWritten
		}
		if readSize <= 0 {
			break
		}

		n, err := src.Read(buffer[:readSize])
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			if writeErr != nil {
				return bytesWritten, writeErr
			}

			bytesWritten += int64(written)
			*totalBytesProcessed += int64(written)

			// Send progress callback after each chunk
			if progressCallback != nil {
				percent := int(*totalBytesProcessed * 100 / totalSize)
				progressCallback(*totalBytesProcessed, totalSize, percent)
			}
		}

		if err != nil && err != io.EOF {
			return bytesWritten, err
		}
		if err == io.EOF {
			break
		}
	}

	return bytesWritten, nil
}

// CleanupZipFiles removes all ZIP part files
func (zs *ZipSplitter) CleanupZipFiles(zipParts []string) {
	for _, zipFile := range zipParts {
		if err := os.Remove(zipFile); err != nil {
			zs.logger.Warn("Failed to remove ZIP file", zap.String("file", zipFile), zap.Error(err))
		} else {
			zs.logger.Info("Removed ZIP file", zap.String("file", zipFile))
		}
	}
}
