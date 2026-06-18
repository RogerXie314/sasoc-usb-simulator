package model

import (
	"database/sql"
	"fmt"
	"time"
)

// CabinetSlotStatus 管控柜插槽状态记录
type CabinetSlotStatus struct {
	StationID string `json:"stationId"`
	DoorNo    int    `json:"doorNo"`
	Status    int    `json:"status"`   // 0=未知,1=关闭,2=开启,3=开启超时,4=故障
	Reason    string `json:"reason"`   // 故障原因描述
	UpdatedAt string `json:"updatedAt"`
}

// UpsertCabinetSlotStatus 插入或更新插槽状态
func UpsertCabinetSlotStatus(db *sql.DB, s *CabinetSlotStatus) error {
	query := `
		INSERT INTO cabinet_slot_status (station_id, door_no, status, reason, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(station_id, door_no) DO UPDATE SET
			status = excluded.status,
			reason = excluded.reason,
			updated_at = excluded.updated_at
	`
	_, err := db.Exec(query, s.StationID, s.DoorNo, s.Status, s.Reason, time.Now().Format("2006-01-02 15:04:05"))
	if err != nil {
		return fmt.Errorf("upsert cabinet slot status: %w", err)
	}
	return nil
}

// GetCabinetSlotStatus 查询单个插槽状态
func GetCabinetSlotStatus(db *sql.DB, stationID string, doorNo int) (*CabinetSlotStatus, error) {
	query := `SELECT station_id, door_no, status, reason, updated_at FROM cabinet_slot_status WHERE station_id = ? AND door_no = ?`
	row := db.QueryRow(query, stationID, doorNo)
	var s CabinetSlotStatus
	var updatedAt string
	err := row.Scan(&s.StationID, &s.DoorNo, &s.Status, &s.Reason, &updatedAt)
	if err == sql.ErrNoRows {
		// 未持久化过，返回默认状态（关闭）
		return &CabinetSlotStatus{
			StationID: stationID,
			DoorNo:    doorNo,
			Status:    1,
			Reason:    "",
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get cabinet slot status: %w", err)
	}
	s.UpdatedAt = updatedAt
	return &s, nil
}

// ListCabinetSlotStatusesByStation 查询某站点的全部插槽状态
func ListCabinetSlotStatusesByStation(db *sql.DB, stationID string) ([]*CabinetSlotStatus, error) {
	query := `SELECT station_id, door_no, status, reason, updated_at FROM cabinet_slot_status WHERE station_id = ? ORDER BY door_no`
	rows, err := db.Query(query, stationID)
	if err != nil {
		return nil, fmt.Errorf("list cabinet slot statuses: %w", err)
	}
	defer rows.Close()

	var result []*CabinetSlotStatus
	for rows.Next() {
		var s CabinetSlotStatus
		var updatedAt string
		if err := rows.Scan(&s.StationID, &s.DoorNo, &s.Status, &s.Reason, &updatedAt); err != nil {
			return nil, fmt.Errorf("scan cabinet slot status: %w", err)
		}
		s.UpdatedAt = updatedAt
		result = append(result, &s)
	}
	return result, rows.Err()
}

// DeleteCabinetSlotStatus 删除单条插槽状态记录（恢复默认用）
func DeleteCabinetSlotStatus(db *sql.DB, stationID string, doorNo int) error {
	query := `DELETE FROM cabinet_slot_status WHERE station_id = ? AND door_no = ?`
	_, err := db.Exec(query, stationID, doorNo)
	if err != nil {
		return fmt.Errorf("delete cabinet slot status: %w", err)
	}
	return nil
}

// DeleteCabinetSlotStatusesByStation 删除某站点全部插槽状态（整柜恢复用）
func DeleteCabinetSlotStatusesByStation(db *sql.DB, stationID string) error {
	query := `DELETE FROM cabinet_slot_status WHERE station_id = ?`
	_, err := db.Exec(query, stationID)
	if err != nil {
		return fmt.Errorf("delete cabinet slot statuses by station: %w", err)
	}
	return nil
}
