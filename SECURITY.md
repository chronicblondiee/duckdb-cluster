# Security Guide

This document describes the security features available in duckdb-cluster.

## Overview

duckdb-cluster provides comprehensive security features including:

- **Authentication** — JWT tokens and API keys
- **Authorization** — Role-based access control (RBAC)
- **TLS/mTLS** — Secure communication
- **Rate Limiting** — Per-tenant and per-API-key limits

## Quick Start

### Enable Authentication

```yaml
# config.yaml
security:
  authentication:
    enabled: true
    jwt_secret: "your-secret-key-here"  # Generate with: openssl rand -base64 32
    token_expiration: 24h
    allow_anonymous: false
```

### Generate a Token

```bash
curl -X POST http://localhost:8080/admin/auth/token \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "alice",
    "username": "alice",
    "roles": ["admin"],
    "tenant_id": "default"
  }'
```

Response:
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 86400
}
```

### Use the Token

```bash
curl -X POST http://localhost:8080/query \
  -H "Authorization: Bearer <your-token>" \
  -H "Content-Type: application/json" \
  -d '{"sql": "SELECT * FROM users LIMIT 10"}'
```

## Authentication

### JWT Tokens

JWT tokens are signed with HS256 and include:

- User ID
- Username
- Roles (array)
- Tenant ID
- Expiration time

### API Keys

Register an API key for long-lived access:

```bash
curl -X POST http://localhost:8080/admin/auth/apikey \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "service-account",
    "username": "my-service",
    "roles": ["writer"],
    "tenant_id": "tenant-1"
  }'
```

Response:
```json
{
  "api_key": "a1b2c3d4e5f6...",
  "user_id": "service-account",
  "username": "my-service",
  "roles": ["writer"],
  "tenant_id": "tenant-1"
}
```

Use with:
```bash
curl -X POST http://localhost:8080/query \
  -H "Authorization: ApiKey <your-api-key>" \
  -H "Content-Type: application/json" \
  -d '{"sql": "INSERT INTO events VALUES (1, NOW())", "partition_key": "user-123"}'
```

### Revoke an API Key

```bash
curl -X DELETE http://localhost:8080/admin/auth/apikey \
  -H "Content-Type: application/json" \
  -d '{"api_key": "<key-to-revoke>"}'
```

## Authorization (RBAC)

### Predefined Roles

| Role | Permissions | Use Case |
|------|-------------|----------|
| **admin** | All permissions | System administrators |
| **writer** | Read, write, table management | Application servers |
| **reader** | Read, table introspection | Analytics applications |
| **read** | Read only, health checks | Monitoring tools |

### Permissions

```
query:read         - Execute SELECT queries
query:write        - Execute INSERT/UPDATE queries
query:delete       - Execute DELETE queries

admin:shards       - Manage shards
admin:tables       - List and inspect tables
admin:stats        - View cluster statistics
admin:health       - View health status
admin:users        - Manage users and API keys

system:config      - Modify system configuration
system:shutdown    - Shutdown the cluster
```

### Custom Roles

You can define custom roles programmatically:

```go
customRole := &security.Role{
    Name: "analyst",
    Permissions: []security.Permission{
        security.PermissionQueryRead,
        security.PermissionAdminTables,
        security.PermissionAdminStats,
    },
}
authorizer.RegisterRole(customRole)
```

## TLS/mTLS

### Generate Certificates

```bash
# Generate CA
openssl req -x509 -newkey rsa:4096 -days 365 -nodes \
  -keyout ca.key -out ca.crt \
  -subj "/CN=duckdb-cluster-ca"

# Generate server certificate
openssl req -newkey rsa:4096 -nodes \
  -keyout server.key -out server.csr \
  -subj "/CN=duckdb-cluster"

openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out server.crt -days 365

# Generate client certificate (for mTLS)
openssl req -newkey rsa:4096 -nodes \
  -keyout client.key -out client.csr \
  -subj "/CN=duckdb-cluster-client"

openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key \
  -CAcreateserial -out client.crt -days 365
```

### Configure TLS

```yaml
# config.yaml
security:
  tls:
    enabled: true
    cert_file: "/etc/duckdb-cluster/server.crt"
    key_file: "/etc/duckdb-cluster/server.key"
    ca_file: "/etc/duckdb-cluster/ca.crt"
    client_auth: "verify"  # Options: none, require, verify
    server_name: "duckdb-cluster"
```

## Rate Limiting

### Configuration

```yaml
# config.yaml
security:
  rate_limit:
    enabled: true
    requests_per_second: 1000  # 1000 req/s
    burst: 2000                # Allow bursts up to 2000
    per_tenant: true           # Separate limits per tenant
    per_api_key: false         # Or separate limits per API key
```

### Rate Limit Modes

- **Global**: Single rate limit for all requests
- **Per-Tenant**: Separate limit for each tenant (recommended)
- **Per-API-Key**: Separate limit for each API key

### Rate Limit Response

When rate limited, you'll receive:

```
HTTP/2 429 Too Many Requests
Content-Type: text/plain

Too Many Requests: rate limit exceeded for 'tenant:tenant-1'
```

## Best Practices

### 1. Token Management

- **Rotate secrets regularly** — Change JWT secret periodically
- **Use short expiration** — 24 hours or less for tokens
- **Revoke compromised keys** — Immediately revoke compromised API keys

### 2. Role Assignment

- **Principle of least privilege** — Grant minimum required permissions
- **Use service accounts** — Separate API keys for each service
- **Monitor access** — Review user permissions regularly

### 3. TLS Configuration

- **Use mTLS in production** — Verify client certificates
- **Use strong ciphers** — TLS 1.3 is enforced by default
- **Rotate certificates** — Set expiration to 90 days

### 4. Rate Limiting

- **Set realistic limits** — Based on your capacity and usage
- **Use per-tenant limits** — Prevent noisy neighbors
- **Monitor rejections** — Track rate limit errors

## Security Checklist

Before deploying to production:

- [ ] Enable authentication (`security.authentication.enabled: true`)
- [ ] Generate strong JWT secret (32+ bytes)
- [ ] Set `allow_anonymous: false`
- [ ] Enable TLS for gRPC communication
- [ ] Use mTLS with client verification
- [ ] Enable rate limiting
- [ ] Assign appropriate roles to users
- [ ] Rotate secrets and certificates regularly
- [ ] Monitor authentication failures
- [ ] Set up audit logging (future feature)

## Troubleshooting

### "Unauthorized" Errors

1. Check if authentication is enabled
2. Verify token/API key is valid
3. Ensure `Authorization` header is correct format
4. Check token expiration

### "Forbidden" Errors

1. Verify user has required permissions
2. Check user roles are correct
3. Ensure endpoint matches permission requirements

### "Too Many Requests" Errors

1. Check rate limit configuration
2. Verify requests are within limits
3. Consider increasing limits or implementing backoff
4. Check if tenant/API key is correct

### TLS Connection Errors

1. Verify certificates are valid and not expired
2. Check CA certificate is correct
3. Ensure server name matches certificate
4. Verify client certificate (for mTLS)

## API Reference

### POST /admin/auth/token

Generate a JWT token.

**Request:**
```json
{
  "user_id": "alice",
  "username": "alice",
  "roles": ["admin"],
  "tenant_id": "default"
}
```

**Response:**
```json
{
  "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
  "expires_in": 86400
}
```

### POST /admin/auth/apikey

Register an API key.

**Request:**
```json
{
  "user_id": "service-1",
  "username": "my-service",
  "roles": ["writer"],
  "tenant_id": "tenant-1"
}
```

**Response:**
```json
{
  "api_key": "a1b2c3d4e5f6...",
  "user_id": "service-1",
  "username": "my-service",
  "roles": ["writer"],
  "tenant_id": "tenant-1"
}
```

### DELETE /admin/auth/apikey

Revoke an API key.

**Request:**
```json
{
  "api_key": "a1b2c3d4e5f6..."
}
```

**Response:** `204 No Content`

## Further Reading

- [Configuration Reference](./README.md#configuration)
- [Checkpoint 007](./checkpoint/CHECKPOINT_007.md) — Security implementation details
- [JWT RFC 7519](https://datatracker.ietf.org/doc/html/rfc7519)
- [TLS 1.3 RFC 8446](https://datatracker.ietf.org/doc/html/rfc8446)
