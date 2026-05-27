// ── Constants ────────────────────────────────────────────────
const STATUS_COLORS = {
  running:  '#4caf50',
  serving:  '#2196f3',
  failed:   '#f44336',
  starting: '#ff9800',
  stopped:  '#9e9e9e',
};
const DEFAULT_STATUS_COLOR = '#9e9e9e';

// ── Main ────────────────────────────────────────────────────
document.addEventListener('DOMContentLoaded', () => {
  loadDashboard();
});

/**
 * Fetch project list from /api/list and render the table.
 */
async function loadDashboard() {
  const tbody = document.getElementById('project-rows');
  const countEl = document.getElementById('project-count');
  const sslEl = document.getElementById('ssl-status');

  try {
    const resp = await fetch('/api/list');
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);

    const data = await resp.json();
    const projects = data.projects || [];
    countEl.textContent = projects.length;
    sslEl.textContent = data.ssl_status || '—';

    if (projects.length === 0) {
      tbody.innerHTML = '<tr><td colspan="4" class="empty">暂无部署</td></tr>';
      return;
    }

    tbody.innerHTML = projects.map(p => {
      const color = STATUS_COLORS[p.status] || DEFAULT_STATUS_COLOR;
      return `
        <tr>
          <td><a href="${escapeHtml(p.url)}">${escapeHtml(p.name)}</a></td>
          <td>${escapeHtml(p.type)}</td>
          <td style="color:${color};font-weight:bold">${escapeHtml(p.status)}</td>
          <td><a href="${escapeHtml(p.url)}">${escapeHtml(p.url)}</a></td>
        </tr>`;
    }).join('\n');

  } catch (err) {
    tbody.innerHTML = `<tr><td colspan="4" class="error-row">加载失败: ${escapeHtml(err.message)}</td></tr>`;
    countEl.textContent = '—';
    sslEl.textContent = '—';
  }
}

/**
 * Escape HTML entities to prevent XSS.
 */
function escapeHtml(str) {
  if (!str) return '';
  const div = document.createElement('div');
  div.appendChild(document.createTextNode(str));
  return div.innerHTML;
}
