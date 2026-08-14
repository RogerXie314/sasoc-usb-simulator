package model

import (
	"database/sql"
	"fmt"
	"time"
)

// InsertStation 插入或更新安检站记录（UPSERT：SN 已存在时更新）
func InsertStation(db *sql.DB, s *SimStationRow) error {
	now := time.Now().Format(time.DateTime)
	_, err := db.Exec(`INSERT INTO sim_station
		(id, sn, model, version, ip, mac, name, device_id, status, uuid, config, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			sn = excluded.sn, model = excluded.model, version = excluded.version,
			ip = excluded.ip, mac = excluded.mac, name = excluded.name,
			device_id = excluded.device_id, status = excluded.status,
			uuid = excluded.uuid, config = excluded.config, updated_at = excluded.updated_at`,
		s.ID, s.SN, s.Model, s.Version, s.IP, s.MAC, s.Name, s.DeviceID, s.Status, s.UUID, s.Config, now, now,
	)
	if err != nil {
		return fmt.Errorf("upsert sim_station: %w", err)
	}
	return nil
}

// GetStation 按 ID 查询单条安检站
func GetStation(db *sql.DB, id string) (*SimStationRow, error) {
	row := db.QueryRow(`SELECT id, sn, model, version, ip, mac, name, device_id, status, uuid, config
		FROM sim_station WHERE id = ?`, id)
	var s SimStationRow
	if err := row.Scan(&s.ID, &s.SN, &s.Model, &s.Version, &s.IP, &s.MAC, &s.Name,
		&s.DeviceID, &s.Status, &s.UUID, &s.Config); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get sim_station %s: %w", id, err)
	}
	return &s, nil
}

// ListStations 列出所有安检站，支持按 status 过滤
func ListStations(db *sql.DB, status string) ([]*SimStationRow, error) {
	var query string
	var args []interface{}

	if status != "" {
		query = `SELECT id, sn, model, version, ip, mac, name, device_id, status, uuid, config
			FROM sim_station WHERE status = ? ORDER BY sn ASC`
		args = append(args, status)
	} else {
		query = `SELECT id, sn, model, version, ip, mac, name, device_id, status, uuid, config
			FROM sim_station ORDER BY sn ASC`
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sim_station: %w", err)
	}
	defer rows.Close()

	var result []*SimStationRow
	for rows.Next() {
		var s SimStationRow
		if err := rows.Scan(&s.ID, &s.SN, &s.Model, &s.Version, &s.IP, &s.MAC, &s.Name,
			&s.DeviceID, &s.Status, &s.UUID, &s.Config); err != nil {
			return nil, fmt.Errorf("scan sim_station: %w", err)
		}
		result = append(result, &s)
	}
	return result, rows.Err()
}

// UpdateStation 更新安检站信息（按 ID）
func UpdateStation(db *sql.DB, s *SimStationRow) error {
	now := time.Now().Format(time.DateTime)
	res, err := db.Exec(`UPDATE sim_station SET
		sn = ?, model = ?, version = ?, ip = ?, mac = ?, name = ?,
		device_id = ?, status = ?, uuid = ?, config = ?, updated_at = ?
		WHERE id = ?`,
		s.SN, s.Model, s.Version, s.IP, s.MAC, s.Name,
		s.DeviceID, s.Status, s.UUID, s.Config, now, s.ID,
	)
	if err != nil {
		return fmt.Errorf("update sim_station %s: %w", s.ID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// UpdateStationStatus 仅更新状态
func UpdateStationStatus(db *sql.DB, id, status string) error {
	now := time.Now().Format(time.DateTime)
	res, err := db.Exec(`UPDATE sim_station SET status = ?, updated_at = ? WHERE id = ?`,
		status, now, id)
	if err != nil {
		return fmt.Errorf("update sim_station status %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteStation 按 ID 删除安检站
func DeleteStation(db *sql.DB, id string) error {
	res, err := db.Exec(`DELETE FROM sim_station WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete sim_station %s: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// CountStations 统计各状态的安检站数量
func CountStations(db *sql.DB) (total int, online int, err error) {
	row := db.QueryRow(`SELECT COUNT(*) FROM sim_station`)
	if err := row.Scan(&total); err != nil {
		return 0, 0, fmt.Errorf("count sim_station: %w", err)
	}
	row = db.QueryRow(`SELECT COUNT(*) FROM sim_station WHERE status = 'online'`)
	if err := row.Scan(&online); err != nil {
		return total, 0, fmt.Errorf("count online sim_station: %w", err)
	}
	return total, online, nil
}
