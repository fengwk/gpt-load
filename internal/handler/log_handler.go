package handler

import (
	"fmt"
	app_errors "gpt-load/internal/errors"
	"gpt-load/internal/i18n"
	"gpt-load/internal/models"
	"gpt-load/internal/response"
	"log"
	"math"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"gorm.io/gorm"
)

// LogResponse defines the structure for log entries in the API response
type LogResponse struct {
	models.RequestLog
}

var logListColumns = []string{
	"id",
	"timestamp",
	"group_id",
	"group_name",
	"parent_group_name",
	"key_value",
	"model",
	"is_success",
	"source_ip",
	"status_code",
	"request_path",
	"duration",
	"error_message",
	"request_type",
	"upstream_addr",
	"is_stream",
}

// GetLogs handles fetching request logs with filtering and pagination.
func (s *Server) GetLogs(c *gin.Context) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}

	pageSize, err := strconv.Atoi(c.DefaultQuery("page_size", strconv.Itoa(response.DefaultPageSize)))
	if err != nil || pageSize <= 0 {
		pageSize = response.DefaultPageSize
	}
	if pageSize > response.MaxPageSize {
		pageSize = response.MaxPageSize
	}

	baseQuery := s.LogService.GetLogsQuery(c)

	var totalItems int64
	if err := baseQuery.Count(&totalItems).Error; err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	var logs []models.RequestLog
	offset := (page - 1) * pageSize
	listQuery := baseQuery.Select(logListColumns).Order("timestamp desc").Limit(pageSize).Offset(offset)
	if err := listQuery.Find(&logs).Error; err != nil {
		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	// 解密所有日志中的密钥用于前端显示
	for i := range logs {
		s.decryptLogKeyValue(&logs[i])
	}

	totalPages := int(math.Ceil(float64(totalItems) / float64(pageSize)))
	pagination := &response.PaginatedResponse{
		Items: logs,
		Pagination: response.Pagination{
			Page:       page,
			PageSize:   pageSize,
			TotalItems: totalItems,
			TotalPages: totalPages,
		},
	}

	response.Success(c, pagination)
}

// GetLog handles fetching a full request log by ID.
func (s *Server) GetLog(c *gin.Context) {
	logID := c.Param("id")
	if logID == "" {
		response.Error(c, app_errors.NewAPIError(app_errors.ErrBadRequest, "invalid log ID"))
		return
	}

	var requestLog models.RequestLog
	if err := s.DB.First(&requestLog, "id = ?", logID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			response.Error(c, app_errors.ErrResourceNotFound)
			return
		}

		response.Error(c, app_errors.ParseDBError(err))
		return
	}

	s.decryptLogKeyValue(&requestLog)
	response.Success(c, requestLog)
}

func (s *Server) decryptLogKeyValue(requestLog *models.RequestLog) {
	if requestLog == nil || requestLog.KeyValue == "" {
		return
	}

	decryptedValue, err := s.EncryptionSvc.Decrypt(requestLog.KeyValue)
	if err != nil {
		logrus.WithError(err).WithField("log_id", requestLog.ID).Error("Failed to decrypt log key value")
		requestLog.KeyValue = "failed-to-decrypt"
		return
	}

	requestLog.KeyValue = decryptedValue
}

// ExportLogs handles exporting filtered log keys to a CSV file.
func (s *Server) ExportLogs(c *gin.Context) {
	filename := fmt.Sprintf("log_keys_export_%s.csv", time.Now().Format("20060102150405"))
	c.Header("Content-Disposition", "attachment; filename="+filename)
	c.Header("Content-Type", "text/csv; charset=utf-8")

	// Stream the response
	err := s.LogService.StreamLogKeysToCSV(c, c.Writer)
	if err != nil {
		log.Printf("Failed to stream log keys to CSV: %v", err)
		c.JSON(500, gin.H{"error": i18n.Message(c, "error.export_logs")})
		return
	}
}
