<div align="center">
  <img src="web/static/logo.png" alt="CyberStrikeAI Logo" width="200">
</div>

# CyberStrikeAI

[Chinese](README_CN.md) | [English](README.md)

CyberStrikeAI is an **AI-native security testing platform** built on Go that integrates 100+ security tools, intelligent orchestration engines, role-based testing and preset security testing roles, Skills system and professional testing skills, as well as complete test life cycle management capabilities. Through the native MCP protocol and AI agents, it supports full-process automation from dialogue instructions to vulnerability discovery, attack chain analysis, knowledge retrieval and result visualization, providing security teams with an auditable, traceable, and collaborative professional testing environment.

## Interface and integration preview
<div align="center">

### System Dashboard Overview
<img src="./images/dashboard.png" alt="System Dashboard" width="100%">
*The dashboard provides a comprehensive overview of system operating status, security vulnerabilities, tool usage and knowledge base, helping users quickly understand the core functions and current status of the platform. *
### Core Function Overview
<table>
<tr>
<td width="33.33%" align="center">
<strong>Web Console</strong><br/><img src="./images/web-console.png" alt="Web Console" width="100%"></td>
<td width="33.33%" align="center">
<strong>Visualization of attack chains</strong><br/><img src="./images/attack-chain.png" alt="Attack Chain" width="100%"></td>
<td width="33.33%" align="center">
<strong>Task Management</strong><br/><img src="./images/task-management.png" alt="Task Management" width="100%"></td>
</tr>
<tr>
<td width="33.33%" align="center">
<strong>Vulnerability Management</strong><br/><img src="./images/vulnerability-management.png" alt="Vulnerability Management" width="100%"></td>
<td width="33.33%" align="center">
<strong>MCP Management</strong><br/><img src="./images/mcp-management.png" alt="MCP Management" width="100%"></td>
<td width="33.33%" align="center">
<strong>MCP stdio mode</strong><br/><img src="./images/mcp-stdio2.png" alt="MCP stdio mode" width="100%"></td>
</tr>
<tr>
<td width="33.33%" align="center">
<strong>Knowledge Base</strong><br/><img src="./images/knowledge-base.png" alt="Knowledge Base" width="100%"></td>
<td width="33.33%" align="center">
<strong>Skills Management</strong><br/><img src="./images/skills.png" alt="Skills Management" width="100%"></td>
<td width="33.33%" align="center">
<strong>Role Management</strong><br/><img src="./images/role-management.png" alt="Role Management" width="100%"></td>
</tr>
</table>

</div>

## Feature overview
- 🤖 Intelligent decision-making engine compatible with OpenAI/DeepSeek/Claude and other models- 🔌 Native MCP protocol, supports HTTP / stdio / SSE transmission mode and external MCP access- 🧰 100+ ready-made tool templates + YAML extension capabilities- 📄 Large result paging, compression and full-text search- 🔗 Attack chain visualization, risk scoring and step playback- 🔒 Web login protection, audit log, SQLite persistence- 📚 Knowledge base function: vector retrieval and hybrid search, providing security expertise for AI- 📁 Conversation group management: supports group creation, pinning, renaming, deletion and other operations- 🛡️ Vulnerability management function: complete vulnerability CRUD operation, supports severity classification, status transfer, filtering by conversation/severity/status, and statistical dashboard- 📋 Batch task management: Create task queues, add tasks in batches, execute them sequentially, support task editing and status tracking- 🎭 Role-based testing: preset security testing roles (penetration testing, CTF, web application scanning, etc.), supporting custom prompt words and tool restrictions- 🎯 Skills skill system: 20+ preset security testing skills (SQL injection, XSS, API security, etc.), which can be attached to roles or called by AI on demand- 📱 **Robot**: Supports DingTalk and Feishu long connections, and can talk to CyberStrikeAI on the mobile phone (for configuration and commands, please see [Robot usage instructions](docs/robot.md))
## Tool Overview
The system is pre-configured with 100+ penetration/attack and defense tools, covering the complete attack chain:
- **Network Scanning**: nmap, masscan, rustscan, arp-scan, nbtscan- **Web application scanning**: sqlmap, nikto, dirb, gobuster, feroxbuster, ffuf, httpx- **Vulnerability Scanning**: nuclei, wpscan, wafw00f, dalfox, xsser- **Subdomain enumeration**: subfinder, amass, findomain, dnsenum, fierce- **Cyberspace search engine**: fofa_search, zoomeye_search- **API Security**: graphql-scanner, arjun, api-fuzzer, api-schema-analyzer- **Container Security**: trivy, clair, docker-bench-security, kube-bench, kube-hunter- **Cloud Security**: prowler, scout-suite, cloudmapper, pacu, terrascan, checkov- **Binary analysis**: gdb, radare2, ghidra, objdump, strings, binwalk- **Exploits**: metasploit, msfvenom, pwntools, ropper, ropgadget- **Password cracking**: hashcat, john, haspump- **Forensic Analysis**: volatility, volatility3, foreignmost, steghide, exiftool- **Post penetration**: linpeas, winpeas, mimikatz, bloodhound, impacket, responder- **CTF Utilities**: stegsolve, zsteg, hash-identifier, fcrackzip, pdfcrack, cyberchef- **System auxiliary**: exec, create-file, delete-file, list-files, modify-file
## Basic usage
### Get started quickly (one command deployment)
**Environmental requirements:**- Go 1.21+ ([Download and install](https://go.dev/dl/))
- Python 3.10+ ([Download and install](https://www.python.org/downloads/))

**One command deployment:**```bash
git clone https://github.com/Ed1s0nZ/CyberStrikeAI.git
cd CyberStrikeAI-main
chmod +x run.sh && ./run.sh
```

The `run.sh` script will do this automatically:- ✅ Check and verify Go and Python environments- ✅ Create Python virtual environment- ✅ Install Python dependency packages- ✅ Download Go dependency modules- ✅ Compile and build the project- ✅ Start server
**First time configuration:**1. **Configure AI model API** (required before first use)- Visit http://localhost:8080 after startup- Enter `Settings` → fill in the API configuration information:     ```yaml
     openai:
       api_key: "sk-your-key"
       base_url: "https://api.openai.com/v1"  # 或 https://api.deepseek.com/v1
       model: "gpt-4o"  # 或 deepseek-chat, claude-3-opus 等
     ```
- Or edit the `config.yaml` file directly before starting2. **Login to the system** - Use the automatically generated password displayed on the console (or set `auth.password` in `config.yaml`)3. **Install security tools (optional)** - Install the required tools as needed:   ```bash
   # macOS
   brew install nmap sqlmap nuclei httpx gobuster feroxbuster subfinder amass
   # Ubuntu/Debian
   sudo apt-get install nmap sqlmap nuclei httpx gobuster feroxbuster
   ```
Tools that are not installed are automatically skipped or used instead.
**Other startup methods:**```bash
# 直接运行（需手动配置环境）
go run cmd/server/main.go

# 手动编译
go build -o cyberstrike-ai cmd/server/main.go
./cyberstrike-ai
```

**Description:** The Python virtual environment (`venv/`) is automatically created and managed by `run.sh`. Tools that require Python (such as `api-fuzzer`, `http-framework-test`, etc.) automatically use this environment.
### Common processes- **Dialogue Test**: Natural language triggers multi-step tool orchestration, SSE real-time output.- **Personalized Testing**: Choose from preset security testing roles (penetration testing, CTF, web application scanning, API security testing, etc.) to customize the AI ​​behavior and available tools. Custom system prompt words can be applied to each role, and the list of available tools can be limited to achieve focused testing scenarios.- **Tool Monitoring**: View task queue, execution logs, and large file attachments.- **Session History**: All conversations and tool calls are saved in SQLite and can be replayed at any time.- **Conversation Grouping**: Organize conversations into different groups by project or topic, support operations such as pinning, renaming, deletion, etc., and store all data persistently.- **Vulnerability Management**: Create, update and track discovered vulnerabilities during testing. Supports filtering by severity (Critical/High/Medium/Low/Information), status (Pending/Confirmed/Fixed/False Positive) and conversations, view statistics and export findings.- **Batch task management**: Create a task queue, add multiple tasks in batches, edit or delete tasks before execution, and then execute them in sequence. Each task will be executed as an independent conversation, supporting complete status tracking (pending/executing/completed/failed/cancelled) and execution history.- **Visual configuration**: switch models, start and stop tools, set the number of iterations, etc. in the interface.
### Default security measures- The settings panel has built-in required verification to prevent missing API Key/Base URL/model.- When `auth.password` is empty, automatically generate a 24-digit strong password and write it back to `config.yaml`.- All APIs (except login) need to carry Bearer Token, which is intercepted by unified authentication middleware.- Each tool execution comes with timeouts, logging and error isolation.
## Advanced use
### Role-based testing- **Default Roles**: The system has 12+ preset security testing roles built-in (penetration testing, CTF, web application scanning, API security testing, binary analysis, cloud security audit, etc.), located in the `roles/` directory.- **Customized prompt words**: Each role can define `user_prompt`, which will be automatically added before user messages to guide the AI ​​to adopt specific testing methods and focus.- **Tool restrictions**: The role can specify a `tools` list to limit the available tools and implement a focused testing process (for example, the CTF role is limited to CTF-specific tools).- **Skills Integration**: Security testing skills can be attached to characters. The skill name will be added to the system prompt word as a prompt, and the AI ​​agent can obtain the skill content on demand through the `read_skill` tool.- **Easy role creation**: Custom roles can be created by adding YAML files in the `roles/` directory. Each role defines the `name`, `description`, `user_prompt`, `icon`, `tools`, `skills`, `enabled` fields.- **Web Interface Integration**: Select the role through the drop-down menu in the chat interface. Character selection affects AI behavior and available tool suggestions.
**Example of creating a custom role:**1. Create a YAML file (such as `roles/custom-role.yaml`) in the `roles/` directory:   ```yaml
   name: 自定义角色
   description: 专用测试场景
   user_prompt: 你是一个专注于 API 安全的专业安全测试人员...
   icon: "\U0001F4E1"
   tools:
     - api-fuzzer
     - arjun
     - graphql-scanner
   skills:
     - api-security-testing
     - sql-injection-testing
   enabled: true
   ```
2. Restart the service or reload the configuration, and the role will appear in the role selection drop-down menu.
### Skills Skill System- **Default Skills**: The system has built-in 20+ preset security testing skills (SQL injection, XSS, API security, cloud security, container security, etc.), located in the `skills/` directory.- **Skill tips in prompt words**: When a character is selected, the skill name attached to the character will be added to the system prompt word as a recommendation. Skill content will not be automatically injected, and the AI ​​agent must use the `read_skill` tool to obtain skill details when needed.- **On-demand call**: AI agents can also access skills on demand through built-in tools (`list_skills`, `read_skill`), allowing for dynamic acquisition of relevant skills during task execution.- **Structured Format**: Each skill is a directory containing a `SKILL.md` file detailing testing methods, tool usage, best practices and examples. Skills support YAML front matter format for metadata.- **Custom Skills**: Custom skills can be created by adding a directory to the `skills/` directory. Each skill directory should contain a `SKILL.md` file.
**Create custom skills:**1. Create a directory in the `skills/` directory (such as `skills/my-skill/`)2. Create the `SKILL.md` file in this directory and write the skill content3. In the role's YAML file, attach the skill to the role by adding the `skills` field
### Tool orchestration and extension- `tools/*.yaml` defines commands, parameters, prompt words and metadata, and can be hot loaded.- `security.tools_dir` can be enabled in batches by pointing to the directory; inline definition in the main configuration is still supported.- **Large results paging**: Outputs exceeding 200KB will be saved as attachments and can be paginated, filtered, and retrieved by regular expressions through the `query_execution_result` tool.- **Result Compression/Digest**: Multi-megabyte logs can be compressed or summarized before being written to SQLite to reduce file size.
**General steps for customizing tools**1. Copy the existing sample under `tools/` (such as `tools/sample.yaml`).2. Modify basic information such as `name`, `command`, `args`, `short_description`, etc.3. Declare positional parameters or parameters with flags in `parameters[]` to facilitate the automatic assembly of commands by the agent.4. Supplement `description` or `notes` as necessary to give AI additional context or prompts for result interpretation.5. Restart the service or reload the configuration in the interface, and the new tool can be enabled/disabled in the Settings panel.
### Attack chain analysis- The agent parses each conversation and extracts goals, tools, vulnerabilities and causal relationships.- The web side can interactively view link nodes, risk levels and timelines, and supports exporting reports.
### MCP full scene- **Web Mode**: Comes with HTTP MCP service for front-end calling.- **MCP stdio mode**: `go run cmd/mcp-stdio/main.go` can access the Cursor/command line.- **External MCP federation**: Register a third-party MCP (HTTP/stdio/SSE) in the settings, start and stop as needed, and view call statistics and health in real time.
#### MCP stdio quick integration1. **Compile the executable file** (execute in the project root directory):   ```bash
   go build -o cyberstrike-ai-mcp cmd/mcp-stdio/main.go
   ```
2. **Configure in Cursor**Open `Settings → Tools & MCP → Add Custom MCP`, select **Command**, and specify the compiled program and configuration file:   ```json
   {
     "mcpServers": {
       "cyberstrike-ai": {
         "command": "/absolute/path/to/cyberstrike-ai-mcp",
         "args": [
           "--config",
           "/absolute/path/to/config.yaml"
         ]
       }
     }
   }
   ```
Replace the path with your actual local address, and Cursor will automatically start the stdio version of MCP.
#### MCP HTTP Quick Integration1. Confirm `mcp.enabled: true` in `config.yaml` and adjust `mcp.host` / `mcp.port` as needed (local recommendation `127.0.0.1:8081`).2. Start the main service (`./run.sh` or `go run cmd/server/main.go`). The MCP endpoint is exposed at `http://<host>:<port>/mcp` by default.3. In Cursor, `Add Custom MCP → HTTP`, set `Base URL` to `http://127.0.0.1:8081/mcp`.4. You can also create `.cursor/mcp.json` in the project root directory for team sharing:   ```json
   {
     "mcpServers": {
       "cyberstrike-ai-http": {
         "transport": "http",
         "url": "http://127.0.0.1:8081/mcp"
       }
     }
   }
   ```

#### External MCP Federation (HTTP/stdio/SSE)CyberStrikeAI supports connecting to external MCP servers through three transfer modes:- **HTTP Mode** – traditional request/response communication via HTTP POST- **stdio mode** – inter-process communication via standard input/output- **SSE Mode** – Real-time streaming communication via Server-Sent Events
Add an external MCP server:1. Open the web interface and enter **Settings → External MCP**.2. Click **Add External MCP** to provide the configuration in JSON format:
**HTTP mode example:**   ```json
   {
     "my-http-mcp": {
       "transport": "http",
       "url": "http://127.0.0.1:8081/mcp",
       "description": "HTTP MCP 服务器",
       "timeout": 30
     }
   }
   ```

**stdio mode example:**   ```json
   {
     "my-stdio-mcp": {
       "command": "python3",
       "args": ["/path/to/mcp-server.py"],
       "description": "stdio MCP 服务器",
       "timeout": 30
     }
   }
   ```

**SSE mode example:**   ```json
   {
     "my-sse-mcp": {
       "transport": "sse",
       "url": "http://127.0.0.1:8082/sse",
       "description": "SSE MCP 服务器",
       "timeout": 30
     }
   }
   ```

3. Click **Save**, then click **Start** to connect to the server.4. Monitor connection status, number of tools, and health in real time.
**SSE mode advantages:**- Real-time two-way communication through Server-Sent Events- Suitable for scenarios requiring continuous data flow- Lower latency for push based notifications
The test SSE MCP server used for validation can be found in the `cmd/test-sse-mcp-server/` directory.

### Knowledge base function- **Vector retrieval**: The AI ​​agent can automatically call the `search_knowledge_base` tool to search for security knowledge in the knowledge base during the conversation.- **Hybrid Search**: Combines vector similarity search and keyword matching to improve search accuracy.- **Automatic Index**: Scan the Markdown files in the `knowledge_base/` directory and automatically build a vector embedded index.- **Web Management**: Create, update, and delete knowledge items through the Web interface, supporting category management.- **Retrieval log**: records all knowledge retrieval operations to facilitate auditing and debugging.
**Quick start (using pre-built knowledge base):**1. **Download the knowledge database**: Download the pre-built knowledge database file from [GitHub Releases](https://github.com/Ed1s0nZ/CyberStrikeAI/releases).2. **Extract and place**: Unzip the downloaded knowledge database file (`knowledge.db`) and place it in the `data/` directory of the project.3. **Restart Service**: Restart the CyberStrikeAI service, and the knowledge base can be used directly without rebuilding the index.
**Knowledge base configuration steps:**1. **Enable feature**: Set `knowledge.enabled: true` in `config.yaml`:   ```yaml
   knowledge:
     enabled: true
     base_path: knowledge_base
     embedding:
       provider: openai
       model: text-embedding-v4
       base_url: "https://api.openai.com/v1"  # 或你的嵌入模型 API
       api_key: "sk-xxx"
     retrieval:
       top_k: 5
       similarity_threshold: 0.7
       hybrid_weight: 0.7
   ```
2. **Add knowledge files**: Put the Markdown files into the `knowledge_base/` directory and organize them by category (such as `knowledge_base/SQL injection/README.md`).3. **Scan Index**: Click "Scan Knowledge Base" in the web interface, and the system will automatically import the file and build a vector index.4. **Used in dialogue**: The AI ​​agent will automatically call the knowledge retrieval tool when it needs security knowledge. You can also explicitly request: "Search the knowledge base for SQL injection techniques."
**Knowledge base structure description:**- Files are organized by categories (directory names as categories).- Automatically dice and generate vector embeddings for each Markdown file.- Supports incremental updates, modified files will be automatically re-indexed.

### Automation and Security- **REST API**: Authentication, session, task, monitoring, vulnerability management, role management and other interfaces are all open and can be integrated with CI/CD.- **Role Management API**: Manage security test roles via the `/api/roles` endpoint: `GET /api/roles` (list), `GET /api/roles/:name` (get roles), `POST /api/roles` (create roles), `PUT /api/roles/:name` (update roles), `DELETE /api/roles/:name` (delete roles). Roles are stored in the `roles/` directory in the form of YAML files and support hot reloading.- **Vulnerability Management API**: Manage vulnerabilities via the `/api/vulnerabilities` endpoint: `GET /api/vulnerabilities` (list, supports filtering), `POST /api/vulnerabilities` (create), `GET /api/vulnerabilities/:id` (get), `PUT /api/vulnerabilities/:id` (update), `DELETE /api/vulnerabilities/:id` (delete), `GET /api/vulnerabilities/stats` (statistics).- **Batch Task API**: Manage batch task queues through the `/api/batch-tasks` endpoint: `POST /api/batch-tasks` (create queue), `GET /api/batch-tasks` (list), `GET /api/batch-tasks/:queueId` (get queue), `POST /api/batch-tasks/:queueId/start` (start execution), `POST /api/batch-tasks/:queueId/cancel` (cancel), `DELETE /api/batch-tasks/:queueId` (delete queue), `POST /api/batch-tasks/:queueId/tasks` (add task), `PUT /api/batch-tasks/:queueId/tasks/:taskId` (update task), `DELETE /api/batch-tasks/:queueId/tasks/:taskId` (delete task). Tasks are executed sequentially, each task creates an independent dialogue, and supports complete status tracking.- **Task Control**: Supports pausing/terminating long tasks, rerunning after modifying parameters, and streaming log acquisition.- **Security Management**: `/api/auth/change-password` can rotate passwords on the fly; it is recommended to cooperate with network layer ACL when exposing MCP ports.
## Configuration reference
```yaml
auth:
  password: "change-me"
  session_duration_hours: 12
server:
  host: "0.0.0.0"
  port: 8080
log:
  level: "info"
  output: "stdout"
mcp:
  enabled: true
  host: "0.0.0.0"
  port: 8081
openai:
  api_key: "sk-xxx"
  base_url: "https://api.deepseek.com/v1"
  model: "deepseek-chat"
database:
  path: "data/conversations.db"
  knowledge_db_path: "data/knowledge.db"  # 可选：知识库独立数据库
security:
  tools_dir: "tools"
knowledge:
  enabled: false  # 是否启用知识库功能
  base_path: "knowledge_base"  # 知识库目录路径
  embedding:
    provider: "openai"  # 嵌入模型提供商（目前仅支持 openai）
    model: "text-embedding-v4"  # 嵌入模型名称
    base_url: ""  # 留空则使用 OpenAI 配置的 base_url
    api_key: ""  # 留空则使用 OpenAI 配置的 api_key
  retrieval:
    top_k: 5  # 检索返回的 Top-K 结果数量
    similarity_threshold: 0.7  # 相似度阈值（0-1），低于此值的结果将被过滤
    hybrid_weight: 0.7  # 混合检索权重（0-1），向量检索的权重，1.0 表示纯向量检索，0.0 表示纯关键词检索
roles_dir: "roles"  # 角色配置文件目录（相对于配置文件所在目录）
skills_dir: "skills"  # Skills 目录（相对于配置文件所在目录）
```

### Tool template example (`tools/nmap.yaml`)
```yaml
name: "nmap"
command: "nmap"
args: ["-sT", "-sV", "-sC"]
enabled: true
short_description: "网络资产扫描与服务指纹识别"
parameters:
  - name: "target"
    type: "string"
    description: "IP 或域名"
    required: true
    position: 0
  - name: "ports"
    type: "string"
    flag: "-p"
    description: "端口范围，如 1-1000"
```

### Role configuration example (`roles/penetrationtest.yaml`)
```yaml
name: 渗透测试
description: 专业渗透测试专家，全面深入的漏洞检测
user_prompt: 你是一个专业的网络安全渗透测试专家。请使用专业的渗透测试方法和工具，对目标进行全面的安全测试，包括但不限于SQL注入、XSS、CSRF、文件包含、命令执行等常见漏洞。
icon: "\U0001F3AF"
tools:
  - nmap
  - sqlmap
  - nuclei
  - burpsuite
  - metasploit
  - httpx
  - record_vulnerability
  - list_knowledge_risk_types
  - search_knowledge_base
enabled: true
```

## Related documents
- [Robot Instructions (DingTalk/Feishu)](docs/robot.md): Complete configuration steps, commands and troubleshooting instructions for talking to CyberStrikeAI through DingTalk and Feishu on the mobile phone. **It is recommended to follow this document to avoid detours**.
## Project structure
```
CyberStrikeAI/
├── cmd/                 # Web 服务、MCP stdio 入口及辅助工具
├── internal/            # Agent、MCP 核心、路由与执行器
├── web/                 # 前端静态资源与模板
├── tools/               # YAML 工具目录（含 100+ 示例）
├── roles/               # 角色配置文件目录（含 12+ 预设安全测试角色）
├── skills/              # Skills 目录（含 20+ 预设安全测试技能）
├── docs/                # 说明文档（如机器人使用说明）
├── images/              # 文档配图
├── config.yaml          # 运行配置
├── run.sh               # 启动脚本
└── README*.md
```

## Basic experience example
```
扫描 192.168.1.1 的开放端口
对 192.168.1.1 做 80/443/22 重点扫描
检查 https://example.com/page?id=1 是否存在 SQL 注入
枚举 https://example.com 的隐藏目录与组件漏洞
获取 example.com 的子域并批量执行 nuclei
```

## Advanced script example
```
加载侦察剧本：先 amass/subfinder，再对存活主机进行目录爆破。
挂载基于 Burp 的外部 MCP，完成认证流量回放并回传到攻击链。
将 5MB nuclei 报告压缩并生成摘要，附加到对话记录。
构建最新一次测试的攻击链，只导出风险 >= 高的节点列表。
```

## 404 Starlink Project<img src="./images/404StarLinkLogo.png" width="30%">

CyberStrikeAI has now joined the [404 Starlink Project](https://github.com/knownsec/404StarLink)
## TCH Top-Ranked Intelligent Pentest Project  
<div align="left">
  <a href="https://zc.tencent.com/competition/competitionHackathon?code=cha004" target="_blank">
    <img src="./images/tch.png" alt="TCH Top-Ranked Intelligent Pentest Project" width="30%">
  </a>
</div>

## Stargazers over time
![Stargazers over time](https://starchart.cc/Ed1s0nZ/CyberStrikeAI.svg)

---

## ⚠️ Disclaimer
**This tool is for educational and authorized testing purposes only! **
CyberStrikeAI is a professional security testing platform designed to help security researchers, penetration testers and IT professionals conduct security assessments and vulnerability research with explicit authorization.
**By using this tool you agree to:**- Only use this tool on systems for which you have express written authorization- Comply with all applicable laws, regulations and ethical principles- Take full responsibility for any unauthorized use or misuse- Will not use this tool for any illegal or malicious purposes
**The developers are not responsible for any abuse! ** Please ensure that your use complies with local laws and regulations and has explicit authorization from the owner of the target system.
---

Welcome to submit Issue/PR to contribute new tool templates or optimization suggestions!
## Translation and Multilingual Support
The application interface, templates, documentation, and tooling have all been translated into **English** to support a wider array of audiences! If you need Chinese support, please refer to the `README_CN.md`.

## Docker Deployment

To deploy CyberStrikeAI easily, we recommend using Docker. The Docker deployment allows you to isolate the dependencies such as security tools, making deployment portable.

### Prerequisites
* Docker and Docker Compose installed.

### Step-by-Step Instructions

1. **Clone the repository:**
   ```bash
   git clone https://github.com/Ed1s0nZ/CyberStrikeAI.git
   cd CyberStrikeAI
   ```

2. **Create a `Dockerfile`:**
   At the root of the project, a Dockerfile can be added to package your application and security tools. (We assume you have or can create a basic Alpine/Ubuntu based image that installs python3, nmap, masscan, sqlmap, etc.)

   Example `Dockerfile`:
   ```dockerfile
   FROM golang:1.24-alpine AS builder
   WORKDIR /app
   COPY . .
   RUN go mod download
   RUN CGO_ENABLED=1 GOOS=linux go build -a -installsuffix cgo -o cyberstrike cmd/server/main.go

   FROM alpine:latest
   WORKDIR /app

   # Install essential security tools for agents
   RUN apk add --no-cache bash nmap nmap-scripts curl python3 \
       && wget https://github.com/sqlmapproject/sqlmap/tarball/master -O sqlmap.tar.gz \
       && tar -xzf sqlmap.tar.gz && mv sqlmap* sqlmap

   COPY --from=builder /app/cyberstrike .
   COPY --from=builder /app/config.yaml .
   COPY --from=builder /app/web/ web/
   COPY --from=builder /app/tools/ tools/
   COPY --from=builder /app/roles/ roles/
   COPY --from=builder /app/skills/ skills/

   EXPOSE 8081
   CMD ["./cyberstrike"]
   ```

3. **Build the image:**
   ```bash
   docker build -t cyberstrike-ai:latest .
   ```

4. **Run the container:**
   Make sure to map the persistence data directories to the host to save your chat histories, knowledge base and configs.
   ```bash
   docker run -d \
     --name cyberstrike \
     -p 8081:8081 \
     -v $(pwd)/data:/app/data \
     cyberstrike-ai:latest
   ```

5. Access the Web UI via `http://localhost:8081`. The default access password can be configured in your `config.yaml`.

## Cloud Deployment (PaaS)

CyberStrikeAI requires persistent storage (like SQLite for memory, logs, knowledge base), and it relies on various command-line tools and operating system dependencies for security tests.

### Why not Vercel or Netlify?
Platforms like **Vercel** and **Netlify** are primarily designed for serverless functions and static frontend applications. Because serverless functions have a maximum execution timeout (often 10s-60s) and provide ephemeral file systems (read-only), they are **not suitable** for running long-living AI Agent websocket connections, executing local CLI commands (e.g. `nmap`, `sqlmap`), or storing persistent SQLite data.

### Recommended Alternatives: Railway / Render / Fly.io
If you want to host CyberStrikeAI in the cloud, you should use a Docker-compatible Platform-as-a-Service (PaaS) that supports container hosting and persistent volumes.

#### Deploying to Railway
1. Push your cloned code (including the `Dockerfile` we created above) to your own GitHub repository.
2. Go to [Railway.app](https://railway.app/) and create a new project.
3. Select "Deploy from GitHub repo" and choose your CyberStrikeAI fork.
4. **Important**: Add a Volume in Railway settings and mount it to `/app/data` to ensure your database and configurations persist across deployments.
5. Set your environment variables (e.g. `OPENAI_API_KEY`) under Variables.

#### Deploying to Render
1. Create a new "Web Service" in [Render.com](https://render.com/).
2. Connect your GitHub repository.
3. Choose "Docker" as the runtime environment.
4. In the settings, create a **Disk** and mount it to `/app/data`.
5. Add any required environment variables to your service and click Deploy.

*(Note: Always remember that deploying hacking or scanning tools on cloud providers may be subject to their Terms of Service. Ensure you only scan targets you have permission for and respect cloud provider policies on network scanning.)*
