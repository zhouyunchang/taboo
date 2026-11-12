# taboo Node SDK

`@taboo/sdk`（`packages/sdk-node`）与 Go SDK 使用同一组方法：登录、TOTP、refresh、机器身份、OAuth client credentials、文件夹、密钥读写、版本、回滚、导出、删除、`.env` 导入、引用展开和扫描报告。运行时只依赖 Node 22 标准库，不生成第二套客户端。

```js
import { Client } from '@taboo/sdk';

const client = new Client('https://taboo.example');
client.withIdentity(clientID, clientSecret);
const value = await client.getSecret(projectID, '/', 'DB_URL', { resolve: true });
```

`resolve: true` 展开 `${env:KEY}` 与 `${project:env:KEY}`。`importEnv` 的 `preview` 为 true 时不写库；已有 key 需要 `skip` 或 `overwrite`。`createScanReport` 提交规则、相对路径、行号和命中原文；服务端计算组织 HMAC 后不保存原文。

`consumer.test.js` 在 `scripts/test-consumers.sh` 里和 Go、Python 一起跑。单独执行且未设置 `TABOO_TEST_CONSUMER=1` 时会跳过。
