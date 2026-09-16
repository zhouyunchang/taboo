// 信封加密（Envelope Encryption）三层结构 —— 对齐设计文档 §3.2
//   Root Key (KEK, 主密钥)  <- 文件 / 环境变量（KMS 适配层为 v1 后续迭代）
//     └─ Org Data Key (DEK, 每组织一把)  <- 加密后存 DB，使用时惰性解密入内存
//        └─ Secret Value (AES-256-GCM)  <- DB 只存密文 + nonce
// 算法：AES-256-GCM（认证加密），nonce 12 字节随机，绝不复用。
// 注意：本 MVP 用 Node.js 标准库实现以快速验证设计；Go 版将改用 Argon2id（密码哈希）
// 与 google/tink 风格封装，设计契约不变。
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';

const KEY_LEN = 32;
const NONCE_LEN = 12;

export function loadMasterKey(dataDir) {
  // 1) 环境变量优先：TABOO_MASTER_KEY，支持 base64 或 hex（32 字节）
  const env = process.env.TABOO_MASTER_KEY;
  if (env) {
    const buf = /^[0-9a-f]{64}$/i.test(env)
      ? Buffer.from(env, 'hex')
      : Buffer.from(env, 'base64');
    if (buf.length !== KEY_LEN) throw new Error('TABOO_MASTER_KEY must be 32 bytes (hex or base64)');
    return buf;
  }
  // 2) 本地文件模式：首次启动自动生成（0600 权限），对标 Vault 的 unseal key 简化版
  fs.mkdirSync(dataDir, { recursive: true });
  const keyFile = path.join(dataDir, 'master.key');
  if (!fs.existsSync(keyFile)) {
    const key = crypto.randomBytes(KEY_LEN);
    fs.writeFileSync(keyFile, key.toString('hex'), { mode: 0o600 });
    console.log(`[taboo] master key generated at ${keyFile} (keep it safe, back it up)`);
  }
  const raw = fs.readFileSync(keyFile, 'utf8').trim();
  const buf = Buffer.from(raw, 'hex');
  if (buf.length !== KEY_LEN) throw new Error(`invalid master key file: ${keyFile}`);
  return buf;
}

export function generateDEK() {
  return crypto.randomBytes(KEY_LEN);
}

// ---- AES-256-GCM 加解密（KEK 包 DEK、DEK 包明文，共用同一原语） ----

export function encrypt(masterKey, plaintext) {
  const nonce = crypto.randomBytes(NONCE_LEN);
  const cipher = crypto.createCipheriv('aes-256-gcm', masterKey, nonce);
  const ct = Buffer.concat([cipher.update(plaintext, 'utf8'), cipher.final()]);
  const tag = cipher.getAuthTag();
  // 存储格式: nonce | tag | ciphertext  (base64)
  return Buffer.concat([nonce, tag, ct]).toString('base64');
}

export function decrypt(masterKey, payload) {
  const buf = Buffer.from(payload, 'base64');
  const nonce = buf.subarray(0, NONCE_LEN);
  const tag = buf.subarray(NONCE_LEN, NONCE_LEN + 16);
  const ct = buf.subarray(NONCE_LEN + 16);
  const decipher = crypto.createDecipheriv('aes-256-gcm', masterKey, nonce);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(ct), decipher.final()]).toString('utf8');
}

// ---- 组织 DEK 惰性缓存 ----
const dekCache = new Map(); // orgId -> Buffer(32)

export function getOrgDEK(masterKey, org) {
  let dek = dekCache.get(org.id);
  if (!dek) {
    dek = Buffer.from(decrypt(masterKey, org.dek_encrypted), 'hex');
    dekCache.set(org.id, dek);
  }
  return dek;
}

// ---- 密码哈希：MVP 用内置 scrypt；Go 版迁移至 Argon2id(m=64MB,t=3,p=4) ----

const SCRYPT_PARAMS = { N: 16384, r: 8, p: 1 }; // MVP 参数，生产环境上调

export function hashPassword(password) {
  const salt = crypto.randomBytes(16);
  const hash = crypto.scryptSync(password, salt, 64, SCRYPT_PARAMS);
  return `scrypt$${SCRYPT_PARAMS.N}$${SCRYPT_PARAMS.r}$${SCRYPT_PARAMS.p}$${salt.toString('hex')}$${hash.toString('hex')}`;
}

export function verifyPassword(password, stored) {
  const [algo, N, r, p, saltHex, hashHex] = String(stored).split('$');
  if (algo !== 'scrypt') return false;
  const hash = crypto.scryptSync(password, Buffer.from(saltHex, 'hex'), 64, {
    N: Number(N), r: Number(r), p: Number(p),
  });
  return crypto.timingSafeEqual(hash, Buffer.from(hashHex, 'hex'));
}

// ---- JWT (HS256) —— MVP 实现，TTL 15min ----

const b64u = (buf) => Buffer.from(buf).toString('base64url');

export function signJWT(payload, secret, ttlSec = 900) {
  const header = b64u(JSON.stringify({ alg: 'HS256', typ: 'JWT' }));
  const body = b64u(JSON.stringify({ ...payload, exp: Math.floor(Date.now() / 1000) + ttlSec }));
  const sig = crypto.createHmac('sha256', secret).update(`${header}.${body}`).digest('base64url');
  return `${header}.${body}.${sig}`;
}

export function verifyJWT(token, secret) {
  const [header, body, sig] = String(token).split('.');
  if (!header || !body || !sig) return null;
  const expect = crypto.createHmac('sha256', secret).update(`${header}.${body}`).digest();
  const got = Buffer.from(sig, 'base64url');
  if (expect.length !== got.length || !crypto.timingSafeEqual(expect, got)) return null;
  try {
    const payload = JSON.parse(Buffer.from(body, 'base64url').toString());
    if (!payload.exp || payload.exp < Math.floor(Date.now() / 1000)) return null;
    return payload;
  } catch {
    return null;
  }
}

export function sha256(input) {
  return crypto.createHash('sha256').update(input).digest('hex');
}

export const randomId = () => crypto.randomUUID();
