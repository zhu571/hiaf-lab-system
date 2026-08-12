// benchmark/issues-list.js
// 场景 5：项目 Issue 列表（GAS-TARGET 项目，8 条种子 issue 中 5 条属该项目）。
import http from 'k6/http';
import { login, BASE, PROJECT_ID, readThresholds } from './common.js';

export const options = {
  vus: 20,
  duration: '30s',
  thresholds: readThresholds(),
};

export function setup() {
  return login({ username: 'lisi', password: 'Test1234!' });
}

export default function (data) {
  const res = http.get(`${BASE}/api/v1/projects/${PROJECT_ID}/issues`, {
    headers: { Authorization: `Bearer ${data.access_token}` },
    tags: { name: 'issues-list' },
  });
  if (res.status !== 200) {
    console.error(`issues list failed: ${res.status} ${res.body.slice(0, 200)}`);
  }
}
