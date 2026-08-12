// benchmark/daily-report-today.js
// 场景 2：今日日报（幂等获取/创建）。POST 需 Idempotency-Key + CSRF。
// setup 阶段登录一次取 token+csrf，VU=20 复用（JWT 校验成本已含在每请求延迟内）。
import { login, postWithCSRF, readThresholds } from './common.js';

export const options = {
  vus: 20,
  duration: '30s',
  thresholds: readThresholds(),
};

export function setup() {
  return login({ username: 'lisi', password: 'Test1234!' });
}

export default function (data) {
  const res = postWithCSRF('/api/v1/daily-reports/today', data.access_token, data.csrf_token);
  if (res.status !== 200) {
    console.error(`daily-report today failed: ${res.status} ${res.body.slice(0, 200)}`);
  }
}
