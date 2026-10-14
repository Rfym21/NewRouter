package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"new-router/common"

	"gorm.io/gorm"
)

func buildLogExportQuery(filters LogExportFilters, userId int, isAdmin bool) (*gorm.DB, error) {
	filter := LogFilter{Type: filters.LogType, ModelName: filters.ModelName, TokenName: filters.TokenName,
		StartTimestamp: filters.StartTimestamp, EndTimestamp: filters.EndTimestamp, Group: filters.Group,
		RequestId: filters.RequestId, UpstreamRequestId: filters.UpstreamRequestId}
	tx := LOG_DB.Model(&Log{})
	if isAdmin {
		filter.Username, filter.Channel = filters.Username, filters.Channel
	} else {
		tx = tx.Where("logs.user_id = ?", userId)
	}
	return filter.apply(tx)
}

func countLogsForExport(filters LogExportFilters, userId int, isAdmin bool) (int64, error) {
	tx, err := buildLogExportQuery(filters, userId, isAdmin)
	if err != nil {
		return 0, err
	}
	var total int64
	err = tx.Count(&total).Error
	return total, err
}

func buildLogExportHeader(columns []string) []string {
	columnByKey := logExportColumnMap()
	header := make([]string, 0, len(columns))
	for _, key := range columns {
		if column, ok := columnByKey[key]; ok {
			header = append(header, column.Label)
		}
	}
	return header
}

func buildLogExportRow(log *Log, columns []string) []string {
	row := make([]string, 0, len(columns))
	for _, column := range columns {
		value := getLogExportValue(log, column)
		if value != "" && strings.ContainsRune("=+-@\t\r", rune(value[0])) {
			if _, err := strconv.ParseFloat(value, 64); err != nil {
				value = "'" + value
			}
		}
		row = append(row, value)
	}
	return row
}

func getLogExportValue(log *Log, column string) string {
	other, _ := common.StrToMap(log.Other)
	if other == nil {
		other = map[string]interface{}{}
	}
	switch column {
	case "request_id":
		return log.RequestId
	case "stream":
		return strconv.FormatBool(log.IsStream)
	case "cache_read":
		return strconv.FormatFloat(toFloat64(other["cache_tokens"]), 'f', -1, 64)
	case "cache_write":
		value := toFloat64(other["cache_creation_tokens_5m"]) + toFloat64(other["cache_creation_tokens_1h"])
		if value == 0 {
			value = toFloat64(other["cache_creation_tokens"])
		}
		return strconv.FormatFloat(value, 'f', -1, 64)
	case "time":
		return time.Unix(log.CreatedAt, 0).Format("2006-01-02 15:04:05")
	case "channel":
		if log.ChannelName != "" {
			return fmt.Sprintf("%d - %s", log.ChannelId, log.ChannelName)
		}
		if log.ChannelId != 0 {
			return strconv.Itoa(log.ChannelId)
		}
		return ""
	case "username":
		return log.Username
	case "token":
		return log.TokenName
	case "group":
		if log.Group != "" {
			return log.Group
		}
		if group, ok := other["group"].(string); ok {
			return group
		}
		return ""
	case "type":
		return getLogTypeText(log.Type)
	case "model":
		upstreamModel, _ := other["upstream_model_name"].(string)
		isMapped, _ := other["is_model_mapped"].(bool)
		if isMapped && upstreamModel != "" {
			return log.ModelName + " -> " + upstreamModel
		}
		return log.ModelName
	case "use_time":
		if log.Type != LogTypeConsume && log.Type != LogTypeError {
			return ""
		}
		parts := []string{fmt.Sprintf("%d s", log.UseTime)}
		if log.IsStream {
			if frt, ok := other["frt"]; ok {
				parts = append(parts, fmt.Sprintf("%.1f s 首字", toFloat64(frt)/1000))
			}
			parts = append(parts, "流")
		} else {
			parts = append(parts, "非流")
		}
		return strings.Join(parts, " / ")
	case "prompt":
		parts := []string{strconv.Itoa(log.PromptTokens)}
		appendPositiveNumber := func(label string, value any) {
			num := toFloat64(value)
			if num > 0 {
				parts = append(parts, fmt.Sprintf("%s %.0f", label, num))
			}
		}
		appendPositiveNumber("缓存读", other["cache_tokens"])
		appendPositiveNumber("缓存写", other["cache_creation_tokens"])
		return strings.Join(parts, " / ")
	case "completion":
		return strconv.Itoa(log.CompletionTokens)
	case "cost":
		if other["billing_source"] == "subscription" {
			return fmt.Sprintf("订阅抵扣: %d", log.Quota)
		}
		return strconv.Itoa(log.Quota)
	case "retry":
		if adminInfo, ok := other["admin_info"].(map[string]any); ok {
			if channels, ok := adminInfo["use_channel"].([]any); ok {
				parts := make([]string, 0, len(channels))
				for _, channel := range channels {
					parts = append(parts, fmt.Sprint(channel))
				}
				return strings.Join(parts, "->")
			}
		}
		return ""
	case "ip":
		return log.Ip
	case "details":
		return log.Content
	default:
		return ""
	}
}

func getLogTypeText(logType int) string {
	switch logType {
	case LogTypeTopup:
		return "充值"
	case LogTypeConsume:
		return "消费"
	case LogTypeManage:
		return "管理"
	case LogTypeSystem:
		return "系统"
	case LogTypeError:
		return "错误"
	case LogTypeRefund:
		return "退款"
	default:
		return "未知"
	}
}

func toFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case json.Number:
		n, _ := v.Float64()
		return n
	default:
		return 0
	}
}

func sanitizeUserLogs(logs []*Log) {
	for _, log := range logs {
		log.ChannelName = ""
		log.Other = formatLogOtherJSON(log.Other, logOtherVisibilityUser)
	}
}

func fillLogChannelNames(logs []*Log) error {
	channelIds := make(map[int]struct{})
	for _, log := range logs {
		if log.ChannelId != 0 {
			channelIds[log.ChannelId] = struct{}{}
		}
	}
	if len(channelIds) == 0 {
		return nil
	}
	ids := make([]int, 0, len(channelIds))
	for id := range channelIds {
		ids = append(ids, id)
	}
	var channels []struct {
		Id   int    `gorm:"column:id"`
		Name string `gorm:"column:name"`
	}
	if common.MemoryCacheEnabled {
		for _, channelId := range ids {
			if cacheChannel, err := CacheGetChannel(channelId); err == nil {
				channels = append(channels, struct {
					Id   int    `gorm:"column:id"`
					Name string `gorm:"column:name"`
				}{Id: channelId, Name: cacheChannel.Name})
			}
		}
	} else if err := DB.Table("channels").Select("id, name").Where("id IN ?", ids).Find(&channels).Error; err != nil {
		return err
	}
	channelMap := make(map[int]string, len(channels))
	for _, channel := range channels {
		channelMap[channel.Id] = channel.Name
	}
	for i := range logs {
		logs[i].ChannelName = channelMap[logs[i].ChannelId]
	}
	return nil
}
