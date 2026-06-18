package model

import (
	"database/sql"
	"fmt"
)

// InsertUsb 插入或更新 U 盘设备（UPSERT）
func InsertUsb(db *sql.DB, u *SimUsbRow) error {
	_, err := db.Exec(`INSERT INTO sim_usb
		(id, sn, model, firmware_version, qualified, area_name, claim_info, inserted, station_id, door_no, write_delay, read_delay, write_fail, read_fail)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			sn = excluded.sn, model = excluded.model, firmware_version = excluded.firmware_version,
			qualified = excluded.qualified, area_name = excluded.area_name, claim_info = excluded.claim_info,
			inserted = excluded.inserted, station_id = excluded.station_id, door_no = excluded.door_no,
			write_delay = excluded.write_delay, read_delay = excluded.read_delay, write_fail = excluded.write_fail, read_fail = excluded.read_fail`,
		u.ID, u.SN, u.Model, u.FirmwareVersion, u.Qualified, u.AreaName, u.ClaimInfo, u.Inserted, u.StationID, u.DoorNo,
		u.WriteDelay, u.ReadDelay, u.WriteFail, u.ReadFail,
	)
	if err != nil {
		return fmt.Errorf("upsert sim_usb %s: %w", u.ID, err)
	}
	return nil
}

// DeleteUsb 删除 U 盘设备
func DeleteUsb(db *sql.DB, id string) error {
	_, err := db.Exec(`DELETE FROM sim_usb WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete sim_usb %s: %w", id, err)
	}
	return nil
}

// GetUsb 按 ID 查询 U 盘
func GetUsb(db *sql.DB, id string) (*SimUsbRow, error) {
	row := db.QueryRow(`SELECT id, sn, model, firmware_version, qualified, area_name, claim_info, inserted, station_id, door_no, write_delay, read_delay, write_fail, read_fail
		FROM sim_usb WHERE id = ?`, id)
	var u SimUsbRow
	if err := row.Scan(&u.ID, &u.SN, &u.Model, &u.FirmwareVersion, &u.Qualified, &u.AreaName,
		&u.ClaimInfo, &u.Inserted, &u.StationID, &u.DoorNo, &u.WriteDelay, &u.ReadDelay, &u.WriteFail, &u.ReadFail); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get sim_usb %s: %w", id, err)
	}
	return &u, nil
}

// ListUsbs 列出所有 U 盘
func ListUsbs(db *sql.DB) ([]*SimUsbRow, error) {
	rows, err := db.Query(`SELECT id, sn, model, firmware_version, qualified, area_name, claim_info, inserted, station_id, door_no, write_delay, read_delay, write_fail, read_fail
		FROM sim_usb ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list sim_usb: %w", err)
	}
	defer rows.Close()

	var result []*SimUsbRow
	for rows.Next() {
		var u SimUsbRow
		if err := rows.Scan(&u.ID, &u.SN, &u.Model, &u.FirmwareVersion, &u.Qualified, &u.AreaName,
			&u.ClaimInfo, &u.Inserted, &u.StationID, &u.DoorNo, &u.WriteDelay, &u.ReadDelay, &u.WriteFail, &u.ReadFail); err != nil {
			return nil, fmt.Errorf("scan sim_usb: %w", err)
		}
		result = append(result, &u)
	}
	return result, rows.Err()
}
