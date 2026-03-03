---
name: api-security-testing
Description: Professional skills and methodologies for API security testing
version: 1.0.0
---

# API security testing

## Overview

API security testing is an important part of ensuring the security of API interfaces. This skill provides methods, tools and best practices for API security testing.

## Test scope

### 1. Authentication and authorization

**Test items:**
- Token validity verification
- Token expiration processing
- Permission control
- Role permission verification

### 2. Input verification

**Test items:**
- Parameter type verification
- Data length limit
- Special character handling
- SQL injection protection
- XSS protection

### 3. Business logic

**Test items:**
- Workflow verification
- state transition
- Concurrency control
- Business rules

### 4. Error handling

**Test items:**
- Misinformation leaked
- stack trace
- Exposure of sensitive information

## Test method

### 1. API discovery

**Identify API endpoint:**
```bash
# Use directory scanning
gobuster dir -u https://target.com -w api-wordlist.txt

# Passive scanning using Burp Suite
# Browse the application and observe API calls

# Analyze JavaScript files
# Find API endpoint definition
```

### 2. Certification Test

**Token test:**
```http
# Test invalid token
GET /api/user
Authorization: Bearer invalid_token

# Test expired token
GET /api/user
Authorization: Bearer expired_token

# Test without Token
GET /api/user
```

**JWT Test:**
```bash
# Use jwt_tool
python jwt_tool.py <JWT_TOKEN>

# Test algorithm confusion
python jwt_tool.py <JWT_TOKEN> -X a

# Test key brute force cracking
python jwt_tool.py <JWT_TOKEN> -C -d wordlist.txt
```

### 3. Authorization test

**Horizontal permissions:**
```http
#User A accesses user B's resources
GET /api/user/123
Authorization: Bearer user_a_token

# should return 403
```

**Vertical Permissions:**
```http
# Ordinary users access the administrator interface
GET /api/admin/users
Authorization: Bearer user_token

# should return 403
```

### 4. Input validation test

**SQL injection:**
```http
POST /api/search
{
  "query": "test' OR '1'='1"
}
```

**Command injection:**
```http
POST /api/execute
{
  "command": "ping; id"
}
```

**XXE：**
```http
POST /api/parse
Content-Type: application/xml

<?xml version="1.0"?>
<!DOCTYPE foo [<!ENTITY xxe SYSTEM "file:///etc/passwd">]>
<foo>&xxe;</foo>
```

### 5. Rate Limit Test

**Test rate limit:**
```python
import requests

for i in range(1000):
    response = requests.get('https://target.com/api/endpoint')
    print(f"Request {i}: {response.status_code}")
```

## Tool usage

### Postman

**Create test collection:**
1. Import API documentation
2. Set up authentication
3. Create test cases
4. Run automated tests

### Burp Suite

**API Scan:**
1. Configure API endpoints
2. Set up authentication
3. Run an active scan
4. Analyze results

### OWASP ZAP

```bash
# API scan
zap-cli quick-scan --self-contained \
  --start-options '-config api.disablekey=true' \
  http://target.com/api
```

### REST-Attacker

```bash
# Scan OpenAPI specification
rest-attacker scan openapi.yaml
```

## Common vulnerabilities

### 1. Authentication bypass

**Token verification defects:**
- Weak Token generation
- Token is predictable
- Token does not verify signature

### 2. Privilege Elevation

**IDOR：**
- direct object reference
- Resource ownership not verified

### 3. Information leakage

**error message:**
- Detailed error message
- stack trace
- Sensitive data

### 4. Injection vulnerability

**Common injections:**
-SQL injection
- NoSQL injection
- Command injection
- XXE

### 5. Business logic

**Logical flaw:**
- Price operations
- Quantity limit bypass
- Status modification

## Test list

### Certification Test
- [ ] Token validity verification
- [ ] Token expiration processing
- [ ] Weak Token detection
- [ ] Token replay attack

### Authorization test
- [ ] Horizontal permission test
- [ ] Vertical permission test
- [ ] Role permission verification
- [ ] Resource access control

### Input validation
- [ ] SQL injection testing
- [ ] XSS testing
- [ ] command injection test
- [ ] XXE test
- [ ] parameter pollution

### Business logic
- [ ] Workflow verification
- [ ] state transition
- [ ] Concurrency control
- [ ] Business Rules

### Error handling
- [ ] Error information leaked
- [ ] stack trace
- [ ] Exposure of sensitive information

## Protective measures

### Recommended plan

1. **Certification**
- Use strong tokens
- Implement Token refresh
- Verify Token signature

2. **Authorization**
- Role-based access control
- Resource ownership verification
- Principle of least privilege

3. **Input verification**
- Parameter type verification
- Data length limit
- Whitelist verification

4. **Error handling**
- Unified error response
- No details disclosed
- Record error log

5. **Rate Limiting**
- Implement API current limiting
- Prevent brute force cracking
- Monitor abnormal requests

## Notes

- Only conducted in an authorized testing environment
- Avoid impact on API
- Pay attention to the differences between different API versions
- Pay attention to request frequency when testing