---
name: secure-code-review
Description: Professional skills and methodologies for secure code review
version: 1.0.0
---

# Security code review

## Overview

Security code reviews are an important way to identify security vulnerabilities in your code. This skill provides methods, tools, and best practices for secure code review.

## Scope of review

### 1. Input verification

**Check items:**
- User input validation
- Parameter validation
- Data filtering
- Boundary checking

### 2. Output encoding

**Check items:**
- XSS protection
- Output encoding
- Content security policy
- Response header settings

### 3. Authentication and authorization

**Check items:**
- Authentication mechanism
- Session management
- Permission control
- Password handling

### 4. Encryption and keys

**Check items:**
- Data encryption
- Key management
- Hash algorithm
- Random number generation

## Review method

### 1. Static analysis

**Using SAST Tools:**
```bash
# SonarQube
sonar-scanner

# Checkmarx
# Use the web interface

# Fortify
sourceanalyzer -b project build.sh
sourceanalyzer -b project -scan

# Semgrep
semgrep --config=auto .
```

### 2. Manual review

**Review Checklist:**
- [ ] input validation
- [ ] output encoding
- [ ] SQL injection
- [ ] XSS vulnerability
- [ ] Authentication and authorization
- [ ] encryption used
- [ ] error handling
- [ ] logging

### 3. Code pattern recognition

**Dangerous function:**
```python
# Python dangerous functions
eval()
exec()
pickle.loads()
os.system()
subprocess.call()
```

```java
// Java dangerous functions
Runtime.exec()
ProcessBuilder()
Class.forName()
```

```php
// PHP dangerous functions
eval()
exec()
system()
passthru()
```

## Common vulnerability patterns

### SQL injection

**Danger code:**
```java
String query = "SELECT * FROM users WHERE id = " + userId;
Statement stmt = connection.createStatement();
ResultSet rs = stmt.executeQuery(query);
```

**Security Code:**
```java
String query = "SELECT * FROM users WHERE id = ?";
PreparedStatement stmt = connection.prepareStatement(query);
stmt.setInt(1, userId);
ResultSet rs = stmt.executeQuery();
```

### XSS vulnerability

**Danger code:**
```javascript
document.innerHTML = userInput;
element.innerHTML = "<div>" + userInput + "</div>";
```

**Security Code:**
```javascript
element.textContent = userInput;
element.setAttribute("data-value", userInput);
// Or use encoding library
element.innerHTML = escapeHtml(userInput);
```

### Command injection

**Danger code:**
```python
import os
os.system("ping " + user_input)
```

**Security Code:**
```python
import subprocess
subprocess.run(["ping", "-c", "1", validated_input])
```

### Path traversal

**Danger code:**
```java
String filePath = "/uploads/" + fileName;
File file = new File(filePath);
```

**Security Code:**
```java
String basePath = "/uploads/";
String fileName = Paths.get(fileName).getFileName().toString();
String filePath = basePath + fileName;
File file = new File(filePath);
if (!file.getCanonicalPath().startsWith(basePath)) {
    throw new SecurityException("Invalid path");
}
```

### Hardcoded key

**Danger code:**
```java
String apiKey = "1234567890abcdef";
String password = "admin123";
```

**Security Code:**
```java
String apiKey = System.getenv("API_KEY");
String password = keyStore.getPassword("db_password");
```

## Tool usage

### SonarQube

```bash
# Start SonarQube
docker run -d -p 9000:9000 sonarqube

# Run scan
sonar-scanner \
  -Dsonar.projectKey=myproject \
  -Dsonar.sources=. \
  -Dsonar.host.url=http://localhost:9000
```

### Semgrep

```bash
# Install
pip install semgrep

# Run scan
semgrep --config=auto .

# Usage rules
semgrep --config=p/security-audit .
```

### CodeQL

```bash
#Create database
codeql database create database --language=java --source-root=.

# Run query
codeql database analyze database security-and-quality.qls --format=sarif-latest
```

## Review Checklist

### Input validation
- [ ] All user input is validated
- [ ] Use whitelist verification
- [ ] Validate data type and range
- [ ] handles special characters

### Output encoding
- [ ] HTML output encoding
- [ ] URL encoding
- [ ] JavaScript encoding
- [ ] SQL parameterization

### Authentication and authorization
- [ ] Strong password policy
- [ ] Secure session management
- [ ] Permission verification
- [ ] Multi-factor authentication

### Encryption
- [ ] Use strong encryption algorithms
- [ ] Key secure storage
- [ ] transmission encryption
- [ ] Storage Encryption

### Error handling
- [ ] Do not disclose sensitive information
- [ ] Unified error response
- [ ] Log errors
- [ ] Exception handling

## Best Practices

### 1. Secure Coding Standards

- Follow OWASP Top 10
- Use secure coding guidelines
- Code review process
- Safety training

### 2. Automation tools

- Integrated SAST tools
- CI/CD security check
- Automated scanning
- Result analysis

### 3. Code review process

- Peer review
- Reviewed by security experts
- Regular review
- Log issues

## Notes

- Combine tools and human review
- Pay attention to business logic vulnerabilities
- Regularly update tool rules
- Build a safe coding culture