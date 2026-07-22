package center

import (
	"fmt"
	"regexp"

	"github.com/gin-gonic/gin"
	db "github.com/wordgate/qtoolkit/db"
	"github.com/wordgate/qtoolkit/log"
	"gorm.io/gorm"
)

var countryCodeRe = regexp.MustCompile(`^[a-z]{2}$`)

// ===== logic layer: 校验集中在此,handler 只做参数绑定与响应输出 =====

func adminCreateEnterpriseLineFull(customerID, nodeID uint64, countryCode string, lineNo int) (*EnterpriseLine, error) {
	if !countryCodeRe.MatchString(countryCode) {
		return nil, fmt.Errorf("invalid country code %q (want ISO alpha-2 lowercase)", countryCode)
	}
	if lineNo < 1 {
		return nil, fmt.Errorf("lineNo must be >= 1")
	}
	var cust EnterpriseCustomer
	if err := db.Get().First(&cust, customerID).Error; err != nil {
		return nil, fmt.Errorf("customer %d not found", customerID)
	}
	var node SlaveNode
	if err := db.Get().First(&node, nodeID).Error; err != nil {
		return nil, fmt.Errorf("node %d not found", nodeID)
	}
	if node.Class != NodeClassPrivate || node.PrivateOwnerUserID == nil || *node.PrivateOwnerUserID != cust.UserID {
		return nil, fmt.Errorf("node %d is not a private node owned by customer account", nodeID)
	}
	line := &EnterpriseLine{CustomerID: customerID, NodeID: nodeID, CountryCode: countryCode, LineNo: lineNo}
	if err := db.Get().Create(line).Error; err != nil {
		return nil, err
	}
	return line, nil
}

func adminCreateEnterpriseLine(customerID, nodeID uint64, cc string, lineNo int) error {
	_, err := adminCreateEnterpriseLineFull(customerID, nodeID, cc, lineNo)
	return err
}

func adminDeleteEnterpriseLine(lineID uint64) error {
	var cnt int64
	db.Get().Model(&EnterpriseRouterBinding{}).Where("line_id = ?", lineID).Count(&cnt)
	if cnt > 0 {
		return fmt.Errorf("line %d is bound to a router slot; unbind first", lineID)
	}
	return db.Get().Delete(&EnterpriseLine{}, lineID).Error
}

func adminUpsertBinding(gatewayDeviceID uint64, slot int, lineID uint64) error {
	if slot < 1 || slot > 8 {
		return fmt.Errorf("slot must be 1..8")
	}
	var dev Device
	if err := db.Get().First(&dev, gatewayDeviceID).Error; err != nil || !dev.IsGateway {
		return fmt.Errorf("device %d is not a gateway", gatewayDeviceID)
	}
	var line EnterpriseLine
	if err := db.Get().First(&line, lineID).Error; err != nil {
		return fmt.Errorf("line %d not found", lineID)
	}
	// 校验 line 归属与 device 归属同一账号(防跨客户误绑)
	var cust EnterpriseCustomer
	if err := db.Get().First(&cust, line.CustomerID).Error; err != nil || cust.UserID != dev.UserID {
		return fmt.Errorf("line %d and device %d belong to different accounts", lineID, gatewayDeviceID)
	}
	var existing EnterpriseRouterBinding
	err := db.Get().Where("gateway_device_id = ? AND slot = ?", gatewayDeviceID, slot).First(&existing).Error
	if err == nil {
		existing.LineID = lineID
		return db.Get().Save(&existing).Error
	}
	return db.Get().Create(&EnterpriseRouterBinding{GatewayDeviceID: gatewayDeviceID, Slot: slot, LineID: lineID}).Error
}

// ===== handlers =====

func api_admin_list_enterprise_customers(c *gin.Context) {
	pagination := PaginationFromRequest(c)
	var customers []EnterpriseCustomer
	query := db.Get().Model(&EnterpriseCustomer{})
	if err := query.Count(&pagination.Total).Error; err != nil {
		log.Errorf(c, "failed to count enterprise customers: %v", err)
		Error(c, ErrorSystemError, "failed to count enterprise customers")
		return
	}
	if err := query.Order("id DESC").Offset(pagination.Offset()).Limit(pagination.PageSize).Find(&customers).Error; err != nil {
		log.Errorf(c, "failed to list enterprise customers: %v", err)
		Error(c, ErrorSystemError, "failed to list enterprise customers")
		return
	}
	ListWithData(c, customers, pagination)
}

type adminCreateEnterpriseCustomerRequest struct {
	Company string `json:"company"`
	Contact string `json:"contact"`
	UserID  uint64 `json:"userId"`
}

func api_admin_create_enterprise_customer(c *gin.Context) {
	var req adminCreateEnterpriseCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if req.Company == "" || req.UserID == 0 {
		Error(c, ErrorInvalidArgument, "company and userId are required")
		return
	}
	cust := &EnterpriseCustomer{Company: req.Company, Contact: req.Contact, UserID: req.UserID}
	if err := db.Get().Create(cust).Error; err != nil {
		log.Errorf(c, "failed to create enterprise customer: %v", err)
		Error(c, ErrorSystemError, "failed to create enterprise customer")
		return
	}
	Success(c, cust)
	WriteAuditLog(c, "enterprise_customer_create", "enterprise_customer", fmt.Sprint(cust.ID), nil)
}

type adminUpdateEnterpriseCustomerRequest struct {
	Company *string `json:"company"`
	Contact *string `json:"contact"`
	Status  *string `json:"status"`
}

func api_admin_update_enterprise_customer(c *gin.Context) {
	id := c.Param("id")
	var req adminUpdateEnterpriseCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	var cust EnterpriseCustomer
	if err := db.Get().First(&cust, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "enterprise customer not found")
		} else {
			log.Errorf(c, "failed to find enterprise customer %s: %v", id, err)
			Error(c, ErrorSystemError, "failed to find enterprise customer")
		}
		return
	}
	updateData := make(map[string]any)
	if req.Company != nil {
		updateData["company"] = *req.Company
	}
	if req.Contact != nil {
		updateData["contact"] = *req.Contact
	}
	if req.Status != nil {
		updateData["status"] = *req.Status
	}
	if len(updateData) > 0 {
		if err := db.Get().Model(&cust).Updates(updateData).Error; err != nil {
			log.Errorf(c, "failed to update enterprise customer %s: %v", id, err)
			Error(c, ErrorSystemError, "failed to update enterprise customer")
			return
		}
	}
	db.Get().First(&cust, id)
	Success(c, &cust)
	WriteAuditLog(c, "enterprise_customer_update", "enterprise_customer", id, nil)
}

func api_admin_list_enterprise_lines(c *gin.Context) {
	customerID := c.Param("id")
	var lines []EnterpriseLine
	if err := db.Get().Preload("Node").Where("customer_id = ?", customerID).Order("id").Find(&lines).Error; err != nil {
		log.Errorf(c, "failed to list enterprise lines for customer %s: %v", customerID, err)
		Error(c, ErrorSystemError, "failed to list enterprise lines")
		return
	}
	ItemsAll(c, lines)
}

type adminCreateEnterpriseLineRequest struct {
	CustomerID  uint64 `json:"customerId"`
	NodeID      uint64 `json:"nodeId"`
	CountryCode string `json:"countryCode"`
	LineNo      int    `json:"lineNo"`
}

func api_admin_create_enterprise_line(c *gin.Context) {
	var req adminCreateEnterpriseLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	line, err := adminCreateEnterpriseLineFull(req.CustomerID, req.NodeID, req.CountryCode, req.LineNo)
	if err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	Success(c, line)
	WriteAuditLog(c, "enterprise_line_create", "enterprise_line", fmt.Sprint(line.ID), nil)
}

type adminUpdateEnterpriseLineRequest struct {
	Status string `json:"status"`
}

func api_admin_update_enterprise_line(c *gin.Context) {
	id := c.Param("id")
	var req adminUpdateEnterpriseLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	var line EnterpriseLine
	if err := db.Get().First(&line, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "enterprise line not found")
		} else {
			log.Errorf(c, "failed to find enterprise line %s: %v", id, err)
			Error(c, ErrorSystemError, "failed to find enterprise line")
		}
		return
	}
	if req.Status != "" {
		if err := db.Get().Model(&line).Update("status", req.Status).Error; err != nil {
			log.Errorf(c, "failed to update enterprise line %s: %v", id, err)
			Error(c, ErrorSystemError, "failed to update enterprise line")
			return
		}
	}
	db.Get().First(&line, id)
	Success(c, &line)
	WriteAuditLog(c, "enterprise_line_update", "enterprise_line", id, nil)
}

func api_admin_delete_enterprise_line(c *gin.Context) {
	id := c.Param("id")
	var line EnterpriseLine
	if err := db.Get().First(&line, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			Error(c, ErrorNotFound, "enterprise line not found")
		} else {
			log.Errorf(c, "failed to find enterprise line %s: %v", id, err)
			Error(c, ErrorSystemError, "failed to find enterprise line")
		}
		return
	}
	if err := adminDeleteEnterpriseLine(line.ID); err != nil {
		Error(c, ErrorInvalidOperation, err.Error())
		return
	}
	SuccessEmpty(c)
	WriteAuditLog(c, "enterprise_line_delete", "enterprise_line", id, nil)
}

func api_admin_list_enterprise_bindings(c *gin.Context) {
	var bindings []EnterpriseRouterBinding
	query := db.Get().Preload("Line").Preload("Line.Node")
	if deviceID := c.Query("deviceId"); deviceID != "" {
		query = query.Where("gateway_device_id = ?", deviceID)
	}
	if customerID := c.Query("customerId"); customerID != "" {
		query = query.Joins("JOIN enterprise_lines ON enterprise_lines.id = enterprise_router_bindings.line_id").
			Where("enterprise_lines.customer_id = ?", customerID)
	}
	if err := query.Order("gateway_device_id, slot").Find(&bindings).Error; err != nil {
		log.Errorf(c, "failed to list enterprise bindings: %v", err)
		Error(c, ErrorSystemError, "failed to list enterprise bindings")
		return
	}
	ItemsAll(c, bindings)
}

type adminUpsertEnterpriseBindingRequest struct {
	GatewayDeviceID uint64 `json:"gatewayDeviceId"`
	Slot            int    `json:"slot"`
	LineID          uint64 `json:"lineId"`
}

func api_admin_upsert_enterprise_binding(c *gin.Context) {
	var req adminUpsertEnterpriseBindingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	if err := adminUpsertBinding(req.GatewayDeviceID, req.Slot, req.LineID); err != nil {
		Error(c, ErrorInvalidArgument, err.Error())
		return
	}
	SuccessEmpty(c)
	WriteAuditLog(c, "enterprise_binding_upsert", "enterprise_router_binding",
		fmt.Sprintf("dev=%d slot=%d", req.GatewayDeviceID, req.Slot), nil)
}

func api_admin_delete_enterprise_binding(c *gin.Context) {
	id := c.Param("id")
	if err := db.Get().Delete(&EnterpriseRouterBinding{}, id).Error; err != nil {
		log.Errorf(c, "failed to delete enterprise binding %s: %v", id, err)
		Error(c, ErrorSystemError, "failed to delete enterprise binding")
		return
	}
	SuccessEmpty(c)
	WriteAuditLog(c, "enterprise_binding_delete", "enterprise_router_binding", id, nil)
}
