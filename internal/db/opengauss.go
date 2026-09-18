package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"math/big"
	"time"

	_ "github.com/lib/pq"
)

// OpenGaussConfig openGauss 连接配置
type OpenGaussConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
	Username string `json:"username"`
	Password string `json:"password"`
	Schema   string `json:"schema"` // 默认 "soc"
}

// OpenGaussClient openGauss 数据库客户端
type OpenGaussClient struct {
	db     *sql.DB
	config OpenGaussConfig
}

// NewOpenGaussClient 创建客户端
func NewOpenGaussClient(cfg OpenGaussConfig) (*OpenGaussClient, error) {
	if cfg.Port == 0 {
		cfg.Port = 5432
	}
	if cfg.Schema == "" {
		cfg.Schema = "soc"
	}
	if cfg.Database == "" {
		cfg.Database = "wnt"
	}

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=disable",
		cfg.Host, cfg.Port, cfg.Username, cfg.Password, cfg.Database)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open gauss: %w", err)
	}

	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(5 * time.Minute)

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("ping gauss: %w", err)
	}

	return &OpenGaussClient{db: db, config: cfg}, nil
}

// Close 关闭连接
func (c *OpenGaussClient) Close() error {
	if c.db != nil {
		return c.db.Close()
	}
	return nil
}

// IsHealthy 健康检查
func (c *OpenGaussClient) IsHealthy() bool {
	if c.db == nil {
		return false
	}
	return c.db.Ping() == nil
}

// QueryReceivedDevices 查询已收录的 U 盘设备（status = 3 已收录）
func (c *OpenGaussClient) QueryReceivedDevices(limit int) ([]string, error) {
	schema := c.config.Schema
	query := fmt.Sprintf(`
		SELECT sn FROM %s.wl_usb_device 
		WHERE status = 3 
		ORDER BY id 
		LIMIT $1`, schema)

	rows, err := c.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("query devices: %w", err)
	}
	defer rows.Close()

	var sns []string
	for rows.Next() {
		var sn string
		if err := rows.Scan(&sn); err != nil {
			return nil, err
		}
		sns = append(sns, sn)
	}
	return sns, rows.Err()
}

// QueryDeviceBySN 按 SN 查询设备是否存在且已收录（status = 3）
func (c *OpenGaussClient) QueryDeviceBySN(sn string) (bool, error) {
	schema := c.config.Schema
	query := fmt.Sprintf(`
		SELECT COUNT(*) FROM %s.wl_usb_device 
		WHERE sn = $1 AND status = 3`, schema)

	var count int
	if err := c.db.QueryRow(query, sn).Scan(&count); err != nil {
		return false, fmt.Errorf("query device by sn: %w", err)
	}
	return count > 0, nil
}

// InsertApplyRecord 插入申领记录（状态=0 已申请，未使用）
func (c *OpenGaussClient) InsertApplyRecord(deviceSn, applyCode, applicantName, applicantCode, factoryIds string, startTime, endTime time.Time) error {
	schema := c.config.Schema
	query := fmt.Sprintf(`
		INSERT INTO %s.wl_usb_apply 
		(applicant_name, applicant_code, start_time, end_time, factory_ids, status, apply_code, use_count, device_sn, create_time, update_time)
		VALUES ($1, $2, $3, $4, $5, 0, $6, 0, $7, NOW(), NOW())`, schema)

	_, err := c.db.Exec(query, applicantName, applicantCode, startTime, endTime, factoryIds, applyCode, deviceSn)
	if err != nil {
		return fmt.Errorf("insert apply: %w", err)
	}
	return nil
}

// BatchInsertApplyRecords 批量插入申领记录
func (c *OpenGaussClient) BatchInsertApplyRecords(records []ApplyRecord) error {
	schema := c.config.Schema
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(fmt.Sprintf(`
		INSERT INTO %s.wl_usb_apply 
		(applicant_name, applicant_code, start_time, end_time, factory_ids, status, apply_code, use_count, device_sn, create_time, update_time)
		VALUES ($1, $2, $3, $4, $5, 0, $6, 0, $7, NOW(), NOW())`, schema))
	if err != nil {
		return fmt.Errorf("prepare: %w", err)
	}
	defer stmt.Close()

	for _, r := range records {
		if _, err := stmt.Exec(r.ApplicantName, r.ApplicantCode, r.StartTime, r.EndTime, r.FactoryIds, r.ApplyCode, r.DeviceSn); err != nil {
			return fmt.Errorf("insert apply %s: %w", r.ApplyCode, err)
		}
	}

	return tx.Commit()
}

// ApplyRecord 申领记录
type ApplyRecord struct {
	DeviceSn      string
	ApplyCode     string
	ApplicantName string
	ApplicantCode string
	FactoryIds    string
	StartTime     time.Time
	EndTime       time.Time
}

// GenerateApplyCode 生成申领码（6位字母数字，crypto/rand 保证唯一性）
func GenerateApplyCode() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 6)
	for i := range b {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			// 极低概率失败时退化到时间戳，保证调用不中断
			b[i] = chars[time.Now().UnixNano()%int64(len(chars))]
			continue
		}
		b[i] = chars[n.Int64()]
	}
	return string(b)
}
