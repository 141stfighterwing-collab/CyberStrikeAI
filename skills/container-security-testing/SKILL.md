---
name: container-security-testing
Description: Professional skills and methodologies for container security testing
version: 1.0.0
---

# Container security testing

## Overview

Container security testing is an important part of ensuring the security of containerized applications. This skill provides methods, tools and best practices for container security testing, covering container technologies such as Docker and Kubernetes.

## Test scope

### 1. Mirror security

**Check items:**
-Basic image vulnerability
- Dependency package vulnerability
- Mirror configuration
- Sensitive information

### 2. Runtime security

**Check items:**
-Container permissions
- Resource limitations
- Network isolation
- file system

### 3. Orchestration security

**Check items:**
- Kubernetes configuration
- Service Account
- RBAC
- Network strategy

## Docker security testing

### Image Scan

**Using Trivy:**
```bash
# Scan images
trivy image nginx:latest

# Scan local mirrors
trivy image --input nginx.tar

# Only show high-risk vulnerabilities
trivy image --severity HIGH,CRITICAL nginx:latest
```

**Use Clair:**
```bash
# Start Clair
docker run -d --name clair clair:latest

# Scan images
clair-scanner --ip 192.168.1.100 nginx:latest
```

**Using Docker Bench:**
```bash
#Run Docker security benchmark
docker run --rm --net host --pid host --userns host --cap-add audit_control \
  -e DOCKER_CONTENT_TRUST=$DOCKER_CONTENT_TRUST \
  -v /etc:/etc:ro \
  -v /usr/bin/containerd:/usr/bin/containerd:ro \
  -v /usr/bin/runc:/usr/bin/runc:ro \
  -v /usr/lib/systemd:/usr/lib/systemd:ro \
  -v /var/lib:/var/lib:ro \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  --label docker_bench_security \
  docker/docker-bench-security
```

### Container configuration check

**Check Dockerfile:**
```dockerfile
# Security question example
FROM ubuntu:latest  # Use the latest tag
RUN apt-get update && apt-get install -y curl  # No version specified
COPY . /app  # May contain sensitive files
ENV PASSWORD=secret  # Hardcoded password
USER root  # Use root user
```

**Security Best Practices:**
```dockerfile
# Use a specific version
FROM ubuntu:20.04

#Specify package version
RUN apt-get update && apt-get install -y curl=7.68.0-1ubuntu2.7

# Use non-root users
RUN useradd -m appuser
USER appuser

# Minimize image
FROM alpine:3.15

#Multi-stage build
FROM golang:1.18 AS builder
WORKDIR /app
COPY . .
RUN go build -o app

FROM alpine:3.15
COPY --from=builder /app/app /app
```

### Runtime check

**Check container permissions:**
```bash
# Check privileged containers
docker ps --filter "label=privileged=true"

# Check the mounted host directory
docker inspect container_name | grep -A 10 Mounts

# Check container network
docker network inspect network_name
```

**Check resource limits:**
```bash
# Check memory limit
docker stats container_name

# Check CPU limits
docker inspect container_name | grep -i cpu
```

## Kubernetes security testing

### Configuration check

**Using kube-bench:**
```bash
# Run kube-bench
kube-bench run

# Check for a specific benchmark
kube-bench run --targets master,node,etcd
```

**Use kube-hunter:**
```bash
# Run kube-hunter
kube-hunter --remote target-ip

# Active mode
kube-hunter --active
```

### Pod security

**Check Pod Security Policy:**
```yaml
# Insecure Pod configuration
apiVersion: v1
kind: Pod
spec:
  containers:
  - name: app
    image: nginx
    securityContext:
      privileged: true  # Privileged mode
      runAsUser: 0  # Root user
```

**Security Configuration:**
```yaml
apiVersion: v1
kind: Pod
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    fsGroup: 2000
  containers:
  - name: app
    image: nginx
    securityContext:
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop:
        - ALL
        add:
        - NET_BIND_SERVICE
```

### RBAC check

**Check role permissions:**
```bash
# List all roles
kubectl get roles --all-namespaces

# Check role bindings
kubectl get rolebindings --all-namespaces

# Check cluster roles
kubectl get clusterroles

# Check user permissions
kubectl auth can-i --list --as=system:serviceaccount:default:sa-name
```

**FAQ:**
- Excessive permissions
- Unused characters
- Unused service accounts

### Network Strategy

**Check network policy:**
```bash
# List all network policies
kubectl get networkpolicies --all-namespaces

# Check network policy configuration
kubectl describe networkpolicy policy-name -n namespace
```

**Network Policy Example:**
```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny
spec:
  podSelector: {}
  policyTypes:
  - Ingress
  - Egress
```

## Tool usage

### Falco

**Runtime Security Monitoring:**
```bash
# Install Falco
helm repo add falcosecurity https://falcosecurity.github.io/charts
helm install falco falcosecurity/falco

# Check rules
falco -r /etc/falco/rules.d/
```

### Aqua Security

```bash
# Scan images
aqua image scan nginx:latest

# Scan Kubernetes cluster
aqua k8s scan
```

### Snyk

```bash
# Scan Dockerfile
snyk test --docker nginx:latest

# Scan Kubernetes configuration
snyk iac test k8s/
```

## Test list

### Mirror security
- [ ] Scan base image for vulnerabilities
- [ ] Scan dependent package vulnerabilities
- [ ] Check Dockerfile configuration
- [ ] Check for sensitive information leakage

### Runtime security
- [ ] Check container permissions
- [ ] Check resource limits
- [ ] Check network isolation
- [ ] Check file system mounts

### Orchestration Security
- [ ] Check Kubernetes configuration
- [ ] Check RBAC configuration
- [ ] Check network policy
- [ ] Check Pod security policy

## Common security issues

### 1. Mirror vulnerability

**question:**
- The base image contains vulnerabilities
- Dependent packages contain vulnerabilities
- Not updated in time

**repair:**
- Scan images regularly
- Update the base image in a timely manner
- Use a minimized image

### 2. Excessive permissions

**question:**
-Container runs as root
- Privileged mode
- Mount sensitive directories

**repair:**
- Use a non-root user
- Disable privileged mode
- Restrict file system access

### 3. Configuration error

**question:**
- Default configuration is unsafe
- Missing network policy
- RBAC configuration error

**repair:**
- Follow security best practices
- Implement network strategy
- Properly configure RBAC

### 4. Sensitive information leakage

**question:**
- The image contains the key
- Environment variables exposed
- Configuration file leaked

**repair:**
- Use key management
- Avoid hardcoding
- Use Secret object

## Best Practices

### 1. Mirror security

- Use official base image
- Update images regularly
- Scan for image vulnerabilities
- Minimize image size

### 2. Runtime security

- Use a non-root user
- Restrict container permissions
- Enforce resource limits
- Enable security context

### 3. Orchestration security

- Configure network policy
- Implement RBAC
- Use Pod security policy
- Enable audit logging

## Notes

- Test only in authorized environment
- Avoid impact on the production environment
- Pay attention to the differences between different container platforms
- Conduct regular security scans