package center

import (
	"testing"
)

func TestOrderForUsersMetaHandling(t *testing.T) {
	// 创建测试订单
	order := Order{}
	
	// 创建测试计划和活动
	plan := &Plan{
		PID:   "test_plan",
		Label: "Test Plan",
		Price: 999,
		Month: 1,
	}
	
	campaign := &Campaign{
		ID:          1,
		Code:        "TEST20",
		Type:        "discount",
		Value:       80, // 80% = 8折
		Description: "Test Campaign",
	}
	
	// 测试设置和获取订单 Meta 信息
	testUserUUIDs := []string{"user1", "user2", "user3", "user4", "user5"}
	forMyself := true
	
	err := order.SetOrderMeta(plan, campaign, testUserUUIDs, forMyself)
	if err != nil {
		t.Fatalf("Failed to set order meta: %v", err)
	}

	// 检查 Meta 是否正确存储
	if order.Meta == "" {
		t.Error("Expected Meta to be set, got empty string")
	}

	// 测试获取用户UUID列表
	retrievedUserUUIDs := order.GetForUsers()
	if len(retrievedUserUUIDs) != len(testUserUUIDs) {
		t.Errorf("Expected %d user UUIDs, got %d", len(testUserUUIDs), len(retrievedUserUUIDs))
	}

	// 验证内容
	for i, userUUID := range testUserUUIDs {
		if retrievedUserUUIDs[i] != userUUID {
			t.Errorf("Expected user UUID %s at position %d, got %s", userUUID, i, retrievedUserUUIDs[i])
		}
	}
	
	// 测试 forMyself 标志
	if !order.GetForMyself() {
		t.Error("Expected forMyself to be true")
	}
	
	// 测试获取计划信息
	retrievedPlan, err := order.GetPlan()
	if err != nil {
		t.Fatalf("Failed to get plan: %v", err)
	}
	if retrievedPlan.PID != plan.PID {
		t.Errorf("Expected plan PID %s, got %s", plan.PID, retrievedPlan.PID)
	}
}

func TestCanPayForUsers(t *testing.T) {

	// 创建测试用户
	user := User{ID: 1}

	// 测试用例1：空列表应该允许（可能只是为自己购买）
	testUsers1 := []uint64{}
	if !CanPayForUsers(&user, testUsers1) {
		t.Error("Expected user to be able to pay for empty user list")
	}

	// 注意：由于函数现在需要数据库查询来验证委托关系，
	// 在单元测试中我们只能测试基本逻辑。
	// 完整的权限检查需要在集成测试中进行。
}

