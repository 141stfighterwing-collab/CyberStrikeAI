# CyberStrikeAI Robot Instructions

[English](robot_en.md)

This document explains how to talk to CyberStrikeAI through **DingTalk** and **Feishu** (long connection mode), which can be used on the mobile phone without opening a web page on the server. Follow these steps to avoid common detours.

---

## 1. Where to configure in CyberStrikeAI

1. Log in to CyberStrikeAI web client
2. Navigate to the left to enter **System Settings**
3. Click **Robot Settings** in the settings category on the left (located between "Basic Settings" and "Security Settings")
4. Check and fill in the boxes according to the platform (Client ID/Client Secret for DingTalk, App ID/App Secret for Feishu)
5. Click **App Configuration** to save
6. **Restart the CyberStrikeAI application** (just save without restarting, the robot will not connect)

The configuration will be written to the `robots` section of `config.yaml` and can also be edited directly in the configuration file. **After modifying the DingTalk/Feishu configuration, you must restart it for the long connection to take effect. **

---

## 2. Supported platforms (long connection)

| Platform | Description |
|------|------|
| DingTalk | Using Stream long connection, the program actively connects to DingTalk to receive messages |
| Feishu | Using long connections, the program actively connects to Feishu to receive messages |

The following third section will clarify by platform: what to do on the open platform, which fields to copy, and which column to fill in in CyberStrikeAI.

---

## 3. Configuration items and detailed steps for each platform

### 3.1 DingTalk

**Let’s figure it out first: the two DingTalk robots are different**

| Type | Where to create | Can you do "user sends message → robot reply" | Does this program support |
|------|------------|----------------------------------|----------------|
| **Customized robot** | DingTalk group: Group settings → Add robot → Customize (Webhook) | ❌ No, you can only send messages to the group | ❌ Not supported |
| **Internal enterprise application robot** | [DingTalk Open Platform](https://open.dingtalk.com) Create an application and activate the robot | ✅ Can | ✅ Support |

If you have the Webhook address (`oapi.dingtalk.com/robot/send?access_token=xxx`) and signing key (`SEC...`) of a "custom robot" in your hand, you cannot fill them in directly into this program**. You must follow the steps below to create an "enterprise internal application" on the open platform and get the **Client ID** and **Client Secret**.

---

**Complete steps for DingTalk configuration (do it in order)**

1. **Open DingTalk Open Platform**
Visit [https://open.dingtalk.com](https://open.dingtalk.com) with your browser and log in with the **Enterprise Administrator** account.

2. **Enter application development**
Select **Application Development** on the left → **Internal Enterprise Development** → click **Create Application** (or select an existing application). Create after filling in basic information such as the application name.

3. **Get Client ID and Client Secret**
- Click on **Certificates and Basic Information** on the left (under "Basic Information").
- There are **Client ID (formerly AppKey)** and **Client Secret (formerly AppSecret)** on the page.
- Click to copy, **Do not type by hand**, note: the number **0** and the letter **o**, the number **1** and the letter **l** are easy to copy wrong (for example, `ding9gf9tiozuc504aer` has the number **504** in the middle, not 5o4).

4. **Activate the robot and select Stream mode**
- Left **Application Abilities** → **Robots**.
- Turn on the "Robot Configuration" switch.
- Fill in the robot name, introduction, etc. (required fields are filled in as prompted).
- **Key**: Select **"Stream Mode"** (streaming access) as the message receiving method. If there is only "HTTP callback" or Stream is not selected, this program will not receive the message.
- Save.

5. **Permissions and Release**
- **Permission Management** on the left: Search for "robot", "message", etc., check **receive messages**, **send messages** and other robot-related permissions, and confirm authorization.
- **Version Management and Release** on the left: If there are unreleased configurations, click **Publish New Version** / **Go Online**, otherwise the modification will not take effect.

6. **Fill in CyberStrikeAI**
- Go back to CyberStrikeAI → System Settings → Robot Settings → DingTalk.
- Check "Enable DingTalk Bot".
- **Client ID (AppKey)** Paste the Client ID copied in step 3.
- **Client Secret** Paste the Client Secret copied in step 3.
- Click **Apply Configuration** and then **Restart CyberStrikeAI**.

---

**CyberStrikeAI DingTalk field comparison**

| Fill in the fields in CyberStrikeAI | Source on DingTalk Open Platform |
|------------------------|------------------------|
| Enable DingTalk robot | Check to enable |
| Client ID (AppKey) | Credentials and basic information → **Client ID (formerly AppKey)** |
| Client Secret | Credentials and basic information → **Client Secret (formerly AppSecret)** |

---

### 3.2 Feishu (Lark)

| Configuration items | Description |
|--------|------|
| Enable Feishu robot | Check to activate Feishu long connection |
| App ID | App ID in Feishu Open Platform application certificate |
| App Secret | App Secret in Feishu Open Platform Application Credentials |
| Verify Token | For event subscription (optional) |

**Feishu configuration brief steps**: Log in [Feishu Open Platform] (https://open.feishu.cn) → Create a self-built enterprise application → Obtain **App ID** and **App Secret** in "Credentials and Basic Information" → Activate **Robot** in "Application Capabilities" and enable the corresponding permissions → Publish the application → Fill in the App ID and App Secret into CyberStrikeAI robot settings → Save and **restart the application**.

---

## 4. Robot commands

Send the following **text commands** to the robot on DingTalk/Feishu (only text is supported):

| Command | Description |
|------|------|
| **Help** | Display command help and instructions |
| **List** or **Conversation List** | List the titles and conversation IDs of all conversations |
| **切换 \<对话ID\>** or **Continue \<对话ID\>** | 指定对话 ID，后续消息在该对话中继续 |
| **New Conversation** | Open a new conversation, follow-up messages will be in the new conversation |
| **Clear** | Clear the current conversation context (the effect is the same as "new conversation") |
| **Current** | Display the current conversation ID and title |
| **Stop** | Interrupt the currently executing task |
| **role** or **role list** | List all available roles (penetration testing, CTF, web application scanning, etc.) |
| **角色 \<角色名\>** or **Switch role \<角色名\>** | 切换当前使用的角色 |
| **删除 \<对话ID\>** | 删除指定对话 |
| **Version** | Displays the current CyberStrikeAI version number |

In addition to the above commands, **directly input any text** will be sent to the AI ​​as a user message, consistent with the web dialogue logic (penetration testing/security analysis, etc.).

---

## 5. How to use (Do you want @robot?)

- **Personal chat (recommended)**: **Search and open the bot** in DingTalk/Feishu, enter the **private chat** with the bot, and directly enter "help" or any text, **no need for @**.
- **Group Chat**: If a robot is added to a group, only messages sent after **@robot** in the group will be received and replied by the robot; group messages without @ will not trigger the robot.

Summary: When chatting alone with the robot, you can send it directly; when using it in a group, you need to use @robot to post the content.

---

## 6. Recommended use process (to avoid missing steps)

1. **On the open platform**: Follow Section 3 to complete DingTalk or Feishu application creation, voucher copying, robot activation (DingTalk must select **Stream mode**), permissions and publishing.
2. **In CyberStrikeAI**: System Settings → Robot Settings → Check the corresponding platform, paste Client ID/App ID, Client Secret/App Secret → Click **Application Configuration**.
3. **Restart the CyberStrikeAI process** (otherwise the long connection will not be established).
4. **On DingTalk/Feishu** on your mobile phone: Find the robot (send it directly for individual chats, and @robot for group chats), send "help" or test any content.

If there is no response after sending a message, first read **Section 9 Troubleshooting** and **Section 10 Common Detours**.

---

## 7. Configuration file example

Example of robot-related snippets in `config.yaml`:

```yaml
robots:
  dingtalk:
    enabled: true
    client_id: "your_dingtalk_app_key"
    client_secret: "your_dingtalk_app_secret"
  lark:
    enabled: true
    app_id: "your_lark_app_id"
    app_secret: "your_lark_app_secret"
    verify_token: ""
```

After modification, you need to **restart the application**, and the long connection is established when the application starts.

---

## 8. How to verify whether it is available (no DingTalk/Feishu client required)

When DingTalk or Feishu are not installed, you can use the **test interface** to verify whether the robot logic is normal:

1. First log in to the CyberStrikeAI web client (make sure you are logged in).
2. Use curl to call the test interface (requires the login cookie):

```bash
# Replace YOUR_COOKIE with the Cookie obtained after logging in (Browser F12 → Network → Any request → Cookie in the request header)
curl -X POST "http://localhost:8080/api/robot/test" \
  -H "Content-Type: application/json" \
  -H "Cookie: YOUR_COOKIE" \
  -d '{"platform":"dingtalk","user_id":"test_user","text":"帮助"}'
```

If the returned JSON contains `"reply":"[CyberStrikeAI robot command]..."`, it means that the command is processed normally. You can try `"text":"list"`, `"text":"current"`, etc. again.

接口说明：`POST /api/robot/test`（需登录），请求体 `{"platform":"可选","user_id":"可选","text":"必填"}`，响应 `{"reply":"回复内容"}`。

---

## 9. Troubleshooting when there is no response when sending messages on DingTalk

Check in order:

0. **Sleep with the laptop lid closed/after disconnecting from the Internet**
Both DingTalk and Feishu use long connections to receive messages, and the connection will be disconnected after sleep or disconnection. The program will **automatically reconnect** (retry within about 5 seconds to 60 seconds). Wake up or restore the network and wait for a while before sending the message; if there is still no response, you can restart the CyberStrikeAI process.

1. **Client ID / Client Secret is completely consistent with the open platform**
**Copy and paste** from "Certificates and Basic Information", do not type by hand. Note the number **0** and the letter **o**, the number **1** and the letter **l** (for example, `ding9gf9tiozuc504aer` has **504** in the middle, not 5o4).

2. **Whether the application was restarted after saving the configuration**
The robot's long connection is established when the application starts. Click "Apply Configuration" on the web side to only write the configuration file. **The CyberStrikeAI process** must be restarted before the DingTalk connection will take effect.

3. **Look at the program log**
- After starting, you should see: `DingTalk Stream is connecting...`, `DingTalk Stream has been started (no public network required), waiting to receive messages`.
- If `DingTalk Stream long connection exit` appears with an error message, it is mostly a **Client ID / Client Secret error** or **the open platform has not opened streaming access**.
- After sending a message in DingTalk, if it is received, there should be a log: `DingTalk received message`; if not, it means that DingTalk did not push the message to this program (check back to see if the open platform "robot" is activated and whether **Stream mode** is selected).

4. **Open platform side**
The application needs to be **published**; **Stream access (Stream)** needs to be enabled in the "Robot" capability for receiving messages (only HTTP callbacks are not enough); in permission management, the robot must have permissions to receive and send messages.

---

## 10. Common detours (avoid pitfalls)

- **Wrong robot type** used: The "custom" robot (Webhook + signature) added in the DingTalk group cannot be used for dialogue. This program only supports robots in the open platform "Enterprise Internal Application"**.
- **Only save without restarting**: After changing the robot configuration in CyberStrikeAI, you must **restart the application**, otherwise the long connection will not be established.
- **Client ID copied incorrectly**: If the open platform is `504`, fill in `504` instead of `5o4`; try to copy and paste.
- **DingTalk only has HTTP callback enabled but not Stream**: This program receives messages through **Stream long connection**. The message receiving method of the robot in the open platform must select **Stream mode**.
- **The application is not published**: After modifying the robot or permissions in the open platform, you must **publish the new version** in "Version Management and Release", otherwise it will not take effect.

---

## 11. Precautions

- Both DingTalk and Feishu **only process text messages**; other types (such as pictures and voices) will prompt that they are not supported or ignored.
- Conversations and the web side share the same set of conversation data: conversations created in the bot will be seen in the "Conversations" list on the web side, and vice versa.
- The robot execution logic is consistent with **`/api/agent-loop/stream`** (including progress callbacks and process details written to the database). It only does not push SSE to the client, and finally sends the complete reply back to DingTalk/Feishu/Enterprise WeChat in one go.
