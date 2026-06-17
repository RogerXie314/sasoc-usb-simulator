package pressure

import (
	"encoding/json"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"
)

// ReportData 报告数据
type ReportData struct {
	ScenarioName  string
	TestID        int64
	StartTime     time.Time
	StopTime      time.Time
	Duration      time.Duration
	StationCount  int
	Summary       map[string]interface{}
	Snapshots     []MetricsSnapshot
	SnapshotsJSON template.JS
}

// GenerateReport 生成 HTML 报告并返回文件路径
func (e *PressureTestEngine) GenerateReport() (string, error) {
	snapshots := e.collector.AllSnapshots()
	summary := e.collector.Summary()

	e.mu.Lock()
	stationCount := len(e.stations)
	startTime := e.startTime
	stopTime := e.stopTime
	testID := e.testID
	scenarioName := e.config.ScenarioName
	e.mu.Unlock()

	if stopTime.IsZero() {
		stopTime = time.Now()
	}

	snapshotsJSON, err := json.Marshal(snapshots)
	if err != nil {
		return "", fmt.Errorf("marshal snapshots: %w", err)
	}

	data := ReportData{
		ScenarioName:  scenarioName,
		TestID:        testID,
		StartTime:     startTime,
		StopTime:      stopTime,
		Duration:      stopTime.Sub(startTime),
		StationCount:  stationCount,
		Summary:       summary,
		Snapshots:     snapshots,
		SnapshotsJSON: template.JS(snapshotsJSON),
	}

	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return "", fmt.Errorf("parse report template: %w", err)
	}

	filename := fmt.Sprintf("pressure_report_%s_%d.html",
		time.Now().Format("20060102_150405"), testID)
	reportDir := "reports"
	if err := os.MkdirAll(reportDir, 0755); err != nil {
		return "", fmt.Errorf("create reports directory: %w", err)
	}

	reportPath := filepath.Join(reportDir, filename)
	f, err := os.Create(reportPath)
	if err != nil {
		return "", fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("execute report template: %w", err)
	}

	return reportPath, nil
}

// GenerateReportFromData 从给定数据生成 HTML 报告（独立函数，便于测试）
func GenerateReportFromData(data ReportData, outputDir string) (string, error) {
	snapshotsJSON, err := json.Marshal(data.Snapshots)
	if err != nil {
		return "", fmt.Errorf("marshal snapshots: %w", err)
	}
	data.SnapshotsJSON = template.JS(snapshotsJSON)

	tmpl, err := template.New("report").Parse(reportTemplate)
	if err != nil {
		return "", fmt.Errorf("parse report template: %w", err)
	}

	filename := fmt.Sprintf("pressure_report_%s_%d.html",
		time.Now().Format("20060102_150405"), data.TestID)

	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("create output directory: %w", err)
	}

	reportPath := filepath.Join(outputDir, filename)
	f, err := os.Create(reportPath)
	if err != nil {
		return "", fmt.Errorf("create report file: %w", err)
	}
	defer f.Close()

	if err := tmpl.Execute(f, data); err != nil {
		return "", fmt.Errorf("execute report template: %w", err)
	}

	return reportPath, nil
}

const reportTemplate = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>压力测试报告 - {{.ScenarioName}}</title>
<script src="https://cdn.jsdelivr.net/npm/echarts@5.5.0/dist/echarts.min.js"></script>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif; background: #f0f2f5; color: #333; line-height: 1.6; }
.container { max-width: 1200px; margin: 0 auto; padding: 24px; }
.header { background: linear-gradient(135deg, #1a237e, #283593); color: #fff; padding: 32px; border-radius: 12px; margin-bottom: 24px; }
.header h1 { font-size: 28px; margin-bottom: 8px; }
.header .meta { font-size: 14px; opacity: 0.85; }
.header .meta span { margin-right: 24px; }
.card { background: #fff; border-radius: 12px; padding: 24px; margin-bottom: 24px; box-shadow: 0 2px 8px rgba(0,0,0,0.06); }
.card h2 { font-size: 20px; margin-bottom: 16px; color: #1a237e; border-left: 4px solid #1a237e; padding-left: 12px; }
.summary-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(200px, 1fr)); gap: 16px; }
.stat-item { background: #fafafa; border-radius: 8px; padding: 16px; text-align: center; border: 1px solid #e8e8e8; }
.stat-item .label { font-size: 13px; color: #888; margin-bottom: 4px; }
.stat-item .value { font-size: 28px; font-weight: 700; color: #1a237e; }
.stat-item .value.success { color: #2e7d32; }
.stat-item .value.danger { color: #c62828; }
.stat-item .value.warn { color: #f57f17; }
table { width: 100%; border-collapse: collapse; margin-top: 12px; }
table th, table td { padding: 10px 16px; text-align: left; border-bottom: 1px solid #e8e8e8; font-size: 14px; }
table th { background: #f5f5f5; color: #555; font-weight: 600; }
table tr:hover { background: #fafafa; }
.chart-container { width: 100%; height: 400px; margin-top: 16px; }
.charts-row { display: grid; grid-template-columns: 1fr 1fr; gap: 24px; }
@media (max-width: 768px) { .charts-row { grid-template-columns: 1fr; } }
.footer { text-align: center; padding: 16px; color: #aaa; font-size: 12px; }
</style>
</head>
<body>
<div class="container">

<div class="header">
  <h1>压力测试报告</h1>
  <div class="meta">
    <span>场景: {{.ScenarioName}}</span>
    <span>测试ID: {{.TestID}}</span>
    <span>开始: {{.StartTime.Format "2006-01-02 15:04:05"}}</span>
    <span>结束: {{.StopTime.Format "2006-01-02 15:04:05"}}</span>
    <span>持续时间: {{.Duration}}</span>
  </div>
</div>

<div class="card">
  <h2>汇总统计</h2>
  <div class="summary-grid">
    <div class="stat-item">
      <div class="label">站点总数</div>
      <div class="value">{{.StationCount}}</div>
    </div>
    <div class="stat-item">
      <div class="label">注册成功</div>
      <div class="value success">{{if .Summary.registrationSuccess}}{{.Summary.registrationSuccess}}{{else}}0{{end}}</div>
    </div>
    <div class="stat-item">
      <div class="label">注册失败</div>
      <div class="value danger">{{if .Summary.registrationFail}}{{.Summary.registrationFail}}{{else}}0{{end}}</div>
    </div>
    <div class="stat-item">
      <div class="label">注册成功率</div>
      <div class="value {{if gt (printf "%.0f" .Summary.registrationSuccessRate) "90"}}success{{else}}warn{{end}}">{{printf "%.1f" .Summary.registrationSuccessRate}}%</div>
    </div>
    <div class="stat-item">
      <div class="label">平均注册延迟</div>
      <div class="value">{{printf "%.1f" .Summary.avgRegLatencyMs}}ms</div>
    </div>
    <div class="stat-item">
      <div class="label">心跳总数</div>
      <div class="value">{{if .Summary.heartbeatTotal}}{{.Summary.heartbeatTotal}}{{else}}0{{end}}</div>
    </div>
    <div class="stat-item">
      <div class="label">心跳成功率</div>
      <div class="value {{if gt (printf "%.0f" .Summary.heartbeatSuccessRate) "95"}}success{{else}}warn{{end}}">{{printf "%.1f" .Summary.heartbeatSuccessRate}}%</div>
    </div>
    <div class="stat-item">
      <div class="label">平均心跳延迟</div>
      <div class="value">{{printf "%.1f" .Summary.avgHbLatencyMs}}ms</div>
    </div>
    <div class="stat-item">
      <div class="label">吞吐量</div>
      <div class="value">{{printf "%.1f" .Summary.throughput}}/s</div>
    </div>
    <div class="stat-item">
      <div class="label">断线数</div>
      <div class="value {{if .Summary.droppedConnections}}danger{{else}}success{{end}}">{{if .Summary.droppedConnections}}{{.Summary.droppedConnections}}{{else}}0{{end}}</div>
    </div>
    <div class="stat-item">
      <div class="label">峰值在线</div>
      <div class="value">{{if .Summary.peakOnline}}{{.Summary.peakOnline}}{{else}}0{{end}}</div>
    </div>
    <div class="stat-item">
      <div class="label">采样次数</div>
      <div class="value">{{if .Summary.snapshotCount}}{{.Summary.snapshotCount}}{{else}}0{{end}}</div>
    </div>
  </div>
</div>

<div class="card">
  <h2>注册延迟趋势</h2>
  <div id="chartRegLatency" class="chart-container"></div>
</div>

<div class="card">
  <h2>心跳延迟与吞吐量趋势</h2>
  <div class="charts-row">
    <div id="chartHbLatency" class="chart-container"></div>
    <div id="chartThroughput" class="chart-container"></div>
  </div>
</div>

<div class="card">
  <h2>在线站点数与成功率</h2>
  <div class="charts-row">
    <div id="chartOnlineCount" class="chart-container"></div>
    <div id="chartSuccessRate" class="chart-container"></div>
  </div>
</div>

<div class="footer">
  Generated by USB-Simulator Pressure Engine &middot; {{.StopTime.Format "2006-01-02 15:04:05"}}
</div>

</div>

<script>
(function() {
  var snapshots = {{.SnapshotsJSON}};
  if (!snapshots || snapshots.length === 0) return;

  var times = snapshots.map(function(s) {
    var t = new Date(s.Timestamp);
    return t.getHours().toString().padStart(2,'0') + ':' +
           t.getMinutes().toString().padStart(2,'0') + ':' +
           t.getSeconds().toString().padStart(2,'0');
  });

  function createChart(domId, option) {
    var dom = document.getElementById(domId);
    if (!dom) return;
    var chart = echarts.init(dom);
    chart.setOption(option);
    window.addEventListener('resize', function() { chart.resize(); });
  }

  // 注册延迟趋势
  createChart('chartRegLatency', {
    tooltip: { trigger: 'axis' },
    legend: { data: ['平均注册延迟(ms)'] },
    grid: { left: 60, right: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { rotate: 30 } },
    yAxis: { type: 'value', name: '延迟(ms)' },
    series: [{
      name: '平均注册延迟(ms)',
      type: 'line',
      data: snapshots.map(function(s) { return s.avgRegLatencyMs.toFixed(1); }),
      smooth: true,
      areaStyle: { opacity: 0.15 },
      itemStyle: { color: '#1a237e' }
    }]
  });

  // 心跳延迟趋势
  createChart('chartHbLatency', {
    tooltip: { trigger: 'axis' },
    legend: { data: ['平均心跳延迟(ms)'] },
    grid: { left: 60, right: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { rotate: 30 } },
    yAxis: { type: 'value', name: '延迟(ms)' },
    series: [{
      name: '平均心跳延迟(ms)',
      type: 'line',
      data: snapshots.map(function(s) { return s.avgHbLatencyMs.toFixed(1); }),
      smooth: true,
      areaStyle: { opacity: 0.15 },
      itemStyle: { color: '#2e7d32' }
    }]
  });

  // 吞吐量趋势
  createChart('chartThroughput', {
    tooltip: { trigger: 'axis' },
    legend: { data: ['吞吐量(msg/s)'] },
    grid: { left: 60, right: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { rotate: 30 } },
    yAxis: { type: 'value', name: 'msg/s' },
    series: [{
      name: '吞吐量(msg/s)',
      type: 'bar',
      data: snapshots.map(function(s) { return s.throughput.toFixed(1); }),
      itemStyle: { color: '#283593' }
    }]
  });

  // 在线站点数
  createChart('chartOnlineCount', {
    tooltip: { trigger: 'axis' },
    legend: { data: ['在线站点数'] },
    grid: { left: 60, right: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { rotate: 30 } },
    yAxis: { type: 'value', name: '站点数' },
    series: [{
      name: '在线站点数',
      type: 'line',
      data: snapshots.map(function(s) { return s.onlineCount; }),
      smooth: true,
      areaStyle: { opacity: 0.2 },
      itemStyle: { color: '#00838f' }
    }]
  });

  // 成功率趋势
  createChart('chartSuccessRate', {
    tooltip: { trigger: 'axis' },
    legend: { data: ['注册成功率(%)', '心跳成功率(%)'] },
    grid: { left: 60, right: 30, bottom: 40 },
    xAxis: { type: 'category', data: times, axisLabel: { rotate: 30 } },
    yAxis: { type: 'value', name: '%', max: 100 },
    series: [
      {
        name: '注册成功率(%)',
        type: 'line',
        data: snapshots.map(function(s) {
          if (s.registrationTotal === 0) return 0;
          return (s.registrationSuccess / s.registrationTotal * 100).toFixed(1);
        }),
        smooth: true,
        itemStyle: { color: '#1a237e' }
      },
      {
        name: '心跳成功率(%)',
        type: 'line',
        data: snapshots.map(function(s) {
          if (s.heartbeatTotal === 0) return 0;
          return (s.heartbeatSuccess / s.heartbeatTotal * 100).toFixed(1);
        }),
        smooth: true,
        itemStyle: { color: '#2e7d32' }
      }
    ]
  });
})();
</script>
</body>
</html>`
