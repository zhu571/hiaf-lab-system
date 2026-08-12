// benchmark/common.js
// 共享工具：基准地址、用户池、登录、CSRF 令牌、幂等键生成。
// 用法：k6 run -e BENCH_BASE_URL=http://127.0.0.1:8000 benchmark/<scenario>.js

import http from 'k6/http';

export const BASE = __ENV.BENCH_BASE_URL || 'http://127.0.0.1:8000';

// 迁移 009 种子用户（本地测试库，密码均为 Test1234!）
export const USERS = [
  { username: 'haofan', password: 'Test1234!' },
  { username: 'lisi', password: 'Test1234!' },
  { username: 'zhangsan', password: 'Test1234!' },
  { username: 'wangwu', password: 'Test1234!' },
  { username: 'zhaoliu', password: 'Test1234!' },
];

// 种子项目（迁移 009）：低温气体靶（active，lisi 为 member）
export const PROJECT_ID = 'b0000000-0000-4000-8000-000000000001';

// 登录并返回 { access_token, csrf_token }
export function login(user) {
  const res = httpPost('/api/v1/auth/login', { username: user.username, password: user.password });
  checkLogin(res, user.username);
  const body = res.json().data;
  return { access_token: body.access_token, csrf_token: body.csrf_token };
}

// 唯一幂等键（写场景必须，复用 409 duplicate_idempotency_key）
export function idemKey() {
  return `bench_${Date.now()}_${__VU}_${__ITER}_${Math.random().toString(36).slice(2, 10)}`;
}

// 携带 CSRF 的 POST（daily-reports/today 等用户写路径强校验 CSRF）
export function postWithCSRF(path, token, csrf, body) {
  const headers = {
    Authorization: `Bearer ${token}`,
    'Content-Type': 'application/json',
    'Idempotency-Key': idemKey(),
  };
  if (csrf) {
    headers['X-CSRF-Token'] = csrf;
    headers.Cookie = `csrf_token=${csrf}`;
  }
  return http.post(`${BASE}${path}`, body ? JSON.stringify(body) : undefined, { headers });
}

// 读场景公共阈值：p95 < 500ms，错误率 < 1%
export function readThresholds() {
  return {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500'],
  };
}

function httpPost(path, body) {
  return http.post(`${BASE}${path}`, JSON.stringify(body), {
    headers: { 'Content-Type': 'application/json' },
  });
}

function checkLogin(res, username) {
  if (res.status !== 200) {
    throw new Error(`login failed for ${username}: ${res.status} ${res.body}`);
  }
}
