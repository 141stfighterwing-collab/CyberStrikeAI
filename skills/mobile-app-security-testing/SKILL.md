---
name: mobile-app-security-testing
Description: Professional skills and methodologies for mobile application security testing
version: 1.0.0
---

# Mobile application security testing

## Overview

Mobile application security testing is an important part of ensuring the security of mobile applications. This skill provides methods, tools and best practices for mobile application security testing, covering Android and iOS platforms.

## Test scope

### 1. Application security

**Check items:**
- Code obfuscation
- Decompilation protection
- Debugging protection
- Certificate binding

### 2. Data Security

**Check items:**
- Data encryption
- Key management
- Sensitive data storage
- Data transfer

### 3. Authentication and authorization

**Check items:**
- Authentication mechanism
- Token management
- Biometrics
- Session management

### 4. Communication security

**Check items:**
- TLS/SSL configuration
- Certificate verification
- API security
- Man-in-the-middle attack protection

## Android security testing

### Static analysis

**Using APKTool:**
```bash
#Decompile APK
apktool d app.apk

# View AndroidManifest.xml
cat app/AndroidManifest.xml

# View Smali code
find app/smali -name "*.smali"
```

**Using Jadx:**
```bash
#Decompile APK
jadx -d output app.apk

# View Java source code
find output -name "*.java"
```

**Using MobSF:**
```bash
# Start MobSF
docker run -it -p 8000:8000 opensecurity/mobsf

# Upload APK for analysis
# Visit http://localhost:8000
```

### Dynamic analysis

**Using Frida:**
```javascript
// Hook function
Java.perform(function() {
    var MainActivity = Java.use("com.example.MainActivity");
    MainActivity.onCreate.implementation = function(savedInstanceState) {
        console.log("[*] onCreate called");
        this.onCreate(savedInstanceState);
    };
});
```

**Using Objection:**
```bash
# Start Objection
objection -g com.example.app explore

#Hook function
android hooking watch class_method com.example.MainActivity.onCreate
```

**Using Burp Suite:**
```bash
# Configure proxy
# Android set proxy to point to Burp Suite
#Install Burp certificate
```

### Common vulnerabilities

**Hardcoded key:**
```java
// Unsafe code
String apiKey = "1234567890abcdef";
String password = "admin123";
```

**Unsafe Storage:**
```java
// SharedPreferences stores sensitive data
SharedPreferences prefs = getSharedPreferences("data", MODE_WORLD_READABLE);
prefs.edit().putString("password", password).apply();
```

**Certificate verification bypass:**
```java
// Don't verify certificate
TrustManager[] trustAllCerts = new TrustManager[] {
    new X509TrustManager() {
        public X509Certificate[] getAcceptedIssuers() { return null; }
        public void checkClientTrusted(X509Certificate[] certs, String authType) { }
        public void checkServerTrusted(X509Certificate[] certs, String authType) { }
    }
};
```

## iOS security testing

### Static analysis

**Use class-dump:**
```bash
# Export header file
class-dump app.ipa

# View header files
find app -name "*.h"
```

**Using Hopper:**
```bash
# Use Hopper to disassemble
# Open the app binary file
# Analyze assembly code
```

**Use otool:**
```bash
# View Mach-O information
otool -L app

# View string
strings app | grep -i "password\|key\|secret"
```

### Dynamic analysis

**Using Frida:**
```javascript
// Hook Objective-C method
var className = ObjC.classes.ViewController;
var method = className['- login:password:'];
Interceptor.attach(method.implementation, {
    onEnter: function(args) {
        console.log("[*] Login called");
        console.log("Username: " + ObjC.Object(args[2]).toString());
        console.log("Password: " + ObjC.Object(args[3]).toString());
    }
});
```

**Using Cycript:**
```bash
# Attach to process
cycript -p app

#Execute command
[UIApplication sharedApplication]
```

### Common vulnerabilities

**Hardcoded key:**
```objective-c
// Unsafe code
NSString *apiKey = @"1234567890abcdef";
NSString *password = @"admin123";
```

**Unsafe Storage:**
```objective-c
// Keychain improperly stored
NSUserDefaults *defaults = [NSUserDefaults standardUserDefaults];
[defaults setObject:password forKey:@"password"];
```

**Certificate verification bypass:**
```objective-c
// Don't verify certificate
- (void)connection:(NSURLConnection *)connection 
didReceiveAuthenticationChallenge:(NSURLAuthenticationChallenge *)challenge {
    [challenge.sender useCredential:[NSURLCredential credentialForTrust:challenge.protectionSpace.serverTrust] 
          forAuthenticationChallenge:challenge];
}
```

## Tool usage

### MobSF

```bash
# Start MobSF
docker run -it -p 8000:8000 opensecurity/mobsf

# Upload application for analysis
# Support Android and iOS
```

### Frida

```bash
# Install Frida
pip install frida-tools

# run script
frida -U -f com.example.app -l script.js
```

### Objection

```bash
# Install Objection
pip install objection

# Start Objection
objection -g com.example.app explore
```

### Burp Suite

**Configure proxy:**
1. Configure Burp Suite listener
2. Mobile device proxy settings
3. Install Burp certificate
4. Interception and analysis of traffic

## Test list

### Application Security
- [ ] Code obfuscation check
- [ ] Decompilation protection
- [ ] debug protection
- [ ] Certificate binding

### Data Security
- [ ] Data encryption check
- [ ] Key Management
- [ ] Sensitive data storage
- [ ] Data transmission security

### Authentication and authorization
- [ ] Authentication mechanism testing
- [ ] Token management
- [ ] Session Management
- [ ] Biometrics

### Communication security
- [ ] TLS/SSL configuration
- [ ] Certificate verification
- [ ] API security testing
- [ ] Man-in-the-middle attack protection

## Common security issues

### 1. Hardcoded keys

**question:**
- API key hardcoded
- Password hardcoded
- Encryption keys are hardcoded

**repair:**
- Use key management services
- Use environment variables
- Use secure storage

### 2. Insecure storage

**question:**
- Store sensitive data in clear text
- Use unsafe storage methods
- Data is not encrypted

**repair:**
- Use encrypted storage
- Use Keychain/Keystore
- Implement data encryption

### 3. Certificate verification bypass

**question:**
- Does not verify SSL certificates
- Accept self-signed certificates
- Certificate pinning is not implemented

**repair:**
- Implement certificate pinning
- Verify certificate chain
- Use system certificate store

### 4. Debugging information leakage

**question:**
- Logs contain sensitive information
- Misinformation leaked
- Debug mode is not disabled

**repair:**
- Remove debugging code
- Limit log output
- Disable debugging in production environment

## Best Practices

### 1. Code security

- Implement code obfuscation
- Disable debugging functionality
- Implement anti-debugging protection
- Use certificate binding

### 2. Data Security

- Encrypt sensitive data
- Use secure storage
- Implement key management
- Restrict data access

### 3. Communication security

- Use TLS/SSL
- Implement certificate pinning
- Verify server certificate
- Use secure API

### 4. Authentication security

- Implement strong authentication
- Security Token Management
- Implement session management
- Use biometrics

## Notes

- Test only in authorized environment
- Comply with laws and regulations
- Pay attention to the differences between platforms
- Protect user privacy