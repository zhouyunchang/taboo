// taboo scan —— Git 提交前密钥泄漏扫描（M3 #6，设计文档 §2）
// 规则库 150+ 条，分级 critical / warning；支持 .tabooignore、行内 taboo:ignore、
// baseline（.taboo-baseline.json）抑制存量误报；--staged 只扫暂存区；--json 机器可读输出。
import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { execFileSync } from 'node:child_process';

// ---------- 规则库 ----------
// level: critical（命中即失败）/ warning（提示）；entropy=true 时对捕获组做香农熵校验
const R = (id, level, re, desc, opts = {}) => ({ id, level, re, desc, ...opts });

export const RULES = [
  // ===== 云厂商 =====
  R('aws-access-key-id', 'critical', /\b(?:AKIA|ASIA|ABIA|ACCA|AGPA|AIDA|AIPA|ANPA|ANVA|AROA)[0-9A-Z]{16}\b/, 'AWS Access Key ID'),
  R('aws-secret-access-key', 'critical', /aws_secret_access_key\s*[:=]\s*['"]?([0-9A-Za-z/+]{40})['"]?/i, 'AWS Secret Access Key', { group: 1 }),
  R('aws-session-token', 'critical', /aws_session_token\s*[:=]\s*['"]?([A-Za-z0-9/+=]{100,})['"]?/i, 'AWS Session Token', { group: 1 }),
  R('aws-mws-key', 'critical', /\bamzn\.mws\.[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b/, 'Amazon MWS Auth Token'),
  R('google-api-key', 'critical', /\bAIza[0-9A-Za-z_-]{35}\b/, 'Google API Key'),
  R('google-oauth-access-token', 'critical', /\bya29\.[0-9A-Za-z_-]+\b/, 'Google OAuth Access Token'),
  R('google-service-account-json', 'critical', /"type"\s*:\s*"service_account"/, 'GCP Service Account JSON'),
  R('azure-storage-account-key', 'critical', /AccountKey=[0-9A-Za-z+/=]{40,}/i, 'Azure Storage Account Key'),
  R('azure-connection-string', 'critical', /(?:DefaultEndpointsProtocol|BlobEndpoint|EndpointSuffix)=.*?(?:SharedAccessKey|AccountKey)=/i, 'Azure Connection String with Key'),
  R('azure-ad-client-secret', 'critical', /client_secret\s*[:=]\s*['"]?([0-9A-Za-z~_-]{30,})['"]?/i, 'Azure AD Client Secret', { group: 1, entropy: true }),
  R('alibaba-access-key-id', 'critical', /\bLTAI[0-9A-Za-z]{12,20}\b/, 'Alibaba Cloud AccessKey ID'),
  R('alibaba-secret-assignment', 'critical', /(?:alibaba|aliyun|ali_cloud)[\w-]*secret\s*[:=]\s*['"]?([0-9A-Za-z/+]{30,})['"]?/i, 'Alibaba Cloud Secret', { group: 1, entropy: true }),
  R('tencent-cloud-key', 'critical', /(?:tencent|qcloud|secretid)[\w-]*(?:secret|key)\s*[:=]\s*['"]?([0-9A-Za-z/+]{30,})['"]?/i, 'Tencent Cloud Secret', { group: 1, entropy: true }),
  R('ibm-cloud-api-key', 'critical', /ibm[_-]?cloud[_-]?(?:api[_-]?key)?\s*[:=]\s*['"]?([0-9A-Za-z_-]{20,})['"]?/i, 'IBM Cloud API Key', { group: 1, entropy: true }),
  R('oracle-cloud-key', 'critical', /oracle[\w-]*(?:api[_-]?key|secret)\s*[:=]\s*['"]?([0-9A-Za-z/+]{30,})['"]?/i, 'Oracle Cloud Credential', { group: 1, entropy: true }),
  R('digitalocean-token', 'critical', /\b(?:dop_v1|doo_v1|dor_v1)_[a-f0-9]{64}\b/, 'DigitalOcean Token'),
  R('digitalocean-access-key', 'critical', /digitalocean[\w-]*(?:token|key)\s*[:=]\s*['"]?([0-9A-Za-z_-]{30,})['"]?/i, 'DigitalOcean Credential', { group: 1, entropy: true }),
  R('cloudflare-api-token', 'critical', /cloudflare[\w-]*(?:token|key)\s*[:=]\s*['"]?([0-9A-Za-z_-]{30,})['"]?/i, 'Cloudflare API Token', { group: 1, entropy: true }),
  R('cloudflare-global-api-key', 'critical', /cf[_-]?api[_-]?key\s*[:=]\s*['"]?([0-9a-f]{37})['"]?/i, 'Cloudflare Global API Key', { group: 1 }),
  // ===== 代码托管 / CI =====
  R('github-pat', 'critical', /\bghp_[0-9A-Za-z]{36}\b/, 'GitHub Personal Access Token'),
  R('github-oauth-token', 'critical', /\bgho_[0-9A-Za-z]{36}\b/, 'GitHub OAuth Token'),
  R('github-user-to-server', 'critical', /\bghu_[0-9A-Za-z]{36}\b/, 'GitHub User-to-Server Token'),
  R('github-server-to-server', 'critical', /\bghs_[0-9A-Za-z]{36}\b/, 'GitHub Server-to-Server Token'),
  R('github-refresh-token', 'critical', /\bghr_[0-9A-Za-z]{36}\b/, 'GitHub Refresh Token'),
  R('github-fine-grained-pat', 'critical', /\bgithub_pat_[0-9A-Za-z_]{82}\b/, 'GitHub Fine-Grained PAT'),
  R('gitlab-pat', 'critical', /\bglpat-[0-9A-Za-z_-]{20}\b/, 'GitLab Personal Access Token'),
  R('gitlab-runner-token', 'critical', /\b(?:glrt-|GR1348941)[0-9A-Za-z_-]{20,}\b/, 'GitLab Runner Token'),
  R('gitlab-trigger-token', 'critical', /trigger[_-]?token\s*[:=]\s*['"]?([0-9a-f]{32})['"]?/i, 'GitLab Trigger Token', { group: 1 }),
  R('bitbucket-token', 'critical', /\bATCTT3xFfGN0[0-9A-Za-z_-]{20,}\b/, 'Bitbucket Access Token'),
  R('npm-access-token', 'critical', /\bnpm_[0-9A-Za-z]{36}\b/, 'npm Access Token'),
  R('npmrc-auth-token', 'critical', /_authToken\s*=\s*([0-9A-Za-z_-]{30,})/, '.npmrc _authToken', { group: 1 }),
  R('pypi-api-token', 'critical', /\bpypi-[0-9A-Za-z_-]{30,}\b/, 'PyPI API Token'),
  R('rubygems-api-key', 'critical', /\brubygems_[0-9a-f]{48}\b/, 'RubyGems API Key'),
  R('codecov-token', 'critical', /codecov[\w-]*(?:token|upload)[\w-]*\s*[:=]\s*['"]?([0-9a-f-]{36})['"]?/i, 'Codecov Token', { group: 1 }),
  R('snyk-token', 'critical', /snyk[_-]?(?:api[_-]?)?token\s*[:=]\s*['"]?([0-9a-f-]{36})['"]?/i, 'Snyk Token', { group: 1 }),
  R('sonarcloud-token', 'critical', /\bsqu_[0-9a-f]{40}\b/, 'SonarCloud Token'),
  R('circleci-token', 'critical', /circle[_-]?token\s*[:=]\s*['"]?([0-9a-f]{40})['"]?/i, 'CircleCI Token', { group: 1 }),
  R('travis-token', 'critical', /travis[\w-]*token\s*[:=]\s*['"]?([0-9A-Za-z_-]{20,})['"]?/i, 'Travis CI Token', { group: 1, entropy: true }),
  R('heroku-api-key', 'critical', /heroku[\w-]*(?:api[_-]?key|token)\s*[:=]\s*['"]?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})['"]?/i, 'Heroku API Key', { group: 1 }),
  R('docker-registry-auth', 'critical', /"auth"\s*:\s*"([0-9A-Za-z+/=]{20,})"/, 'Docker Registry Auth', { group: 1, entropy: true }),
  R('docker-hub-token', 'critical', /docker[_-]?(?:hub[_-]?)?(?:token|password)\s*[:=]\s*['"]?([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})['"]?/i, 'Docker Hub Token', { group: 1 }),
  // ===== 即时通讯 =====
  R('slack-bot-token', 'critical', /\bxox[baprs]-[0-9A-Za-z-]{10,}\b/, 'Slack Bot/User Token'),
  R('slack-app-token', 'critical', /\bxapp-[0-9A-Za-z-]{10,}\b/, 'Slack App-Level Token'),
  R('slack-webhook-url', 'critical', /https:\/\/hooks\.slack\.com\/services\/T[0-9A-Z]+\/B[0-9A-Z]+\/[0-9A-Za-z]+/, 'Slack Webhook URL'),
  R('discord-bot-token', 'critical', /\b[MN][A-Za-z0-9]{23}\.[\w-]{6}\.[\w-]{27,}\b/, 'Discord Bot Token'),
  R('discord-webhook-url', 'critical', /https:\/\/(?:canary\.|ptb\.)?discord(?:app)?\.com\/api\/webhooks\/\d+\/[\w-]+/, 'Discord Webhook URL'),
  R('telegram-bot-token', 'critical', /\b\d{8,10}:[0-9A-Za-z_-]{35}\b/, 'Telegram Bot Token'),
  R('teams-webhook-url', 'critical', /https:\/\/[0-9a-z-]+\.webhook\.office\.com\/webhookb2\/[\w@-]+/i, 'Microsoft Teams Webhook URL'),
  // ===== AI / ML =====
  R('openai-api-key', 'critical', /\bsk-[0-9A-Za-z_-]{20,}\b/, 'OpenAI API Key'),
  R('openai-project-key', 'critical', /\bsk-proj-[0-9A-Za-z_-]{20,}\b/, 'OpenAI Project API Key'),
  R('anthropic-api-key', 'critical', /\bsk-ant-[0-9A-Za-z_-]{20,}\b/, 'Anthropic API Key'),
  R('huggingface-token', 'critical', /\bhf_[0-9A-Za-z]{30,}\b/, 'Hugging Face Access Token'),
  R('cohere-api-key', 'critical', /cohere[\w-]*(?:api[_-]?key|token)\s*[:=]\s*['"]?([0-9A-Za-z]{30,})['"]?/i, 'Cohere API Key', { group: 1, entropy: true }),
  R('replicate-token', 'critical', /\br8_[0-9A-Za-z]{30,}\b/, 'Replicate API Token'),
  R('stability-ai-key', 'critical', /stability[\w-]*(?:api[_-]?key|token)\s*[:=]\s*['"]?([0-9A-Za-z]{30,})['"]?/i, 'Stability AI Key', { group: 1, entropy: true }),
  R('azure-openai-key', 'critical', /(?:azure|openai)[\w-]*(?:api[_-]?key|key)\s*[:=]\s*['"]?([0-9a-f]{32})['"]?/i, 'Azure OpenAI Key', { group: 1 }),
  // ===== 支付 =====
  R('stripe-secret-key', 'critical', /\b[sr]k_live_[0-9A-Za-z]{24}\b/, 'Stripe Secret/Restricted Key'),
  R('stripe-webhook-secret', 'critical', /\bwhsec_[0-9A-Za-z]{32}\b/, 'Stripe Webhook Signing Secret'),
  R('stripe-publishable-key', 'warning', /\bpk_live_[0-9A-Za-z]{24}\b/, 'Stripe Publishable Key（可公开，确认无误报）'),
  R('paypal-client-secret', 'critical', /paypal[\w-]*client[_-]?secret\s*[:=]\s*['"]?([A-Za-z0-9_-]{30,})['"]?/i, 'PayPal Client Secret', { group: 1, entropy: true }),
  R('square-access-token', 'critical', /\bsq0at-[0-9A-Za-z_-]{22,}\b/, 'Square Access Token'),
  R('square-client-secret', 'critical', /\bsq0csp-[0-9A-Za-z_-]{43}\b/, 'Square Client Secret'),
  R('shopify-access-token', 'critical', /\bshpat_[0-9A-Za-z_-]{32}\b/, 'Shopify Access Token'),
  R('shopify-shared-secret', 'critical', /\bshpss_[0-9A-Za-z_-]{32}\b/, 'Shopify Shared Secret'),
  R('shopify-custom-app', 'critical', /\bshpca_[0-9A-Za-z_-]{32}\b/, 'Shopify Custom App Access Token'),
  R('shopify-private-app', 'critical', /\bshppa_[0-9A-Za-z_-]{32}\b/, 'Shopify Private App Access Token'),
  R('razorpay-secret', 'critical', /razorpay[\w-]*secret\s*[:=]\s*['"]?([0-9A-Za-z]{20,})['"]?/i, 'Razorpay Secret', { group: 1, entropy: true }),
  R('braintree-key', 'critical', /braintree[\w-]*(?:private[_-]?key|token)\s*[:=]\s*['"]?([0-9a-f]{16,})['"]?/i, 'Braintree Private Key', { group: 1 }),
  // ===== 消息 / 邮件 API =====
  R('sendgrid-api-key', 'critical', /\bSG\.[0-9A-Za-z_-]{22}\.[0-9A-Za-z_-]{43}\b/, 'SendGrid API Key'),
  R('twilio-api-secret', 'critical', /\bSK[0-9a-f]{32}\b/, 'Twilio API Secret'),
  R('twilio-auth-token', 'critical', /twilio[\w-]*(?:auth[_-]?token|token)\s*[:=]\s*['"]?([0-9a-f]{32})['"]?/i, 'Twilio Auth Token', { group: 1 }),
  R('mailgun-api-key', 'critical', /\bkey-[0-9a-zA-Z]{32}\b/, 'Mailgun API Key'),
  R('mailchimp-api-key', 'critical', /\b[0-9a-f]{32}-us\d{1,2}\b/, 'Mailchimp API Key'),
  R('postmark-server-token', 'critical', /\bPMAT-[0-9a-f]{40}\b/, 'Postmark Server Token'),
  R('resend-api-key', 'critical', /\bre_[0-9A-Za-z]{20,}\b/, 'Resend API Key'),
  R('brevo-api-key', 'critical', /brevo[\w-]*(?:api[_-]?key|token)\s*[:=]\s*['"]?([0-9A-Za-z]{30,})['"]?/i, 'Brevo API Key', { group: 1, entropy: true }),
  R('sparkpost-api-key', 'critical', /sparkpost[\w-]*(?:api[_-]?key|token)\s*[:=]\s*['"]?([0-9A-Za-z]{30,})['"]?/i, 'SparkPost API Key', { group: 1, entropy: true }),
  // ===== 监控 / SaaS =====
  R('sentry-auth-token', 'critical', /\bsntryu_[0-9a-f]{64}\b/, 'Sentry User Auth Token'),
  R('sentry-dsn-with-secret', 'critical', /https:\/\/[0-9a-f]{32}@[a-z0-9]+\.(?:ingest\.)?sentry\.io\//, 'Sentry DSN with Secret'),
  R('newrelic-api-key', 'critical', /\bNRAK-[0-9A-Z]{32}\b/, 'New Relic API Key'),
  R('newrelic-license-key', 'critical', /\b[a-f0-9]{40}NRAL\b/, 'New Relic License Key'),
  R('datadog-api-key', 'critical', /datadog[\w-]*(?:api[_-]?key|app[_-]?key)\s*[:=]\s*['"]?([0-9a-f]{32})['"]?/i, 'Datadog API/App Key', { group: 1 }),
  R('pagerduty-token', 'critical', /pagerduty[\w-]*token\s*[:=]\s*['"]?([0-9A-Za-z_+=-]{20,})['"]?/i, 'PagerDuty Token', { group: 1, entropy: true }),
  R('grafana-service-account', 'critical', /\bglc_[A-Za-z0-9+/=_-]{100,}\b/, 'Grafana Service Account Token'),
  R('grafana-api-key', 'critical', /grafana[\w-]*(?:api[_-]?key|token)\s*[:=]\s*['"]?(eyJ[A-Za-z0-9_-]{10,})['"]?/i, 'Grafana API Key', { group: 1 }),
  R('vercel-token', 'critical', /vercel[\w-]*(?:token|api[_-]?key)\s*[:=]\s*['"]?([0-9A-Za-z]{20,})['"]?/i, 'Vercel Token', { group: 1, entropy: true }),
  R('netlify-token', 'critical', /netlify[\w-]*(?:token|auth)\s*[:=]\s*['"]?([0-9A-Za-z_-]{40,})['"]?/i, 'Netlify Token', { group: 1, entropy: true }),
  R('supabase-secret', 'critical', /\bsb_secret_[a-z0-9]{20,}\b/, 'Supabase Secret Key'),
  R('supabase-service-key', 'critical', /supabase[\w-]*(?:service[_-]?(?:role[_-]?)?key|service[_-]?key)\s*[:=]\s*['"]?([0-9A-Za-z_-]{30,})['"]?/i, 'Supabase Service Role Key', { group: 1, entropy: true }),
  R('planetscale-token', 'critical', /\bpscale_oauth_[0-9A-Za-z_-]{20,}\b/, 'PlanetScale OAuth Token'),
  R('neon-api-key', 'critical', /\bnapi_[0-9A-Za-z_-]{20,}\b/, 'Neon API Key'),
  R('upstash-credential', 'critical', /upstash[\w-]*(?:token|credential)\s*[:=]\s*['"]?([0-9A-Za-z_-]{20,})['"]?/i, 'Upstash Credential', { group: 1, entropy: true }),
  R('railway-token', 'critical', /railway[\w-]*token\s*[:=]\s*['"]?([0-9a-f-]{36})['"]?/i, 'Railway Token', { group: 1 }),
  R('render-api-key', 'critical', /render[\w-]*api[_-]?key\s*[:=]\s*['"]?([0-9A-Za-z_-]{30,})['"]?/i, 'Render API Key', { group: 1, entropy: true }),
  R('algolia-api-key', 'critical', /algolia[\w-]*(?:api[_-]?key|search[_-]?key|admin[_-]?key)\s*[:=]\s*['"]?([0-9a-f]{32})['"]?/i, 'Algolia API Key', { group: 1 }),
  R('mapbox-secret-token', 'critical', /\bsk\.eyJ1Ijoi[\w.-]+\.[\w.-]+/, 'Mapbox Secret Token'),
  R('pusher-app-secret', 'critical', /pusher[\w-]*(?:app[_-]?)?secret\s*[:=]\s*['"]?([0-9a-f]{32})['"]?/i, 'Pusher App Secret', { group: 1 }),
  R('airtable-pat', 'critical', /\bpat[0-9A-Za-z]{14,}\b/, 'Airtable Personal Access Token'),
  R('notion-integration-token', 'critical', /\bsecret_[0-9A-Za-z]{43}\b/, 'Notion Integration Token'),
  R('notion-token-v2', 'critical', /\bv2_[0-9A-Za-z_-]{43,}\b/, 'Notion v2 Token'),
  R('linear-api-token', 'critical', /\blin_api_[0-9A-Za-z]{20,}\b/, 'Linear API Token'),
  R('dropbox-access-token', 'critical', /\bsl\.[0-9A-Za-z_-]{20,}\.[0-9A-Za-z_-]{20,}\b/, 'Dropbox Short-Lived Access Token'),
  R('asana-pat', 'critical', /asana[\w-]*(?:token|pat)\s*[:=]\s*['"]?([0-9]\.[0-9A-Za-z_-]{20,})['"]?/i, 'Asana PAT', { group: 1 }),
  R('trello-token', 'critical', /trello[\w-]*token\s*[:=]\s*['"]?([0-9a-f]{64})['"]?/i, 'Trello Token', { group: 1 }),
  R('jira-api-token', 'critical', /jira[\w-]*(?:api[_-]?token|token)\s*[:=]\s*['"]?([0-9A-Za-z]{20,})['"]?/i, 'Jira API Token', { group: 1, entropy: true }),
  R('zendesk-api-token', 'critical', /zendesk[\w-]*(?:api[_-]?token|token)\s*[:=]\s*['"]?([0-9A-Za-z]{20,})['"]?/i, 'Zendesk API Token', { group: 1, entropy: true }),
  R('intercom-token', 'critical', /intercom[\w-]*(?:token|secret)\s*[:=]\s*['"]?([0-9A-Za-z_-]{20,})['"]?/i, 'Intercom Token', { group: 1, entropy: true }),
  R('hubspot-api-key', 'critical', /hubspot[\w-]*api[_-]?key\s*[:=]\s*['"]?([0-9a-f-]{36})['"]?/i, 'HubSpot API Key', { group: 1 }),
  R('salesforce-token', 'critical', /salesforce[\w-]*(?:token|secret)\s*[:=]\s*['"]?([0-9A-Za-z._-]{20,})['"]?/i, 'Salesforce Token', { group: 1, entropy: true }),
  R('docusign-secret', 'critical', /docusign[\w-]*(?:secret|rsa[_-]?private[_-]?key)\s*[:=]\s*['"]?([0-9A-Za-z/+_-]{20,})['"]?/i, 'DocuSign Secret', { group: 1, entropy: true }),
  R('okta-api-token', 'critical', /\b00[a-zA-Z0-9_-]{40}\b/, 'Okta API Token'),
  R('auth0-client-secret', 'critical', /auth0[\w-]*client[_-]?secret\s*[:=]\s*['"]?([0-9A-Za-z_-]{40,})['"]?/i, 'Auth0 Client Secret', { group: 1, entropy: true }),
  R('vault-token', 'critical', /\bhvs\.[0-9A-Za-z_-]{24,}\b/, 'HashiCorp Vault Service Token'),
  R('hcp-api-key', 'critical', /\b[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}\b.{0,40}hcp/i, 'HCP API Key Pair'),
  R('tailscale-api-key', 'critical', /tailscale[\w-]*api[_-]?key\s*[:=]\s*['"]?(tskey-[0-9A-Za-z_-]{20,})['"]?/i, 'Tailscale API Key', { group: 1 }),
  R('wireguard-private-key', 'critical', /(?:private[_-]?key|wireguard)[\w-]*\s*[:=]\s*['"]?([A-Za-z0-9+/]{43}=)['"]?/i, 'WireGuard Private Key', { group: 1 }),
  R('age-secret-key', 'critical', /\bAGE-SECRET-KEY-1[0-9A-Z]{20,}\b/, 'age Secret Key'),
  // ===== 私钥 / 证书 =====
  R('private-key-generic', 'critical', /-----BEGIN (?:RSA |EC |DSA |OPENSSH |ENCRYPTED |PGP )?PRIVATE KEY(?: BLOCK)?-----/, 'Private Key Block'), // taboo:ignore
  R('rsa-private-key', 'critical', /-----BEGIN RSA PRIVATE KEY-----/, 'RSA Private Key'), // taboo:ignore
  R('ec-private-key', 'critical', /-----BEGIN EC PRIVATE KEY-----/, 'EC Private Key'), // taboo:ignore
  R('openssh-private-key', 'critical', /-----BEGIN OPENSSH PRIVATE KEY-----/, 'OpenSSH Private Key'), // taboo:ignore
  R('encrypted-private-key', 'critical', /-----BEGIN ENCRYPTED PRIVATE KEY-----/, 'Encrypted Private Key'), // taboo:ignore
  R('pgp-private-key', 'critical', /-----BEGIN PGP PRIVATE KEY BLOCK-----/, 'PGP Private Key'), // taboo:ignore
  R('putty-private-key', 'critical', /PuTTY-User-Key-File-2:/, 'PuTTY Private Key'), // taboo:ignore
  R('pem-key-assignment', 'critical', /(?:private[_-]?key|secret[_-]?key|rsa[_-]?key)\s*[:=]\s*['"]?-----BEGIN/, 'PEM Key in Assignment'),
  // ===== 认证材料 =====
  R('jwt', 'warning', /\beyJ[A-Za-z0-9_-]{10,}\.eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b/, 'JSON Web Token（确认是否敏感）'),
  R('bearer-token-header', 'warning', /Authorization:\s*Bearer\s+([0-9A-Za-z._~+/=-]{20,})/i, 'Hardcoded Bearer Token', { group: 1 }),
  R('basic-auth-header', 'critical', /Authorization:\s*Basic\s+([0-9A-Za-z+/]{20,}={0,2})/i, 'Hardcoded Basic Auth', { group: 1, entropy: true }),
  R('x-api-key-header', 'warning', /[xX]-[aA][pP][iI]-[kK][eE][yY]\s*[:=]\s*['"]?([0-9A-Za-z_-]{16,})['"]?/, 'X-API-Key Header Value', { group: 1, entropy: true }),
  R('oauth-client-secret', 'critical', /client_secret\s*[:=]\s*['"]([0-9A-Za-z._~-]{16,})['"]/, 'OAuth Client Secret', { group: 1, entropy: true }),
  R('cookie-secret-assignment', 'critical', /(?:cookie|session)[_-]?secret\s*[:=]\s*['"]?([0-9A-Za-z/_+=-]{16,})['"]?/i, 'Cookie/Session Secret', { group: 1, entropy: true }),
  R('webhook-secret-assignment', 'critical', /webhook[\w-]*secret\s*[:=]\s*['"]?([0-9A-Za-z/_+=-]{16,})['"]?/i, 'Webhook Signing Secret', { group: 1, entropy: true }),
  R('signing-key-assignment', 'critical', /signing[_-]?(?:key|secret)\s*[:=]\s*['"]?([0-9A-Za-z/_+=-]{16,})['"]?/i, 'Signing Key', { group: 1, entropy: true }),
  R('encryption-key-assignment', 'critical', /encryption[_-]?key\s*[:=]\s*['"]?([0-9A-Za-z/+_=-]{16,})['"]?/i, 'Encryption Key', { group: 1, entropy: true }),
  R('smtp-password', 'critical', /smtp[\w-]*password\s*[:=]\s*['"]?([^\s'"]{8,})['"]?/i, 'SMTP Password', { group: 1 }),
  R('db-connection-string', 'critical', /\b(?:postgres(?:ql)?|mysql|mariadb|mongodb(?:\+srv)?|redis|rediss|amqp|amqps):\/\/[^\s:/'"]+:[^\s@/'"]+@/, 'DB Connection String with Password'),
  R('url-embedded-credential', 'warning', /\b[a-z][a-z0-9+.-]*:\/\/[^\s:/'"]+:[^\s@/'"]+@[\w.-]+/i, 'URL with Embedded Credentials'),
  R('netrc-credential', 'critical', /machine\s+[\w.-]+\s+login\s+\S+\s+password\s+\S+/, '.netrc Credential'),
  R('htpasswd-entry', 'critical', /^[^#\s:]+:(?:\$apr1\$|\$2y\$|\$5\$|\$6\$)[^\s:]+/, 'htpasswd Hash Entry'),
  R('ansible-vault', 'critical', /\$ANSIBLE_VAULT;1\.[12];AES256/, 'Ansible Vault Data (unencrypted inline?)'),
  R('kubernetes-dockerconfig-auth', 'critical', /\.dockerconfigjson:\s*[A-Za-z0-9+/=]{20,}/, 'K8s dockerconfigjson Secret'),
  R('keystore-password', 'critical', /(?:keystore|truststore)[\w-]*password\s*[:=]\s*['"]?([^\s'"]{6,})['"]?/i, 'Keystore Password', { group: 1 }),
  R('basic-credential-env', 'critical', /(?:CREDENTIALS|LOGIN|USER)_?PASSWORD\s*[:=]\s*['"]([^'"]{8,})['"]/i, 'Hardcoded *_PASSWORD', { group: 1 }),
  // ===== 通用高熵赋值（最后一道网，全部熵校验） =====
  R('generic-api-key', 'critical', /\b(?:api[_-]?key|apikey|api_secret)\b\s*[:=]\s*['"]([0-9A-Za-z/+_.~=-]{16,})['"]/i, 'Generic API Key Assignment', { group: 1, entropy: true }),
  R('generic-secret-assignment', 'critical', /\b(?:secret|app_secret|client[_-]?secret|consumer[_-]?secret)\b\s*[:=]\s*['"]([0-9A-Za-z/+_.~=-]{16,})['"]/i, 'Generic Secret Assignment', { group: 1, entropy: true }),
  R('generic-access-token', 'critical', /\b(?:access[_-]?token|auth[_-]?token|refresh[_-]?token|id[_-]?token|bearer)\b\s*[:=]\s*['"]([0-9A-Za-z/+_.~=-]{20,})['"]/i, 'Generic Token Assignment', { group: 1, entropy: true }),
  R('generic-password-assignment', 'critical', /\b(?:password|passwd|pwd)\b\s*[:=]\s*['"]([0-9A-Za-z/+_.~=-]{12,})['"]/i, 'Generic Password Assignment', { group: 1, entropy: true }),
  R('generic-private-key-material', 'critical', /\b(?:private[_-]?key|secret[_-]?key)\b\s*[:=]\s*['"]([0-9A-Za-z+/=]{32,})['"]/i, 'Generic Key Material Assignment', { group: 1, entropy: true }),
  R('generic-long-base64', 'warning', /['"]([A-Za-z0-9+/]{48,}={0,2})['"]/, 'Long Base64 Literal', { group: 1, entropy: true }),
  R('generic-long-hex', 'warning', /['"]([0-9a-fA-F]{40,})['"]/, 'Long Hex Literal', { group: 1, entropy: true }),
  R('aws-style-raw-secret', 'warning', /['"]([0-9A-Za-z/+]{40})['"]/, 'AWS-Style 40-char Secret', { group: 1, entropy: true }),
];

// ---------- 工具 ----------
const SKIP_DIRS = new Set(['.git', 'node_modules', 'dist', 'build', 'coverage', '.next', '.turbo', 'vendor', '__pycache__', '.venv', 'target']);
const SKIP_EXTS = new Set(['.png', '.jpg', '.jpeg', '.gif', '.webp', '.ico', '.pdf', '.zip', '.gz', '.tar', '.7z', '.rar', '.exe', '.dll', '.so', '.dylib', '.class', '.pyc', '.woff', '.woff2', '.ttf', '.eot', '.mp3', '.mp4', '.mov', '.avi', '.jar', '.war', '.db', '.sqlite', '.sqlite3', '.lockb']);
const MAX_FILE = 1024 * 1024; // 1MB
const PLACEHOLDER = /^(x{3,}|0{3,}|1{3,}|\*{3,}|-{3,}|your[_-]?|example|sample|dummy|test|placeholder|changeme|insert|replace|redacted|none|null|undefined|\$\{|process\.|os\.environ)/i;

function shannon(s) {
  if (!s) return 0;
  const freq = new Map();
  for (const c of s) freq.set(c, (freq.get(c) || 0) + 1);
  let h = 0;
  for (const n of freq.values()) { const p = n / s.length; h -= p * Math.log2(p); }
  return h;
}

function loadIgnorePatterns(root) {
  const f = path.join(root, '.tabooignore');
  const pats = [];
  try {
    for (const line of fs.readFileSync(f, 'utf8').split('\n')) {
      const t = line.trim();
      if (t && !t.startsWith('#')) pats.push(t);
    }
  } catch { /* 无 .tabooignore */ }
  return pats;
}

function ignoredBy(rel, pats) {
  rel = rel.split(path.sep).join('/');
  for (let p of pats) {
    const negate = p.startsWith('!');
    if (negate) p = p.slice(1);
    p = p.replace(/^\/+/, '');
    const hit = p.endsWith('/')
      ? rel.startsWith(p)
      : p.includes('*')
        ? new RegExp('^' + p.split('*').map((x) => x.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')).join('[^/]*') + '$').test(rel)
        : rel === p || rel.startsWith(p + '/');
    if (hit) return !negate;
  }
  return false;
}

function walk(root, pats, out = []) {
  for (const ent of fs.readdirSync(root, { withFileTypes: true })) {
    if (ent.name === '.tabooignore' || ent.name === '.taboo-baseline.json') continue;
    const full = path.join(root, ent.name);
    const rel = path.relative(process.cwd(), full) || ent.name;
    if (ignoredBy(rel, pats)) continue;
    if (ent.isDirectory()) {
      if (!SKIP_DIRS.has(ent.name)) walk(full, pats, out);
    } else if (ent.isFile() && !SKIP_EXTS.has(path.extname(ent.name).toLowerCase())) {
      try { if (fs.statSync(full).size <= MAX_FILE) out.push(full); } catch { /* 忽略不可读 */ }
    }
  }
  return out;
}

const isBinary = (buf) => buf.subarray(0, 8000).includes(0);

function lineHash(ruleId, rel, lineNo, text) {
  return crypto.createHash('sha1').update(`${ruleId}|${rel}|${lineNo}|${text.trim()}`).digest('hex').slice(0, 16);
}

function loadBaseline(root) {
  try { return new Set(JSON.parse(fs.readFileSync(path.join(root, '.taboo-baseline.json'), 'utf8')).map((e) => e.hash)); }
  catch { return new Set(); }
}

// ---------- 扫描核心 ----------
export function scanText(text, rel, { baseline, onBaseline } = {}) {
  const findings = [];
  const lines = text.split('\n');
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (line.includes('taboo:ignore')) continue; // 行内抑制
    for (const rule of RULES) {
      rule.re.lastIndex = 0;
      const m = rule.re.exec(line);
      if (!m) continue;
      const raw = rule.group ? m[rule.group] : m[0];
      if (rule.entropy) {
        const candidate = raw || '';
        if (candidate.length < 12 || shannon(candidate) < 4.0 || PLACEHOLDER.test(candidate)) continue;
      }
      const hash = lineHash(rule.id, rel, i + 1, line);
      const f = { rule: rule.id, level: rule.level, desc: rule.desc, file: rel, line: i + 1, match: (raw || m[0]).slice(0, 80), hash };
      if (baseline?.has(hash)) { onBaseline?.(f); continue; }
      findings.push(f);
    }
  }
  return findings;
}

export function stagedFiles() {
  try {
    const out = execFileSync('git', ['diff', '--cached', '--name-only', '--diff-filter=ACM'], { encoding: 'utf8' });
    return out.split('\n').map((s) => s.trim()).filter(Boolean);
  } catch { return []; }
}

export function stagedContent(file) {
  try { return execFileSync('git', ['show', `:${file}`], { encoding: 'utf8', maxBuffer: 8 * 1024 * 1024 }); }
  catch { try { return fs.readFileSync(file, 'utf8'); } catch { return ''; } }
}

// ---------- CLI 入口 ----------
export function runScan(cliArgs) {
  const json = cliArgs.includes('--json');
  const staged = cliArgs.includes('--staged') || cliArgs.includes('--pre-commit');
  const updateBaseline = cliArgs.includes('--update-baseline');
  if (cliArgs.includes('--install-hook')) return installHook();
  if (cliArgs.includes('--list-rules')) {
    for (const r of RULES) console.log(`${r.level.padEnd(8)} ${r.id}`);
    console.log(`\ntotal: ${RULES.length} rules`);
    return 0;
  }

  const root = process.cwd();
  const pats = loadIgnorePatterns(root);
  const baseline = updateBaseline ? new Set() : loadBaseline(root);
  let suppressed = 0;
  const onBaseline = () => { suppressed++; };
  const findings = [];
  const scanned = [];

  if (staged) {
    for (const f of stagedFiles()) {
      if (ignoredBy(f, pats) || SKIP_EXTS.has(path.extname(f).toLowerCase())) continue;
      scanned.push(f);
      findings.push(...scanText(stagedContent(f), f, { baseline, onBaseline }));
    }
  } else {
    for (const full of walk(root, pats)) {
      const rel = path.relative(root, full).split(path.sep).join('/');
      let buf;
      try { buf = fs.readFileSync(full); } catch { continue; }
      if (isBinary(buf)) continue;
      scanned.push(rel);
      findings.push(...scanText(buf.toString('utf8'), rel, { baseline, onBaseline }));
    }
  }

  findings.sort((a, b) => (a.level === b.level ? a.file.localeCompare(b.file) : a.level === 'critical' ? -1 : 1));

  if (updateBaseline) {
    const entries = findings.map((f) => ({ hash: f.hash, rule: f.rule, file: f.file, line: f.line }));
    fs.writeFileSync(path.join(root, '.taboo-baseline.json'), JSON.stringify(entries, null, 2) + '\n');
    console.log(`baseline updated: ${entries.length} finding(s) suppressed → .taboo-baseline.json`);
    return 0;
  }

  if (json) {
    console.log(JSON.stringify({ tool: 'taboo-scan', version: 1, rules: RULES.length, scanned: scanned.length, suppressed, findings }, null, 2));
  } else if (findings.length === 0) {
    console.log(`taboo scan ✓ clean — ${scanned.length} file(s) scanned, ${RULES.length} rules, ${suppressed} baseline-suppressed`);
  } else {
    console.log(`taboo scan ✗ ${findings.length} potential secret(s) in ${scanned.length} scanned file(s)${suppressed ? ` (${suppressed} baseline-suppressed)` : ''}\n`);
    let last = null;
    for (const f of findings) {
      if (f.file !== last) { console.log(`  ${f.file}`); last = f.file; }
      const mark = f.level === 'critical' ? '✗' : '⚠';
      console.log(`    ${mark} ${f.line}:${f.rule} — ${f.desc}`);
      console.log(`      ${f.match}`);
    }
    console.log('\n抑制误报：行尾加注释 `# taboo:ignore`，或写入 .tabooignore / .taboo-baseline.json');
  }
  return findings.length > 0 ? 1 : 0;
}

function installHook() {
  const dir = path.join(process.cwd(), '.git', 'hooks');
  if (!fs.existsSync(dir)) { console.error('error: not a git repository'); return 1; }
  const hook = path.join(dir, 'pre-commit');
  const script = `#!/bin/sh
# taboo scan —— 提交前密钥泄漏扫描
taboo scan --pre-commit || {
  echo "taboo: 发现疑似密钥，提交被拦截。确认误报请加 # taboo:ignore 或更新 baseline"
  exit 1
}
`;
  fs.writeFileSync(hook, script, { mode: 0o755 });
  console.log(`pre-commit hook installed → ${hook}`);
  return 0;
}
