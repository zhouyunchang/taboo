// TOTP 2FA（M2 #4）端到端验证 —— 针对 Go 后端
import crypto from 'node:crypto';
const BASE = process.env.TABOO_BASE || 'http://localhost:7100';
let failures = 0;

async function call(method, path, { token, body } = {}) {
  const res = await fetch(BASE + path, {
    method,
    headers: {
      ...(body ? { 'Content-Type': 'application/json' } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: body ? JSON.stringify(body) : undefined,
  });
  const data = await res.json().catch(() => ({}));
  return { status: res.status, data };
}
const check = (name, cond, extra = '') => {
  if (cond) console.log(`  ✓ ${name}`);
  else { failures++; console.error(`  ✗ ${name} ${extra}`); }
};

// RFC 6238 TOTP（SHA1/30s/6 位），base32 无填充解码
function b32decode(s) {
  const A = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ234567';
  let bits = 0, buf = 0; const out = [];
  for (const ch of s.replace(/=+$/, '')) {
    buf = buf << 5 | A.indexOf(ch); bits += 5;
    if (bits >= 8) { out.push((buf >> (bits - 8)) & 0xff); bits -= 8; }
  }
  return Buffer.from(out);
}
function totp(secret, step = Math.floor(Date.now() / 1000 / 30)) {
  const msg = Buffer.alloc(8);
  msg.writeBigUInt64BE(BigInt(step));
  const h = crypto.createHmac('sha1', b32decode(secret)).update(msg).digest();
  const o = h[h.length - 1] & 0x0f;
  const bin = ((h[o] & 0x7f) << 24 | h[o + 1] << 16 | h[o + 2] << 8 | h[o + 3]) >>> 0;
  return String(bin % 1_000_000).padStart(6, '0');
}

const email = `totp-smoke-${Date.now()}@taboo.dev`;
const password = 'password123';
console.log('== taboo totp smoke ==');

// 1. 注册 + 首次登录（未开 2FA，直接拿 token）
const reg = await call('POST', '/api/v1/auth/register', { body: { email, password } });
check('register 201', reg.status === 201);
const me0 = await call('GET', '/api/v1/me', { token: reg.data.tokens.access });
check('me totp_enabled=false', me0.data.totp_enabled === false);

// 2. setup：secret + otpauth URL + 二维码 PNG
const setup = await call('POST', '/api/v1/auth/totp/setup', { token: reg.data.tokens.access });
check('setup 200', setup.status === 200 && !!setup.data.secret && !!setup.data.otpauth_url,
  JSON.stringify(setup.data).slice(0, 120));
check('qr_png is data:image/png;base64', typeof setup.data.qr_png === 'string' && setup.data.qr_png.startsWith('data:image/png;base64,'));
check('otpauth_url well-formed', /^otpauth:\/\/totp\/taboo:[^?]*\?.*secret=/.test(setup.data.otpauth_url), setup.data.otpauth_url);

// 3. verify：错码 401，对码激活 + 恢复码
const bad = await call('POST', '/api/v1/auth/totp/verify', { token: reg.data.tokens.access, body: { code: '000000' } });
check('verify wrong code 401', bad.status === 401, JSON.stringify(bad.data));
const code1 = totp(setup.data.secret);
const act = await call('POST', '/api/v1/auth/totp/verify', { token: reg.data.tokens.access, body: { code: code1 } });
check('verify activates 200', act.status === 200 && act.data.enabled === true);
check('10 recovery codes', Array.isArray(act.data.recovery_codes) && act.data.recovery_codes.length === 10
  && act.data.recovery_codes.every((c) => /^\w+-\w+-\w+$/.test(c)));
const me1 = await call('GET', '/api/v1/me', { token: reg.data.tokens.access });
check('me totp_enabled=true', me1.data.totp_enabled === true);

// 4. 登录 → totp_required + challenge，无 token
const login1 = await call('POST', '/api/v1/auth/login', { body: { email, password } });
check('login totp_required', login1.status === 200 && login1.data.totp_required === true && !!login1.data.challenge && !login1.data.tokens);
const challenge = login1.data.challenge;

// 5. 二次验证：错码 / 篡改 challenge / 对码
const bad2 = await call('POST', '/api/v1/auth/totp/login', { body: { challenge, code: '999999' } });
check('2fa wrong code 401', bad2.status === 401, JSON.stringify(bad2.data));
const bad3 = await call('POST', '/api/v1/auth/totp/login', { body: { challenge: challenge.slice(0, -2) + 'xx', code: code1 } });
check('tampered challenge 401', bad3.status === 401);
// 激活已消费当前时间窗，下一步登录需用下一个时间窗的码（防重放语义正确）
const code2 = totp(setup.data.secret, Math.floor(Date.now() / 1000 / 30) + 1);
const login2 = await call('POST', '/api/v1/auth/totp/login', { body: { challenge, code: code2 } });
check('2fa login issues tokens', login2.status === 200 && !!login2.data.tokens?.access);

// 6. 防重放：同一窗口内复用刚消费的码 → 401
const replay = await call('POST', '/api/v1/auth/totp/login', { body: { challenge, code: code2 } });
check('replay rejected 401', replay.status === 401, JSON.stringify(replay.data));

// 7. 恢复码登录 + 用后即废
const login3 = await call('POST', '/api/v1/auth/login', { body: { email, password } });
const rc = act.data.recovery_codes[0];
const rcLogin = await call('POST', '/api/v1/auth/totp/login', { body: { challenge: login3.data.challenge, code: rc } });
check('recovery code login 200', rcLogin.status === 200 && !!rcLogin.data.tokens?.access);
const login4 = await call('POST', '/api/v1/auth/login', { body: { email, password } });
const rcReplay = await call('POST', '/api/v1/auth/totp/login', { body: { challenge: login4.data.challenge, code: rc } });
check('recovery code reuse 401', rcReplay.status === 401);

// 8. disable：需密码，错密码 401，对密码关闭
const noauth = await call('POST', '/api/v1/auth/totp/disable', { body: { password } });
check('disable unauth 401', noauth.status === 401);
const wrongPw = await call('POST', '/api/v1/auth/totp/disable', { token: rcLogin.data.tokens.access, body: { password: 'wrong-password' } });
check('disable wrong password 401', wrongPw.status === 401);
const off = await call('POST', '/api/v1/auth/totp/disable', { token: rcLogin.data.tokens.access, body: { password } });
check('disable 200', off.status === 200 && off.data.enabled === false);
const me2 = await call('GET', '/api/v1/me', { token: rcLogin.data.tokens.access });
check('me totp_enabled=false after disable', me2.data.totp_enabled === false);

// 9. 关闭后登录恢复直出 token
const login5 = await call('POST', '/api/v1/auth/login', { body: { email, password } });
check('login direct tokens after disable', login5.status === 200 && !!login5.data.tokens?.access && !login5.data.totp_required);

// 10. setup 未认证 401 + 审计落库
const setupNoauth = await call('POST', '/api/v1/auth/totp/setup');
check('setup unauth 401', setupNoauth.status === 401);
const meFinal = await call('GET', '/api/v1/me', { token: login5.data.tokens.access });
const orgSlug = meFinal.data.orgs[0].slug;
const audit = await call('GET', `/api/v1/orgs/${orgSlug}/audit?limit=200`, { token: login5.data.tokens.access });
const acts = new Set(audit.data.logs.map((l) => l.action));
check('audit has totp enable/disable', acts.has('auth.totp.enable') && acts.has('auth.totp.disable'), [...acts].join(','));

console.log(failures ? `\nFAILED: ${failures} check(s)` : '\nALL TOTP CHECKS PASSED');
process.exit(failures ? 1 : 0);
