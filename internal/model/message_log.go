package model

import (
	"database/sql"
	"fmt"
	"time"
)

// InsertMessageLog 插入一条消息日志
func InsertMessageLog(db *sql.DB, m *MessageLogRow) (int64, error) {
	now := time.Now().Format(time.DateTime)
	res, err := db.Exec(`INSERT INTO message_log
		(station_id, direction, cmdid, request_body, response_body, latency_ms, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.StationID, m.Direction, m.CmdID, m.RequestBody, m.ResponseBody, m.LatencyMs, now,
	)
	if err != nil {
		return 0, fmt.Errorf("insert message_log: %w", err)
	}
	return res.LastInsertId()
}

// GetMessageLog 按ID查询单条消息日志
func GetMessageLog(db *sql.DB, id int64) (*MessageLogRow, error) {
	row := db.QueryRow(`SELECT id, station_id, direction, cmdid, request_body, response_body, latency_ms, created_at
		FROM message_log WHERE id = ?`, id)
	var m MessageLogRow
	if err := row.Scan(&m.ID, &m.StationID, &m.Direction, &m.CmdID,
		&m.RequestBody, &m.ResponseBody, &m.LatencyMs, &m.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get message_log %d: %w", id, err)
	}
	return &m, nil
}

// MessageLogFilter 消息日志查询过滤条件
type MessageLogFilter struct {
	StationID string // 按 station_id 过滤
	CmdID     int    // 按 cmdid 过滤（0 表示不过滤）
	Direction string // 按 direction 过滤（空表示不过滤）
	StartTime string // 起始时间（空表示不限）
	EndTime   string // 结束时间（空表示不限）
	Limit     int    // 返回条数上限（0 默认 100）
	Offset    int    // 偏移量
}

// ListMessageLogs 按条件查询消息日志
func ListMessageLogs(db *sql.DB, f MessageLogFilter) ([]*MessageLogRow, int, error) {
	where := "WHERE 1=1"
	var args []interface{}

	if f.StationID != "" {
		where += " AND station_id = ?"
		args = append(args, f.StationID)
	}
	if f.CmdID > 0 {
		where += " AND cmdid = ?"
		args = append(args, f.CmdID)
	}
	if f.Direction != "" {
		where += " AND direction = ?"
		args = append(args, f.Direction)
	}
	if f.StartTime != "" {
		where += " AND created_at >= ?"
		args = append(args, f.StartTime)
	}
	if f.EndTime != "" {
		where += " AND created_at <= ?"
		args = append(args, f.EndTime)
	}

	// 计算总数
	var total int
	countRow := db.QueryRow("SELECT COUNT(*) FROM message_log "+where, args...)
	if err := countRow.Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count message_log: %w", err)
	}

	// 查询数据
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	query := fmt.Sprintf(
		`SELECT id, station_id, direction, cmdid, request_body, response_body, latency_ms, created_at
		FROM message_log %s ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		where,
	)
	args = append(args, limit, f.Offset)

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list message_log: %w", err)
	}
	defer rows.Close()

	var result []*MessageLogRow
	for rows.Next() {
		var m MessageLogRow
		if err := rows.Scan(&m.ID, &m.StationID, &m.Direction, &m.CmdID,
			&m.RequestBody, &m.ResponseBody, &m.LatencyMs, &m.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan message_log: %w", err)
		}
		result = append(result, &m)
	}
	return result, total, rows.Err()
}

// DeleteMessageLogsByStation 删除指定安检站的所有消息日志
func DeleteMessageLogsByStation(db *sql.DB, stationID string) (int64, error) {
	res, err := db.Exec(`DELETE FROM message_log WHERE station_id = ?`, stationID)
	if err != nil {
		return 0, fmt.Errorf("delete message_log for station %s: %w", stationID, err)
	}
	return res.RowsAffected()
}

// PurgeOldMessageLogs 清理指定天数之前的消息日志
func PurgeOldMessageLogs(db *sql.DB, days int) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -days).Format(time.DateTime)
	res, err := db.Exec(`DELETE FROM message_log WHERE created_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("purge message_log older than %d days: %w", days, err)
	}
	return res.RowsAffected()
}

// ClearMessageLogs 清空所有消息日志
func ClearMessageLogs(db *sql.DB) error {
	_, err := db.Exec(`DELETE FROM message_log`)
	if err != nil {
		return fmt.Errorf("clear message_log: %w", err)
	}
	return nil
}
