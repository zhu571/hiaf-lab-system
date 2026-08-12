// benchmark/login.js
// 场景 1：登录。argon2id(time=3, memory=64MB) 校验较重，VU=5 防 IP 限流；
// 本地基准将 LOGIN_RATE_LIMIT_IP_MAX 置 0（文档记录该测试条件）。
import http from 'k6/http';
import { USERS, BASE, idemKey } from './common.js';

export const options = {
  vus: 5,
  duration: '30s',
  thresholds: {
    http_req_failed: ['rate<0.01'],
    // argon2id 固有成本较高，阈值放宽到 2s，仅兜底严重回归
    http_req_duration: ['p(95)<2000'],
  },
};

export default function () {
  const user = USERS[__VU % USERS.length];
  const res = http.post(`${BASE}/api/v1/auth/login`, JSON.stringify({
    username: user.username,
    password: user.password,
  }), {
    headers: { 'Content-Type': 'application/json', 'Idempotency-Key': idemKey() },
    tags: { name: 'login' },
  });
  if (res.status !== 200) {
    console.error(`login failed: ${res.status} ${res.body.slice(0, 200)}`);
  }
}
