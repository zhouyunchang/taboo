# taboo Python SDK

`packages/sdk-python/taboo.py` 只用标准库，方法与 Go SDK 对齐：登录、TOTP、refresh、机器身份、OAuth client credentials、文件夹、密钥读写、版本、回滚、导出、删除、`.env` 导入、引用展开和扫描报告。

```python
from taboo import Client

client = Client("https://taboo.example")
client.with_identity(client_id, client_secret)
value = client.get_secret(project_id, "dev", "/", "DB_URL", resolve=True)
```

`resolve=True` 展开 `${env:KEY}` 与 `${project:env:KEY}`。`import_env(..., preview=True)` 不写库。`create_scan_report` 提交规则、相对路径、行号和命中原文；服务端计算组织 HMAC 后不保存原文。

把本目录加入 `PYTHONPATH` 后 `import taboo`。`test_consumer.py` 由 `scripts/test-consumers.sh` 执行；未设置 `TABOO_TEST_CONSUMER=1` 时直接跳过。
