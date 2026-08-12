// benchmark/projects-list.js
// 场景 3：项目列表（带统计：member_count / open_issue_count / log_count）。
import http from 'k6/http';
import { login, BASE, readThresholds } from './common.js';

export const options = {
  vus: 20,
  duration: '30s',
  thresholds: readThresholds(),
};

export function setup() {
  return login({ username: 'lisi', password: 'Test1234!' });
}

export default function (data) {
  const res = http.get(`${BASE}/api/v1/projects`, {
    headers: { Authorization: `Bearer ${data.access_token}` },
    tags: { name: 'projects-list' },
  });
  if (res.status !== 200) {
    console.error(`projects list failed: ${res.status} ${res.body.slice(0, 200)}`);
  }
}
