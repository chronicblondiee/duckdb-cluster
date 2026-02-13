package backup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chronicblondiee/duckdb-cluster/internal/config"
)

func TestNewBackupManager(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath:   filepath.Join(tmpDir, "backups"),
			Compression: true,
			Retention:   3,
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	if bm == nil {
		t.Fatal("backup manager is nil")
	}

	// Check that backup directory was created
	if _, err := os.Stat(cfg.Backup.LocalPath); os.IsNotExist(err) {
		t.Fatal("backup directory was not created")
	}
}

func TestFindShardFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard files
	for i := 0; i < 3; i++ {
		shardPath := filepath.Join(tmpDir, "shard-"+string(rune('0'+i))+".duckdb")
		if err := os.WriteFile(shardPath, []byte("mock data"), 0644); err != nil {
			t.Fatalf("failed to create mock shard file: %v", err)
		}
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath: filepath.Join(tmpDir, "backups"),
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	shardFiles, err := bm.findShardFiles()
	if err != nil {
		t.Fatalf("failed to find shard files: %v", err)
	}

	if len(shardFiles) != 3 {
		t.Errorf("expected 3 shard files, got %d", len(shardFiles))
	}
}

func TestCreateBackup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard files
	for i := 0; i < 2; i++ {
		shardPath := filepath.Join(tmpDir, "shard-"+string(rune('0'+i))+".duckdb")
		if err := os.WriteFile(shardPath, []byte("mock shard data"), 0644); err != nil {
			t.Fatalf("failed to create mock shard file: %v", err)
		}
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath:   filepath.Join(tmpDir, "backups"),
			Compression: false,
			Retention:   5,
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()
	metadata, err := bm.CreateBackup(ctx, BackupTypeFull)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	if metadata.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", metadata.Status)
	}

	if metadata.ShardCount != 2 {
		t.Errorf("expected 2 shards, got %d", metadata.ShardCount)
	}

	if len(metadata.Shards) != 2 {
		t.Errorf("expected 2 shard info entries, got %d", len(metadata.Shards))
	}

	// Verify backup directory exists
	backupDir := filepath.Join(cfg.Backup.LocalPath, metadata.ID)
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Error("backup directory was not created")
	}

	// Verify metadata file exists
	metadataPath := filepath.Join(backupDir, "metadata.txt")
	if _, err := os.Stat(metadataPath); os.IsNotExist(err) {
		t.Error("metadata file was not created")
	}
}

func TestCreateBackupWithCompression(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard file
	shardPath := filepath.Join(tmpDir, "shard-0.duckdb")
	if err := os.WriteFile(shardPath, []byte("mock shard data for compression test"), 0644); err != nil {
		t.Fatalf("failed to create mock shard file: %v", err)
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath:   filepath.Join(tmpDir, "backups"),
			Compression: true,
			Retention:   5,
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()
	metadata, err := bm.CreateBackup(ctx, BackupTypeFull)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	if metadata.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", metadata.Status)
	}

	// Verify compressed file exists
	backupDir := filepath.Join(cfg.Backup.LocalPath, metadata.ID)
	compressedPath := filepath.Join(backupDir, "shard-0.duckdb.gz")
	if _, err := os.Stat(compressedPath); os.IsNotExist(err) {
		t.Error("compressed backup file was not created")
	}
}

func TestListBackups(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard file
	shardPath := filepath.Join(tmpDir, "shard-0.duckdb")
	if err := os.WriteFile(shardPath, []byte("mock data"), 0644); err != nil {
		t.Fatalf("failed to create mock shard file: %v", err)
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath: filepath.Join(tmpDir, "backups"),
			Retention: 5,
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()

	// Create two backups
	_, err = bm.CreateBackup(ctx, BackupTypeFull)
	if err != nil {
		t.Fatalf("failed to create first backup: %v", err)
	}

	time.Sleep(10 * time.Millisecond) // Ensure different timestamps

	_, err = bm.CreateBackup(ctx, BackupTypeFull)
	if err != nil {
		t.Fatalf("failed to create second backup: %v", err)
	}

	// List backups
	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}

	if len(backups) != 2 {
		t.Errorf("expected 2 backups, got %d", len(backups))
	}
}

func TestRestore(t *testing.T) {
	tmpDir := t.TempDir()
	restoreDir := filepath.Join(tmpDir, "restore")

	// Create mock shard file
	shardPath := filepath.Join(tmpDir, "shard-0.duckdb")
	testData := []byte("test shard data for restore")
	if err := os.WriteFile(shardPath, testData, 0644); err != nil {
		t.Fatalf("failed to create mock shard file: %v", err)
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath:   filepath.Join(tmpDir, "backups"),
			Compression: false,
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()

	// Create backup
	metadata, err := bm.CreateBackup(ctx, BackupTypeFull)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Restore backup
	if err := bm.Restore(ctx, metadata.ID, restoreDir); err != nil {
		t.Fatalf("failed to restore backup: %v", err)
	}

	// Verify restored file
	restoredPath := filepath.Join(restoreDir, "shard-0.duckdb")
	restoredData, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}

	if string(restoredData) != string(testData) {
		t.Error("restored data does not match original data")
	}
}

func TestRestoreWithCompression(t *testing.T) {
	tmpDir := t.TempDir()
	restoreDir := filepath.Join(tmpDir, "restore")

	// Create mock shard file
	shardPath := filepath.Join(tmpDir, "shard-0.duckdb")
	testData := []byte("test shard data for compressed restore")
	if err := os.WriteFile(shardPath, testData, 0644); err != nil {
		t.Fatalf("failed to create mock shard file: %v", err)
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath:   filepath.Join(tmpDir, "backups"),
			Compression: true,
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()

	// Create compressed backup
	metadata, err := bm.CreateBackup(ctx, BackupTypeFull)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Restore backup
	if err := bm.Restore(ctx, metadata.ID, restoreDir); err != nil {
		t.Fatalf("failed to restore backup: %v", err)
	}

	// Verify restored file
	restoredPath := filepath.Join(restoreDir, "shard-0.duckdb")
	restoredData, err := os.ReadFile(restoredPath)
	if err != nil {
		t.Fatalf("failed to read restored file: %v", err)
	}

	if string(restoredData) != string(testData) {
		t.Error("restored data does not match original data")
	}
}

func TestDeleteBackup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard file
	shardPath := filepath.Join(tmpDir, "shard-0.duckdb")
	if err := os.WriteFile(shardPath, []byte("mock data"), 0644); err != nil {
		t.Fatalf("failed to create mock shard file: %v", err)
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath: filepath.Join(tmpDir, "backups"),
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()

	// Create backup
	metadata, err := bm.CreateBackup(ctx, BackupTypeFull)
	if err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	// Verify backup exists
	backupDir := filepath.Join(cfg.Backup.LocalPath, metadata.ID)
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		t.Fatal("backup directory does not exist")
	}

	// Delete backup
	if err := bm.DeleteBackup(metadata.ID); err != nil {
		t.Fatalf("failed to delete backup: %v", err)
	}

	// Verify backup was deleted
	if _, err := os.Stat(backupDir); !os.IsNotExist(err) {
		t.Error("backup directory still exists after deletion")
	}
}

func TestApplyRetention(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard file
	shardPath := filepath.Join(tmpDir, "shard-0.duckdb")
	if err := os.WriteFile(shardPath, []byte("mock data"), 0644); err != nil {
		t.Fatalf("failed to create mock shard file: %v", err)
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath: filepath.Join(tmpDir, "backups"),
			Retention: 2, // Keep only 2 backups
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()

	// Create 4 backups
	for i := 0; i < 4; i++ {
		_, err = bm.CreateBackup(ctx, BackupTypeFull)
		if err != nil {
			t.Fatalf("failed to create backup %d: %v", i, err)
		}
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
	}

	// List backups - retention should have been applied
	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}

	// Should only have 2 backups due to retention policy
	if len(backups) != 2 {
		t.Errorf("expected 2 backups after retention, got %d", len(backups))
	}
}

func TestBackupScheduler(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard file
	shardPath := filepath.Join(tmpDir, "shard-0.duckdb")
	if err := os.WriteFile(shardPath, []byte("mock data"), 0644); err != nil {
		t.Fatalf("failed to create mock shard file: %v", err)
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath:        filepath.Join(tmpDir, "backups"),
			ScheduleInterval: 100 * time.Millisecond, // Very short interval for testing
			Retention:        10,
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	// Start scheduler
	if err := bm.Start(ctx); err != nil {
		t.Fatalf("failed to start backup manager: %v", err)
	}

	// Wait for scheduler to run a few times
	time.Sleep(350 * time.Millisecond)

	// Stop scheduler
	if err := bm.Stop(); err != nil {
		t.Fatalf("failed to stop backup manager: %v", err)
	}

	// Check that at least one backup was created
	backups, err := bm.ListBackups()
	if err != nil {
		t.Fatalf("failed to list backups: %v", err)
	}

	if len(backups) < 1 {
		t.Error("expected at least 1 scheduled backup, got 0")
	}
}

func TestCreateTarBackup(t *testing.T) {
	tmpDir := t.TempDir()

	// Create mock shard files
	for i := 0; i < 2; i++ {
		shardPath := filepath.Join(tmpDir, "shard-"+string(rune('0'+i))+".duckdb")
		if err := os.WriteFile(shardPath, []byte("mock data"), 0644); err != nil {
			t.Fatalf("failed to create mock shard file: %v", err)
		}
	}

	cfg := &config.Config{
		Common: config.CommonConfig{
			DataDir: tmpDir,
		},
		Backup: config.BackupConfig{
			LocalPath: filepath.Join(tmpDir, "backups"),
		},
	}

	bm, err := NewBackupManager(cfg)
	if err != nil {
		t.Fatalf("failed to create backup manager: %v", err)
	}

	ctx := context.Background()
	tarPath, err := bm.CreateTarBackup(ctx)
	if err != nil {
		t.Fatalf("failed to create tar backup: %v", err)
	}

	// Verify tar file exists
	if _, err := os.Stat(tarPath); os.IsNotExist(err) {
		t.Error("tar backup file was not created")
	}

	// Verify tar file is not empty
	info, err := os.Stat(tarPath)
	if err != nil {
		t.Fatalf("failed to stat tar file: %v", err)
	}

	if info.Size() == 0 {
		t.Error("tar backup file is empty")
	}
}
