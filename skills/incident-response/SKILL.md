---
name: incident-response
Description: Professional skills and methodologies for security incident response
version: 1.0.0
---

# Security incident response

## Overview

Security incident response is a key process for handling security incidents. This skill provides methods, tools, and best practices for security incident response.

## Response process

### 1. Preparation stage

**Preparation:**
- Build a response team
- Develop a response plan
- Prepare tools and resources
- Establish communication channels

### 2. Identification stage

**Identification event:**
- Monitor alarms
- Anomaly detection
- Log analysis
- User reports

### 3. Containment Phase

**Containment measures:**
- Isolate affected systems
- Disable account
- Block network connection
- Backup evidence

### 4. Clearing phase

**Remove threats:**
- Remove malware
- Bug fixes
- Reset credentials
- Clean the backdoor

### 5. Recovery Phase

**Restore system:**
- Restore backup
- Verify system integrity
- Monitoring system
- Gradual restoration of services

### 6. Summary stage

**Summary experience:**
- incident reporting
- Lessons learned
- Improvement measures
- Update process

## Tool usage

### Log analysis

**Using Splunk:**
```bash
# Search logs
index=security event_type="failed_login"

# Statistical analysis
index=security | stats count by src_ip

# Time series analysis
index=security | timechart count by event_type
```

**Use ELK:**
```bash
# Elasticsearch query
GET /logs/_search
{
  "query": {
    "match": {
      "event_type": "malware"
    }
  }
}
```

### Forensic Tools

**Use Volatility:**
```bash
#Analyze memory image
volatility -f memory.dump imageinfo

# List processes
volatility -f memory.dump --profile=Win7SP1x64 pslist

#Extract process memory
volatility -f memory.dump --profile=Win7SP1x64 memdump -p 1234 -D output/
```

**Using Autopsy:**
```bash
# Start Autopsy
#Create case
# Add evidence
# Analyze data
```

### Network Analysis

**Using Wireshark:**
```bash
# Capture traffic
wireshark -i eth0

#Analyze PCAP files
wireshark -r capture.pcap

# Filter traffic
# Display filter: ip.addr == 192.168.1.100
# Capture filter: host 192.168.1.100
```

**Use tcpdump:**
```bash
# Capture traffic
tcpdump -i eth0 -w capture.pcap

# Analyze traffic
tcpdump -r capture.pcap -A
```

## Event type

### Malware

**Response steps:**
1. Isolate the affected system
2. Collect samples
3. Analyze malware
4. Eliminate threats
5. Fix bugs

**tool:**
- VirusTotal
- Cuckoo Sandbox
- YARA rules

### Data Breach

**Response steps:**
1. Confirm the scope of leakage
2. Contain leakage
3. Assess the impact
4. Notify relevant parties
5. Fix bugs

**Check items:**
- Amount of data leaked
- Affected users
-Leakage channels
- Data sensitivity

### Denial of service

**Response steps:**
1. Confirm the attack type
2. Enable protective measures
3. Filter malicious traffic
4. Monitor system status
5. Restore normal service

**Protective Measures:**
- DDoS protection service
- Traffic cleaning
- Current limiting measures
- CDN protection

### Unauthorized access

**Response steps:**
1. Disable affected accounts
2. Reset credentials
3. Check access logs
4. Evaluate data access
5. Fix bugs

**Check items:**
- Access time
- Access content
- Visit source
- Data modification

## Response list

### Preparation stage
- [ ] Establish response team
- [ ] Develop a response plan
- [ ] Preparation Tools
- [ ] Establish communication channels

### Identification stage
- [ ] Confirm event
- [ ] Collect information
- [ ] Assess impact
- [ ] Recording timeline

### Containment Phase
- [ ] Isolation system
- [ ] Disable account
- [ ] Block connection
- [ ] Backup evidence

### Clearing phase
- [ ] Remove threats
- [ ] Bug fixes
- [ ] Reset credentials
- [ ] Verification Clear

### Recovery phase
- [ ] Restore system
- [ ] Verify integrity
- [ ] Monitoring system
- [ ] Restoration of service

### Summary stage
- [ ] Prepare report
- [ ] Summarize experience
- [ ] Improvement measures
- [ ] Update process

## Best Practices

### 1. Preparation

- Build a response team
- Develop a response plan
- Regular drills
- Preparation tools

### 2. Response

- Quick response
- Systematic processing
- Record all operations
- Protect evidence

### 3. Communication

- Internal communication
- External notifications
- Status updates
- After-action report

### 4. Improvements

- Event analysis
- Process improvement
- Tool updates
- Training and improvement

## Notes

- Quick response
- Protect evidence
- Record operations
- Comply with laws and regulations