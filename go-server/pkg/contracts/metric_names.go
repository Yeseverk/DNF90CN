package contracts

import (
	"strconv"
	"strings"
	"unicode"
)

const (
	// MetricPrefixLongHeng 是所有框架指标名的统一前缀。
	MetricPrefixLongHeng = "longheng"

	// MetricLabelService 标记服务名。
	MetricLabelService = "service"

	// MetricLabelNode 标记节点 ID。
	MetricLabelNode = "node"

	// MetricLabelRoute 标记协议或 RPC 路由。
	MetricLabelRoute = "route"

	// MetricLabelResult 标记操作结果。
	MetricLabelResult = "result"

	// MetricLabelStatus 标记状态值。
	MetricLabelStatus = "status"

	// MetricLabelComponent 标记平台或运行时组件。
	MetricLabelComponent = "component"

	// MetricLabelKind 标记对象类型。
	MetricLabelKind = "kind"

	// MetricLabelGID 标记游戏区或战区 ID。
	MetricLabelGID = "gid"

	// MetricLabelSID 标记分片 ID。
	MetricLabelSID = "sid"
)

const (
	// MetricResultOK 表示操作成功。
	MetricResultOK = "ok"

	// MetricResultError 表示操作失败。
	MetricResultError = "error"

	// MetricResultDropped 表示请求或事件被丢弃。
	MetricResultDropped = "dropped"

	// MetricResultTimeout 表示操作超时。
	MetricResultTimeout = "timeout"
)

// MetricName 生成跨代码、告警和仪表盘共用的稳定指标名。
func MetricName(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, metricNameFields(part)...)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "_")
}

// PlatformMetric 生成平台核心能力指标名，例如 longheng_platform_bus_publish_total。
func PlatformMetric(component string, parts ...string) string {
	return MetricName(append([]string{MetricPrefixLongHeng, "platform", component}, parts...)...)
}

// RuntimeMetric 生成可选系统模块指标名，例如 longheng_runtime_matchmaker_ticket_total。
func RuntimeMetric(component string, parts ...string) string {
	return MetricName(append([]string{MetricPrefixLongHeng, "runtime", component}, parts...)...)
}

// ServiceMetric 生成服务层指标名，例如 longheng_service_gateway_connections_total。
func ServiceMetric(service string, parts ...string) string {
	return MetricName(append([]string{MetricPrefixLongHeng, "service", service}, parts...)...)
}

// MetricLabels 构造指标标签并跳过空 key，避免调用侧反复写样板代码。
func MetricLabels(values ...string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	labels := make(map[string]string, len(values)/2)
	for idx := 0; idx+1 < len(values); idx += 2 {
		key := strings.TrimSpace(values[idx])
		if key == "" {
			continue
		}
		labels[key] = strings.TrimSpace(values[idx+1])
	}
	if len(labels) == 0 {
		return nil
	}
	return labels
}

// ShardLabels 是区服/分片指标的通用标签构造器。
func ShardLabels(gid, sid int64) map[string]string {
	return MetricLabels(
		MetricLabelGID, strconv.FormatInt(gid, 10),
		MetricLabelSID, strconv.FormatInt(sid, 10),
	)
}

func metricNameFields(part string) []string {
	part = strings.TrimSpace(part)
	if part == "" {
		return nil
	}
	fields := strings.FieldsFunc(part, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.ToLower(strings.Trim(field, "_"))
		if field != "" {
			out = append(out, field)
		}
	}
	return out
}
