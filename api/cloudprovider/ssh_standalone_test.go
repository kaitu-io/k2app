package cloudprovider

import (
	"context"
	"fmt"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupTestDB creates an in-memory SQLite database for testing
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create slave_nodes table
	err = db.Exec(`CREATE TABLE slave_nodes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME,
		ipv4 VARCHAR(20) NOT NULL UNIQUE,
		ipv6 VARCHAR(20),
		name VARCHAR(255) NOT NULL,
		country VARCHAR(5),
		region VARCHAR(50),
		secret_token VARCHAR(64)
	)`).Error
	if err != nil {
		t.Fatalf("failed to create slave_nodes table: %v", err)
	}

	// Create cloud_instances table
	err = db.Exec(`CREATE TABLE cloud_instances (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_at DATETIME,
		updated_at DATETIME,
		deleted_at DATETIME,
		provider VARCHAR(20) NOT NULL,
		account_name VARCHAR(50) NOT NULL,
		instance_id VARCHAR(100) NOT NULL,
		name VARCHAR(100),
		ip_address VARCHAR(45) NOT NULL,
		ipv6_address VARCHAR(100),
		region VARCHAR(50),
		traffic_used_bytes INTEGER DEFAULT 0,
		traffic_total_bytes INTEGER DEFAULT 0,
		traffic_reset_at INTEGER DEFAULT 0,
		expires_at INTEGER DEFAULT 0,
		status VARCHAR(20) DEFAULT 'unknown',
		last_synced_at INTEGER DEFAULT 0,
		sync_error TEXT
	)`).Error
	if err != nil {
		t.Fatalf("failed to create cloud_instances table: %v", err)
	}

	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		if sqlDB != nil {
			sqlDB.Close()
		}
	})

	return db
}

// insertSlaveNode inserts a test slave node
func insertSlaveNode(t *testing.T, db *gorm.DB, ipv4, name, region string) {
	t.Helper()
	err := db.Exec(`INSERT INTO slave_nodes (ipv4, name, region, secret_token) VALUES (?, ?, ?, 'test-token')`,
		ipv4, name, region).Error
	if err != nil {
		t.Fatalf("failed to insert slave node: %v", err)
	}
}

// insertCloudInstance inserts a test cloud instance
func insertCloudInstance(t *testing.T, db *gorm.DB, ipAddress, provider, accountName, instanceID string) {
	t.Helper()
	err := db.Exec(`INSERT INTO cloud_instances (ip_address, provider, account_name, instance_id, name, region) VALUES (?, ?, ?, ?, 'Test Instance', 'us-west')`,
		ipAddress, provider, accountName, instanceID).Error
	if err != nil {
		t.Fatalf("failed to insert cloud instance: %v", err)
	}
}

func TestSSHStandaloneProvider_ListOrphanSlaveNodes(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := NewSSHStandaloneProvider(db)

	// Insert 3 slave nodes
	insertSlaveNode(t, db, "192.168.1.1", "Node 1", "us-west")
	insertSlaveNode(t, db, "192.168.1.2", "Node 2", "eu-central")
	insertSlaveNode(t, db, "192.168.1.3", "Node 3", "ap-east")

	// Insert cloud instance for node 1 (so node 1 is NOT orphan)
	insertCloudInstance(t, db, "192.168.1.1", "aws_lightsail", "my-account", "i-12345")

	// List instances - should only return nodes 2 and 3 (orphans)
	// Note: This will fail SSH execution but return fallback status
	statuses, err := provider.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(statuses) != 2 {
		t.Errorf("ListInstances returned %d instances, want 2 (orphan nodes)", len(statuses))
	}

	// Verify correct IPs are returned
	foundIPs := make(map[string]bool)
	for _, status := range statuses {
		foundIPs[status.IPAddress] = true
	}

	if foundIPs["192.168.1.1"] {
		t.Error("Node 1 should not be in orphan list (has CloudInstance)")
	}
	if !foundIPs["192.168.1.2"] {
		t.Error("Node 2 should be in orphan list")
	}
	if !foundIPs["192.168.1.3"] {
		t.Error("Node 3 should be in orphan list")
	}
}

func TestSSHStandaloneProvider_GetOrphanStatus(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := NewSSHStandaloneProvider(db)

	// Insert slave node
	insertSlaveNode(t, db, "192.168.1.1", "Test Node", "us-west")

	// Set up mock SSH executor for testing
	now := time.Now()
	SetSSHExecByIP(func(ctx context.Context, ip string, command string) (string, error) {
		// Return mock vnstat output with current year/month so parseVnstatJSON matches
		return fmt.Sprintf(`{"vnstatversion":"2.6","jsonversion":"2","interfaces":[{"name":"eth0","traffic":{"month":[{"date":{"year":%d,"month":%d},"rx":1073741824,"tx":2147483648}]}}]}`,
			now.Year(), int(now.Month())), nil
	})
	defer SetSSHExecByIP(nil) // Clean up

	// Get status for orphan node (no CloudInstance)
	status, err := provider.GetInstanceStatus(ctx, "192.168.1.1")
	if err != nil {
		t.Fatalf("GetInstanceStatus failed: %v", err)
	}

	if status.InstanceID != "192.168.1.1" {
		t.Errorf("InstanceID = %q, want %q", status.InstanceID, "192.168.1.1")
	}
	if status.Name != "Test Node" {
		t.Errorf("Name = %q, want %q", status.Name, "Test Node")
	}
	if status.Region != "us-west" {
		t.Errorf("Region = %q, want %q", status.Region, "us-west")
	}
	// 1GB rx + 2GB tx = 3GB
	expectedBytes := int64(1073741824 + 2147483648)
	if status.TrafficUsedBytes != expectedBytes {
		t.Errorf("TrafficUsedBytes = %d, want %d", status.TrafficUsedBytes, expectedBytes)
	}
}

func TestSSHStandaloneProvider_GetNonOrphanReturnsError(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := NewSSHStandaloneProvider(db)

	// Insert slave node and corresponding cloud instance
	insertSlaveNode(t, db, "192.168.1.1", "Test Node", "us-west")
	insertCloudInstance(t, db, "192.168.1.1", "aws_lightsail", "my-account", "i-12345")

	// Get status should fail - this node has a CloudInstance
	_, err := provider.GetInstanceStatus(ctx, "192.168.1.1")
	if err == nil {
		t.Error("GetInstanceStatus should return error for non-orphan node")
	}
}

func TestSSHStandaloneProvider_GetNonExistentReturnsError(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := NewSSHStandaloneProvider(db)

	// Get status for non-existent node
	_, err := provider.GetInstanceStatus(ctx, "192.168.1.999")
	if err == nil {
		t.Error("GetInstanceStatus should return error for non-existent node")
	}
}

func TestSSHStandaloneProvider_UnsupportedOperations(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := NewSSHStandaloneProvider(db)

	// CreateInstance should return NotSupportedError
	t.Run("CreateInstance returns NotSupportedError", func(t *testing.T) {
		_, err := provider.CreateInstance(ctx, CreateInstanceOptions{})
		if err == nil {
			t.Error("CreateInstance should return error, got nil")
		}
		if !IsNotSupported(err) {
			t.Errorf("CreateInstance should return NotSupportedError, got %T: %v", err, err)
		}
	})

	// DeleteInstance should return NotSupportedError
	t.Run("DeleteInstance returns NotSupportedError", func(t *testing.T) {
		_, err := provider.DeleteInstance(ctx, "any-id")
		if err == nil {
			t.Error("DeleteInstance should return error, got nil")
		}
		if !IsNotSupported(err) {
			t.Errorf("DeleteInstance should return NotSupportedError, got %T: %v", err, err)
		}
	})

	// ChangeIP should return NotSupportedError
	t.Run("ChangeIP returns NotSupportedError", func(t *testing.T) {
		_, err := provider.ChangeIP(ctx, "any-instance", ChangeIPOptions{})
		if err == nil {
			t.Error("ChangeIP should return error, got nil")
		}
		if !IsNotSupported(err) {
			t.Errorf("ChangeIP should return NotSupportedError, got %T: %v", err, err)
		}
	})

	// ListRegions should return empty slice
	t.Run("ListRegions returns empty", func(t *testing.T) {
		regions, err := provider.ListRegions(ctx)
		if err != nil {
			t.Fatalf("ListRegions failed: %v", err)
		}
		if len(regions) != 0 {
			t.Errorf("ListRegions should return empty slice, got %d items", len(regions))
		}
	})

	// ListPlans should return empty slice
	t.Run("ListPlans returns empty", func(t *testing.T) {
		plans, err := provider.ListPlans(ctx, "any-region")
		if err != nil {
			t.Fatalf("ListPlans failed: %v", err)
		}
		if len(plans) != 0 {
			t.Errorf("ListPlans should return empty slice, got %d items", len(plans))
		}
	})

	// ListImages should return empty slice
	t.Run("ListImages returns empty", func(t *testing.T) {
		images, err := provider.ListImages(ctx, "any-region")
		if err != nil {
			t.Fatalf("ListImages failed: %v", err)
		}
		if len(images) != 0 {
			t.Errorf("ListImages should return empty slice, got %d items", len(images))
		}
	})
}

func TestSSHStandaloneProvider_Name(t *testing.T) {
	db := setupTestDB(t)
	provider := NewSSHStandaloneProvider(db)

	if got := provider.Name(); got != ProviderSSHStandalone {
		t.Errorf("Name() = %q, want %q", got, ProviderSSHStandalone)
	}
}

func TestSSHStandaloneProvider_EmptyOrphanList(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := NewSSHStandaloneProvider(db)

	// Insert slave node with matching cloud instance
	insertSlaveNode(t, db, "192.168.1.1", "Node 1", "us-west")
	insertCloudInstance(t, db, "192.168.1.1", "aws_lightsail", "my-account", "i-12345")

	// All nodes have cloud instances, so orphan list should be empty
	statuses, err := provider.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}

	if len(statuses) != 0 {
		t.Errorf("ListInstances returned %d instances, want 0 (no orphans)", len(statuses))
	}
}

func TestSSHStandaloneProvider_DeletedCloudInstanceMakesOrphan(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()

	provider := NewSSHStandaloneProvider(db)

	// Insert slave node and cloud instance
	insertSlaveNode(t, db, "192.168.1.1", "Node 1", "us-west")
	insertCloudInstance(t, db, "192.168.1.1", "aws_lightsail", "my-account", "i-12345")

	// Initially no orphans
	statuses, err := provider.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances failed: %v", err)
	}
	if len(statuses) != 0 {
		t.Errorf("Initially expected 0 orphans, got %d", len(statuses))
	}

	// Soft delete the cloud instance
	err = db.Exec(`UPDATE cloud_instances SET deleted_at = datetime('now') WHERE ip_address = ?`, "192.168.1.1").Error
	if err != nil {
		t.Fatalf("Failed to soft delete cloud instance: %v", err)
	}

	// Now the slave node should be an orphan
	statuses, err = provider.ListInstances(ctx)
	if err != nil {
		t.Fatalf("ListInstances after delete failed: %v", err)
	}
	if len(statuses) != 1 {
		t.Errorf("After cloud instance delete, expected 1 orphan, got %d", len(statuses))
	}
}
