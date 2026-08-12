// benchmark/test-data-list.js
// 场景 4：项目测试数据列表（GAS-TARGET 项目，本地库预插 20 条）。
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
  const res = http.get(`${BASE}/api/v1/projects/${PROJECT_ID}/test-data`, {
    headers: { Authorization: `Bearer ${data.access_token}` },
    tags: { name: 'test-data-list' },
  });
  if (res.status !== 200) {
    console.error(`test-data list failed: ${res.status} ${res.body.slice(0, 200)}`);
  }
}
