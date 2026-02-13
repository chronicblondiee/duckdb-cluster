package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
)

// BackupType represents the type of backup
type BackupType string

const (
	BackupTypeFull        BackupType = "full"
	BackupTypeIncremental BackupType = "incremental"
)

// BackupMetadata contains information about a backup
type BackupMetadata struct {
	ID           string                 `json:"id"`
	Type         BackupType             `json:"type"`
	Timestamp    time.Time              `json:"timestamp"`
	ShardCount   int                    `json:"shard_count"`
	TotalSizeGB  float64                `json:"total_size_gb"`
	Duration     time.Duration          `json:"duration"`
	Status       string                 `json:"status"` // "success", "failed", "in_progress"
	Error        string                 `json:"error,omitempty"`
	Shards       []ShardBackupInfo      `json:"shards"`
	BaseBackupID string                 `json:"base_backup_id,omitempty"` // For incremental backups
	Config       map[string]interface{} `json:"config,omitempty"`
}

// ShardBackupInfo contains information about a single shard backup
type ShardBackupInfo struct {
	ShardID   int     `json:"shard_id"`
	Path      string  `json:"path"`
	SizeBytes int64   `json:"size_bytes"`
	Checksum  string  `json:"checksum,omitempty"`
	Error     string  `json:"error,omitempty"`
	Success   bool    `json:"success"`
}

// BackupManager handles backup and restore operations
type BackupManager struct {
	config   config.BackupConfig
	dataPath string
	mu       sync.RWMutex

	// Background scheduler
	schedulerCancel context.CancelFunc
	schedulerDone   chan struct{}
}

// NewBackupManager creates a new backup manager
func NewBackupManager(cfg *config.Config) (*BackupManager, error) {
	if cfg.Backup.LocalPath == "" {
		cfg.Backup.LocalPath = filepath.Join(cfg.Common.DataDir, "backups")
	}

	// Create backup directory if it doesn't exist
	if err := os.MkdirAll(cfg.Backup.LocalPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create backup directory: %w", err)
	}

	bm := &BackupManager{
		config:   cfg.Backup,
		dataPath: cfg.Common.DataDir,
	}

	return bm, nil
}

// Start begins the backup scheduler if enabled
func (bm *BackupManager) Start(ctx context.Context) error {
	if bm.config.ScheduleInterval <= 0 {
		return nil // Scheduling disabled
	}

	schedulerCtx, cancel := context.WithCancel(ctx)
	bm.schedulerCancel = cancel
	bm.schedulerDone = make(chan struct{})

	go bm.runScheduler(schedulerCtx)

	return nil
}

// Stop stops the backup scheduler
func (bm *BackupManager) Stop() error {
	if bm.schedulerCancel != nil {
		bm.schedulerCancel()
		<-bm.schedulerDone
	}
	return nil
}

// runScheduler runs periodic backups
func (bm *BackupManager) runScheduler(ctx context.Context) {
	defer close(bm.schedulerDone)

	ticker := time.NewTicker(bm.config.ScheduleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Perform backup in background
			go func() {
				_, err := bm.CreateBackup(ctx, BackupTypeFull)
				if err != nil {
					// Log error (would use logger in production)
					fmt.Printf("scheduled backup failed: %v\n", err)
				}
			}()
		}
	}
}

// CreateBackup creates a new backup
func (bm *BackupManager) CreateBackup(ctx context.Context, backupType BackupType) (*BackupMetadata, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	startTime := time.Now()
	backupID := fmt.Sprintf("%s-%d", backupType, startTime.UnixNano()/1e6) // Use milliseconds for uniqueness

	metadata := &BackupMetadata{
		ID:         backupID,
		Type:       backupType,
		Timestamp:  startTime,
		Status:     "in_progress",
		Shards:     []ShardBackupInfo{},
		ShardCount: 0,
	}

	// Find all shard files
	shardFiles, err := bm.findShardFiles()
	if err != nil {
		metadata.Status = "failed"
		metadata.Error = err.Error()
		return metadata, err
	}

	metadata.ShardCount = len(shardFiles)

	// Create backup directory
	backupDir := filepath.Join(bm.config.LocalPath, backupID)
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		metadata.Status = "failed"
		metadata.Error = err.Error()
		return metadata, fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Backup each shard
	var totalSize int64
	for shardID, shardPath := range shardFiles {
		shardInfo, err := bm.backupShard(ctx, shardID, shardPath, backupDir)
		if err != nil {
			shardInfo.Error = err.Error()
			shardInfo.Success = false
		} else {
			shardInfo.Success = true
			totalSize += shardInfo.SizeBytes
		}
		metadata.Shards = append(metadata.Shards, shardInfo)
	}

	// Calculate duration and size
	metadata.Duration = time.Since(startTime)
	metadata.TotalSizeGB = float64(totalSize) / (1024 * 1024 * 1024)

	// Check if all shards succeeded
	allSuccess := true
	for _, shard := range metadata.Shards {
		if !shard.Success {
			allSuccess = false
			break
		}
	}

	if allSuccess {
		metadata.Status = "success"
	} else {
		metadata.Status = "failed"
		metadata.Error = "one or more shards failed to backup"
	}

	// Save metadata
	if err := bm.saveMetadata(backupID, metadata); err != nil {
		return metadata, fmt.Errorf("failed to save metadata: %w", err)
	}

	// Apply retention policy
	if err := bm.applyRetention(); err != nil {
		// Log but don't fail the backup
		fmt.Printf("failed to apply retention policy: %v\n", err)
	}

	return metadata, nil
}

// findShardFiles finds all DuckDB shard files in the data directory
func (bm *BackupManager) findShardFiles() (map[int]string, error) {
	shardFiles := make(map[int]string)

	entries, err := os.ReadDir(bm.dataPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read data directory: %w", err)
	}

	shardID := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Match files like: shard-0.duckdb, shard-1.duckdb, etc.
		if strings.HasPrefix(name, "shard-") && strings.HasSuffix(name, ".duckdb") {
			shardFiles[shardID] = filepath.Join(bm.dataPath, name)
			shardID++
		}
	}

	return shardFiles, nil
}

// backupShard backs up a single shard file
func (bm *BackupManager) backupShard(ctx context.Context, shardID int, sourcePath, backupDir string) (ShardBackupInfo, error) {
	info := ShardBackupInfo{
		ShardID: shardID,
		Path:    sourcePath,
	}

	// Get file info
	fileInfo, err := os.Stat(sourcePath)
	if err != nil {
		return info, fmt.Errorf("failed to stat shard file: %w", err)
	}
	info.SizeBytes = fileInfo.Size()

	// Determine destination path
	filename := filepath.Base(sourcePath)
	destPath := filepath.Join(backupDir, filename)

	// Apply compression if enabled
	if bm.config.Compression {
		destPath += ".gz"
		if err := bm.compressFile(sourcePath, destPath); err != nil {
			return info, fmt.Errorf("failed to compress shard: %w", err)
		}
	} else {
		if err := bm.copyFile(sourcePath, destPath); err != nil {
			return info, fmt.Errorf("failed to copy shard: %w", err)
		}
	}

	return info, nil
}

// copyFile copies a file from source to destination
func (bm *BackupManager) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

// compressFile compresses a file using gzip
func (bm *BackupManager) compressFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	gzipWriter := gzip.NewWriter(destFile)
	defer gzipWriter.Close()

	_, err = io.Copy(gzipWriter, sourceFile)
	return err
}

// saveMetadata saves backup metadata to a file
func (bm *BackupManager) saveMetadata(backupID string, metadata *BackupMetadata) error {
	// In a real implementation, this would save to JSON file
	// For now, we'll just create a marker file
	backupDir := filepath.Join(bm.config.LocalPath, backupID)
	metadataPath := filepath.Join(backupDir, "metadata.txt")

	f, err := os.Create(metadataPath)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = fmt.Fprintf(f, "Backup ID: %s\nType: %s\nTimestamp: %s\nStatus: %s\nShard Count: %d\nSize (GB): %.2f\nDuration: %s\n",
		metadata.ID, metadata.Type, metadata.Timestamp.Format(time.RFC3339),
		metadata.Status, metadata.ShardCount, metadata.TotalSizeGB, metadata.Duration)

	return err
}

// ListBackups lists all available backups
func (bm *BackupManager) ListBackups() ([]BackupMetadata, error) {
	bm.mu.RLock()
	defer bm.mu.RUnlock()

	return bm.listBackupsUnlocked()
}

// listBackupsUnlocked is an internal method that lists backups without acquiring a lock
func (bm *BackupManager) listBackupsUnlocked() ([]BackupMetadata, error) {
	entries, err := os.ReadDir(bm.config.LocalPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read backup directory: %w", err)
	}

	backups := []BackupMetadata{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Parse backup ID
		backupID := entry.Name()
		parts := strings.Split(backupID, "-")
		if len(parts) < 2 {
			continue
		}

		backupType := BackupType(parts[0])
		if backupType != BackupTypeFull && backupType != BackupTypeIncremental {
			continue
		}

		// Read metadata if available
		metadataPath := filepath.Join(bm.config.LocalPath, backupID, "metadata.txt")
		if _, err := os.Stat(metadataPath); err == nil {
			// Simple metadata parsing (in production, use JSON)
			backups = append(backups, BackupMetadata{
				ID:   backupID,
				Type: backupType,
			})
		}
	}

	return backups, nil
}

// Restore restores from a backup
func (bm *BackupManager) Restore(ctx context.Context, backupID string, targetPath string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	backupDir := filepath.Join(bm.config.LocalPath, backupID)

	// Check if backup exists
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return fmt.Errorf("backup %s does not exist", backupID)
	}

	// If targetPath not specified, use default data path
	if targetPath == "" {
		targetPath = bm.dataPath
	}

	// Create target directory if it doesn't exist
	if err := os.MkdirAll(targetPath, 0755); err != nil {
		return fmt.Errorf("failed to create target directory: %w", err)
	}

	// List files in backup directory
	entries, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Restore each shard file
	for _, entry := range entries {
		if entry.IsDir() || entry.Name() == "metadata.txt" {
			continue
		}

		sourcePath := filepath.Join(backupDir, entry.Name())
		destName := entry.Name()

		// Handle compressed files
		if strings.HasSuffix(destName, ".gz") {
			destName = strings.TrimSuffix(destName, ".gz")
			destPath := filepath.Join(targetPath, destName)
			if err := bm.decompressFile(sourcePath, destPath); err != nil {
				return fmt.Errorf("failed to decompress %s: %w", entry.Name(), err)
			}
		} else {
			destPath := filepath.Join(targetPath, destName)
			if err := bm.copyFile(sourcePath, destPath); err != nil {
				return fmt.Errorf("failed to restore %s: %w", entry.Name(), err)
			}
		}
	}

	return nil
}

// decompressFile decompresses a gzip file
func (bm *BackupManager) decompressFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	gzipReader, err := gzip.NewReader(sourceFile)
	if err != nil {
		return err
	}
	defer gzipReader.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, gzipReader)
	return err
}

// DeleteBackup deletes a backup
func (bm *BackupManager) DeleteBackup(backupID string) error {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	backupDir := filepath.Join(bm.config.LocalPath, backupID)

	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return fmt.Errorf("backup %s does not exist", backupID)
	}

	return os.RemoveAll(backupDir)
}

// applyRetention applies the backup retention policy
// Note: This method assumes the caller already holds the lock
func (bm *BackupManager) applyRetention() error {
	if bm.config.Retention <= 0 {
		return nil // No retention policy
	}

	backups, err := bm.listBackupsUnlocked()
	if err != nil {
		return err
	}

	// If we have more backups than retention policy allows, delete oldest
	if len(backups) > bm.config.Retention {
		// Sort by ID (which includes timestamp)
		toDelete := len(backups) - bm.config.Retention
		for i := 0; i < toDelete; i++ {
			// Call deleteBackupUnlocked since we already hold the lock
			backupDir := filepath.Join(bm.config.LocalPath, backups[i].ID)
			if err := os.RemoveAll(backupDir); err != nil {
				return err
			}
		}
	}

	return nil
}

// CreateTarBackup creates a tar.gz backup of all shards
func (bm *BackupManager) CreateTarBackup(ctx context.Context) (string, error) {
	bm.mu.Lock()
	defer bm.mu.Unlock()

	backupID := fmt.Sprintf("full-%d", time.Now().UnixNano()/1e6) // Use milliseconds for uniqueness
	backupPath := filepath.Join(bm.config.LocalPath, fmt.Sprintf("%s.tar.gz", backupID))

	// Create tar.gz file
	tarFile, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create tar file: %w", err)
	}
	defer tarFile.Close()

	gzipWriter := gzip.NewWriter(tarFile)
	defer gzipWriter.Close()

	tarWriter := tar.NewWriter(gzipWriter)
	defer tarWriter.Close()

	// Find and add all shard files
	shardFiles, err := bm.findShardFiles()
	if err != nil {
		return "", err
	}

	for _, shardPath := range shardFiles {
		if err := bm.addFileToTar(tarWriter, shardPath); err != nil {
			return "", fmt.Errorf("failed to add %s to tar: %w", shardPath, err)
		}
	}

	return backupPath, nil
}

// addFileToTar adds a file to a tar archive
func (bm *BackupManager) addFileToTar(tw *tar.Writer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return err
	}

	header := &tar.Header{
		Name:    filepath.Base(filePath),
		Size:    stat.Size(),
		Mode:    int64(stat.Mode()),
		ModTime: stat.ModTime(),
	}

	if err := tw.WriteHeader(header); err != nil {
		return err
	}

	_, err = io.Copy(tw, file)
	return err
}
