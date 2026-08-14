package model

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// InitDB 打开 SQLite 数据库并执行 schema 迁移
func InitDB(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", path, err)
	}

	// SQLite 性能优化
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-2000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("set pragma %q: %w", p, err)
		}
	}

	// 设置连接池
	db.SetMaxOpenConns(1) // SQLite 单写
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema migration: %w", err)
	}

	return db, nil
}

// migrate 创建/升级数据库表
func migrate(db *sql.DB) error {
	schema := `
CREATE TABLE IF NOT EXISTS sim_station (
    id          TEXT PRIMARY KEY,
    sn          TEXT    NOT NULL DEFAULT '',
    model       TEXT    NOT NULL DEFAULT '',
    version     TEXT    NOT NULL DEFAULT '',
    ip          TEXT    NOT NULL DEFAULT '',
    mac         TEXT    NOT NULL DEFAULT '',
    name        TEXT    NOT NULL DEFAULT '',
    device_id   INTEGER NOT NULL DEFAULT 0,
    status      TEXT    NOT NULL DEFAULT 'idle',
    uuid        TEXT    NOT NULL DEFAULT '',
    config      TEXT    NOT NULL DEFAULT '{}',
    created_at  DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE TABLE IF NOT EXISTS sim_usb (
    id               TEXT PRIMARY KEY,
    sn               TEXT    NOT NULL DEFAULT '',
    model            TEXT    NOT NULL DEFAULT '',
    firmware_version TEXT    NOT NULL DEFAULT '',
    qualified        INTEGER NOT NULL DEFAULT 1,
    area_name        TEXT    NOT NULL DEFAULT '',
    claim_info       TEXT    NOT NULL DEFAULT '{}',
    inserted         INTEGER NOT NULL DEFAULT 0,
    station_id       TEXT    NOT NULL DEFAULT '',
    door_no          INTEGER NOT NULL DEFAULT 0,
    write_delay      INTEGER NOT NULL DEFAULT 0,
    read_delay       INTEGER NOT NULL DEFAULT 0,
    write_fail       INTEGER NOT NULL DEFAULT 0,
    read_fail        INTEGER NOT NULL DEFAULT 0,
    created_at       DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE TABLE IF NOT EXISTS message_log (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    station_id    TEXT    NOT NULL DEFAULT '',
    direction     TEXT    NOT NULL DEFAULT '',
    cmdid         INTEGER NOT NULL DEFAULT 0,
    request_body  TEXT    NOT NULL DEFAULT '',
    response_body TEXT    NOT NULL DEFAULT '',
    latency_ms    INTEGER NOT NULL DEFAULT 0,
    created_at    DATETIME NOT NULL DEFAULT (datetime('now','localtime'))
);

CREATE TABLE IF NOT EXISTS pressure_test (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    scenario_name TEXT    NOT NULL DEFAULT '',
    config        TEXT    NOT NULL DEFAULT '{}',
    status        TEXT    NOT NULL DEFAULT 'idle',
    started_at    DATETIME,
    stopped_at    DATETIME
);

CREATE TABLE IF NOT EXISTS pressure_metric (
    id                    INTEGER PRIMARY KEY AUTOINCREMENT,
    test_id               INTEGER NOT NULL DEFAULT 0,
    timestamp             DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    online_count          INTEGER NOT NULL DEFAULT 0,
    heartbeat_success_rate REAL    NOT NULL DEFAULT 0,
    avg_latency_ms        REAL    NOT NULL DEFAULT 0,
    throughput            REAL    NOT NULL DEFAULT 0,
    FOREIGN KEY (test_id) REFERENCES pressure_test(id)
);

CREATE INDEX IF NOT EXISTS idx_sim_station_sn       ON sim_station(sn);
CREATE INDEX IF NOT EXISTS idx_sim_station_status    ON sim_station(status);
CREATE INDEX IF NOT EXISTS idx_sim_usb_station_id    ON sim_usb(station_id);
CREATE INDEX IF NOT EXISTS idx_sim_usb_inserted      ON sim_usb(inserted);
CREATE INDEX IF NOT EXISTS idx_message_log_station   ON message_log(station_id);
CREATE INDEX IF NOT EXISTS idx_message_log_cmdid     ON message_log(cmdid);
CREATE INDEX IF NOT EXISTS idx_message_log_created   ON message_log(created_at);
CREATE TABLE IF NOT EXISTS cabinet_slot_status (
    station_id   TEXT    NOT NULL,
    door_no      INTEGER NOT NULL DEFAULT 0,
    status       INTEGER NOT NULL DEFAULT 1,  -- 0=未知,1=关闭,2=开启,3=开启超时,4=故障
    reason       TEXT    NOT NULL DEFAULT '', -- 故障原因描述
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now','localtime')),
    PRIMARY KEY (station_id, door_no)
);

CREATE INDEX IF NOT EXISTS idx_pressure_test_status  ON pressure_test(status);
CREATE INDEX IF NOT EXISTS idx_pressure_metric_test  ON pressure_metric(test_id);
CREATE INDEX IF NOT EXISTS idx_cabinet_slot_station ON cabinet_slot_status(station_id);
`
	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("exec schema: %w", err)
	}

	// 兼容性迁移：给旧数据库补 uuid 列
	var colCount int
	_ = db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('sim_station') WHERE name='uuid'`).Scan(&colCount)
	if colCount == 0 {
		if _, err := db.Exec(`ALTER TABLE sim_station ADD COLUMN uuid TEXT NOT NULL DEFAULT ''`); err != nil {
			return fmt.Errorf("migrate add uuid column: %w", err)
		}
	}
	return nil
}

// SimStationRow sim_station 表行
type SimStationRow struct {
	ID       string `json:"id"`
	SN       string `json:"sn"`
	Model    string `json:"model"`
	Version  string `json:"version"`
	IP       string `json:"ip"`
	MAC      string `json:"mac"`
	Name     string `json:"name"`
	DeviceID int    `json:"deviceId"`
	Status   string `json:"status"`
	UUID     string `json:"uuid"`
	Config   string `json:"config"` // JSON string
}

// SimUsbRow sim_usb 表行
type SimUsbRow struct {
	ID              string `json:"id"`
	SN              string `json:"sn"`
	Model           string `json:"model"`
	FirmwareVersion string `json:"firmwareVersion"`
	Qualified       int    `json:"qualified"`
	AreaName        string `json:"areaName"`
	ClaimInfo       string `json:"claimInfo"` // JSON string
	Inserted        int    `json:"inserted"`
	StationID       string `json:"stationId"`
	DoorNo          int    `json:"doorNo"`
	WriteDelay      int    `json:"writeDelay"`
	ReadDelay       int    `json:"readDelay"`
	WriteFail       int    `json:"writeFail"`
	ReadFail        int    `json:"readFail"`
}

// MessageLogRow message_log 表行
type MessageLogRow struct {
	ID           int64     `json:"id"`
	StationID    string    `json:"stationId"`
	Direction    string    `json:"direction"`
	CmdID        int       `json:"cmdid"`
	RequestBody  string    `json:"requestBody"`
	ResponseBody string    `json:"responseBody"`
	LatencyMs    int       `json:"latencyMs"`
	CreatedAt    time.Time `json:"createdAt"`
}

// PressureTestRow pressure_test 表行
type PressureTestRow struct {
	ID           int64      `json:"id"`
	ScenarioName string     `json:"scenarioName"`
	Config       string     `json:"config"`
	Status       string     `json:"status"`
	StartedAt    *time.Time `json:"startedAt"`
	StoppedAt    *time.Time `json:"stoppedAt"`
}

// PressureMetricRow pressure_metric 表行
type PressureMetricRow struct {
	ID                   int64     `json:"id"`
	TestID               int64     `json:"testId"`
	Timestamp            time.Time `json:"timestamp"`
	OnlineCount          int       `json:"onlineCount"`
	HeartbeatSuccessRate float64   `json:"heartbeatSuccessRate"`
	AvgLatencyMs         float64   `json:"avgLatencyMs"`
	Throughput           float64   `json:"throughput"`
}
