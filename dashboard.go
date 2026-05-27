package main

import (
	"fmt"
	"net/http"
	"strings"
)

func handleDashboard(w http.ResponseWriter, r *http.Request, mgr *Manager) {
	projects := mgr.ListAll()

	var rows strings.Builder
	for _, p := range projects {
		statusColor := map[string]string{
			"running":  "#4caf50",
			"serving":  "#2196f3",
			"failed":   "#f44336",
			"starting": "#ff9800",
			"stopped":  "#9e9e9e",
		}[p.Status]
		if statusColor == "" {
			statusColor = "#9e9e9e"
		}
		rows.WriteString(fmt.Sprintf(
			"<tr><td><a href=\"%s\">%s</a></td><td>%s</td>"+
				"<td style=\"color:%s;font-weight:bold\">%s</td>"+
				"<td><a href=\"%s\">%s</a></td></tr>\n",
			p.URL, p.Name, p.Type, statusColor, p.Status, p.URL, p.URL,
		))
	}

	sslStatus := "未配置"
	if mgr.nginx.HasSSL() {
		sslStatus = "已启用"
	}

	html := fmt.Sprintf(`<!doctype html>
<html lang="zh">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>AI Web Gateway</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { font-family: system-ui, -apple-system, sans-serif; margin: 2rem; background: #0d1117; color: #c9d1d9; }
  h1   { color: #58a6ff; margin-bottom: .5rem; }
  .subtitle { color: #8b949e; font-size: .9rem; margin-bottom: 1.5rem; }
  .tag { display: inline-block; background: #21262d; border-radius: 6px; padding: .1rem .5rem; font-size: .8rem; margin-right: .5rem; }
  table { border-collapse: collapse; width: 100%%; margin-top: 1rem; }
  th, td { padding: .6rem 1rem; border: 1px solid #30363d; text-align: left; font-size: .9rem; }
  th   { background: #161b22; color: #8b949e; font-weight: 600; }
  tr:hover { background: #161b22; }
  a    { color: #58a6ff; text-decoration: none; }
  a:hover { text-decoration: underline; }
  .empty { color: #8b949e; text-align: center; padding: 2rem; }
  .api-section { margin-top: 2rem; padding-top: 1rem; border-top: 1px solid #30363d; }
  .api-section h2 { color: #8b949e; font-size: 1rem; margin-bottom: .5rem; }
  code { background: #21262d; padding: .1rem .3rem; border-radius: 3px; font-size: .85rem; }
</style>
</head>
<body>
<h1>AI Web Gateway</h1>
<p class="subtitle">
  已部署 <strong>%d</strong> 个项目 &nbsp;|&nbsp;
  SSL: <span class="tag">%s</span>
</p>
<table>
<tr><th>项目</th><th>类型</th><th>状态</th><th>访问地址</th></tr>
%s
</table>
<div class="api-section">
  <h2>API 端点</h2>
  <p style="color:#8b949e;font-size:.85rem;line-height:1.8">
    <code>GET  /api/list</code> — 列出所有部署<br>
    <code>POST /api/{project}</code> — 上传 tar.gz 部署<br>
    <code>GET  /api/{project}</code> — 查看状态与日志<br>
    <code>POST /api/{project}/restart</code> — 重启后端<br>
    <code>DELETE /api/{project}</code> — 删除项目<br>
    <code>POST /api/ssl/upload</code> — 上传 SSL 证书<br>
    <code>DELETE /api/ssl</code> — 移除 SSL 证书
  </p>
</div>
</body>
</html>
`, len(projects), sslStatus, rows.String())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(html))
}
