let currentConversationId = null;

// @ mention related status
let mentionTools = [];
let mentionToolsLoaded = false;
let mentionToolsLoadingPromise = null;
let mentionSuggestionsEl = null;
let mentionFilteredTools = [];
let externalMcpNames = []; // List of external MCP names
const mentionState = {
    active: false,
    startIndex: -1,
    query: '',
    selectedIndex: 0,
};

// IME input method status tracking
let isComposing = false;

// Input box draft saving related
const DRAFT_STORAGE_KEY = 'cyberstrike-chat-draft';
let draftSaveTimer = null;
const DRAFT_SAVE_DELAY = 500; // 500ms anti-shake delay

// Related to conversation file upload (the backend will splice the path and content and send them to the large model, and the frontend will no longer send the file list repeatedly)
const MAX_CHAT_FILES = 10;
const CHAT_FILE_DEFAULT_PROMPT = 'Please analyze based on the content of the uploaded file.';
/** @type {{ fileName: string, content: string, mimeType: string }[]} */
let chatAttachments = [];

// Save the input box draft to localStorage (anti-shake version)
function saveChatDraftDebounced(content) {
    // Clear previous timer
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
    }
    
    // Set new timer
    draftSaveTimer = setTimeout(() => {
        saveChatDraft(content);
    }, DRAFT_SAVE_DELAY);
}

// Save input box draft to localStorage
function saveChatDraft(content) {
    try {
        if (content && content.trim().length > 0) {
            localStorage.setItem(DRAFT_STORAGE_KEY, content);
        } else {
            // Clear saved draft if content is empty
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        }
    } catch (error) {
        // LocalStorage may be full or unavailable, failing silently
        console.warn('Failed to save draft:', error);
    }
}

// Restore input box draft from localStorage
function restoreChatDraft() {
    try {
        const chatInput = document.getElementById('chat-input');
        if (!chatInput) {
            return;
        }
        
        // If the input box already contains content, the draft will not be restored (to avoid overwriting user input)
        if (chatInput.value && chatInput.value.trim().length > 0) {
            return;
        }
        
        const draft = localStorage.getItem(DRAFT_STORAGE_KEY);
        if (draft && draft.trim().length > 0) {
            chatInput.value = draft;
            // Adjust the height of the input box to fit the content
            adjustTextareaHeight(chatInput);
        }
    } catch (error) {
        console.warn('Restore draft failed:', error);
    }
}

// Clear saved drafts
function clearChatDraft() {
    try {
        // Synchronous clearing to ensure immediate effect
        localStorage.removeItem(DRAFT_STORAGE_KEY);
    } catch (error) {
        console.warn('Clear draft failed:', error);
    }
}

// Adjust textarea height to fit content
function adjustTextareaHeight(textarea) {
    if (!textarea) return;
    
    // First reset the height to auto, and then immediately set it to a fixed value to ensure that scrollHeight can be accurately obtained.
    textarea.style.height = 'auto';
    // Force the browser to recalculate the layout
    void textarea.offsetHeight;
    
    // Calculate new height (minimum 40px, maximum no more than 300px)
    const scrollHeight = textarea.scrollHeight;
    const newHeight = Math.min(Math.max(scrollHeight, 40), 300);
    textarea.style.height = newHeight + 'px';
    
    // If the content is empty or has very little content, immediately reset to the minimum height
    if (!textarea.value || textarea.value.trim().length === 0) {
        textarea.style.height = '40px';
    }
}

// Send message
async function sendMessage() {
    const input = document.getElementById('chat-input');
    let message = input.value.trim();
    const hasAttachments = chatAttachments && chatAttachments.length > 0;

    if (!message && !hasAttachments) {
        return;
    }
    // When there is an attachment and the user has not entered it, just send a short default prompt (the backend will splice the path and file content to the large model)
    if (hasAttachments && !message) {
        message = CHAT_FILE_DEFAULT_PROMPT;
    }

    // Display user messages (including attachment names to facilitate user confirmation)
    const displayMessage = hasAttachments
        ? message + '\n' + chatAttachments.map(a => '📎 ' + a.fileName).join('\n')
        : message;
    addMessage('user', displayMessage);
    
    // Clear the anti-shake timer to prevent re-saving the draft after clearing the input box
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
        draftSaveTimer = null;
    }
    
    // Clear drafts immediately to prevent recovery on page refresh
    clearChatDraft();
    // Use sync to ensure drafts are cleared
    try {
        localStorage.removeItem(DRAFT_STORAGE_KEY);
    } catch (e) {
        // Ignore errors
    }
    
    // Immediately clear the input box and clear the draft (before sending the request)
    input.value = '';
    // Force reset input box height to initial height (40px)
    input.style.height = '40px';

    // Build request body (including attachments)
    const body = {
        message: message,
        conversationId: currentConversationId,
        role: typeof getCurrentRole === 'function' ? getCurrentRole() : ''
    };
    if (hasAttachments) {
        body.attachments = chatAttachments.map(a => ({
            fileName: a.fileName,
            content: a.content,
            mimeType: a.mimeType || ''
        }));
    }
    // Clear attachment list after sending
    chatAttachments = [];
    renderChatFileChips();

    // Create a progress message container (use detailed progress display)
    const progressId = addProgressMessage();
    const progressElement = document.getElementById(progressId);
    registerProgressTask(progressId, currentConversationId);
    loadActiveTasks();
    let assistantMessageId = null;
    let mcpExecutionIds = [];
    
    try {
        const response = await apiFetch('/api/agent-loop/stream', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(body),
        });
        
        if (!response.ok) {
            throw new Error('Request failed:' + response.status);
        }
        
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        
        while (true) {
            const { done, value } = await reader.read();
            if (done) break;
            
            buffer += decoder.decode(value, { stream: true });
            const lines = buffer.split('\n');
            buffer = lines.pop(); // Keep the last incomplete line
            
            for (const line of lines) {
                if (line.startsWith('data: ')) {
                    try {
                        const eventData = JSON.parse(line.slice(6));
                        handleStreamEvent(eventData, progressElement, progressId, 
                                         () => assistantMessageId, (id) => { assistantMessageId = id; },
                                         () => mcpExecutionIds, (ids) => { mcpExecutionIds = ids; });
                    } catch (e) {
                        console.error('Failed to parse event data:', e, line);
                    }
                }
            }
        }
        
        // Process remaining buffer
        if (buffer.trim()) {
            const lines = buffer.split('\n');
            for (const line of lines) {
                if (line.startsWith('data: ')) {
                    try {
                        const eventData = JSON.parse(line.slice(6));
                        handleStreamEvent(eventData, progressElement, progressId,
                                         () => assistantMessageId, (id) => { assistantMessageId = id; },
                                         () => mcpExecutionIds, (ids) => { mcpExecutionIds = ids; });
                    } catch (e) {
                        console.error('Failed to parse event data:', e, line);
                    }
                }
            }
        }
        
        // After the message is sent successfully, again make sure the draft is cleared
        clearChatDraft();
        try {
            localStorage.removeItem(DRAFT_STORAGE_KEY);
        } catch (e) {
            // Ignore errors
        }
        
    } catch (error) {
        removeMessage(progressId);
        addMessage('system', 'Mistake:' + error.message);
        // When sending fails, the draft is not restored because the message is already displayed in the dialog box
    }
}

// ---------- Conversation file upload ----------
function renderChatFileChips() {
    const list = document.getElementById('chat-file-list');
    if (!list) return;
    list.innerHTML = '';
    if (!chatAttachments.length) return;
    chatAttachments.forEach((a, i) => {
        const chip = document.createElement('div');
        chip.className = 'chat-file-chip';
        chip.setAttribute('role', 'listitem');
        const name = document.createElement('span');
        name.className = 'chat-file-chip-name';
        name.title = a.fileName;
        name.textContent = a.fileName;
        const remove = document.createElement('button');
        remove.type = 'button';
        remove.className = 'chat-file-chip-remove';
        remove.title = 'Remove';
        remove.innerHTML = '×';
        remove.setAttribute('aria-label', 'Remove' + a.fileName);
        remove.addEventListener('click', () => removeChatAttachment(i));
        chip.appendChild(name);
        chip.appendChild(remove);
        list.appendChild(chip);
    });
}

function removeChatAttachment(index) {
    chatAttachments.splice(index, 1);
    renderChatFileChips();
}

// When there is an attachment and the input box is empty, fill in a default prompt (editable); the backend will separately splice the path and content to the large model
function appendChatFilePrompt() {
    const input = document.getElementById('chat-input');
    if (!input || !chatAttachments.length) return;
    if (!input.value.trim()) {
        input.value = CHAT_FILE_DEFAULT_PROMPT;
        adjustTextareaHeight(input);
    }
}

function readFileAsAttachment(file) {
    return new Promise((resolve, reject) => {
        const mimeType = file.type || '';
        const isTextLike = /^text\//i.test(mimeType) || /^(application\/(json|xml|javascript)|image\/svg\+xml)/i.test(mimeType);
        const reader = new FileReader();
        reader.onload = () => {
            let content = reader.result;
            if (typeof content === 'string' && content.startsWith('data:')) {
                content = content.replace(/^data:[^;]+;base64,/, '');
            }
            resolve({ fileName: file.name, content: content, mimeType: mimeType });
        };
        reader.onerror = () => reject(reader.error);
        if (isTextLike) {
            reader.readAsText(file, 'UTF-8');
        } else {
            reader.readAsDataURL(file);
        }
    });
}

function addFilesToChat(files) {
    if (!files || !files.length) return;
    const next = Array.from(files);
    if (chatAttachments.length + next.length > MAX_CHAT_FILES) {
        alert('Maximum simultaneous uploads' + MAX_CHAT_FILES + 'Files, currently selected' + chatAttachments.length + 'Indivual.');
        return;
    }
    const addOne = (file) => {
        return readFileAsAttachment(file).then((a) => {
            chatAttachments.push(a);
            renderChatFileChips();
            appendChatFilePrompt();
        }).catch(() => {
            alert('Failed to read file:' + file.name);
        });
    };
    let p = Promise.resolve();
    next.forEach((file) => { p = p.then(() => addOne(file)); });
    p.then(() => {});
}

function setupChatFileUpload() {
    const inputEl = document.getElementById('chat-file-input');
    const container = document.getElementById('chat-input-container') || document.querySelector('.chat-input-container');
    if (!inputEl || !container) return;

    inputEl.addEventListener('change', function () {
        const files = this.files;
        if (files && files.length) {
            addFilesToChat(files);
        }
        this.value = '';
    });

    container.addEventListener('dragover', function (e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.add('drag-over');
    });
    container.addEventListener('dragleave', function (e) {
        e.preventDefault();
        e.stopPropagation();
        if (!this.contains(e.relatedTarget)) {
            this.classList.remove('drag-over');
        }
    });
    container.addEventListener('drop', function (e) {
        e.preventDefault();
        e.stopPropagation();
        this.classList.remove('drag-over');
        const files = e.dataTransfer && e.dataTransfer.files;
        if (files && files.length) addFilesToChat(files);
    });
}

// Make sure chat-input-container has an id (if not written in the template)
function ensureChatInputContainerId() {
    const c = document.querySelector('.chat-input-container');
    if (c && !c.id) c.id = 'chat-input-container';
}

function setupMentionSupport() {
    mentionSuggestionsEl = document.getElementById('mention-suggestions');
    if (mentionSuggestionsEl) {
        mentionSuggestionsEl.style.display = 'none';
        mentionSuggestionsEl.addEventListener('mousedown', (event) => {
            // Prevent the input box from going out of focus when clicking on the candidate item
            event.preventDefault();
        });
    }
    ensureMentionToolsLoaded().catch(() => {
        // Ignore loading errors and try again later
    });
}

// Refresh tool list (reset loaded status, force reload)
function refreshMentionTools() {
    mentionToolsLoaded = false;
    mentionTools = [];
    externalMcpNames = [];
    mentionToolsLoadingPromise = null;
    // If the @ function is currently in use, trigger a reload immediately
    if (mentionState.active) {
        ensureMentionToolsLoaded().catch(() => {
            // Ignore loading errors
        });
    }
}

// Expose the refresh function to the window object for other modules to call
if (typeof window !== 'undefined') {
    window.refreshMentionTools = refreshMentionTools;
}

function ensureMentionToolsLoaded() {
    // Check if the character has changed and force a reload if so
    if (typeof window !== 'undefined' && window._mentionToolsRoleChanged) {
        mentionToolsLoaded = false;
        mentionTools = [];
        delete window._mentionToolsRoleChanged;
    }
    
    if (mentionToolsLoaded) {
        return Promise.resolve(mentionTools);
    }
    if (mentionToolsLoadingPromise) {
        return mentionToolsLoadingPromise;
    }
    mentionToolsLoadingPromise = fetchMentionTools().finally(() => {
        mentionToolsLoadingPromise = null;
    });
    return mentionToolsLoadingPromise;
}

// A unique identifier for the build tool, used to distinguish tools with the same name but from different sources
function getToolKeyForMention(tool) {
    // If it is an external tool, use external_mcp::tool.name as the unique identifier
    // If it is an internal tool, use tool.name as the identifier
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    return tool.name;
}

async function fetchMentionTools() {
    const pageSize = 100;
    let page = 1;
    let totalPages = 1;
    const seen = new Set();
    const collected = [];

    try {
        // Get the currently selected role (obtained from the function of roles.js)
        const roleName = typeof getCurrentRole === 'function' ? getCurrentRole() : '';

        // Also get the external MCP list
        try {
            const mcpResponse = await apiFetch('/api/external-mcp');
            if (mcpResponse.ok) {
                const mcpData = await mcpResponse.json();
                externalMcpNames = Object.keys(mcpData.servers || {}).filter(name => {
                    const server = mcpData.servers[name];
                    // Include only connected and enabled MCPs
                    return server.status === 'connected' && 
                           (server.config.external_mcp_enable || (server.config.enabled && !server.config.disabled));
                });
            }
        } catch (mcpError) {
            console.warn('Failed to load external MCP list:', mcpError);
            externalMcpNames = [];
        }

        while (page <= totalPages && page <= 20) {
            // Build the API URL and, if a role is specified, add the role query parameter
            let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            if (roleName && roleName !== 'Default') {
                url += `&role=${encodeURIComponent(roleName)}`;
            }

            const response = await apiFetch(url);
            if (!response.ok) {
                break;
            }
            const result = await response.json();
            const tools = Array.isArray(result.tools) ? result.tools : [];
            tools.forEach(tool => {
                if (!tool || !tool.name) {
                    return;
                }
                // Use unique identifiers to deduplicate instead of just tool names
                const toolKey = getToolKeyForMention(tool);
                if (seen.has(toolKey)) {
                    return;
                }
                seen.add(toolKey);

                // Determine the tool's enabled status in the current role
                // If there is a role_enabled field, use it (indicating that the role is specified)
                // Otherwise use the enabled field (meaning no role specified or all tools used)
                let roleEnabled = tool.enabled !== false;
                if (tool.role_enabled !== undefined && tool.role_enabled !== null) {
                    roleEnabled = tool.role_enabled;
                }

                collected.push({
                    name: tool.name,
                    description: tool.description || '',
                    enabled: tool.enabled !== false, // The enabled status of the tool itself
                    roleEnabled: roleEnabled, // Enabled status in current role
                    isExternal: !!tool.is_external,
                    externalMcp: tool.external_mcp || '',
                    toolKey: toolKey, // Save unique identifier
                });
            });
            totalPages = result.total_pages || 1;
            page += 1;
            if (page > totalPages) {
                break;
            }
        }
        mentionTools = collected;
        mentionToolsLoaded = true;
    } catch (error) {
        console.warn('Failed to load tool list, @mentions functionality may not be available:', error);
    }
    return mentionTools;
}

function handleChatInputInput(event) {
    const textarea = event.target;
    updateMentionStateFromInput(textarea);
    // Automatically adjust input box height
    // Use requestAnimationFrame to ensure adjustments are made immediately after the DOM is updated, especially when content is removed
    requestAnimationFrame(() => {
        adjustTextareaHeight(textarea);
    });
    // Save input content to localStorage (anti-shake)
    saveChatDraftDebounced(textarea.value);
}

function handleChatInputClick(event) {
    updateMentionStateFromInput(event.target);
}

function handleChatInputKeydown(event) {
    // If you are using an input method (IME), the Enter key should be used to confirm the candidate word, not to send the message
    // Use event.isComposing or isComposing flag to determine
    if (event.isComposing || isComposing) {
        return;
    }

    if (mentionState.active && mentionSuggestionsEl && mentionSuggestionsEl.style.display !== 'none') {
        if (event.key === 'ArrowDown') {
            event.preventDefault();
            moveMentionSelection(1);
            return;
        }
        if (event.key === 'ArrowUp') {
            event.preventDefault();
            moveMentionSelection(-1);
            return;
        }
        if (event.key === 'Enter' || event.key === 'Tab') {
            event.preventDefault();
            applyMentionSelection();
            return;
        }
        if (event.key === 'Escape') {
            event.preventDefault();
            deactivateMentionState();
            return;
        }
    }

    if (event.key === 'Enter' && !event.shiftKey) {
        event.preventDefault();
        sendMessage();
    }
}

function updateMentionStateFromInput(textarea) {
    if (!textarea) {
        deactivateMentionState();
        return;
    }
    const caret = textarea.selectionStart || 0;
    const textBefore = textarea.value.slice(0, caret);
    const atIndex = textBefore.lastIndexOf('@');

    if (atIndex === -1) {
        deactivateMentionState();
        return;
    }

    // Restrict trigger character to be preceded by blank or start position
    if (atIndex > 0) {
        const boundaryChar = textBefore[atIndex - 1];
        if (boundaryChar && !/\s/.test(boundaryChar) && !'([{，。,.;:!?'.includes(boundaryChar)) {
            deactivateMentionState();
            return;
        }
    }

    const querySegment = textBefore.slice(atIndex + 1);

    if (querySegment.includes(' ') || querySegment.includes('\n') || querySegment.includes('\t') || querySegment.includes('@')) {
        deactivateMentionState();
        return;
    }

    if (querySegment.length > 60) {
        deactivateMentionState();
        return;
    }

    mentionState.active = true;
    mentionState.startIndex = atIndex;
    mentionState.query = querySegment.toLowerCase();
    mentionState.selectedIndex = 0;

    if (!mentionToolsLoaded) {
        renderMentionSuggestions({ showLoading: true });
    } else {
        updateMentionCandidates();
        renderMentionSuggestions();
    }

    ensureMentionToolsLoaded().then(() => {
        if (mentionState.active) {
            updateMentionCandidates();
            renderMentionSuggestions();
        }
    });
}

function updateMentionCandidates() {
    if (!mentionState.active) {
        mentionFilteredTools = [];
        return;
    }
    const normalizedQuery = (mentionState.query || '').trim().toLowerCase();
    let filtered = mentionTools;

    if (normalizedQuery) {
        // Check for exact match of external MCP name
        const exactMatchedMcp = externalMcpNames.find(mcpName => 
            mcpName.toLowerCase() === normalizedQuery
        );

        if (exactMatchedMcp) {
            // If the MCP name is exactly matched, only all tools under the MCP will be displayed.
            filtered = mentionTools.filter(tool => {
                return tool.externalMcp && tool.externalMcp.toLowerCase() === exactMatchedMcp.toLowerCase();
            });
        } else {
            // Check for partial match of MCP name
            const partialMatchedMcps = externalMcpNames.filter(mcpName => 
                mcpName.toLowerCase().includes(normalizedQuery)
            );
            
            // Normal match: filter by tool name and description, also match MCP name
            filtered = mentionTools.filter(tool => {
                const nameMatch = tool.name.toLowerCase().includes(normalizedQuery);
                const descMatch = tool.description && tool.description.toLowerCase().includes(normalizedQuery);
                const mcpMatch = tool.externalMcp && tool.externalMcp.toLowerCase().includes(normalizedQuery);
                
                // If the MCP name is partially matched, all tools under the MCP are also included.
                const mcpPartialMatch = partialMatchedMcps.some(mcpName => 
                    tool.externalMcp && tool.externalMcp.toLowerCase() === mcpName.toLowerCase()
                );
                
                return nameMatch || descMatch || mcpMatch || mcpPartialMatch;
            });
        }
    }

    filtered = filtered.slice().sort((a, b) => {
        // If a role is specified, tools enabled in the current role will be displayed first.
        if (a.roleEnabled !== undefined || b.roleEnabled !== undefined) {
            const aRoleEnabled = a.roleEnabled !== undefined ? a.roleEnabled : a.enabled;
            const bRoleEnabled = b.roleEnabled !== undefined ? b.roleEnabled : b.enabled;
            if (aRoleEnabled !== bRoleEnabled) {
                return aRoleEnabled ? -1 : 1; // Enabled tools first
            }
        }

        if (normalizedQuery) {
            // Tools that exactly match the MCP name are displayed first
            const aMcpExact = a.externalMcp && a.externalMcp.toLowerCase() === normalizedQuery;
            const bMcpExact = b.externalMcp && b.externalMcp.toLowerCase() === normalizedQuery;
            if (aMcpExact !== bMcpExact) {
                return aMcpExact ? -1 : 1;
            }
            
            const aStarts = a.name.toLowerCase().startsWith(normalizedQuery);
            const bStarts = b.name.toLowerCase().startsWith(normalizedQuery);
            if (aStarts !== bStarts) {
                return aStarts ? -1 : 1;
            }
        }
        // If a role is specified, use roleEnabled; otherwise, use enabled
        const aEnabled = a.roleEnabled !== undefined ? a.roleEnabled : a.enabled;
        const bEnabled = b.roleEnabled !== undefined ? b.roleEnabled : b.enabled;
        if (aEnabled !== bEnabled) {
            return aEnabled ? -1 : 1;
        }
        return a.name.localeCompare(b.name, 'zh-CN');
    });

    mentionFilteredTools = filtered;
    if (mentionFilteredTools.length === 0) {
        mentionState.selectedIndex = 0;
    } else if (mentionState.selectedIndex >= mentionFilteredTools.length) {
        mentionState.selectedIndex = 0;
    }
}

function renderMentionSuggestions({ showLoading = false } = {}) {
    if (!mentionSuggestionsEl || !mentionState.active) {
        hideMentionSuggestions();
        return;
    }

    const currentQuery = mentionState.query || '';
    const existingList = mentionSuggestionsEl.querySelector('.mention-suggestions-list');
    const canPreserveScroll = !showLoading &&
        existingList &&
        mentionSuggestionsEl.dataset.lastMentionQuery === currentQuery;
    const previousScrollTop = canPreserveScroll ? existingList.scrollTop : 0;

    if (showLoading) {
        mentionSuggestionsEl.innerHTML = '<div class="mention-empty">Loading tools...</div>';
        mentionSuggestionsEl.style.display = 'block';
        delete mentionSuggestionsEl.dataset.lastMentionQuery;
        return;
    }

    if (!mentionFilteredTools.length) {
        mentionSuggestionsEl.innerHTML = '<div class="mention-empty">No matching tool</div>';
        mentionSuggestionsEl.style.display = 'block';
        mentionSuggestionsEl.dataset.lastMentionQuery = currentQuery;
        return;
    }

    const itemsHtml = mentionFilteredTools.map((tool, index) => {
        const activeClass = index === mentionState.selectedIndex ? 'active' : '';
        // If the tool has a roleEnabled field (specifying a role), use it; otherwise use enabled
        const toolEnabled = tool.roleEnabled !== undefined ? tool.roleEnabled : tool.enabled;
        const disabledClass = toolEnabled ? '' : 'disabled';
        const badge = tool.isExternal ? '<span class="mention-item-badge">External</span>' : '<span class="mention-item-badge internal">Built-in</span>';
        const nameHtml = escapeHtml(tool.name);
        const description = tool.description && tool.description.length > 0 ? escapeHtml(tool.description) : 'No description yet';
        const descHtml = `<div class="mention-item-desc">${description}</div>`;
        // Display status labels based on the tool's enabled status in the current role
        const statusLabel = toolEnabled ? 'Available' : (tool.roleEnabled !== undefined ? 'Disabled (current role)' : 'Disabled');
        const statusClass = toolEnabled ? 'enabled' : 'disabled';
        const originLabel = tool.isExternal
            ? (tool.externalMcp ? `来源：${escapeHtml(tool.externalMcp)}` : 'Source: External MCP')
            : 'Source: built-in tools';

        return `
            <button type="button" class="mention-item ${activeClass} ${disabledClass}" data-index="${index}">
                <div class="mention-item-name">
                    <span class="mention-item-icon">🔧</span>
                    <span class="mention-item-text">@${nameHtml}</span>
                    ${badge}
                </div>
                ${descHtml}
                <div class="mention-item-meta">
                    <span class="mention-status ${statusClass}">${statusLabel}</span>
                    <span class="mention-origin">${originLabel}</span>
                </div>
            </button>
        `;
    }).join('');

    const listWrapper = document.createElement('div');
    listWrapper.className = 'mention-suggestions-list';
    listWrapper.innerHTML = itemsHtml;

    mentionSuggestionsEl.innerHTML = '';
    mentionSuggestionsEl.appendChild(listWrapper);
    mentionSuggestionsEl.style.display = 'block';
    mentionSuggestionsEl.dataset.lastMentionQuery = currentQuery;

    if (canPreserveScroll) {
        listWrapper.scrollTop = previousScrollTop;
    }

    listWrapper.querySelectorAll('.mention-item').forEach(item => {
        item.addEventListener('mousedown', (event) => {
            event.preventDefault();
            const idx = parseInt(item.dataset.index, 10);
            if (!Number.isNaN(idx)) {
                mentionState.selectedIndex = idx;
            }
            applyMentionSelection();
        });
    });

    scrollMentionSelectionIntoView();
}

function hideMentionSuggestions() {
    if (mentionSuggestionsEl) {
        mentionSuggestionsEl.style.display = 'none';
        mentionSuggestionsEl.innerHTML = '';
        delete mentionSuggestionsEl.dataset.lastMentionQuery;
    }
}

function deactivateMentionState() {
    mentionState.active = false;
    mentionState.startIndex = -1;
    mentionState.query = '';
    mentionState.selectedIndex = 0;
    mentionFilteredTools = [];
    hideMentionSuggestions();
}

function moveMentionSelection(direction) {
    if (!mentionFilteredTools.length) {
        return;
    }
    const max = mentionFilteredTools.length - 1;
    let nextIndex = mentionState.selectedIndex + direction;
    if (nextIndex < 0) {
        nextIndex = max;
    } else if (nextIndex > max) {
        nextIndex = 0;
    }
    mentionState.selectedIndex = nextIndex;
    updateMentionActiveHighlight();
}

function updateMentionActiveHighlight() {
    if (!mentionSuggestionsEl) {
        return;
    }
    const items = mentionSuggestionsEl.querySelectorAll('.mention-item');
    if (!items.length) {
        return;
    }
    items.forEach(item => item.classList.remove('active'));

    let targetIndex = mentionState.selectedIndex;
    if (targetIndex < 0) {
        targetIndex = 0;
    }
    if (targetIndex >= items.length) {
        targetIndex = items.length - 1;
        mentionState.selectedIndex = targetIndex;
    }

    const activeItem = items[targetIndex];
    if (activeItem) {
        activeItem.classList.add('active');
        scrollMentionSelectionIntoView(activeItem);
    }
}

function scrollMentionSelectionIntoView(targetItem = null) {
    if (!mentionSuggestionsEl) {
        return;
    }
    const activeItem = targetItem || mentionSuggestionsEl.querySelector('.mention-item.active');
    if (activeItem && typeof activeItem.scrollIntoView === 'function') {
        activeItem.scrollIntoView({
            block: 'nearest',
            inline: 'nearest',
            behavior: 'auto'
        });
    }
}

function applyMentionSelection() {
    const textarea = document.getElementById('chat-input');
    if (!textarea || mentionState.startIndex === -1 || !mentionFilteredTools.length) {
        deactivateMentionState();
        return;
    }

    const selectedTool = mentionFilteredTools[mentionState.selectedIndex] || mentionFilteredTools[0];
    if (!selectedTool) {
        deactivateMentionState();
        return;
    }

    const caret = textarea.selectionStart || 0;
    const before = textarea.value.slice(0, mentionState.startIndex);
    const after = textarea.value.slice(caret);
    const mentionText = `@${selectedTool.name}`;
    const needsSpace = after.length === 0 || !/^\s/.test(after);
    const insertText = mentionText + (needsSpace ? ' ' : '');

    textarea.value = before + insertText + after;
    const newCaret = before.length + insertText.length;
    textarea.focus();
    textarea.setSelectionRange(newCaret, newCaret);
    
    // Adjust the height of the input box and save the draft
    adjustTextareaHeight(textarea);
    saveChatDraftDebounced(textarea.value);

    deactivateMentionState();
}

function initializeChatUI() {
    const chatInputEl = document.getElementById('chat-input');
    if (chatInputEl) {
        // Set correct height on initialization
        adjustTextareaHeight(chatInputEl);
        // Restore saved drafts (only restore when input box is empty to avoid overwriting user input)
        if (!chatInputEl.value || chatInputEl.value.trim() === '') {
            // Check whether there is a recent message in the conversation (within 30 seconds). If there is, it may be a message that was just sent and the draft will not be restored.
            const messagesDiv = document.getElementById('chat-messages');
            let shouldRestoreDraft = true;
            if (messagesDiv && messagesDiv.children.length > 0) {
                // Check the time of the last message
                const lastMessage = messagesDiv.lastElementChild;
                if (lastMessage) {
                    const timeDiv = lastMessage.querySelector('.message-time');
                    if (timeDiv && timeDiv.textContent) {
                        // If the last message is a user message and the time is very recent, the draft will not be restored
                        const isUserMessage = lastMessage.classList.contains('user');
                        if (isUserMessage) {
                            // Check the message time. If it is within the last 30 seconds, the draft will not be restored.
                            const now = new Date();
                            const messageTimeText = timeDiv.textContent;
                            // Simple check: If the message time displays the current time (format: HH:MM) and it is a user message, the draft will not be restored.
                            // A more precise way is to check the creation time of the message, but you need to get it from the message element
                            // A simple strategy is adopted here: if the last message is a user message and the input box is empty, it may have just been sent and the draft will not be restored.
                            shouldRestoreDraft = false;
                        }
                    }
                }
            }
            if (shouldRestoreDraft) {
                restoreChatDraft();
            } else {
                // Even if you do not restore the draft, you must clear the draft in localStorage to avoid accidentally restoring it next time.
                clearChatDraft();
            }
        }
    }

    const messagesDiv = document.getElementById('chat-messages');
    if (messagesDiv && messagesDiv.childElementCount === 0) {
        addMessage('assistant', 'The system is ready. Please enter your testing requirements and the system will automatically perform the corresponding security tests.');
    }

    addAttackChainButton(currentConversationId);
    loadActiveTasks(true);
    if (activeTaskInterval) {
        clearInterval(activeTaskInterval);
    }
    activeTaskInterval = setInterval(() => loadActiveTasks(), ACTIVE_TASK_REFRESH_INTERVAL);
    setupMentionSupport();
    ensureChatInputContainerId();
    setupChatFileUpload();
}

// Message counter to ensure unique ID
let messageCounter = 0;

// Add independent scroll container for table in message bubble
function wrapTablesInBubble(bubble) {
    const tables = bubble.querySelectorAll('table');
    tables.forEach(table => {
        // Check if the table already has a wrapping container
        if (table.parentElement && table.parentElement.classList.contains('table-wrapper')) {
            return;
        }
        
        // Create a table wrapper
        const wrapper = document.createElement('div');
        wrapper.className = 'table-wrapper';
        
        // Move table into wrapper container
        table.parentNode.insertBefore(wrapper, table);
        wrapper.appendChild(table);
    });
}

// Add message
function addMessage(role, content, mcpExecutionIds = null, progressId = null, createdAt = null) {
    const messagesDiv = document.getElementById('chat-messages');
    const messageDiv = document.createElement('div');
    messageCounter++;
    const id = 'msg-' + Date.now() + '-' + messageCounter + '-' + Math.random().toString(36).substr(2, 9);
    messageDiv.id = id;
    messageDiv.className = 'message ' + role;
    
    // Create avatar
    const avatar = document.createElement('div');
    avatar.className = 'message-avatar';
    if (role === 'user') {
        avatar.textContent = 'U';
    } else if (role === 'assistant') {
        avatar.textContent = 'A';
    } else {
        avatar.textContent = 'S';
    }
    messageDiv.appendChild(avatar);
    
    // Create message content container
    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'message-content';
    
    // Create message bubble
    const bubble = document.createElement('div');
    bubble.className = 'message-bubble';
    
    // Parse Markdown or HTML format
    let formattedContent;
    const defaultSanitizeConfig = {
        ALLOWED_TAGS: ['p', 'br', 'strong', 'em', 'u', 's', 'code', 'pre', 'blockquote', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'ul', 'ol', 'li', 'a', 'img', 'table', 'thead', 'tbody', 'tr', 'th', 'td', 'hr'],
        ALLOWED_ATTR: ['href', 'title', 'alt', 'src', 'class'],
        ALLOW_DATA_ATTR: false,
    };
    
    // HTML entity encoding function
    const escapeHtml = (text) => {
        if (!text) return '';
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    };
    
    // Note: The code block content does not need to be escaped because:
    // 1. After Markdown is parsed, the code block will be wrapped in <code> or <pre> tags
    // 2. The browser will not execute the HTML inside the <code> and <pre> tags (they are text nodes)
    // 3. DOMPurify will retain the text content within these tags
    // This can prevent XSS and display the code normally.
    
    const parseMarkdown = (raw) => {
        if (typeof marked === 'undefined') {
            return null;
        }
        try {
            marked.setOptions({
                breaks: true,
                gfm: true,
            });
            return marked.parse(raw);
        } catch (e) {
            console.error('Markdown parsing failed:', e);
            return null;
        }
    };
    
    // For user messages, escape HTML directly without Markdown parsing to preserve all special characters
    if (role === 'user') {
        formattedContent = escapeHtml(content).replace(/\n/g, '<br>');
    } else if (typeof DOMPurify !== 'undefined') {
        // Parse Markdown directly (code blocks will be wrapped in <code>/<pre>, DOMPurify will retain its text content)
        let parsedContent = parseMarkdown(content);
        if (!parsedContent) {
            parsedContent = content;
        }
        
        // Use DOMPurify to clean up and only add necessary URL validation hooks (DOMPurify will handle event handlers etc. by default)
        if (DOMPurify.addHook) {
            // Remove hooks that may have existed before
            try {
                DOMPurify.removeHook('uponSanitizeAttribute');
            } catch (e) {
                // Hook does not exist, ignore
            }
            
            // Only verify URL attributes to prevent dangerous protocols (DOMPurify will handle event handlers, styles, etc. by default)
            DOMPurify.addHook('uponSanitizeAttribute', (node, data) => {
                const attrName = data.attrName.toLowerCase();
                
                // Only validate URL attributes (src, href)
                if ((attrName === 'src' || attrName === 'href') && data.attrValue) {
                    const value = data.attrValue.trim().toLowerCase();
                    // No dangerous agreements
                    if (value.startsWith('javascript:') || 
                        value.startsWith('vbscript:') ||
                        value.startsWith('data:text/html') ||
                        value.startsWith('data:text/javascript')) {
                        data.keepAttr = false;
                        return;
                    }
                    // For img's src, suspicious short URLs are prohibited (preventing 404 and XSS)
                    if (attrName === 'src' && node.tagName && node.tagName.toLowerCase() === 'img') {
                        if (value.length <= 2 || /^[a-z]$/i.test(value)) {
                            data.keepAttr = false;
                            return;
                        }
                    }
                }
            });
        }
        
        formattedContent = DOMPurify.sanitize(parsedContent, defaultSanitizeConfig);
    } else if (typeof marked !== 'undefined') {
        const parsedContent = parseMarkdown(content);
        if (parsedContent) {
            formattedContent = parsedContent;
        } else {
            formattedContent = escapeHtml(content).replace(/\n/g, '<br>');
        }
    } else {
        formattedContent = escapeHtml(content).replace(/\n/g, '<br>');
    }
    
    bubble.innerHTML = formattedContent;
    
    // Final security check: only process obviously suspicious images (prevent 404 and XSS)
    // DOMPurify has already processed most of the XSS vectors, only the necessary additions are made here.
    const images = bubble.querySelectorAll('img');
    images.forEach(img => {
        const src = img.getAttribute('src');
        if (src) {
            const trimmedSrc = src.trim();
            // Only check obvious suspicious URLs (short strings, single characters)
            if (trimmedSrc.length <= 2 || /^[a-z]$/i.test(trimmedSrc)) {
                img.remove();
            }
        } else {
            img.remove();
        }
    });
    
    // Add separate scroll container for each table
    wrapTablesInBubble(bubble);
    
    contentWrapper.appendChild(bubble);
    
    // Save the original content to the message element for use with copy functionality
    if (role === 'assistant') {
        messageDiv.dataset.originalContent = content;
    }
    
    // Add a copy button to the assistant message (copy the entire reply content) - place it in the lower right corner of the message bubble
    if (role === 'assistant') {
        const copyBtn = document.createElement('button');
        copyBtn.className = 'message-copy-btn';
        copyBtn.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><rect x="9" y="9" width="13" height="13" rx="2" ry="2" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg><span>Copy</span>';
        copyBtn.title = 'Copy message content';
        copyBtn.onclick = function(e) {
            e.stopPropagation();
            copyMessageToClipboard(messageDiv, this);
        };
        bubble.appendChild(copyBtn);
    }
    
    // Add timestamp
    const timeDiv = document.createElement('div');
    timeDiv.className = 'message-time';
    // If there is a creation time passed in, use it; otherwise use the current time
    let messageTime;
    if (createdAt) {
        // Process strings or Date objects
        if (typeof createdAt === 'string') {
            messageTime = new Date(createdAt);
        } else if (createdAt instanceof Date) {
            messageTime = createdAt;
        } else {
            messageTime = new Date(createdAt);
        }
        // If parsing fails, use the current time
        if (isNaN(messageTime.getTime())) {
            messageTime = new Date();
        }
    } else {
        messageTime = new Date();
    }
    timeDiv.textContent = messageTime.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
    contentWrapper.appendChild(timeDiv);
    
    // If there is an MCP execution ID or progress ID, add a view details area (uniformly use the "penetration test details" style)
    if (role === 'assistant' && ((mcpExecutionIds && Array.isArray(mcpExecutionIds) && mcpExecutionIds.length > 0) || progressId)) {
        const mcpSection = document.createElement('div');
        mcpSection.className = 'mcp-call-section';
        
        const mcpLabel = document.createElement('div');
        mcpLabel.className = 'mcp-call-label';
        mcpLabel.textContent = '📋 Penetration testing details';
        mcpSection.appendChild(mcpLabel);
        
        const buttonsContainer = document.createElement('div');
        buttonsContainer.className = 'mcp-call-buttons';
        
        // If there is an MCP execution ID, add an MCP call details button
        if (mcpExecutionIds && Array.isArray(mcpExecutionIds) && mcpExecutionIds.length > 0) {
            mcpExecutionIds.forEach((execId, index) => {
                const detailBtn = document.createElement('button');
                detailBtn.className = 'mcp-detail-btn';
                detailBtn.innerHTML = `<span>Call #${index + 1}</span>`;
                detailBtn.onclick = () => showMCPDetail(execId);
                buttonsContainer.appendChild(detailBtn);
                // Asynchronously get tool name and update button text
                updateButtonWithToolName(detailBtn, execId, index + 1);
            });
        }
        
        // If there is a progress ID, add an expand details button (use the "expand details" text uniformly)
        if (progressId) {
            const progressDetailBtn = document.createElement('button');
            progressDetailBtn.className = 'mcp-detail-btn process-detail-btn';
            progressDetailBtn.innerHTML = '<span>Expand details</span>';
            progressDetailBtn.onclick = () => toggleProcessDetails(progressId, messageDiv.id);
            buttonsContainer.appendChild(progressDetailBtn);
            // Store the progress ID in the message element
            messageDiv.dataset.progressId = progressId;
        }
        
        mcpSection.appendChild(buttonsContainer);
        contentWrapper.appendChild(mcpSection);
    }
    
    messageDiv.appendChild(contentWrapper);
    messagesDiv.appendChild(messageDiv);
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
    return id;
}

// Copy message content to clipboard (using original Markdown format)
function copyMessageToClipboard(messageDiv, button) {
    try {
        // Get the original saved Markdown content
        const originalContent = messageDiv.dataset.originalContent;
        
        if (!originalContent) {
            // If original content is not saved, try to extract from rendered HTML (downgrade scenario)
            const bubble = messageDiv.querySelector('.message-bubble');
            if (bubble) {
                const tempDiv = document.createElement('div');
                tempDiv.innerHTML = bubble.innerHTML;
                
                // Remove the copy button itself (avoid copying the button text)
                const copyBtnInTemp = tempDiv.querySelector('.message-copy-btn');
                if (copyBtnInTemp) {
                    copyBtnInTemp.remove();
                }
                
                // Extract plain text content
                let textContent = tempDiv.textContent || tempDiv.innerText || '';
                textContent = textContent.replace(/\n{3,}/g, '\n\n').trim();
                
                navigator.clipboard.writeText(textContent).then(() => {
                    showCopySuccess(button);
                }).catch(err => {
                    console.error('Copy failed:', err);
                    alert('Copy failed, please manually select content to copy');
                });
            }
            return;
        }
        
        // Use original Markdown content
        navigator.clipboard.writeText(originalContent).then(() => {
            showCopySuccess(button);
        }).catch(err => {
            console.error('Copy failed:', err);
            alert('Copy failed, please manually select content to copy');
        });
    } catch (error) {
        console.error('Error copying message:', error);
        alert('Copy failed, please manually select content to copy');
    }
}

// Show copy success prompt
function showCopySuccess(button) {
    if (button) {
        const originalText = button.innerHTML;
        button.innerHTML = '<svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M20 6L9 17l-5-5" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round" fill="none"/></svg><span>Copied</span>';
        button.style.color = '#10b981';
        button.style.background = 'rgba(16, 185, 129, 0.1)';
        button.style.borderColor = 'rgba(16, 185, 129, 0.3)';
        setTimeout(() => {
            button.innerHTML = originalText;
            button.style.color = '';
            button.style.background = '';
            button.style.borderColor = '';
        }, 2000);
    }
}

// Rendering process details
function renderProcessDetails(messageId, processDetails) {
    const messageElement = document.getElementById(messageId);
    if (!messageElement) {
        return;
    }
    
    // Find or create an MCP call area
    let mcpSection = messageElement.querySelector('.mcp-call-section');
    if (!mcpSection) {
        mcpSection = document.createElement('div');
        mcpSection.className = 'mcp-call-section';
        
        const contentWrapper = messageElement.querySelector('.message-content');
        if (contentWrapper) {
            contentWrapper.appendChild(mcpSection);
        } else {
            return;
        }
    }
    
    // Make sure you have label and button containers (unified structure)
    let mcpLabel = mcpSection.querySelector('.mcp-call-label');
    let buttonsContainer = mcpSection.querySelector('.mcp-call-buttons');
    
    // If there is no label, create one (when no tool is called)
    if (!mcpLabel && !buttonsContainer) {
        mcpLabel = document.createElement('div');
        mcpLabel.className = 'mcp-call-label';
        mcpLabel.textContent = '📋 Penetration testing details';
        mcpSection.appendChild(mcpLabel);
    } else if (mcpLabel && mcpLabel.textContent !== '📋 Penetration testing details') {
        // If the label exists but is not uniformly formatted, update it
        mcpLabel.textContent = '📋 Penetration testing details';
    }
    
    // If there is no button container, create one
    if (!buttonsContainer) {
        buttonsContainer = document.createElement('div');
        buttonsContainer.className = 'mcp-call-buttons';
        mcpSection.appendChild(buttonsContainer);
    }
    
    // Add process details button if you don't have one already
    let processDetailBtn = buttonsContainer.querySelector('.process-detail-btn');
    if (!processDetailBtn) {
        processDetailBtn = document.createElement('button');
        processDetailBtn.className = 'mcp-detail-btn process-detail-btn';
        processDetailBtn.innerHTML = '<span>Expand details</span>';
        processDetailBtn.onclick = () => toggleProcessDetails(null, messageId);
        buttonsContainer.appendChild(processDetailBtn);
    }
    
    // Create a process details container (placed after the button container)
    const detailsId = 'process-details-' + messageId;
    let detailsContainer = document.getElementById(detailsId);
    
    if (!detailsContainer) {
        detailsContainer = document.createElement('div');
        detailsContainer.id = detailsId;
        detailsContainer.className = 'process-details-container';
        // Make sure the container is after the button container
        if (buttonsContainer.nextSibling) {
            mcpSection.insertBefore(detailsContainer, buttonsContainer.nextSibling);
        } else {
            mcpSection.appendChild(detailsContainer);
        }
    }
    
    // Create the timeline (create it even if there is no processDetails so that the expand details button works properly)
    const timelineId = detailsId + '-timeline';
    let timeline = document.getElementById(timelineId);
    
    if (!timeline) {
        const contentDiv = document.createElement('div');
        contentDiv.className = 'process-details-content';
        
        timeline = document.createElement('div');
        timeline.id = timelineId;
        timeline.className = 'progress-timeline';
        
        contentDiv.appendChild(timeline);
        detailsContainer.appendChild(contentDiv);
    }
    
    // If there is no processDetails or is empty, display empty status
    if (!processDetails || processDetails.length === 0) {
        // Show empty status prompt
        timeline.innerHTML = '<div class="progress-timeline-empty">No process details yet (may be executed too fast or detailed events not triggered)</div>';
        // Default folded
        timeline.classList.remove('expanded');
        return;
    }
    
    // Clear the timeline and re-render
    timeline.innerHTML = '';
    
    
    // Render each process details event
    processDetails.forEach(detail => {
        const eventType = detail.eventType || '';
        const title = detail.message || '';
        const data = detail.data || {};
        
        // Render different content based on event type
        let itemTitle = title;
        if (eventType === 'iteration') {
            itemTitle = `第 ${data.iteration || 1} 轮迭代`;
        } else if (eventType === 'thinking') {
            itemTitle = '🤔 AI thinking';
        } else if (eventType === 'tool_calls_detected') {
            itemTitle = `🔧 检测到 ${data.count || 0} 个工具调用`;
        } else if (eventType === 'tool_call') {
            const toolName = data.toolName || 'Unknown tool';
            const index = data.index || 0;
            const total = data.total || 0;
            itemTitle = `🔧 调用工具: ${escapeHtml(toolName)} (${index}/${total})`;
        } else if (eventType === 'tool_result') {
            const toolName = data.toolName || 'Unknown tool';
            const success = data.success !== false;
            const statusIcon = success ? '✅' : '❌';
            itemTitle = `${statusIcon} 工具 ${escapeHtml(toolName)} 执行${success ? 'Finish' : 'Fail'}`;
            
            // If it is a knowledge retrieval tool, add special tags
            if (toolName === BuiltinTools.SEARCH_KNOWLEDGE_BASE && success) {
                itemTitle = `📚 ${itemTitle} - 知识检索`;
            }
        } else if (eventType === 'knowledge_retrieval') {
            itemTitle = '📚 Knowledge retrieval';
        } else if (eventType === 'error') {
            itemTitle = '❌ Error';
        } else if (eventType === 'cancelled') {
            itemTitle = '⛔ Task canceled';
        }
        
        addTimelineItem(timeline, eventType, {
            title: itemTitle,
            message: detail.message || '',
            data: data,
            createdAt: detail.createdAt // Pass the actual event creation time
        });
    });
    
    // Check if there are any errors or cancellation events, and if so, make sure the details are collapsed by default
    const hasErrorOrCancelled = processDetails.some(d => 
        d.eventType === 'error' || d.eventType === 'cancelled'
    );
    if (hasErrorOrCancelled) {
        // Make sure the timeline is collapsed
        timeline.classList.remove('expanded');
        // Update button text to "Expand details"
        const processDetailBtn = messageElement.querySelector('.process-detail-btn');
        if (processDetailBtn) {
            processDetailBtn.innerHTML = '<span>Expand details</span>';
        }
    }
}

// Remove message
function removeMessage(id) {
    const messageDiv = document.getElementById(id);
    if (messageDiv) {
        messageDiv.remove();
    }
}

// Input box event binding (Press Enter to send/@mention)
const chatInput = document.getElementById('chat-input');
if (chatInput) {
    chatInput.addEventListener('keydown', handleChatInputKeydown);
    chatInput.addEventListener('input', handleChatInputInput);
    chatInput.addEventListener('click', handleChatInputClick);
    chatInput.addEventListener('focus', handleChatInputClick);
    // IME input method event monitoring, used to track input method status
    chatInput.addEventListener('compositionstart', () => {
        isComposing = true;
    });
    chatInput.addEventListener('compositionend', () => {
        isComposing = false;
    });
    chatInput.addEventListener('blur', () => {
        setTimeout(() => {
            if (!chatInput.matches(':focus')) {
                deactivateMentionState();
            }
        }, 120);
        // Save draft immediately when out of focus (without waiting for image stabilization)
        if (chatInput.value) {
            saveChatDraft(chatInput.value);
        }
    });
}

// Save draft immediately when page unloads
window.addEventListener('beforeunload', () => {
    const chatInput = document.getElementById('chat-input');
    if (chatInput && chatInput.value) {
        // Save now without image stabilization
        saveChatDraft(chatInput.value);
    }
});

// Asynchronously get tool name and update button text
async function updateButtonWithToolName(button, executionId, index) {
    try {
        const response = await apiFetch(`/api/monitor/execution/${executionId}`);
        if (response.ok) {
            const exec = await response.json();
            const toolName = exec.toolName || 'Unknown tool';
            // Format tool name (if it is name::toolName format, only the toolName part is displayed)
            const displayToolName = toolName.includes('::') ? toolName.split('::')[1] : toolName;
            button.querySelector('span').textContent = `${displayToolName} #${index}`;
        }
    } catch (error) {
        // If the acquisition fails, keep the original text unchanged
        console.error('Failed to get tool name:', error);
    }
}

// Show MCP call details
async function showMCPDetail(executionId) {
    try {
        const response = await apiFetch(`/api/monitor/execution/${executionId}`);
        const exec = await response.json();
        
        if (response.ok) {
            // Fill modal box content
            document.getElementById('detail-tool-name').textContent = exec.toolName || 'Unknown';
            document.getElementById('detail-execution-id').textContent = exec.id || 'N/A';
            const statusEl = document.getElementById('detail-status');
            const normalizedStatus = (exec.status || 'unknown').toLowerCase();
            statusEl.textContent = getStatusText(exec.status);
            statusEl.className = `status-chip status-${normalizedStatus}`;
            document.getElementById('detail-time').textContent = exec.startTime
                ? new Date(exec.startTime).toLocaleString('zh-CN')
                : '—';
            
            // Request parameters
            const requestData = {
                tool: exec.toolName,
                arguments: exec.arguments
            };
            document.getElementById('detail-request').textContent = JSON.stringify(requestData, null, 2);
            
            // Response result + correct information/error information
            const responseElement = document.getElementById('detail-response');
            const successSection = document.getElementById('detail-success-section');
            const successElement = document.getElementById('detail-success');
            const errorSection = document.getElementById('detail-error-section');
            const errorElement = document.getElementById('detail-error');

            // Reset state
            responseElement.className = 'code-block';
            responseElement.textContent = '';
            if (successSection && successElement) {
                successSection.style.display = 'none';
                successElement.textContent = '';
            }
            if (errorSection && errorElement) {
                errorSection.style.display = 'none';
                errorElement.textContent = '';
            }

            if (exec.result) {
                const responseData = {
                    content: exec.result.content,
                    isError: exec.result.isError
                };
                responseElement.textContent = JSON.stringify(responseData, null, 2);

                if (exec.result.isError) {
                    // Error scenario: response result marked red + error message block
                    responseElement.className = 'code-block error';
                    if (exec.error && errorSection && errorElement) {
                        errorSection.style.display = 'block';
                        errorElement.textContent = exec.error;
                    }
                } else {
                    // Success scenario: The response result remains in the normal style, and the correct information is singled out
                    responseElement.className = 'code-block';
                    if (successSection && successElement) {
                        successSection.style.display = 'block';
                        let successText = '';
                        const content = exec.result.content;
                        if (typeof content === 'string') {
                            successText = content;
                        } else if (Array.isArray(content)) {
                            const texts = content
                                .map(item => (item && typeof item === 'object' && typeof item.text === 'string') ? item.text : '')
                                .filter(Boolean);
                            if (texts.length > 0) {
                                successText = texts.join('\n\n');
                            }
                        } else if (content && typeof content === 'object' && typeof content.text === 'string') {
                            successText = content.text;
                        }
                        if (!successText) {
                            successText = 'The execution was successful and no displayable text content was returned.';
                        }
                        successElement.textContent = successText;
                    }
                }
            } else {
                responseElement.textContent = 'No response data yet';
            }
            
            // Show modal box
            document.getElementById('mcp-detail-modal').style.display = 'block';
        } else {
            alert('Failed to get details:' + (exec.error || 'Unknown error'));
        }
    } catch (error) {
        alert('Failed to get details:' + error.message);
    }
}

// Close MCP details modal box
function closeMCPDetail() {
    document.getElementById('mcp-detail-modal').style.display = 'none';
}

// Copy the contents of the details panel
function copyDetailBlock(elementId, triggerBtn = null) {
    const target = document.getElementById(elementId);
    if (!target) {
        return;
    }
    const text = target.textContent || '';
    if (!text.trim()) {
        return;
    }

    const originalLabel = triggerBtn ? (triggerBtn.dataset.originalLabel || triggerBtn.textContent.trim()) : '';
    if (triggerBtn && !triggerBtn.dataset.originalLabel) {
        triggerBtn.dataset.originalLabel = originalLabel;
    }

    const showCopiedState = () => {
        if (!triggerBtn) {
            return;
        }
        triggerBtn.textContent = 'Copied';
        triggerBtn.disabled = true;
        setTimeout(() => {
            triggerBtn.disabled = false;
            triggerBtn.textContent = triggerBtn.dataset.originalLabel || originalLabel || 'Copy';
        }, 1200);
    };

    const fallbackCopy = (value) => {
        return new Promise((resolve, reject) => {
            const textarea = document.createElement('textarea');
            textarea.value = value;
            textarea.style.position = 'fixed';
            textarea.style.opacity = '0';
            document.body.appendChild(textarea);
            textarea.focus();
            textarea.select();
            try {
                const successful = document.execCommand('copy');
                document.body.removeChild(textarea);
                if (successful) {
                    resolve();
                } else {
                    reject(new Error('execCommand failed'));
                }
            } catch (err) {
                document.body.removeChild(textarea);
                reject(err);
            }
        });
    };

    const copyPromise = (navigator.clipboard && typeof navigator.clipboard.writeText === 'function')
        ? navigator.clipboard.writeText(text)
        : fallbackCopy(text);

    copyPromise
        .then(() => {
            showCopiedState();
        })
        .catch(() => {
            if (triggerBtn) {
                triggerBtn.disabled = false;
                triggerBtn.textContent = triggerBtn.dataset.originalLabel || originalLabel || 'Copy';
            }
            alert('Copy failed, please manually select text to copy.');
        });
}


// Start a new conversation
async function startNewConversation() {
    // If you are currently on the group details page, exit the group details first.
    if (currentGroupId) {
        const groupDetailPage = document.getElementById('group-detail-page');
        const chatContainer = document.querySelector('.chat-container');
        if (groupDetailPage) groupDetailPage.style.display = 'none';
        if (chatContainer) chatContainer.style.display = 'flex';
        currentGroupId = null;
        // Refresh conversation list
        loadConversationsWithGroups();
    }
    
    currentConversationId = null;
    currentConversationGroupId = null; // The new conversation does not belong to any group
    document.getElementById('chat-messages').innerHTML = '';
    addMessage('assistant', 'The system is ready. Please enter your testing requirements and the system will automatically perform the corresponding security tests.');
    addAttackChainButton(null);
    updateActiveConversation();
    // Refresh the group list and clear group highlights
    await loadGroups();
    // Refresh the conversation list to ensure the latest historical conversations are displayed
    loadConversationsWithGroups();
    // Clear the anti-shake timer to prevent saving from triggering when resuming a draft
    if (draftSaveTimer) {
        clearTimeout(draftSaveTimer);
        draftSaveTimer = null;
    }
    // Clear drafts, new conversations should not restore previous drafts
    clearChatDraft();
    // Clear input box
    const chatInput = document.getElementById('chat-input');
    if (chatInput) {
        chatInput.value = '';
        adjustTextareaHeight(chatInput);
    }
}

// Load conversation list (grouped by time)
async function loadConversations(searchQuery = '') {
    try {
        let url = '/api/conversations?limit=50';
        if (searchQuery && searchQuery.trim()) {
            url += '&search=' + encodeURIComponent(searchQuery.trim());
        }
        const response = await apiFetch(url);

        const listContainer = document.getElementById('conversations-list');
        if (!listContainer) {
            return;
        }

        // Save scroll position
        const sidebarContent = listContainer.closest('.sidebar-content');
        const savedScrollTop = sidebarContent ? sidebarContent.scrollTop : 0;

        const emptyStateHtml = '<div style="padding: 20px; text-align: center; color: var(--text-muted); font-size: 0.875rem;">No historical conversations yet</div>';
        listContainer.innerHTML = '';

        // If the response is not 200, an empty status is displayed (friendly processing, no error is displayed)
        if (!response.ok) {
            listContainer.innerHTML = emptyStateHtml;
            return;
        }

        const conversations = await response.json();

        if (!Array.isArray(conversations) || conversations.length === 0) {
            listContainer.innerHTML = emptyStateHtml;
            return;
        }

        const now = new Date();
        const todayStart = new Date(now.getFullYear(), now.getMonth(), now.getDate());
        const weekday = todayStart.getDay() === 0 ? 7 : todayStart.getDay();
        const startOfWeek = new Date(todayStart);
        startOfWeek.setDate(todayStart.getDate() - (weekday - 1));
        const yesterdayStart = new Date(todayStart);
        yesterdayStart.setDate(todayStart.getDate() - 1);

        const groups = {
            today: [],
            yesterday: [],
            thisWeek: [],
            earlier: [],
        };

        conversations.forEach(conv => {
            const dateObj = conv.updatedAt ? new Date(conv.updatedAt) : new Date();
            const validDate = isNaN(dateObj.getTime()) ? new Date() : dateObj;
            const groupKey = getConversationGroup(validDate, todayStart, startOfWeek, yesterdayStart);
            groups[groupKey].push({
                ...conv,
                _time: validDate,
                _timeText: formatConversationTimestamp(validDate, todayStart, yesterdayStart),
            });
        });

        const groupOrder = [
            { key: 'today', label: 'Today' },
            { key: 'yesterday', label: 'Yesterday' },
            { key: 'thisWeek', label: 'This week' },
            { key: 'earlier', label: 'Earlier' },
        ];

        const fragment = document.createDocumentFragment();
        let rendered = false;

        groupOrder.forEach(({ key, label }) => {
            const items = groups[key];
            if (!items || items.length === 0) {
                return;
            }
            rendered = true;

            const section = document.createElement('div');
            section.className = 'conversation-group';

            const title = document.createElement('div');
            title.className = 'conversation-group-title';
            title.textContent = label;
            section.appendChild(title);

            items.forEach(itemData => {
                // Determine whether to pin it to the top
                const isPinned = itemData.pinned || false;
                section.appendChild(createConversationListItemWithMenu(itemData, isPinned));
            });

            fragment.appendChild(section);
        });

        if (!rendered) {
            listContainer.innerHTML = emptyStateHtml;
            return;
        }

        listContainer.appendChild(fragment);
        updateActiveConversation();
        
        // Restore scroll position
        if (sidebarContent) {
            // Use requestAnimationFrame to ensure the DOM has been updated
            requestAnimationFrame(() => {
                sidebarContent.scrollTop = savedScrollTop;
            });
        }
    } catch (error) {
        console.error('Failed to load conversation list:', error);
        // Display an empty status when an error occurs instead of an error message (more friendly user experience)
        const listContainer = document.getElementById('conversations-list');
        if (listContainer) {
            const emptyStateHtml = '<div style="padding: 20px; text-align: center; color: var(--text-muted); font-size: 0.875rem;">No historical conversations yet</div>';
            listContainer.innerHTML = emptyStateHtml;
        }
    }
}

function createConversationListItem(conversation) {
    const item = document.createElement('div');
    item.className = 'conversation-item';
    item.dataset.conversationId = conversation.id;
    if (conversation.id === currentConversationId) {
        item.classList.add('active');
    }

    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'conversation-content';

    const title = document.createElement('div');
    title.className = 'conversation-title';
    const titleText = conversation.title || 'Unnamed conversation';
    title.textContent = safeTruncateText(titleText, 60);
    title.title = titleText; // Set full title to view on hover
    contentWrapper.appendChild(title);

    const time = document.createElement('div');
    time.className = 'conversation-time';
    time.textContent = conversation._timeText || formatConversationTimestamp(conversation._time || new Date());
    contentWrapper.appendChild(time);

    item.appendChild(contentWrapper);

    const deleteBtn = document.createElement('button');
    deleteBtn.className = 'conversation-delete-btn';
    deleteBtn.innerHTML = `
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M3 6h18M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2m3 0v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6h14zM10 11v6M14 11v6" 
                  stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
    `;
    deleteBtn.title = 'Delete conversation';
    deleteBtn.onclick = (e) => {
        e.stopPropagation();
        deleteConversation(conversation.id);
    };
    item.appendChild(deleteBtn);

    item.onclick = (e) => {
        e.preventDefault();
        e.stopPropagation();
        loadConversation(conversation.id);
    };
    return item;
}

// Handling history searches
let conversationSearchTimer = null;
function handleConversationSearch(query) {
    // Anti-shake processing to avoid frequent requests
    if (conversationSearchTimer) {
        clearTimeout(conversationSearchTimer);
    }
    
    const searchInput = document.getElementById('conversation-search-input');
    const clearBtn = document.getElementById('conversation-search-clear');
    
    if (clearBtn) {
        if (query && query.trim()) {
            clearBtn.style.display = 'block';
        } else {
            clearBtn.style.display = 'none';
        }
    }
    
    conversationSearchTimer = setTimeout(() => {
        loadConversations(query);
    }, 300); // 300ms anti-shake delay
}

// Clear search
function clearConversationSearch() {
    const searchInput = document.getElementById('conversation-search-input');
    const clearBtn = document.getElementById('conversation-search-clear');
    
    if (searchInput) {
        searchInput.value = '';
    }
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    
    loadConversations('');
}

function formatConversationTimestamp(dateObj, todayStart, yesterdayStart) {
    if (!(dateObj instanceof Date) || isNaN(dateObj.getTime())) {
        return '';
    }
    // If todayStart is not passed in, the current date is used as a reference
    const now = new Date();
    const referenceToday = todayStart || new Date(now.getFullYear(), now.getMonth(), now.getDate());
    const referenceYesterday = yesterdayStart || new Date(referenceToday.getTime() - 24 * 60 * 60 * 1000);
    const messageDate = new Date(dateObj.getFullYear(), dateObj.getMonth(), dateObj.getDate());

    if (messageDate.getTime() === referenceToday.getTime()) {
        return dateObj.toLocaleTimeString('zh-CN', {
            hour: '2-digit',
            minute: '2-digit'
        });
    }
    if (messageDate.getTime() === referenceYesterday.getTime()) {
        return 'Yesterday' + dateObj.toLocaleTimeString('zh-CN', {
            hour: '2-digit',
            minute: '2-digit'
        });
    }
    if (dateObj.getFullYear() === referenceToday.getFullYear()) {
        return dateObj.toLocaleString('zh-CN', {
            month: 'short',
            day: 'numeric',
            hour: '2-digit',
            minute: '2-digit'
        });
    }
    return dateObj.toLocaleString('zh-CN', {
        year: 'numeric',
        month: 'short',
        day: 'numeric',
        hour: '2-digit',
        minute: '2-digit'
    });
}

function getConversationGroup(dateObj, todayStart, startOfWeek, yesterdayStart) {
    if (!(dateObj instanceof Date) || isNaN(dateObj.getTime())) {
        return 'earlier';
    }
    const today = new Date(todayStart.getFullYear(), todayStart.getMonth(), todayStart.getDate());
    const yesterday = new Date(yesterdayStart.getFullYear(), yesterdayStart.getMonth(), yesterdayStart.getDate());
    const messageDay = new Date(dateObj.getFullYear(), dateObj.getMonth(), dateObj.getDate());

    if (messageDay.getTime() === today.getTime() || messageDay > today) {
        return 'today';
    }
    if (messageDay.getTime() === yesterday.getTime()) {
        return 'yesterday';
    }
    if (messageDay >= startOfWeek && messageDay < today) {
        return 'thisWeek';
    }
    return 'earlier';
}

// Load conversation
async function loadConversation(conversationId) {
    try {
        const response = await apiFetch(`/api/conversations/${conversationId}`);
        const conversation = await response.json();
        
        if (!response.ok) {
            alert('Failed to load conversation:' + (conversation.error || 'Unknown error'));
            return;
        }
        
        // If you are currently on the group details page, switch to the conversation interface
        // Exit group details mode and display all recent conversations to provide a better user experience
        if (currentGroupId) {
            const sidebar = document.querySelector('.conversation-sidebar');
            const groupDetailPage = document.getElementById('group-detail-page');
            const chatContainer = document.querySelector('.chat-container');
            
            // Make sure the sidebar is always visible
            if (sidebar) sidebar.style.display = 'flex';
            // Hide the group details page and display the conversation interface
            if (groupDetailPage) groupDetailPage.style.display = 'none';
            if (chatContainer) chatContainer.style.display = 'flex';
            
            // Exit group details mode so the recent conversations list shows all conversations
            // Users can see all conversations in the sidebar for easy switching
            const previousGroupId = currentGroupId;
            currentGroupId = null;
            
            // Refresh the recent conversations list, showing all conversations (including those in groups)
            loadConversationsWithGroups();
        }
        
        // Get the group ID to which the current conversation belongs (used for highlighting)
        // Make sure the grouping map is loaded
        if (Object.keys(conversationGroupMappingCache).length === 0) {
            await loadConversationGroupMapping();
        }
        currentConversationGroupId = conversationGroupMappingCache[conversationId] || null;
        
        // Regardless of whether you are on the group details page, refresh the group list to ensure that the highlighting status is correct.
        // This can clear the highlight status of the previous group and ensure that the UI status is consistent.
        await loadGroups();
        
        // Update current conversation ID
        currentConversationId = conversationId;
        updateActiveConversation();
        
        // If the attack chain modal is open and is not showing the current dialog, close it
        const attackChainModal = document.getElementById('attack-chain-modal');
        if (attackChainModal && attackChainModal.style.display === 'block') {
            if (currentAttackChainConversationId !== conversationId) {
                closeAttackChainModal();
            }
        }
        
        // Clear message area
        const messagesDiv = document.getElementById('chat-messages');
        messagesDiv.innerHTML = '';
        
        // Check if there are recent messages in the conversation and if so, clear the draft (avoids restoring sent messages)
        let hasRecentUserMessage = false;
        if (conversation.messages && conversation.messages.length > 0) {
            const lastMessage = conversation.messages[conversation.messages.length - 1];
            if (lastMessage && lastMessage.role === 'user') {
                // Check the message time, if it is within the last 30 seconds, clear the draft
                const messageTime = new Date(lastMessage.createdAt);
                const now = new Date();
                const timeDiff = now.getTime() - messageTime.getTime();
                if (timeDiff < 30000) { // Within 30 seconds
                    hasRecentUserMessage = true;
                }
            }
        }
        if (hasRecentUserMessage) {
            // If there are recently sent user messages, clear the draft
            clearChatDraft();
            const chatInput = document.getElementById('chat-input');
            if (chatInput) {
                chatInput.value = '';
                adjustTextareaHeight(chatInput);
            }
        }
        
        // Load message
        if (conversation.messages && conversation.messages.length > 0) {
            conversation.messages.forEach(msg => {
                // Check whether the message content is "Processing...", if so, check whether there is an error or cancellation event in processDetails
                let displayContent = msg.content;
                if (msg.role === 'assistant' && msg.content === 'Processing...' && msg.processDetails && msg.processDetails.length > 0) {
                    // Find the last error or canceled event
                    for (let i = msg.processDetails.length - 1; i >= 0; i--) {
                        const detail = msg.processDetails[i];
                        if (detail.eventType === 'error' || detail.eventType === 'cancelled') {
                            displayContent = detail.message || msg.content;
                            break;
                        }
                    }
                }
                
                // The creation time of the delivered message
                const messageId = addMessage(msg.role, displayContent, msg.mcpExecutionIds || [], null, msg.createdAt);
                // For helper messages, always render process details (show expand details button even without processDetails)
                if (msg.role === 'assistant') {
                    // Delay to make sure the message has been rendered
                    setTimeout(() => {
                        renderProcessDetails(messageId, msg.processDetails || []);
                        // If there are process details, check if there are any errors or cancellation events, and if so, make sure the details are collapsed by default
                        if (msg.processDetails && msg.processDetails.length > 0) {
                            const hasErrorOrCancelled = msg.processDetails.some(d => 
                                d.eventType === 'error' || d.eventType === 'cancelled'
                            );
                            if (hasErrorOrCancelled) {
                                collapseAllProgressDetails(messageId, null);
                            }
                        }
                    }, 100);
                }
            });
        } else {
            addMessage('assistant', 'The system is ready. Please enter your testing requirements and the system will automatically perform the corresponding security tests.');
        }
        
        // Scroll to bottom
        messagesDiv.scrollTop = messagesDiv.scrollHeight;
        
        // Add attack chain button
        addAttackChainButton(conversationId);
        
        // Refresh conversation list
        loadConversations();
    } catch (error) {
        console.error('Failed to load conversation:', error);
        alert('Failed to load conversation:' + error.message);
    }
}

// Delete conversation
async function deleteConversation(conversationId, skipConfirm = false) {
    // Confirm deletion (if the caller did not skip confirmation)
    if (!skipConfirm) {
        if (!confirm('Are you sure you want to delete this conversation? This operation is irreversible.')) {
            return;
        }
    }
    
    try {
        const response = await apiFetch(`/api/conversations/${conversationId}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Delete failed');
        }
        
        // If the current conversation is deleted, the conversation interface will be cleared.
        if (conversationId === currentConversationId) {
            currentConversationId = null;
            document.getElementById('chat-messages').innerHTML = '';
            addMessage('assistant', 'The system is ready. Please enter your testing requirements and the system will automatically perform the corresponding security tests.');
            addAttackChainButton(null);
        }
        
        // Update cache - delete immediately to ensure correct recognition during subsequent loads
        delete conversationGroupMappingCache[conversationId];
        // Also removed from the mapping to be retained
        delete pendingGroupMappings[conversationId];
        
        // If you are currently on the group details page, reload the group conversation
        if (currentGroupId) {
            await loadGroupConversations(currentGroupId);
        }
        
        // Refresh conversation list
        loadConversations();
    } catch (error) {
        console.error('Failed to delete conversation:', error);
        alert('Failed to delete conversation:' + error.message);
    }
}

// Update active conversation style
function updateActiveConversation() {
    document.querySelectorAll('.conversation-item').forEach(item => {
        item.classList.remove('active');
        if (currentConversationId && item.dataset.conversationId === currentConversationId) {
            item.classList.add('active');
        }
    });
}

// ==================== Attack chain visualization function ====================

let attackChainCytoscape = null;
let currentAttackChainConversationId = null;
// Manage loading status by conversation ID to achieve decoupling between different conversations
const attackChainLoadingMap = new Map(); // Map<conversationId, boolean>

// Checks whether the specified conversation is loading
function isAttackChainLoading(conversationId) {
    return attackChainLoadingMap.get(conversationId) === true;
}

// Sets the loading status of the specified conversation
function setAttackChainLoading(conversationId, loading) {
    if (loading) {
        attackChainLoadingMap.set(conversationId, true);
    } else {
        attackChainLoadingMap.delete(conversationId);
    }
}

// Added attack chain button (moved to menu, this function remains for compatibility, but top button is no longer shown)
function addAttackChainButton(conversationId) {
    // The attack chain button has been moved to the three-dot menu and the top button no longer needs to be displayed
    // This function is retained for code compatibility, but no longer does anything
    const conversationHeader = document.getElementById('conversation-header');
    if (conversationHeader) {
        conversationHeader.style.display = 'none';
    }
}

function updateAttackChainAvailability() {
    addAttackChainButton(currentConversationId);
}

// Show attack chain modal box
async function showAttackChain(conversationId) {
    // Allow opening if currently displayed conversation ID is different or not loading
    // Also allowed to open if the same conversation is loading (shows loading status)
    if (isAttackChainLoading(conversationId) && currentAttackChainConversationId === conversationId) {
        // If the modal box is already open and displays the same dialog, it will not be opened again.
        const modal = document.getElementById('attack-chain-modal');
        if (modal && modal.style.display === 'block') {
            console.log('The attack chain is loading and the modal box is open');
            return;
        }
    }
    
    currentAttackChainConversationId = conversationId;
    const modal = document.getElementById('attack-chain-modal');
    if (!modal) {
        console.error('Attack chain modal box not found');
        return;
    }
    
    modal.style.display = 'block';
    
    // Empty container
    const container = document.getElementById('attack-chain-container');
    if (container) {
        container.innerHTML = '<div class="loading-spinner">Loading...</div>';
    }
    
    // Hide details panel
    const detailsPanel = document.getElementById('attack-chain-details');
    if (detailsPanel) {
        detailsPanel.style.display = 'none';
    }
    
    // Disable regenerate button
    const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
    if (regenerateBtn) {
        regenerateBtn.disabled = true;
        regenerateBtn.style.opacity = '0.5';
        regenerateBtn.style.cursor = 'not-allowed';
    }
    
    // Load attack chain data
    await loadAttackChain(conversationId);
}

// Load attack chain data
async function loadAttackChain(conversationId) {
    if (isAttackChainLoading(conversationId)) {
        return; // Prevent repeated calls
    }
    
    setAttackChainLoading(conversationId, true);
    
    try {
        const response = await apiFetch(`/api/attack-chain/${conversationId}`);
        
        if (!response.ok) {
            // Handling 409 Conflict (under construction)
            if (response.status === 409) {
                const error = await response.json();
                const container = document.getElementById('attack-chain-container');
                if (container) {
                    container.innerHTML = `
                        <div style="text-align: center; padding: 28px 24px; color: var(--text-secondary);">
                            <div style="display: inline-flex; align-items: center; gap: 8px; font-size: 0.95rem; color: var(--text-primary);">
                                <span role="presentation" aria-hidden="true">⏳</span>
                                <span>The attack chain is being generated, please wait.</span>
                            </div>
                            <button class="btn-secondary" onclick="refreshAttackChain()" style="margin-top: 12px; font-size: 0.78rem; padding: 4px 12px;">
Refresh
                            </button>
                        </div>
                    `;
                }
                // Automatically refresh after 5 seconds (allow refresh, but keep loading to prevent repeated clicks)
                // Use closure to save conversationId to prevent serialization
                setTimeout(() => {
                    // Check if the currently displayed conversation ID matches
                    if (currentAttackChainConversationId === conversationId) {
                        refreshAttackChain();
                    }
                }, 5000);
                // In 409 cases, keep loading to prevent repeated clicks
                // But allow refreshAttackChain to call loadAttackChain to check the status
                // Note: Do not reset the loading status, keep the loading status
                // Restore button state (while keeping the loading state, but allowing the user to refresh manually)
                const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
                if (regenerateBtn) {
                    regenerateBtn.disabled = false;
                    regenerateBtn.style.opacity = '1';
                    regenerateBtn.style.cursor = 'pointer';
                }
                return; // Return early and do not execute setAttackChainLoading(conversationId, false) in the finally block
            }
            
            const error = await response.json();
            throw new Error(error.error || 'Failed to load attack chain');
        }
        
        const chainData = await response.json();
        
        // Check whether the currently displayed conversation ID matches to prevent cross-talk
        if (currentAttackChainConversationId !== conversationId) {
            console.log('The attack chain data has been returned, but the currently displayed dialogue has been switched. Ignore this rendering.', {
                returned: conversationId,
                current: currentAttackChainConversationId
            });
            setAttackChainLoading(conversationId, false);
            return;
        }
        
        // Render attack chain
        renderAttackChain(chainData);
        
        // Update statistics
        updateAttackChainStats(chainData);
        
        // After successful loading, reset the loading status
        setAttackChainLoading(conversationId, false);
        
    } catch (error) {
        console.error('Failed to load attack chain:', error);
        const container = document.getElementById('attack-chain-container');
        if (container) {
            container.innerHTML = `<div class="error-message">Loading failed: ${error.message}</div>`;
        }
        // Also reset loading status on error
        setAttackChainLoading(conversationId, false);
    } finally {
        // Resume regenerate button
        const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
        if (regenerateBtn) {
            regenerateBtn.disabled = false;
            regenerateBtn.style.opacity = '1';
            regenerateBtn.style.cursor = 'pointer';
        }
    }
}

// Render attack chain
function renderAttackChain(chainData) {
    const container = document.getElementById('attack-chain-container');
    if (!container) {
        return;
    }
    
    // Empty container
    container.innerHTML = '';
    
    if (!chainData.nodes || chainData.nodes.length === 0) {
        container.innerHTML = '<div class="empty-message">No attack chain data yet</div>';
        return;
    }
    
    // Computational graph complexity (for dynamically adjusting layout and style)
    const nodeCount = chainData.nodes.length;
    const edgeCount = chainData.edges.length;
    const isComplexGraph = nodeCount > 15 || edgeCount > 25;
    
    // Optimize node labels: smart truncation and line wrapping
    chainData.nodes.forEach(node => {
        if (node.label) {
            // Smart truncation: Prioritize truncation at punctuation marks and spaces
            const maxLength = isComplexGraph ? 18 : 22;
            if (node.label.length > maxLength) {
                let truncated = node.label.substring(0, maxLength);
                // Try truncation at last punctuation or space
                const lastPunct = Math.max(
                    truncated.lastIndexOf('，'),
                    truncated.lastIndexOf('。'),
                    truncated.lastIndexOf('、'),
                    truncated.lastIndexOf(' '),
                    truncated.lastIndexOf('/')
                );
                if (lastPunct > maxLength * 0.6) { // If punctuation marks are well placed
                    truncated = truncated.substring(0, lastPunct + 1);
                }
                node.label = truncated + '...';
            }
        }
    });
    
    // Preparing Cytoscape data
    const elements = [];
    
    // Add nodes and precompute text and border colors while preparing data for type labels
    chainData.nodes.forEach(node => {
        const riskScore = node.risk_score || 0;
        const nodeType = node.type || '';
        
        // Set type label text and identifier based on node type (uses more modern design)
        let typeLabel = '';
        let typeBadge = '';
        let typeColor = '';
        if (nodeType === 'target') {
            typeLabel = 'Target';
            typeBadge = '○';  // Use hollow circles, more modern
            typeColor = '#1976d2';  // Blue
        } else if (nodeType === 'action') {
            typeLabel = 'Action';
            typeBadge = '▷';  // Use simpler triangles
            typeColor = '#f57c00';  // Orange color
        } else if (nodeType === 'vulnerability') {
            typeLabel = 'Loopholes';
            typeBadge = '◇';  // Use hollow diamonds to be more refined
            typeColor = '#d32f2f';  // Red
        } else {
            typeLabel = nodeType;
            typeBadge = '•';
            typeColor = '#666';
        }
        
        // Calculate text color and border color based on risk score
        let textColor, borderColor, textOutlineWidth, textOutlineColor;
        if (riskScore >= 80) {
            // Red background: white text, white border
            textColor = '#fff';
            borderColor = '#fff';
            textOutlineWidth = 1;
            textOutlineColor = '#333';
        } else if (riskScore >= 60) {
            // Orange background: white text, white border
            textColor = '#fff';
            borderColor = '#fff';
            textOutlineWidth = 1;
            textOutlineColor = '#333';
        } else if (riskScore >= 40) {
            // Yellow background: dark text, dark border
            textColor = '#333';
            borderColor = '#cc9900';
            textOutlineWidth = 2;
            textOutlineColor = '#fff';
        } else {
            // Green background: dark green text, dark border
            textColor = '#1a5a1a';
            borderColor = '#5a8a5a';
            textOutlineWidth = 2;
            textOutlineColor = '#fff';
        }
        
        // Save node data, using original tags (type tags will be added to the style)
        elements.push({
            data: {
                id: node.id,
                label: node.label,  // Original tag
                originalLabel: node.label,  // Save original tags for searching
                type: nodeType,
                typeLabel: typeLabel,  // Save type label text
                typeBadge: typeBadge,  // Save type identifier
                typeColor: typeColor,  // Save type color
                riskScore: riskScore,
                toolExecutionId: node.tool_execution_id || '',
                metadata: node.metadata || {},
                textColor: textColor,
                borderColor: borderColor,
                textOutlineWidth: textOutlineWidth,
                textOutlineColor: textOutlineColor
            }
        });
    });
    
    // Add edges (only add edges that exist in both source and target nodes)
    const nodeIds = new Set(chainData.nodes.map(node => node.id));
    
    // Save valid edges for ELK layout
    const validEdges = [];
    chainData.edges.forEach(edge => {
        // Verify that the source and destination nodes exist
        if (nodeIds.has(edge.source) && nodeIds.has(edge.target)) {
            validEdges.push(edge);
            elements.push({
                data: {
                    id: edge.id,
                    source: edge.source,
                    target: edge.target,
                    type: edge.type || 'leads_to',
                    weight: edge.weight || 1
                }
            });
        } else {
            console.warn('Skip invalid edges: source node or target node does not exist', {
                edgeId: edge.id,
                source: edge.source,
                target: edge.target,
                sourceExists: nodeIds.has(edge.source),
                targetExists: nodeIds.has(edge.target)
            });
        }
    });
    
    // Initialize Cytoscape
    attackChainCytoscape = cytoscape({
        container: container,
        elements: elements,
        style: [
            {
                selector: 'node',
                style: {
                    // Reference Figure 2: Modern card design, clear visual hierarchy
                    'label': function(ele) {
                        const typeLabel = ele.data('typeLabel') || '';
                        const label = ele.data('label') || '';
                        // Concise two-line display: type tag + content
                        return typeLabel + '\n' + label;
                    },
                    // Reasonable node size, refer to Figure 2
                    'width': function(ele) {
                        const type = ele.data('type');
                        if (type === 'target') return isComplexGraph ? 280 : 320;
                        if (type === 'vulnerability') return isComplexGraph ? 260 : 300;
                        return isComplexGraph ? 240 : 280;
                    },
                    'height': function(ele) {
                        const type = ele.data('type');
                        if (type === 'target') return isComplexGraph ? 100 : 120;
                        if (type === 'vulnerability') return isComplexGraph ? 90 : 110;
                        return isComplexGraph ? 80 : 100;
                    },
                    'shape': 'round-rectangle',
                    // Modern background: white card + colored bar on the left
                    'background-color': '#FFFFFF',
                    'background-opacity': 1,
                    // Color bar effect on the left (implemented through the border)
                    'border-width': function(ele) {
                        const type = ele.data('type');
                        return 0;  // No borders, use background color blocks
                    },
                    'border-color': 'transparent',
                    // Text style: clear and easy to read
                    'color': '#2C3E50',  // Dark blue-grey, professional feel
                    'font-size': function(ele) {
                        const type = ele.data('type');
                        if (type === 'target') return isComplexGraph ? '14px' : '16px';
                        if (type === 'vulnerability') return isComplexGraph ? '13px' : '15px';
                        return isComplexGraph ? '13px' : '15px';
                    },
                    'font-weight': '600',  // Medium bold
                    'font-family': '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, "Microsoft YaHei", sans-serif',
                    'text-valign': 'center',
                    'text-halign': 'center',
                    'text-wrap': 'wrap',
                    'text-max-width': function(ele) {
                        const type = ele.data('type');
                        if (type === 'target') return isComplexGraph ? '240px' : '280px';
                        if (type === 'vulnerability') return isComplexGraph ? '220px' : '260px';
                        return isComplexGraph ? '200px' : '240px';
                    },
                    'text-overflow-wrap': 'anywhere',
                    'text-margin-y': 4,
                    'padding': '12px 16px',  // Reasonable padding
                    'line-height': 1.5,
                    'text-outline-width': 0
                }
            },
            {
                // Target node: blue theme
                selector: 'node[type = "target"]',
                style: {
                    'background-color': '#E3F2FD',
                    'color': '#1565C0',
                    'border-width': 3,
                    'border-color': '#2196F3',
                    'border-style': 'solid'
                }
            },
            {
                // Action nodes: display different colors according to status
                selector: 'node[type = "action"]',
                style: {
                    'background-color': function(ele) {
                        const metadata = ele.data('metadata') || {};
                        const findings = metadata.findings || [];
                        const status = metadata.status || '';
                        const hasFindings = Array.isArray(findings) && findings.length > 0;
                        const isFailedInsight = status === 'failed_insight';
                        
                        if (hasFindings && !isFailedInsight) {
                            return '#E8F5E9';  // Light green background
                        } else {
                            return '#F5F5F5';  // Light gray background
                        }
                    },
                    'color': '#424242',
                    'border-width': 2,
                    'border-color': function(ele) {
                        const metadata = ele.data('metadata') || {};
                        const findings = metadata.findings || [];
                        const status = metadata.status || '';
                        const hasFindings = Array.isArray(findings) && findings.length > 0;
                        const isFailedInsight = status === 'failed_insight';
                        
                        if (hasFindings && !isFailedInsight) {
                            return '#4CAF50';  // Green border
                        } else {
                            return '#9E9E9E';  // Gray border
                        }
                    },
                    'border-style': 'solid'
                }
            },
            {
                // Vulnerability nodes: displayed in color based on risk level
                selector: 'node[type = "vulnerability"]',
                style: {
                    'background-color': function(ele) {
                        const riskScore = ele.data('riskScore') || 0;
                        if (riskScore >= 80) return '#FFEBEE';
                        if (riskScore >= 60) return '#FFF3E0';
                        if (riskScore >= 40) return '#FFFDE7';
                        return '#E8F5E9';
                    },
                    'color': function(ele) {
                        const riskScore = ele.data('riskScore') || 0;
                        if (riskScore >= 80) return '#C62828';
                        if (riskScore >= 60) return '#E65100';
                        if (riskScore >= 40) return '#F57C00';
                        return '#2E7D32';
                    },
                    'border-width': 3,
                    'border-color': function(ele) {
                        const riskScore = ele.data('riskScore') || 0;
                        if (riskScore >= 80) return '#F44336';
                        if (riskScore >= 60) return '#FF9800';
                        if (riskScore >= 40) return '#FFC107';
                        return '#4CAF50';
                    },
                    'border-style': 'solid'
                }
            },
            {
                selector: 'edge',
                style: {
                    // Reference Figure 2: Simple and clear connection lines
                    'width': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers') return 2.5;  // The edge where the vulnerability is found is slightly thicker
                        if (type === 'enables') return 2.5;  // The enabling relationship is slightly thicker
                        return 2;  // Normal side
                    },
                    'line-color': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers') return '#42A5F5';  // Blue
                        if (type === 'targets') return '#42A5F5';  // Blue
                        if (type === 'enables') return '#EF5350';  // Red
                        if (type === 'leads_to') return '#90A4AE';  // Gray blue
                        return '#B0BEC5';
                    },
                    'target-arrow-color': function(ele) {
                        const type = ele.data('type');
                        if (type === 'discovers') return '#42A5F5';
                        if (type === 'targets') return '#42A5F5';
                        if (type === 'enables') return '#EF5350';
                        if (type === 'leads_to') return '#90A4AE';
                        return '#B0BEC5';
                    },
                    'target-arrow-shape': 'triangle',
                    'arrow-scale': 1.2,  // Moderate arrow size
                    'curve-style': 'straight',
                    'opacity': 0.7,  // Moderate opacity
                    'line-style': function(ele) {
                        const type = ele.data('type');
                        if (type === 'targets') return 'dashed';
                        return 'solid';
                    },
                    'line-dash-pattern': function(ele) {
                        const type = ele.data('type');
                        if (type === 'targets') return [8, 4];
                        return [];
                    }
                }
            },
            {
                selector: 'node:selected',
                style: {
                    'border-width': 5,
                    'border-color': '#0066ff',
                    'z-index': 999,
                    'opacity': 1,
                    'overlay-opacity': 0.1,
                    'overlay-color': '#0066ff'
                }
            }
        ],
        userPanningEnabled: true,
        userZoomingEnabled: true,
        boxSelectionEnabled: true
    });
    
    // Use ELK layout (high quality DAG layout, reduce edge crossing)
    let layoutOptions = {
        name: 'breadthfirst',
        directed: true,
        spacingFactor: isComplexGraph ? 3.0 : 2.5,
        padding: 40
    };
    
    // Using ELK.js for layout calculations
    // Elk.bundled.js will expose the ELK object, you can use new ELK() directly
    let elkInstance = null;
    if (typeof ELK !== 'undefined') {
        try {
            elkInstance = new ELK();
        } catch (e) {
            console.warn('ELK initialization failed:', e);
        }
    }
    
    if (elkInstance) {
        try {
            
            // Build ELK graph structure
            const elkGraph = {
                id: 'root',
                layoutOptions: {
                    'elk.algorithm': 'layered',
                    'elk.direction': 'DOWN',
                    'elk.spacing.nodeNode': String(isComplexGraph ? 100 : 120),  // Reasonable node spacing
                    'elk.spacing.edgeNode': '50',  // Reasonable edge-to-node spacing
                    'elk.spacing.edgeEdge': '25',  // Reasonable edge spacing
                    'elk.layered.spacing.nodeNodeBetweenLayers': String(isComplexGraph ? 150 : 180),  // Reasonable level spacing
                    'elk.layered.nodePlacement.strategy': 'SIMPLE',  // Use simple strategies to make the layout more dispersed
                    'elk.layered.crossingMinimization.strategy': 'INTERACTIVE',  // Interactive crossover minimization
                    'elk.layered.thoroughness': '10',  // Highest level of optimization
                    'elk.layered.spacing.edgeNodeBetweenLayers': '50',
                    'elk.layered.nodePlacement.strategy': 'BRANDES_KOEPF',
                    'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
                    'elk.layered.crossingMinimization.forceNodeModelOrder': 'true',
                    'elk.layered.cycleBreaking.strategy': 'GREEDY',
                    'elk.layered.thoroughness': '7',
                    'elk.padding': '[top=60,left=100,bottom=60,right=100]',  // Larger left and right margins make the image more spread out
                    'elk.spacing.componentComponent': String(isComplexGraph ? 100 : 120)  // Component spacing
                },
                children: chainData.nodes.map(node => {
                    const type = node.type || '';
                    return {
                        id: node.id,
                        width: type === 'target' ? (isComplexGraph ? 280 : 320) : 
                               type === 'vulnerability' ? (isComplexGraph ? 260 : 300) : 
                               (isComplexGraph ? 240 : 280),
                        height: type === 'target' ? (isComplexGraph ? 100 : 120) : 
                                type === 'vulnerability' ? (isComplexGraph ? 90 : 110) : 
                                (isComplexGraph ? 80 : 100)
                    };
                }),
                edges: validEdges.map(edge => ({
                    id: edge.id,
                    sources: [edge.source],
                    targets: [edge.target]
                }))
            };
            
            // Calculate layout using ELK
            elkInstance.layout(elkGraph).then(laidOutGraph => {
                // Apply ELK computed layout to Cytoscape nodes
                if (laidOutGraph && laidOutGraph.children) {
                    laidOutGraph.children.forEach(elkNode => {
                        const cyNode = attackChainCytoscape.getElementById(elkNode.id);
                        if (cyNode && elkNode.x !== undefined && elkNode.y !== undefined) {
                            cyNode.position({
                                x: elkNode.x + (elkNode.width || 0) / 2,
                                y: elkNode.y + (elkNode.height || 0) / 2
                            });
                        }
                    });
                    
                    // After the layout is completed, display the image in the center
                    setTimeout(() => {
                        centerAttackChain();
                    }, 150);
                } else {
                    throw new Error('ELK layout returns invalid results');
                }
            }).catch(err => {
                console.warn('ELK layout calculation failed, using default layout:', err);
                // Fall back to default layout
                const layout = attackChainCytoscape.layout(layoutOptions);
                layout.one('layoutstop', () => {
                    setTimeout(() => {
                        centerAttackChain();
                    }, 100);
                });
                layout.run();
            });
        } catch (e) {
            console.warn('ELK layout initialization failed, using default layout:', e);
            // Fall back to default layout
            const layout = attackChainCytoscape.layout(layoutOptions);
            layout.one('layoutstop', () => {
                setTimeout(() => {
                    centerAttackChain();
                }, 100);
            });
            layout.run();
        }
    } else {
        console.warn('ELK.js is not loaded, default layout is used. Please check whether the elkjs library is loaded correctly.');
        // Use default layout
        const layout = attackChainCytoscape.layout(layoutOptions);
        layout.one('layoutstop', () => {
            setTimeout(() => {
                centerAttackChain();
            }, 100);
        });
        layout.run();
    }
    
    // Function to center the attack chain
    function centerAttackChain() {
        try {
            if (!attackChainCytoscape) {
                return;
            }
            
            const container = attackChainCytoscape.container();
            if (!container) {
                return;
            }
            
            const containerWidth = container.offsetWidth;
            const containerHeight = container.offsetHeight;
            
            if (containerWidth === 0 || containerHeight === 0) {
                // If container size is 0, delay retry
                setTimeout(centerAttackChain, 100);
                return;
            }
            
            // Center the graph while maintaining reasonable scaling
            const padding = 80;  // Margin
            attackChainCytoscape.fit(undefined, padding);
            
            // Wait for fit to complete before adjusting
            setTimeout(() => {
                const extent = attackChainCytoscape.extent();
                if (!extent || typeof extent.x1 === 'undefined' || typeof extent.x2 === 'undefined' || 
                    typeof extent.y1 === 'undefined' || typeof extent.y2 === 'undefined') {
                    return;
                }
                
                const graphWidth = extent.x2 - extent.x1;
                const graphHeight = extent.y2 - extent.y1;
                const currentZoom = attackChainCytoscape.zoom();
                
                // If the picture is too small, enlarge it appropriately
                const availableWidth = containerWidth - padding * 2;
                const availableHeight = containerHeight - padding * 2;
                const widthScale = graphWidth > 0 ? availableWidth / (graphWidth * currentZoom) : 1;
                const heightScale = graphHeight > 0 ? availableHeight / (graphHeight * currentZoom) : 1;
                const scale = Math.min(widthScale, heightScale);
                
                // Only adjust zoom within a reasonable range (0.8-1.3x)
                if (scale > 1 && scale < 1.3) {
                    attackChainCytoscape.zoom(currentZoom * scale);
                } else if (scale < 0.8) {
                    attackChainCytoscape.zoom(currentZoom * 0.8);
                }
                
                // Make sure the image is centered
                const graphCenterX = (extent.x1 + extent.x2) / 2;
                const graphCenterY = (extent.y1 + extent.y2) / 2;
                const zoom = attackChainCytoscape.zoom();
                const pan = attackChainCytoscape.pan();
                
                const graphCenterViewX = graphCenterX * zoom + pan.x;
                const graphCenterViewY = graphCenterY * zoom + pan.y;
                
                const desiredViewX = containerWidth / 2;
                const desiredViewY = containerHeight / 2;
                
                const deltaX = desiredViewX - graphCenterViewX;
                const deltaY = desiredViewY - graphCenterViewY;
                
                attackChainCytoscape.pan({
                    x: pan.x + deltaX,
                    y: pan.y + deltaY
                });
            }, 100);
        } catch (error) {
            console.warn('Error while centering chart:', error);
        }
    }
    
    // Add click event
    attackChainCytoscape.on('tap', 'node', function(evt) {
        const node = evt.target;
        showNodeDetails(node.data());
    });
    
    // Add hover effects (use event listeners instead of CSS selectors)
    attackChainCytoscape.on('mouseover', 'node', function(evt) {
        const node = evt.target;
        node.style('border-width', 5);
        node.style('z-index', 998);
        node.style('overlay-opacity', 0.05);
        node.style('overlay-color', '#333333');
    });
    
    attackChainCytoscape.on('mouseout', 'node', function(evt) {
        const node = evt.target;
        const type = node.data('type');
        // Restore default border width
        const defaultBorderWidth = type === 'target' ? 5 : 4;
        node.style('border-width', defaultBorderWidth);
        node.style('z-index', 'auto');
        node.style('overlay-opacity', 0);
    });
    
    // Save raw data for filtering
    window.attackChainOriginalData = chainData;
}

// Safely get the source and target nodes of an edge
function getEdgeNodes(edge) {
    try {
        const source = edge.source();
        const target = edge.target();
        
        // Check if the source node and target node exist
        if (!source || !target || source.length === 0 || target.length === 0) {
            return { source: null, target: null, valid: false };
        }
        
        return { source: source, target: target, valid: true };
    } catch (error) {
        console.warn('An error occurred while getting the nodes of the edge:', error, edge.id());
        return { source: null, target: null, valid: false };
    }
}

// Filter attack chain nodes (by search keywords)
function filterAttackChainNodes(searchText) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }
    
    const searchLower = searchText.toLowerCase().trim();
    if (searchLower === '') {
        // Reset all node visibility
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        // Restore default borders
        attackChainCytoscape.nodes().style('border-width', 2);
        return;
    }
    
    // Filter nodes
    attackChainCytoscape.nodes().forEach(node => {
        // Search using original tags, excluding type tags
        const originalLabel = node.data('originalLabel') || node.data('label') || '';
        const label = originalLabel.toLowerCase();
        const type = (node.data('type') || '').toLowerCase();
        const matches = label.includes(searchLower) || type.includes(searchLower);
        
        if (matches) {
            node.style('display', 'element');
            // Highlight matching nodes
            node.style('border-width', 4);
            node.style('border-color', '#0066ff');
        } else {
            node.style('display', 'none');
        }
    });
    
    // Hide edges that have no visible source or target nodes
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }
        
        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });
    
    // Resize view
    attackChainCytoscape.fit(undefined, 60);
}

// Filter attack chain nodes by type
function filterAttackChainByType(type) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }
    
    if (type === 'all') {
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.nodes().style('border-width', 2);
        attackChainCytoscape.fit(undefined, 60);
        return;
    }
    
    // Filter nodes
    attackChainCytoscape.nodes().forEach(node => {
        const nodeType = node.data('type') || '';
        if (nodeType === type) {
            node.style('display', 'element');
        } else {
            node.style('display', 'none');
        }
    });
    
    // Hide edges that have no visible source or target nodes
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }
        
        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });
    
    // Resize view
    attackChainCytoscape.fit(undefined, 60);
}

// Filter attack chain nodes by risk level
function filterAttackChainByRisk(riskLevel) {
    if (!attackChainCytoscape || !window.attackChainOriginalData) {
        return;
    }
    
    if (riskLevel === 'all') {
        attackChainCytoscape.nodes().style('display', 'element');
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.nodes().style('border-width', 2);
        attackChainCytoscape.fit(undefined, 60);
        return;
    }
    
    // Define risk scope
    const riskRanges = {
        'high': [80, 100],
        'medium-high': [60, 79],
        'medium': [40, 59],
        'low': [0, 39]
    };
    
    const [minRisk, maxRisk] = riskRanges[riskLevel] || [0, 100];
    
    // Filter nodes
    attackChainCytoscape.nodes().forEach(node => {
        const riskScore = node.data('riskScore') || 0;
        if (riskScore >= minRisk && riskScore <= maxRisk) {
            node.style('display', 'element');
        } else {
            node.style('display', 'none');
        }
    });
    
    // Hide edges that have no visible source or target nodes
    attackChainCytoscape.edges().forEach(edge => {
        const { source, target, valid } = getEdgeNodes(edge);
        if (!valid) {
            edge.style('display', 'none');
            return;
        }
        
        const sourceVisible = source.style('display') !== 'none';
        const targetVisible = target.style('display') !== 'none';
        if (sourceVisible && targetVisible) {
            edge.style('display', 'element');
        } else {
            edge.style('display', 'none');
        }
    });
    
    // Resize view
    attackChainCytoscape.fit(undefined, 60);
}

// Reset attack chain filtering
function resetAttackChainFilters() {
    // Reset search box
    const searchInput = document.getElementById('attack-chain-search');
    if (searchInput) {
        searchInput.value = '';
    }
    
    // Reset type filter
    const typeFilter = document.getElementById('attack-chain-type-filter');
    if (typeFilter) {
        typeFilter.value = 'all';
    }
    
    // Reset risk screening
    const riskFilter = document.getElementById('attack-chain-risk-filter');
    if (riskFilter) {
        riskFilter.value = 'all';
    }
    
    // Reset all node visibility
    if (attackChainCytoscape) {
        attackChainCytoscape.nodes().forEach(node => {
            node.style('display', 'element');
            node.style('border-width', 2); // Restore default borders
        });
        attackChainCytoscape.edges().style('display', 'element');
        attackChainCytoscape.fit(undefined, 60);
    }
}

// Show node details
function showNodeDetails(nodeData) {
    const detailsPanel = document.getElementById('attack-chain-details');
    const detailsContent = document.getElementById('attack-chain-details-content');
    
    if (!detailsPanel || !detailsContent) {
        return;
    }
    
    // Use requestAnimationFrame to optimize display animations
    requestAnimationFrame(() => {
        detailsPanel.style.display = 'flex';
        // Set transparency on the next frame to ensure smooth display animation
        requestAnimationFrame(() => {
            detailsPanel.style.opacity = '1';
        });
    });
    
    let html = `
        <div class="node-detail-item">
            <strong>Node ID:</strong> <code>${nodeData.id}</code>
        </div>
        <div class="node-detail-item">
            <strong>Type:</strong> ${getNodeTypeLabel(nodeData.type)}
        </div>
        <div class="node-detail-item">
            <strong>Label:</strong> ${escapeHtml(nodeData.originalLabel || nodeData.label)}
        </div>
        <div class="node-detail-item">
            <strong>Risk score:</strong> ${nodeData.riskScore}/100
        </div>
    `;
    
    // Display action node information (tool execution + AI analysis)
    if (nodeData.type === 'action' && nodeData.metadata) {
        if (nodeData.metadata.tool_name) {
            html += `
                <div class="node-detail-item">
                    <strong>Tool name:</strong> <code>${escapeHtml(nodeData.metadata.tool_name)}</code>
                </div>
            `;
        }
        if (nodeData.metadata.tool_intent) {
            html += `
                <div class="node-detail-item">
                    <strong>Tool intent:</strong> <span style="color: #0066ff; font-weight: bold;">${escapeHtml(nodeData.metadata.tool_intent)}</span>
                </div>
            `;
        }
        if (nodeData.metadata.status === 'failed_insight') {
            html += `
                <div class="node-detail-item">
                    <strong>Execution status:</strong> <span style="color: # Ff9800; font-weight: bold;">Failed but there are clues</span>
                </div>
            `;
        }
        if (nodeData.metadata.ai_analysis) {
            html += `
                <div class="node-detail-item">
                    <strong>AI analysis:</strong> <div style="margin-top: 5px; padding: 8px; background: #f5f5f5; border-radius: 4px;">${escapeHtml(nodeData.metadata.ai_analysis)}</div>
                </div>
            `;
        }
        if (nodeData.metadata.findings && Array.isArray(nodeData.metadata.findings) && nodeData.metadata.findings.length > 0) {
            html += `
                <div class="node-detail-item">
                    <strong>Key findings:</strong>
                    <ul style="margin: 5px 0; padding-left: 20px;">
                        ${nodeData.metadata.findings.map(f => `<li>${escapeHtml(f)}</li>`).join('')}
                    </ul>
                </div>
            `;
        }
    }
    
    // Display target information (if it is a target node)
    if (nodeData.type === 'target' && nodeData.metadata && nodeData.metadata.target) {
        html += `
            <div class="node-detail-item">
                <strong>Test objectives:</strong> <code>${escapeHtml(nodeData.metadata.target)}</code>
            </div>
        `;
    }
    
    // Display vulnerability information (if it is a vulnerable node)
    if (nodeData.type === 'vulnerability' && nodeData.metadata) {
        if (nodeData.metadata.vulnerability_type) {
            html += `
                <div class="node-detail-item">
                    <strong>Vulnerability type:</strong> ${escapeHtml(nodeData.metadata.vulnerability_type)}
                </div>
            `;
        }
        if (nodeData.metadata.description) {
            html += `
                <div class="node-detail-item">
                    <strong>Describe:</strong> ${escapeHtml(nodeData.metadata.description)}
                </div>
            `;
        }
        if (nodeData.metadata.severity) {
            html += `
                <div class="node-detail-item">
                    <strong>Severity:</strong> <span style="color: ${getSeverityColor(nodeData.metadata.severity)}; font-weight: bold;">${escapeHtml(nodeData.metadata.severity)}</span>
                </div>
            `;
        }
        if (nodeData.metadata.location) {
            html += `
                <div class="node-detail-item">
                    <strong>Location:</strong> <code>${escapeHtml(nodeData.metadata.location)}</code>
                </div>
            `;
        }
    }
    
    if (nodeData.toolExecutionId) {
        html += `
            <div class="node-detail-item">
                <strong>Tool execution ID:</strong> <code>${nodeData.toolExecutionId}</code>
            </div>
        `;
    }
    
    // Reset the scroll position first to avoid scrolling calculations when content is updated.
    if (detailsContent) {
        detailsContent.scrollTop = 0;
    }
    
    // Use requestAnimationFrame to optimize DOM updates and scrolling
    requestAnimationFrame(() => {
        // Update content
        detailsContent.innerHTML = html;
        
        // Perform scrolling on the next frame to avoid conflict with DOM updates
        requestAnimationFrame(() => {
            // Reset the scroll position of the details content area
            if (detailsContent) {
                detailsContent.scrollTop = 0;
            }
            
            // Reset the scroll position of the sidebar to ensure that the details area is visible
            const sidebar = document.querySelector('.attack-chain-sidebar-content');
            if (sidebar) {
                // Find the location of the details panel
                const detailsPanel = document.getElementById('attack-chain-details');
                if (detailsPanel && detailsPanel.offsetParent !== null) {
                    // Use getBoundingClientRect to get the position for better performance
                    const detailsRect = detailsPanel.getBoundingClientRect();
                    const sidebarRect = sidebar.getBoundingClientRect();
                    const scrollTop = sidebar.scrollTop;
                    const relativeTop = detailsRect.top - sidebarRect.top + scrollTop;
                    sidebar.scrollTop = relativeTop - 20; // Leave a little margin
                }
            }
        });
    });
}

// Get severity color
function getSeverityColor(severity) {
    const colors = {
        'critical': '#ff0000',
        'high': '#ff4444',
        'medium': '#ff8800',
        'low': '#ffbb00'
    };
    return colors[severity.toLowerCase()] || '#666';
}

// Get node type label
function getNodeTypeLabel(type) {
    const labels = {
        'action': 'Action',
        'vulnerability': 'Loopholes',
        'target': 'Target'
    };
    return labels[type] || type;
}

// Update statistics
function updateAttackChainStats(chainData) {
    const statsElement = document.getElementById('attack-chain-stats');
    if (statsElement) {
        const nodeCount = chainData.nodes ? chainData.nodes.length : 0;
        const edgeCount = chainData.edges ? chainData.edges.length : 0;
        statsElement.textContent = `节点: ${nodeCount} | 边: ${edgeCount}`;
    }
}

// Close node details
function closeNodeDetails() {
    const detailsPanel = document.getElementById('attack-chain-details');
    if (detailsPanel) {
        // Add fade animation
        detailsPanel.style.opacity = '0';
        detailsPanel.style.maxHeight = detailsPanel.scrollHeight + 'px';
        
        setTimeout(() => {
            detailsPanel.style.display = 'none';
            detailsPanel.style.maxHeight = '';
            detailsPanel.style.opacity = '';
        }, 300);
    }
    
    // Uncheck node
    if (attackChainCytoscape) {
        attackChainCytoscape.elements().unselect();
    }
}

// Close the attack chain modal box
function closeAttackChainModal() {
    const modal = document.getElementById('attack-chain-modal');
    if (modal) {
        modal.style.display = 'none';
    }
    
    // Close node details
    closeNodeDetails();
    
    // Clean Cytoscape instance
    if (attackChainCytoscape) {
        attackChainCytoscape.destroy();
        attackChainCytoscape = null;
    }
    
    currentAttackChainConversationId = null;
}

// Refresh attack chain (reload)
// Note: This function is allowed to be called during the loading process and is used to check the build status
function refreshAttackChain() {
    if (currentAttackChainConversationId) {
        // Temporarily allow refresh even while loading (useful for checking build status)
        const wasLoading = isAttackChainLoading(currentAttackChainConversationId);
        setAttackChainLoading(currentAttackChainConversationId, false); // Temporary reset, allow refresh
        loadAttackChain(currentAttackChainConversationId).finally(() => {
            // If it was loading before (409 case), restore the loading state
            // Otherwise remain false (completes normally)
            if (wasLoading) {
                // Check whether the loading state still needs to be maintained (if it is still 409, it will be handled in loadAttackChain)
                // Here we assume that if the load is successful, the status is reset
                // If it is still 409, loadAttackChain will remain loaded.
            }
        });
    }
}

// Regenerate attack chain
async function regenerateAttackChain() {
    if (!currentAttackChainConversationId) {
        return;
    }
    
    // Prevent duplicate clicks (only check the loading status of the current conversation)
    if (isAttackChainLoading(currentAttackChainConversationId)) {
        console.log('The attack chain is being generated, please wait...');
        return;
    }
    
    // Save the conversation ID at the time of request to prevent cross-talk
    const savedConversationId = currentAttackChainConversationId;
    setAttackChainLoading(savedConversationId, true);
    
    const container = document.getElementById('attack-chain-container');
    if (container) {
        container.innerHTML = '<div class="loading-spinner">Regenerating...</div>';
    }
    
    // Disable regenerate button
    const regenerateBtn = document.querySelector('button[onclick="regenerateAttackChain()"]');
    if (regenerateBtn) {
        regenerateBtn.disabled = true;
        regenerateBtn.style.opacity = '0.5';
        regenerateBtn.style.cursor = 'not-allowed';
    }
    
    try {
        // Call the regeneration interface
        const response = await apiFetch(`/api/attack-chain/${savedConversationId}/regenerate`, {
            method: 'POST'
        });
        
        if (!response.ok) {
            // Handling 409 Conflict (under construction)
            if (response.status === 409) {
                const error = await response.json();
                if (container) {
                    container.innerHTML = `
                        <div class="loading-spinner" style="text-align: center; padding: 40px;">
                            <div style="margin-bottom: 16px;">⏳ The attack chain is being generated...</div>
                            <div style="color: var(--text-secondary); font-size: 0.875rem;">
Please wait, it will be displayed automatically after the generation is completed.
                            </div>
                            <button class="btn-secondary" onclick="refreshAttackChain()" style="margin-top: 16px;">
Refresh to view progress
                            </button>
                        </div>
                    `;
                }
                // Automatically refresh after 5 seconds
                // SavedConversationId is defined at the beginning of the function
                setTimeout(() => {
                    // Check that the currently displayed conversation ID matches and is still loading
                    if (currentAttackChainConversationId === savedConversationId && 
                        isAttackChainLoading(savedConversationId)) {
                        refreshAttackChain();
                    }
                }, 5000);
                return;
            }
            
            const error = await response.json();
            throw new Error(error.error || 'Failed to regenerate attack chain');
        }
        
        const chainData = await response.json();
        
        // Check whether the currently displayed conversation ID matches to prevent cross-talk
        if (currentAttackChainConversationId !== savedConversationId) {
            console.log('The attack chain data has been returned, but the currently displayed dialogue has been switched. Ignore this rendering.', {
                returned: savedConversationId,
                current: currentAttackChainConversationId
            });
            setAttackChainLoading(savedConversationId, false);
            return;
        }
        
        // Render attack chain
        renderAttackChain(chainData);
        
        // Update statistics
        updateAttackChainStats(chainData);
        
    } catch (error) {
        console.error('Failed to regenerate attack chain:', error);
        if (container) {
            container.innerHTML = `<div class="error-message">Regeneration failed: ${error.message}</div>`;
        }
    } finally {
        setAttackChainLoading(savedConversationId, false);
        
        // Resume regenerate button
        if (regenerateBtn) {
            regenerateBtn.disabled = false;
            regenerateBtn.style.opacity = '1';
            regenerateBtn.style.cursor = 'pointer';
        }
    }
}

// Export attack chain
function exportAttackChain(format) {
    if (!attackChainCytoscape) {
        alert('Please load the attack chain first');
        return;
    }
    
    // Make sure the graph has finished rendering (use a small delay)
    setTimeout(() => {
        try {
            if (format === 'png') {
                try {
                    const pngPromise = attackChainCytoscape.png({
                        output: 'blob',
                        bg: 'white',
                        full: true,
                        scale: 1
                    });
                    
                    // Handling Promises
                    if (pngPromise && typeof pngPromise.then === 'function') {
                        pngPromise.then(blob => {
                            if (!blob) {
                                throw new Error('PNG export returns empty data');
                            }
                            const url = URL.createObjectURL(blob);
                            const a = document.createElement('a');
                            a.href = url;
                            a.download = `attack-chain-${currentAttackChainConversationId || 'export'}-${Date.now()}.png`;
                            document.body.appendChild(a);
                            a.click();
                            document.body.removeChild(a);
                            setTimeout(() => URL.revokeObjectURL(url), 100);
                        }).catch(err => {
                            console.error('Failed to export PNG:', err);
                            alert('Failed to export PNG:' + (err.message || 'Unknown error'));
                        });
                    } else {
                        // If it is not Promise, use it directly
                        const url = URL.createObjectURL(pngPromise);
                        const a = document.createElement('a');
                        a.href = url;
                        a.download = `attack-chain-${currentAttackChainConversationId || 'export'}-${Date.now()}.png`;
                        document.body.appendChild(a);
                        a.click();
                        document.body.removeChild(a);
                        setTimeout(() => URL.revokeObjectURL(url), 100);
                    }
                } catch (err) {
                    console.error('PNG export error:', err);
                    alert('Failed to export PNG:' + (err.message || 'Unknown error'));
                }
            } else if (format === 'svg') {
                try {
                    // Cytoscape.js 3.x does not directly support the .svg() method
                    // Using an alternative: Manually building SVG from Cytoscape data
                    const container = attackChainCytoscape.container();
                    if (!container) {
                        throw new Error('Unable to get container element');
                    }
                    
                    // Get all nodes and edges
                    const nodes = attackChainCytoscape.nodes();
                    const edges = attackChainCytoscape.edges();
                    
                    if (nodes.length === 0) {
                        throw new Error('No nodes to export');
                    }
                    
                    // Calculate the actual bounds of all nodes (including node size)
                    let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity;
                    nodes.forEach(node => {
                        const pos = node.position();
                        const nodeWidth = node.width();
                        const nodeHeight = node.height();
                        const size = Math.max(nodeWidth, nodeHeight) / 2;
                        
                        minX = Math.min(minX, pos.x - size);
                        minY = Math.min(minY, pos.y - size);
                        maxX = Math.max(maxX, pos.x + size);
                        maxY = Math.max(maxY, pos.y + size);
                    });
                    
                    // Also consider the range of edges
                    edges.forEach(edge => {
                        const { source, target, valid } = getEdgeNodes(edge);
                        if (valid) {
                            const sourcePos = source.position();
                            const targetPos = target.position();
                            minX = Math.min(minX, sourcePos.x, targetPos.x);
                            minY = Math.min(minY, sourcePos.y, targetPos.y);
                            maxX = Math.max(maxX, sourcePos.x, targetPos.x);
                            maxY = Math.max(maxY, sourcePos.y, targetPos.y);
                        }
                    });
                    
                    // Add margins
                    const padding = 50;
                    minX -= padding;
                    minY -= padding;
                    maxX += padding;
                    maxY += padding;
                    
                    const width = maxX - minX;
                    const height = maxY - minY;
                    
                    // Create SVG elements
                    const svg = document.createElementNS('http://www.w3.org/2000/svg', 'svg');
                    svg.setAttribute('width', width.toString());
                    svg.setAttribute('height', height.toString());
                    svg.setAttribute('xmlns', 'http://www.w3.org/2000/svg');
                    svg.setAttribute('viewBox', `${minX} ${minY} ${width} ${height}`);
                    
                    // Add white background rectangle
                    const bgRect = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
                    bgRect.setAttribute('x', minX.toString());
                    bgRect.setAttribute('y', minY.toString());
                    bgRect.setAttribute('width', width.toString());
                    bgRect.setAttribute('height', height.toString());
                    bgRect.setAttribute('fill', 'white');
                    svg.appendChild(bgRect);
                    
                    // Create defs for arrow markers
                    const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
                    
                    // Add arrow markers for edges (creates different arrows for different types of edges)
                    const edgeTypes = ['discovers', 'targets', 'enables', 'leads_to'];
                    edgeTypes.forEach((type, index) => {
                        let color = '#999';
                        if (type === 'discovers') color = '#3498db';
                        else if (type === 'targets') color = '#0066ff';
                        else if (type === 'enables') color = '#e74c3c';
                        else if (type === 'leads_to') color = '#666';
                        
                        const marker = document.createElementNS('http://www.w3.org/2000/svg', 'marker');
                        marker.setAttribute('id', `arrowhead-${type}`);
                        marker.setAttribute('markerWidth', '10');
                        marker.setAttribute('markerHeight', '10');
                        marker.setAttribute('refX', '9');
                        marker.setAttribute('refY', '3');
                        marker.setAttribute('orient', 'auto');
                        const polygon = document.createElementNS('http://www.w3.org/2000/svg', 'polygon');
                        polygon.setAttribute('points', '0 0, 10 3, 0 6');
                        polygon.setAttribute('fill', color);
                        marker.appendChild(polygon);
                        defs.appendChild(marker);
                    });
                    svg.appendChild(defs);
                    
                    // Add edges (draw first so nodes will be on top)
                    edges.forEach(edge => {
                        const { source, target, valid } = getEdgeNodes(edge);
                        if (!valid) {
                            return; // Skip invalid edges
                        }
                        
                        const sourcePos = source.position();
                        const targetPos = target.position();
                        const edgeData = edge.data();
                        const edgeType = edgeData.type || 'leads_to';
                        
                        // Get the edge style
                        let lineColor = '#999';
                        if (edgeType === 'discovers') lineColor = '#3498db';
                        else if (edgeType === 'targets') lineColor = '#0066ff';
                        else if (edgeType === 'enables') lineColor = '#e74c3c';
                        else if (edgeType === 'leads_to') lineColor = '#666';
                        
                        // Create paths (supports curves)
                        const path = document.createElementNS('http://www.w3.org/2000/svg', 'path');
                        // Simple straight path (can be improved to a curve)
                        const midX = (sourcePos.x + targetPos.x) / 2;
                        const midY = (sourcePos.y + targetPos.y) / 2;
                        const dx = targetPos.x - sourcePos.x;
                        const dy = targetPos.y - sourcePos.y;
                        const offset = Math.min(30, Math.sqrt(dx * dx + dy * dy) * 0.3);
                        
                        // Use quadratic Bezier curves
                        const controlX = midX + (dy > 0 ? -offset : offset);
                        const controlY = midY + (dx > 0 ? offset : -offset);
                        path.setAttribute('d', `M ${sourcePos.x} ${sourcePos.y} Q ${controlX} ${controlY} ${targetPos.x} ${targetPos.y}`);
                        path.setAttribute('stroke', lineColor);
                        path.setAttribute('stroke-width', '2');
                        path.setAttribute('fill', 'none');
                        path.setAttribute('marker-end', `url(#arrowhead-${edgeType})`);
                        svg.appendChild(path);
                    });
                    
                    // Add node
                    nodes.forEach(node => {
                        const pos = node.position();
                        const nodeData = node.data();
                        const riskScore = nodeData.riskScore || 0;
                        const nodeWidth = node.width();
                        const nodeHeight = node.height();
                        const size = Math.max(nodeWidth, nodeHeight) / 2;
                        
                        // Determine node color
                        let bgColor = '#88cc00';
                        let textColor = '#1a5a1a';
                        let borderColor = '#5a8a5a';
                        if (riskScore >= 80) {
                            bgColor = '#ff4444';
                            textColor = '#fff';
                            borderColor = '#fff';
                        } else if (riskScore >= 60) {
                            bgColor = '#ff8800';
                            textColor = '#fff';
                            borderColor = '#fff';
                        } else if (riskScore >= 40) {
                            bgColor = '#ffbb00';
                            textColor = '#333';
                            borderColor = '#cc9900';
                        }
                        
                        // Determine node shape
                        const nodeType = nodeData.type;
                        let shapeElement;
                        if (nodeType === 'vulnerability') {
                            // Diamond
                            shapeElement = document.createElementNS('http://www.w3.org/2000/svg', 'polygon');
                            const points = [
                                `${pos.x},${pos.y - size}`,
                                `${pos.x + size},${pos.y}`,
                                `${pos.x},${pos.y + size}`,
                                `${pos.x - size},${pos.y}`
                            ].join(' ');
                            shapeElement.setAttribute('points', points);
                        } else if (nodeType === 'target') {
                            // Star shape (five-pointed star)
                            shapeElement = document.createElementNS('http://www.w3.org/2000/svg', 'polygon');
                            const points = [];
                            for (let i = 0; i < 5; i++) {
                                const angle = (i * 4 * Math.PI / 5) - Math.PI / 2;
                                const x = pos.x + size * Math.cos(angle);
                                const y = pos.y + size * Math.sin(angle);
                                points.push(`${x},${y}`);
                            }
                            shapeElement.setAttribute('points', points.join(' '));
                        } else {
                            // Rounded rectangle
                            shapeElement = document.createElementNS('http://www.w3.org/2000/svg', 'rect');
                            shapeElement.setAttribute('x', (pos.x - size).toString());
                            shapeElement.setAttribute('y', (pos.y - size).toString());
                            shapeElement.setAttribute('width', (size * 2).toString());
                            shapeElement.setAttribute('height', (size * 2).toString());
                            shapeElement.setAttribute('rx', '5');
                            shapeElement.setAttribute('ry', '5');
                        }
                        
                        shapeElement.setAttribute('fill', bgColor);
                        shapeElement.setAttribute('stroke', borderColor);
                        shapeElement.setAttribute('stroke-width', '2');
                        svg.appendChild(shapeElement);
                        
                        // Add text labels (use text strokes to improve readability)
                        // Use raw tags without type tag prefix
                        const label = (nodeData.originalLabel || nodeData.label || nodeData.id || '').toString();
                        const maxLength = 15;
                        
                        // Create a text group with stroke and fill
                        const textGroup = document.createElementNS('http://www.w3.org/2000/svg', 'g');
                        textGroup.setAttribute('text-anchor', 'middle');
                        textGroup.setAttribute('dominant-baseline', 'middle');
                        
                        // Handling long text (simple line wrapping)
                        let lines = [];
                        if (label.length > maxLength) {
                            const words = label.split(' ');
                            let currentLine = '';
                            words.forEach(word => {
                                if ((currentLine + word).length <= maxLength) {
                                    currentLine += (currentLine ? ' ' : '') + word;
                                } else {
                                    if (currentLine) lines.push(currentLine);
                                    currentLine = word;
                                }
                            });
                            if (currentLine) lines.push(currentLine);
                            lines = lines.slice(0, 2); // Maximum two lines
                        } else {
                            lines = [label];
                        }
                        
                        // Determine text stroke color (consistent with original rendering)
                        let textOutlineColor = '#fff';
                        let textOutlineWidth = 2;
                        if (riskScore >= 80 || riskScore >= 60) {
                            // Red/orange background: white text, white stroke, dark outline
                            textOutlineColor = '#333';
                            textOutlineWidth = 1;
                        } else if (riskScore >= 40) {
                            // Yellow background: dark text, white stroke
                            textOutlineColor = '#fff';
                            textOutlineWidth = 2;
                        } else {
                            // Green background: dark green text, white strokes
                            textOutlineColor = '#fff';
                            textOutlineWidth = 2;
                        }
                        
                        // Create stroke and fill for each line of text
                        lines.forEach((line, i) => {
                            const textY = pos.y + (i - (lines.length - 1) / 2) * 16;
                            
                            // Stroke text (used to increase contrast and simulate text-outline effect)
                            const strokeText = document.createElementNS('http://www.w3.org/2000/svg', 'text');
                            strokeText.setAttribute('x', pos.x.toString());
                            strokeText.setAttribute('y', textY.toString());
                            strokeText.setAttribute('fill', 'none');
                            strokeText.setAttribute('stroke', textOutlineColor);
                            strokeText.setAttribute('stroke-width', textOutlineWidth.toString());
                            strokeText.setAttribute('stroke-linejoin', 'round');
                            strokeText.setAttribute('stroke-linecap', 'round');
                            strokeText.setAttribute('font-size', '14px');
                            strokeText.setAttribute('font-weight', 'bold');
                            strokeText.setAttribute('font-family', 'Arial, sans-serif');
                            strokeText.setAttribute('text-anchor', 'middle');
                            strokeText.setAttribute('dominant-baseline', 'middle');
                            strokeText.textContent = line;
                            textGroup.appendChild(strokeText);
                            
                            // Fill text (actual visible text)
                            const fillText = document.createElementNS('http://www.w3.org/2000/svg', 'text');
                            fillText.setAttribute('x', pos.x.toString());
                            fillText.setAttribute('y', textY.toString());
                            fillText.setAttribute('fill', textColor);
                            fillText.setAttribute('font-size', '14px');
                            fillText.setAttribute('font-weight', 'bold');
                            fillText.setAttribute('font-family', 'Arial, sans-serif');
                            fillText.setAttribute('text-anchor', 'middle');
                            fillText.setAttribute('dominant-baseline', 'middle');
                            fillText.textContent = line;
                            textGroup.appendChild(fillText);
                        });
                        
                        svg.appendChild(textGroup);
                    });
                    
                    // Convert SVG to string
                    const serializer = new XMLSerializer();
                    let svgString = serializer.serializeToString(svg);
                    
                    // Make sure there is an XML declaration
                    if (!svgString.startsWith('<?xml')) {
                        svgString = '<?xml version="1.0" encoding="UTF-8"?>\n' + svgString;
                    }
                    
                    const blob = new Blob([svgString], { type: 'image/svg+xml;charset=utf-8' });
                    const url = URL.createObjectURL(blob);
                    const a = document.createElement('a');
                    a.href = url;
                    a.download = `attack-chain-${currentAttackChainConversationId || 'export'}-${Date.now()}.svg`;
                    document.body.appendChild(a);
                    a.click();
                    document.body.removeChild(a);
                    setTimeout(() => URL.revokeObjectURL(url), 100);
                } catch (err) {
                    console.error('SVG export error:', err);
                    alert('Exporting SVG failed:' + (err.message || 'Unknown error'));
                }
            } else {
                alert('Unsupported export formats:' + format);
            }
        } catch (error) {
            console.error('Export failed:', error);
            alert('Export failed:' + (error.message || 'Unknown error'));
        }
    }, 100); // Small delay ensures graphics are rendered
}

// ============================================
// Conversation grouping and batch management functions
// ============================================

// Group data management (using API)
let currentGroupId = null; // The group details page currently being viewed
let currentConversationGroupId = null; // The group ID to which the current conversation belongs (used for highlighting)
let contextMenuConversationId = null;
let contextMenuGroupId = null;
let groupsCache = [];
let conversationGroupMappingCache = {};
let pendingGroupMappings = {}; // Group mapping to be retained (used to handle backend API delays)

// Load group list
async function loadGroups() {
    try {
        const response = await apiFetch('/api/groups');
        if (!response.ok) {
            groupsCache = [];
            return;
        }
        const data = await response.json();
        // Make sure groupsCache is a valid array
        if (Array.isArray(data)) {
            groupsCache = data;
        } else {
            // If what is returned is not an array, use an empty array (no warning is printed, because the backend may return an incorrect format but we need to handle it gracefully)
            groupsCache = [];
        }

        const groupsList = document.getElementById('conversation-groups-list');
        if (!groupsList) return;

        groupsList.innerHTML = '';

        if (!Array.isArray(groupsCache) || groupsCache.length === 0) {
            return;
        }

        // Sort the groups: the top group is in front (the backend has been sorted, it only needs to be displayed in order)
        const sortedGroups = [...groupsCache];

            sortedGroups.forEach(group => {
            const groupItem = document.createElement('div');
            groupItem.className = 'group-item';
            // Highlight logic:
            // 1. If you are currently on the group details page, only the current group (currentGroupId) will be highlighted.
            // 2. If you are not on the group details page, highlight the group to which the current conversation belongs (currentConversationGroupId)
            const shouldHighlight = currentGroupId 
                ? (currentGroupId === group.id)
                : (currentConversationGroupId === group.id);
            if (shouldHighlight) {
                groupItem.classList.add('active');
            }
            const isPinned = group.pinned || false;
            if (isPinned) {
                groupItem.classList.add('pinned');
            }
            groupItem.dataset.groupId = group.id;

            const content = document.createElement('div');
            content.className = 'group-item-content';

            const icon = document.createElement('span');
            icon.className = 'group-item-icon';
            icon.textContent = group.icon || '📁';

            const name = document.createElement('span');
            name.className = 'group-item-name';
            name.textContent = group.name;

            content.appendChild(icon);
            content.appendChild(name);

            // If it is a pinned group, add a pushpin icon
            if (isPinned) {
                const pinIcon = document.createElement('span');
                pinIcon.className = 'group-item-pinned';
                pinIcon.innerHTML = '📌';
                pinIcon.title = 'Pinned';
                name.appendChild(pinIcon);
            }
            groupItem.appendChild(content);

            const menuBtn = document.createElement('button');
            menuBtn.className = 'group-item-menu';
            menuBtn.innerHTML = '⋯';
            menuBtn.onclick = (e) => {
                e.stopPropagation();
                showGroupContextMenu(e, group.id);
            };
            groupItem.appendChild(menuBtn);

            groupItem.onclick = () => {
                enterGroupDetail(group.id);
            };

            groupsList.appendChild(groupItem);
        });
    } catch (error) {
        console.error('Failed to load grouped list:', error);
    }
}

// Load conversation list (modified to support grouping and pinning)
async function loadConversationsWithGroups(searchQuery = '') {
    try {
        // Always reload grouping lists and grouping maps to ensure the cache is up to date
        // This can correctly handle the situation after the group is deleted
        await loadGroups();
        await loadConversationGroupMapping();

        // If there are search keywords, use a larger limit to get all matching results
        const limit = (searchQuery && searchQuery.trim()) ? 1000 : 100;
        let url = `/api/conversations?limit=${limit}`;
        if (searchQuery && searchQuery.trim()) {
            url += '&search=' + encodeURIComponent(searchQuery.trim());
        }
        const response = await apiFetch(url);

        const listContainer = document.getElementById('conversations-list');
        if (!listContainer) {
            return;
        }

        // Save scroll position
        const sidebarContent = listContainer.closest('.sidebar-content');
        const savedScrollTop = sidebarContent ? sidebarContent.scrollTop : 0;

        const emptyStateHtml = '<div style="padding: 20px; text-align: center; color: var(--text-muted); font-size: 0.875rem;">No historical conversations yet</div>';
        listContainer.innerHTML = '';

        // If the response is not 200, an empty status is displayed (friendly processing, no error is displayed)
        if (!response.ok) {
            listContainer.innerHTML = emptyStateHtml;
            return;
        }

        const conversations = await response.json();

        if (!Array.isArray(conversations) || conversations.length === 0) {
            listContainer.innerHTML = emptyStateHtml;
            return;
        }
        
        // Separate pinned and normal conversations
        const pinnedConvs = [];
        const normalConvs = [];
        const hasSearchQuery = searchQuery && searchQuery.trim();

        conversations.forEach(conv => {
            // If there is a search keyword, display all matching conversations (global search, including in groups)
            if (hasSearchQuery) {
                // Show all matching conversations when searching, regardless of whether they are in a group
                if (conv.pinned) {
                    pinnedConvs.push(conv);
                } else {
                    normalConvs.push(conv);
                }
                return;
            }

            // If there is no search keyword, use the original logic
            // The "Recent Conversations" list should only show conversations that are not in any group
            // Conversations in the group should not be displayed in "Recent Conversations" regardless of whether they are on the group details page or not.
            if (conversationGroupMappingCache[conv.id]) {
                // The conversation is in a group and should not be displayed in the "Recent Conversations" list
                return;
            }

            if (conv.pinned) {
                pinnedConvs.push(conv);
            } else {
                normalConvs.push(conv);
            }
        });

        // Sort by time
        const sortByTime = (a, b) => {
            const timeA = a.updatedAt ? new Date(a.updatedAt) : new Date(0);
            const timeB = b.updatedAt ? new Date(b.updatedAt) : new Date(0);
            return timeB - timeA;
        };

        pinnedConvs.sort(sortByTime);
        normalConvs.sort(sortByTime);

        const fragment = document.createDocumentFragment();

        // Add pinned conversation
        if (pinnedConvs.length > 0) {
            pinnedConvs.forEach(conv => {
                fragment.appendChild(createConversationListItemWithMenu(conv, true));
            });
        }

        // Add normal conversation
        normalConvs.forEach(conv => {
            fragment.appendChild(createConversationListItemWithMenu(conv, false));
        });

        if (fragment.children.length === 0) {
            listContainer.innerHTML = emptyStateHtml;
            return;
        }

        listContainer.appendChild(fragment);
        updateActiveConversation();
        
        // Restore scroll position
        if (sidebarContent) {
            // Use requestAnimationFrame to ensure the DOM has been updated
            requestAnimationFrame(() => {
                sidebarContent.scrollTop = savedScrollTop;
            });
        }
    } catch (error) {
        console.error('Failed to load conversation list:', error);
        // Display an empty status when an error occurs instead of an error message (more friendly user experience)
        const listContainer = document.getElementById('conversations-list');
        if (listContainer) {
            const emptyStateHtml = '<div style="padding: 20px; text-align: center; color: var(--text-muted); font-size: 0.875rem;">No historical conversations yet</div>';
            listContainer.innerHTML = emptyStateHtml;
        }
    }
}

// Create a dialog item with a menu
function createConversationListItemWithMenu(conversation, isPinned) {
    const item = document.createElement('div');
    item.className = 'conversation-item';
    item.dataset.conversationId = conversation.id;
    if (conversation.id === currentConversationId) {
        item.classList.add('active');
    }

    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'conversation-content';

    const titleWrapper = document.createElement('div');
    titleWrapper.style.display = 'flex';
    titleWrapper.style.alignItems = 'center';
    titleWrapper.style.gap = '4px';

    const title = document.createElement('div');
    title.className = 'conversation-title';
    const titleText = conversation.title || 'Unnamed conversation';
    title.textContent = safeTruncateText(titleText, 60);
    title.title = titleText; // Set full title to view on hover
    titleWrapper.appendChild(title);

    if (isPinned) {
        const pinIcon = document.createElement('span');
        pinIcon.className = 'conversation-item-pinned';
        pinIcon.innerHTML = '📌';
        pinIcon.title = 'Pinned';
        titleWrapper.appendChild(pinIcon);
    }

    contentWrapper.appendChild(titleWrapper);

    const time = document.createElement('div');
    time.className = 'conversation-time';
    const dateObj = conversation.updatedAt ? new Date(conversation.updatedAt) : new Date();
    time.textContent = formatConversationTimestamp(dateObj);
    contentWrapper.appendChild(time);

    // If the conversation belongs to a group, show the group label
    const groupId = conversationGroupMappingCache[conversation.id];
    if (groupId) {
        const group = groupsCache.find(g => g.id === groupId);
        if (group) {
            const groupTag = document.createElement('div');
            groupTag.className = 'conversation-group-tag';
            groupTag.innerHTML = `<span class="group-tag-icon">${group.icon || '📁'}</span><span class="group-tag-name">${group.name}</span>`;
            groupTag.title = `分组: ${group.name}`;
            contentWrapper.appendChild(groupTag);
        }
    }

    item.appendChild(contentWrapper);

    const menuBtn = document.createElement('button');
    menuBtn.className = 'conversation-item-menu';
    menuBtn.innerHTML = '⋯';
    menuBtn.onclick = (e) => {
        e.stopPropagation();
        contextMenuConversationId = conversation.id;
        showConversationContextMenu(e);
    };
    item.appendChild(menuBtn);

    item.onclick = (e) => {
        e.preventDefault();
        e.stopPropagation();
        if (currentGroupId) {
            exitGroupDetail();
        }
        loadConversation(conversation.id);
    };

    return item;
}

// Show conversation context menu
async function showConversationContextMenu(event) {
    const menu = document.getElementById('conversation-context-menu');
    if (!menu) return;

    // Hide the submenu first and ensure that the submenu is closed every time you open the menu.
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu) {
        submenu.style.display = 'none';
        submenuVisible = false;
    }
    // Clear all timers
    clearSubmenuHideTimeout();
    clearSubmenuShowTimeout();
    submenuLoading = false;

    const convId = contextMenuConversationId;
    
    // Update the enabled status of the attack chain menu item
    const attackChainMenuItem = document.getElementById('attack-chain-menu-item');
    if (attackChainMenuItem) {
        if (convId) {
            const isRunning = typeof isConversationTaskRunning === 'function'
                ? isConversationTaskRunning(convId)
                : false;
            if (isRunning) {
                attackChainMenuItem.style.opacity = '0.5';
                attackChainMenuItem.style.cursor = 'not-allowed';
                attackChainMenuItem.onclick = null;
                attackChainMenuItem.title = 'The current conversation is being executed, please generate an attack chain later.';
            } else {
                attackChainMenuItem.style.opacity = '1';
                attackChainMenuItem.style.cursor = 'pointer';
                attackChainMenuItem.onclick = showAttackChainFromContext;
                attackChainMenuItem.title = 'View the attack chain of the current conversation';
            }
        } else {
            attackChainMenuItem.style.opacity = '0.5';
            attackChainMenuItem.style.cursor = 'not-allowed';
            attackChainMenuItem.onclick = null;
            attackChainMenuItem.title = 'Please select a conversation to view the attack chain';
        }
    }
    
    // First get the pinned status of the conversation and update the menu text (before showing the menu)
    if (convId) {
        try {
            let isPinned = false;
            // Check if the conversation is actually in the current group
            const conversationGroupId = conversationGroupMappingCache[convId];
            const isInCurrentGroup = currentGroupId && conversationGroupId === currentGroupId;
            
            if (isInCurrentGroup) {
                // The conversation is in the current group and gets the group's built-in top status
                const response = await apiFetch(`/api/groups/${currentGroupId}/conversations`);
                if (response.ok) {
                    const groupConvs = await response.json();
                    const conv = groupConvs.find(c => c.id === convId);
                    if (conv) {
                        isPinned = conv.groupPinned || false;
                    }
                }
            } else {
                // Not in the group details page, or the conversation is not in the current group, get the global pinned status
                const response = await apiFetch(`/api/conversations/${convId}`);
                if (response.ok) {
                    const conv = await response.json();
                    isPinned = conv.pinned || false;
                }
            }
            
            // Update menu text
            const pinMenuText = document.getElementById('pin-conversation-menu-text');
            if (pinMenuText) {
                pinMenuText.textContent = isPinned ? 'Unpin' : 'Pin this conversation';
            }
        } catch (error) {
            console.error('Failed to get the conversation top status:', error);
            // If retrieval fails, use default text
            const pinMenuText = document.getElementById('pin-conversation-menu-text');
            if (pinMenuText) {
                pinMenuText.textContent = 'Pin this conversation';
            }
        }
    } else {
        // If there is no conversation ID, use the default text
        const pinMenuText = document.getElementById('pin-conversation-menu-text');
        if (pinMenuText) {
            pinMenuText.textContent = 'Pin this conversation';
        }
    }

    // Display the menu after the status acquisition is completed
    menu.style.display = 'block';
    menu.style.visibility = 'visible';
    menu.style.opacity = '1';
    
    // Force reflow to get correct size
    void menu.offsetHeight;
    
    // Calculate menu position to ensure it does not exceed the screen
    const menuRect = menu.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    
    // Get the width of the submenu (if it exists, reuse the submenu variable obtained previously)
    const submenuWidth = submenu ? 180 : 0; // Submenu width + spacing
    
    let left = event.clientX;
    let top = event.clientY;
    
    // If the menu exceeds the right edge, adjust it to the left
    // Consider submenu width
    if (left + menuRect.width + submenuWidth > viewportWidth) {
        left = event.clientX - menuRect.width;
        // If it is still exceeded after adjustment, place it on the left side of the button
        if (left < 0) {
            left = Math.max(8, event.clientX - menuRect.width - submenuWidth);
        }
    }
    
    // If the menu exceeds the bottom boundary, adjust it to the top
    if (top + menuRect.height > viewportHeight) {
        top = Math.max(8, event.clientY - menuRect.height);
    }
    
    // Make sure not to exceed the left margin
    if (left < 0) {
        left = 8;
    }
    
    // Make sure not to exceed the upper boundary
    if (top < 0) {
        top = 8;
    }
    
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';
    
    // If the menu is on the right, the submenu should appear on the left
    if (submenu && left < event.clientX) {
        submenu.style.left = 'auto';
        submenu.style.right = '100%';
        submenu.style.marginLeft = '0';
        submenu.style.marginRight = '4px';
    } else if (submenu) {
        submenu.style.left = '100%';
        submenu.style.right = 'auto';
        submenu.style.marginLeft = '4px';
        submenu.style.marginRight = '0';
    }

    // Click outside to close menu
    const closeMenu = (e) => {
        // Check if click is inside main menu or submenu
        const moveToGroupSubmenuEl = document.getElementById('move-to-group-submenu');
        const clickedInMenu = menu.contains(e.target);
        const clickedInSubmenu = moveToGroupSubmenuEl && moveToGroupSubmenuEl.contains(e.target);
        
        if (!clickedInMenu && !clickedInSubmenu) {
            // Use closeContextMenu to ensure that both the main menu and submenu are closed
            closeContextMenu();
            document.removeEventListener('click', closeMenu);
        }
    };
    setTimeout(() => {
        document.addEventListener('click', closeMenu);
    }, 0);
}

// Show group context menu
async function showGroupContextMenu(event, groupId) {
    const menu = document.getElementById('group-context-menu');
    if (!menu) return;

    contextMenuGroupId = groupId;

    // First get the pinned status of the group and update the menu text (before showing the menu)
    try {
        // First search from cache
        let group = groupsCache.find(g => g.id === groupId);
        let isPinned = false;
        
        if (group) {
            isPinned = group.pinned || false;
        } else {
            // If not in cache, get it from API
            const response = await apiFetch(`/api/groups/${groupId}`);
            if (response.ok) {
                group = await response.json();
                isPinned = group.pinned || false;
            }
        }
        
        // Update menu text
        const pinMenuText = document.getElementById('pin-group-menu-text');
        if (pinMenuText) {
            pinMenuText.textContent = isPinned ? 'Unpin' : 'Pin this group to the top';
        }
    } catch (error) {
        console.error('Failed to obtain group top status:', error);
        // If retrieval fails, use default text
        const pinMenuText = document.getElementById('pin-group-menu-text');
        if (pinMenuText) {
            pinMenuText.textContent = 'Pin this group to the top';
        }
    }

    // Display the menu after the status acquisition is completed
    menu.style.display = 'block';
    menu.style.visibility = 'visible';
    menu.style.opacity = '1';
    
    // Force reflow to get correct size
    void menu.offsetHeight;
    
    // Calculate menu position to ensure it does not exceed the screen
    const menuRect = menu.getBoundingClientRect();
    const viewportWidth = window.innerWidth;
    const viewportHeight = window.innerHeight;
    
    let left = event.clientX;
    let top = event.clientY;
    
    // If the menu exceeds the right edge, adjust it to the left
    if (left + menuRect.width > viewportWidth) {
        left = event.clientX - menuRect.width;
    }
    
    // If the menu exceeds the bottom boundary, adjust it to the top
    if (top + menuRect.height > viewportHeight) {
        top = event.clientY - menuRect.height;
    }
    
    // Make sure not to exceed the left margin
    if (left < 0) {
        left = 8;
    }
    
    // Make sure not to exceed the upper boundary
    if (top < 0) {
        top = 8;
    }
    
    menu.style.left = left + 'px';
    menu.style.top = top + 'px';

    // Click outside to close menu
    const closeMenu = (e) => {
        if (!menu.contains(e.target)) {
            menu.style.display = 'none';
            document.removeEventListener('click', closeMenu);
        }
    };
    setTimeout(() => {
        document.addEventListener('click', closeMenu);
    }, 0);
}

// Rename conversation
async function renameConversation() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    const newTitle = prompt('Please enter a new title:', '');
    if (newTitle === null || !newTitle.trim()) {
        closeContextMenu();
        return;
    }

    try {
        const response = await apiFetch(`/api/conversations/${convId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ title: newTitle.trim() }),
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Update failed');
        }

        // Update frontend display
        const item = document.querySelector(`[data-conversation-id="${convId}"]`);
        if (item) {
            const titleEl = item.querySelector('.conversation-title');
            if (titleEl) {
                titleEl.textContent = newTitle.trim();
            }
        }

        // If it is on the group details page, it also needs to be updated.
        const groupItem = document.querySelector(`.group-conversation-item[data-conversation-id="${convId}"]`);
        if (groupItem) {
            const groupTitleEl = groupItem.querySelector('.group-conversation-title');
            if (groupTitleEl) {
                groupTitleEl.textContent = newTitle.trim();
            }
        }

        // Reload conversation list
        loadConversationsWithGroups();
    } catch (error) {
        console.error('Failed to rename conversation:', error);
        alert('Rename failed:' + (error.message || 'Unknown error'));
    }

    closeContextMenu();
}

// Pinned conversation
async function pinConversation() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    try {
        // Check if the conversation is actually in the current group
        // If a conversation has been moved out of the group, there will be no mapping for the conversation in the conversationGroupMappingCache
        // Or the mapped group ID is not equal to the current group ID
        const conversationGroupId = conversationGroupMappingCache[convId];
        const isInCurrentGroup = currentGroupId && conversationGroupId === currentGroupId;
        
        // If you are currently on the group details page and the conversation is indeed in the current group, use the group's built-in
        if (isInCurrentGroup) {
            // Get the pinned status of the current conversation in the group
            const response = await apiFetch(`/api/groups/${currentGroupId}/conversations`);
            const groupConvs = await response.json();
            const conv = groupConvs.find(c => c.id === convId);
            
            // If the conversation cannot be found, there may be a problem and use the default value.
            const currentPinned = conv && conv.groupPinned !== undefined ? conv.groupPinned : false;
            const newPinned = !currentPinned;

            // Update group built-in like status
            await apiFetch(`/api/groups/${currentGroupId}/conversations/${convId}/pinned`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ pinned: newPinned }),
            });

            // Reload group chat
            loadGroupConversations(currentGroupId);
        } else {
            // If you are not on the group details page, or the conversation is not in the current group, use global pinning.
            const response = await apiFetch(`/api/conversations/${convId}`);
            const conv = await response.json();
            const newPinned = !conv.pinned;

            // Update global pin status
            await apiFetch(`/api/conversations/${convId}/pinned`, {
                method: 'PUT',
                headers: {
                    'Content-Type': 'application/json',
                },
                body: JSON.stringify({ pinned: newPinned }),
            });

            loadConversationsWithGroups();
        }
    } catch (error) {
        console.error('Pinned conversation failed:', error);
        alert('Pin failed:' + (error.message || 'Unknown error'));
    }

    closeContextMenu();
}

// Show Move to Group submenu
async function showMoveToGroupSubmenu() {
    const submenu = document.getElementById('move-to-group-submenu');
    if (!submenu) return;

    // If the submenu is already displayed, there is no need to re-render it
    if (submenuVisible && submenu.style.display === 'block') {
        return;
    }

    // If loading, avoid repeated calls
    if (submenuLoading) {
        return;
    }

    // Clear hidden timer
    clearSubmenuHideTimeout();
    
    // Mark as loading
    submenuLoading = true;
    submenu.innerHTML = '';

    // Make sure the grouped list is loaded - force a reload to ensure the data is up to date
    try {
        // If the cache is empty, force loading
        if (!Array.isArray(groupsCache) || groupsCache.length === 0) {
            await loadGroups();
        } else {
            // Even if the cache is not empty, try to refresh it once to ensure that the data is up to date.
            // But using silent mode, no errors are displayed
            try {
                const response = await apiFetch('/api/groups');
                if (response.ok) {
                    const freshGroups = await response.json();
                    if (Array.isArray(freshGroups)) {
                        groupsCache = freshGroups;
                    }
                }
            } catch (err) {
                // If refresh fails, use cached data
                console.warn('Failed to refresh group list, using cached data:', err);
            }
        }
        
        // Verify cache again
        if (!Array.isArray(groupsCache)) {
            console.warn('GroupsCache is not a valid array, reset to empty array');
            groupsCache = [];
            // If it still doesn't work, try reloading
            if (groupsCache.length === 0) {
                await loadGroups();
            }
        }
    } catch (error) {
        console.error('Failed to load grouped list:', error);
        // Continue displaying menu even if loading fails, using existing cache
    }

    // If you are currently on the group details page, the "Move out of this group" option is displayed.
    if (currentGroupId && contextMenuConversationId) {
        // Check if the conversation is in the current group
        const convInGroup = conversationGroupMappingCache[contextMenuConversationId] === currentGroupId;
        if (convInGroup) {
            const removeItem = document.createElement('div');
            removeItem.className = 'context-submenu-item';
            removeItem.innerHTML = `
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                    <path d="M9 12l6 6M15 12l-6 6" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
                <span>Move out of this group</span>
            `;
            removeItem.onclick = () => {
                removeConversationFromGroup(contextMenuConversationId, currentGroupId);
            };
            submenu.appendChild(removeItem);
            
            // Add divider
            const divider = document.createElement('div');
            divider.className = 'context-menu-divider';
            submenu.appendChild(divider);
        }
    }

    // Verify that groupsCache is a valid array
    if (!Array.isArray(groupsCache)) {
        console.warn('GroupsCache is not a valid array, reset to empty array');
        groupsCache = [];
    }

    // If there are groups, display all groups (excluding groups where the conversation is already in)
    if (groupsCache.length > 0) {
        // Check the group ID the conversation is currently in
        const conversationCurrentGroupId = contextMenuConversationId 
            ? conversationGroupMappingCache[contextMenuConversationId] 
            : null;
        
        groupsCache.forEach(group => {
            // Verify whether the group object is valid
            if (!group || !group.id || !group.name) {
                console.warn('Invalid group object:', group);
                return;
            }
            
            // If the conversation is already in the current group, the group will not be displayed (because it is already in it)
            if (conversationCurrentGroupId && group.id === conversationCurrentGroupId) {
                return;
            }
            
            const item = document.createElement('div');
            item.className = 'context-submenu-item';
            item.innerHTML = `
                <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
                    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
                </svg>
                <span>${group.name}</span>
            `;
            item.onclick = () => {
                moveConversationToGroup(contextMenuConversationId, group.id);
            };
            submenu.appendChild(item);
        });
    } else {
        // If still no grouping, log for debugging
        console.warn('ShowMoveToGroupSubmenu: groupsCache is empty and the group list cannot be displayed');
    }

    // Always show the "Create Group" option
    const addItem = document.createElement('div');
    addItem.className = 'context-submenu-item add-group-item';
    addItem.innerHTML = `
        <svg width="16" height="16" viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg">
            <path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <span>+ Add new group</span>
    `;
    addItem.onclick = () => {
        showCreateGroupModal(true);
    };
    submenu.appendChild(addItem);

    submenu.style.display = 'block';
    submenuVisible = true;
    submenuLoading = false;
    
    // Calculate submenu position to prevent overflow
    setTimeout(() => {
        const submenuRect = submenu.getBoundingClientRect();
        const viewportWidth = window.innerWidth;
        const viewportHeight = window.innerHeight;
        
        // If the submenu exceeds the right boundary, adjust it to the left
        if (submenuRect.right > viewportWidth) {
            submenu.style.left = 'auto';
            submenu.style.right = '100%';
            submenu.style.marginLeft = '0';
            submenu.style.marginRight = '4px';
        }
        
        // If the submenu exceeds the lower boundary, adjust the position
        if (submenuRect.bottom > viewportHeight) {
            const overflow = submenuRect.bottom - viewportHeight;
            const currentTop = parseInt(submenu.style.top) || 0;
            submenu.style.top = (currentTop - overflow - 8) + 'px';
        }
    }, 0);
}

// Hide timers moved to group submenu
let submenuHideTimeout = null;
// Anti-shake timer showing submenu
let submenuShowTimeout = null;
// Is the submenu loading?
let submenuLoading = false;
// Is the submenu displayed?
let submenuVisible = false;

// Hide move to group submenu
function hideMoveToGroupSubmenu() {
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu) {
        submenu.style.display = 'none';
        submenuVisible = false;
    }
}

// Clear timer for hidden submenu
function clearSubmenuHideTimeout() {
    if (submenuHideTimeout) {
        clearTimeout(submenuHideTimeout);
        submenuHideTimeout = null;
    }
}

// Clear timer showing submenu
function clearSubmenuShowTimeout() {
    if (submenuShowTimeout) {
        clearTimeout(submenuShowTimeout);
        submenuShowTimeout = null;
    }
}

// Handle mouse entry into the "Move to Group" menu item (with anti-shake)
function handleMoveToGroupSubmenuEnter() {
    // Clear hidden timer
    clearSubmenuHideTimeout();
    
    // If the submenu is already displayed, there is no need to call it again
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu && submenuVisible && submenu.style.display === 'block') {
        return;
    }
    
    // Clear previous display timer
    clearSubmenuShowTimeout();
    
    // Use anti-shake delayed display to avoid frequent triggering
    submenuShowTimeout = setTimeout(() => {
        showMoveToGroupSubmenu();
        submenuShowTimeout = null;
    }, 100);
}

// Handle mouse leaving the "Move to Group" menu item
function handleMoveToGroupSubmenuLeave(event) {
    const submenu = document.getElementById('move-to-group-submenu');
    if (!submenu) return;
    
    // Clear display timer
    clearSubmenuShowTimeout();
    
    // Check if mouse moves to submenu
    const relatedTarget = event.relatedTarget;
    if (relatedTarget && submenu.contains(relatedTarget)) {
        // Move the mouse to the submenu and do not clear it
        return;
    }
    
    // Clear previous hidden timer
    clearSubmenuHideTimeout();
    
    // Delay hiding, giving the user time to move to the submenu
    submenuHideTimeout = setTimeout(() => {
        hideMoveToGroupSubmenu();
        submenuHideTimeout = null;
    }, 200);
}

// Move conversation to group
async function moveConversationToGroup(convId, groupId) {
    try {
        await apiFetch('/api/groups/conversations', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                conversationId: convId,
                groupId: groupId,
            }),
        });

        // Update cache
        const oldGroupId = conversationGroupMappingCache[convId];
        conversationGroupMappingCache[convId] = groupId;
        
        // Add newly moved conversations to the to-be-retained mapping to prevent backend API delays causing mapping loss
        pendingGroupMappings[convId] = groupId;
        
        // If the current conversation is moved, update currentConversationGroupId
        if (currentConversationId === convId) {
            currentConversationGroupId = groupId;
        }
        
        // If you are currently on the group details page, reload the group conversation
        if (currentGroupId) {
            // If you move out of the current group or move to the current group, you need to reload
            if (currentGroupId === oldGroupId || currentGroupId === groupId) {
                await loadGroupConversations(currentGroupId);
            }
        }
        
        // Regardless of whether you are on the group details page, you need to refresh the recent conversation list
        // Because the recent conversation list will filter the display based on the group mapping cache, it needs to be updated immediately.
        // LoadConversationsWithGroups internally calls loadConversationGroupMapping,
        // LoadConversationGroupMapping will retain the mappings in pendingGroupMappings
        await loadConversationsWithGroups();
        
        // Note: The mappings in pendingGroupMappings will be loaded the next time loadConversationGroupMapping
        // Automatic cleanup on successful load from backend (handled in loadConversationGroupMapping)
        
        // Refresh the group list and update the highlight status
        await loadGroups();
    } catch (error) {
        console.error('Failed to move conversation to group:', error);
        alert('Move failed:' + (error.message || 'Unknown error'));
    }

    closeContextMenu();
}

// Remove conversation from group
async function removeConversationFromGroup(convId, groupId) {
    try {
        await apiFetch(`/api/groups/${groupId}/conversations/${convId}`, {
            method: 'DELETE',
        });

        // Update cache - delete immediately to ensure correct recognition during subsequent loads
        delete conversationGroupMappingCache[convId];
        // Also removed from the mapping to be retained
        delete pendingGroupMappings[convId];
        
        // If removing the current conversation, clear currentConversationGroupId
        if (currentConversationId === convId) {
            currentConversationGroupId = null;
        }
        
        // If you are currently on the group details page, reload the group conversation
        if (currentGroupId === groupId) {
            await loadGroupConversations(groupId);
        }
        
        // Reload the grouping map to ensure the cache is up to date
        await loadConversationGroupMapping();
        
        // Refresh the group list and update the highlight status
        await loadGroups();
        
        // Refresh the recent conversations list so that removed conversations appear immediately
        // Use a temporary variable to save currentGroupId, and then set it to null temporarily to ensure that all conversations that are not in a group are displayed
        const savedGroupId = currentGroupId;
        currentGroupId = null;
        await loadConversationsWithGroups();
        currentGroupId = savedGroupId;
    } catch (error) {
        console.error('Removing conversation from group failed:', error);
        alert('Removal failed:' + (error.message || 'Unknown error'));
    }

    closeContextMenu();
}

// Load conversation grouping map
async function loadConversationGroupMapping() {
    try {
        // Get all groups and then get the conversations for each group
        let groups;
        if (Array.isArray(groupsCache) && groupsCache.length > 0) {
            groups = groupsCache;
        } else {
            const response = await apiFetch('/api/groups');
            if (!response.ok) {
                // If the API request fails, use an empty array and do not print a warning (this is normal error handling)
                groups = [];
            } else {
                groups = await response.json();
                // Make sure groups is a valid array and only print warnings when there are real exceptions
                if (!Array.isArray(groups)) {
                    // Only print warnings when the returned value is not an array and not null/undefined (it may be that the backend returns an incorrect format)
                    if (groups !== null && groups !== undefined) {
                        console.warn('LoadConversationGroupMapping: groups is not a valid array, use empty array', groups);
                    }
                    groups = [];
                }
            }
        }
        
        // Save mappings to be retained
        const preservedMappings = { ...pendingGroupMappings };
        
        conversationGroupMappingCache = {};

        for (const group of groups) {
            const response = await apiFetch(`/api/groups/${group.id}/conversations`);
            const conversations = await response.json();
            // Make sure conversations is a valid array
            if (Array.isArray(conversations)) {
                conversations.forEach(conv => {
                    conversationGroupMappingCache[conv.id] = group.id;
                    // If this conversation is in the to-be-retained map, remove it from the to-be-retained map (because it has been loaded from the backend)
                    if (preservedMappings[conv.id] === group.id) {
                        delete pendingGroupMappings[conv.id];
                    }
                });
            }
        }
        
        // Restore pending mappings (these are mappings that the backend API has not synced yet)
        Object.assign(conversationGroupMappingCache, preservedMappings);
    } catch (error) {
        console.error('Failed to load conversation grouping map:', error);
    }
}

// View attack chain from context menu
function showAttackChainFromContext() {
    const convId = contextMenuConversationId;
    if (!convId) return;
    
    closeContextMenu();
    showAttackChain(convId);
}

// Delete conversation from context menu
function deleteConversationFromContext() {
    const convId = contextMenuConversationId;
    if (!convId) return;

    if (confirm('Are you sure you want to delete this conversation?')) {
        deleteConversation(convId, true); // Skip internal confirmation because it has already been confirmed here
    }
    closeContextMenu();
}

// Close context menu
function closeContextMenu() {
    const menu = document.getElementById('conversation-context-menu');
    if (menu) {
        menu.style.display = 'none';
    }
    const submenu = document.getElementById('move-to-group-submenu');
    if (submenu) {
        submenu.style.display = 'none';
        submenuVisible = false;
    }
    // Clear all timers
    clearSubmenuHideTimeout();
    clearSubmenuShowTimeout();
    submenuLoading = false;
    contextMenuConversationId = null;
}

// Show batch management modal box
let allConversationsForBatch = [];

async function showBatchManageModal() {
    try {
        const response = await apiFetch('/api/conversations?limit=1000');
        
        // If the response is not 200, use an empty array (friendly, no error displayed)
        if (!response.ok) {
            allConversationsForBatch = [];
        } else {
            const data = await response.json();
            allConversationsForBatch = Array.isArray(data) ? data : [];
        }

        const modal = document.getElementById('batch-manage-modal');
        const countEl = document.getElementById('batch-manage-count');
        if (countEl) {
            countEl.textContent = allConversationsForBatch.length;
        }

        renderBatchConversations();
        if (modal) {
            modal.style.display = 'flex';
        }
    } catch (error) {
        console.error('Failed to load conversation list:', error);
        // Use an empty array when an error occurs and do not display an error prompt (more user-friendly experience)
        allConversationsForBatch = [];
        const modal = document.getElementById('batch-manage-modal');
        const countEl = document.getElementById('batch-manage-count');
        if (countEl) {
            countEl.textContent = 0;
        }
        if (modal) {
            renderBatchConversations();
            modal.style.display = 'flex';
        }
    }
}

// Safely truncate Chinese strings to avoid truncation in the middle of Chinese characters
function safeTruncateText(text, maxLength = 50) {
    if (!text || typeof text !== 'string') {
        return text || '';
    }
    
    // Convert string to character array using Array.from (correctly handles Unicode surrogate pairs)
    const chars = Array.from(text);
    
    // If the text length does not exceed the limit, return directly
    if (chars.length <= maxLength) {
        return text;
    }
    
    // Truncate to maximum length (based on number of characters, not code units)
    let truncatedChars = chars.slice(0, maxLength);
    
    // Try truncating at punctuation or spaces to make the truncation more natural
    // Find a suitable breakpoint ahead of the truncation point (no more than 20% of the length)
    const searchRange = Math.floor(maxLength * 0.2);
    const breakChars = ['，', '。', '、', ' ', ',', '.', ';', ':', '!', '?', '！', '？', '/', '\\', '-', '_'];
    let bestBreakPos = truncatedChars.length;
    
    for (let i = truncatedChars.length - 1; i >= truncatedChars.length - searchRange && i >= 0; i--) {
        if (breakChars.includes(truncatedChars[i])) {
            bestBreakPos = i + 1; // Break after punctuation
            break;
        }
    }
    
    // If a suitable breakpoint is found, use it; otherwise, use the original truncation position
    if (bestBreakPos < truncatedChars.length) {
        truncatedChars = truncatedChars.slice(0, bestBreakPos);
    }
    
    // Convert character array back to string, adding ellipses
    return truncatedChars.join('') + '...';
}

// Render batch management conversation list
function renderBatchConversations(filtered = null) {
    const list = document.getElementById('batch-conversations-list');
    if (!list) return;

    const conversations = filtered || allConversationsForBatch;
    list.innerHTML = '';

    conversations.forEach(conv => {
        const row = document.createElement('div');
        row.className = 'batch-conversation-row';
        row.dataset.conversationId = conv.id;

        const checkbox = document.createElement('input');
        checkbox.type = 'checkbox';
        checkbox.className = 'batch-conversation-checkbox';
        checkbox.dataset.conversationId = conv.id;

        const name = document.createElement('div');
        name.className = 'batch-table-col-name';
        const originalTitle = conv.title || 'Unnamed conversation';
        // Use the safe truncation function, limiting the maximum length to 45 characters (leave space for ellipsis)
        const truncatedTitle = safeTruncateText(originalTitle, 45);
        name.textContent = truncatedTitle;
        // Set title attribute to display full text (on mouseover)
        name.title = originalTitle;

        const time = document.createElement('div');
        time.className = 'batch-table-col-time';
        const dateObj = conv.updatedAt ? new Date(conv.updatedAt) : new Date();
        time.textContent = dateObj.toLocaleString('zh-CN', {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit'
        });

        const action = document.createElement('div');
        action.className = 'batch-table-col-action';
        const deleteBtn = document.createElement('button');
        deleteBtn.className = 'batch-delete-btn';
        deleteBtn.innerHTML = '🗑️';
        deleteBtn.onclick = () => deleteConversation(conv.id);
        action.appendChild(deleteBtn);

        row.appendChild(checkbox);
        row.appendChild(name);
        row.appendChild(time);
        row.appendChild(action);

        list.appendChild(row);
    });
}

// Filter batch management conversations
function filterBatchConversations(query) {
    if (!query || !query.trim()) {
        renderBatchConversations();
        return;
    }

    const filtered = allConversationsForBatch.filter(conv => {
        const title = (conv.title || '').toLowerCase();
        return title.includes(query.toLowerCase());
    });

    renderBatchConversations(filtered);
}

// Select all/Deselect all
function toggleSelectAllBatch() {
    const selectAll = document.getElementById('batch-select-all');
    const checkboxes = document.querySelectorAll('.batch-conversation-checkbox');
    
    checkboxes.forEach(cb => {
        cb.checked = selectAll.checked;
    });
}

// Delete selected conversations
async function deleteSelectedConversations() {
    const checkboxes = document.querySelectorAll('.batch-conversation-checkbox:checked');
    if (checkboxes.length === 0) {
        alert('Please select the conversation you want to delete first');
        return;
    }

    if (!confirm(`确定要删除选中的 ${checkboxes.length} 条对话吗？`)) {
        return;
    }

    const ids = Array.from(checkboxes).map(cb => cb.dataset.conversationId);
    
    try {
        for (const id of ids) {
            await deleteConversation(id, true); // Skip internal confirmation because it has already been confirmed during batch deletion
        }
        closeBatchManageModal();
        loadConversationsWithGroups();
    } catch (error) {
        console.error('Delete failed:', error);
        alert('Delete failed:' + (error.message || 'Unknown error'));
    }
}

// Close batch management modal box
function closeBatchManageModal() {
    const modal = document.getElementById('batch-manage-modal');
    if (modal) {
        modal.style.display = 'none';
    }
    const selectAll = document.getElementById('batch-select-all');
    if (selectAll) {
        selectAll.checked = false;
    }
    allConversationsForBatch = [];
}

// Show create group modal box
function showCreateGroupModal(andMoveConversation = false) {
    const modal = document.getElementById('create-group-modal');
    const input = document.getElementById('create-group-name-input');
    const iconBtn = document.getElementById('create-group-icon-btn');
    const iconPicker = document.getElementById('group-icon-picker');
    const customInput = document.getElementById('custom-icon-input');
    
    if (input) {
        input.value = '';
    }
    // Reset icon to default
    if (iconBtn) {
        iconBtn.textContent = '📁';
    }
    // Clear the custom icon input box
    if (customInput) {
        customInput.value = '';
    }
    // Close icon selector
    if (iconPicker) {
        iconPicker.style.display = 'none';
    }
    if (modal) {
        modal.style.display = 'flex';
        modal.dataset.moveConversation = andMoveConversation ? 'true' : 'false';
        if (input) {
            setTimeout(() => input.focus(), 100);
        }
    }
}

// Close the create group modal box
function closeCreateGroupModal() {
    const modal = document.getElementById('create-group-modal');
    if (modal) {
        modal.style.display = 'none';
    }
    const input = document.getElementById('create-group-name-input');
    if (input) {
        input.value = '';
    }
    // Reset icon to default
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (iconBtn) {
        iconBtn.textContent = '📁';
    }
    // Clear the custom icon input box
    const customInput = document.getElementById('custom-icon-input');
    if (customInput) {
        customInput.value = '';
    }
    // Close icon selector
    const iconPicker = document.getElementById('group-icon-picker');
    if (iconPicker) {
        iconPicker.style.display = 'none';
    }
}

// Select suggested tags
function selectSuggestion(name) {
    const input = document.getElementById('create-group-name-input');
    if (input) {
        input.value = name;
        input.focus();
    }
}

// Toggle icon selector display state
function toggleGroupIconPicker() {
    const picker = document.getElementById('group-icon-picker');
    if (picker) {
        const isVisible = picker.style.display !== 'none';
        picker.style.display = isVisible ? 'none' : 'block';
    }
}

// Select group icon
function selectGroupIcon(icon) {
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (iconBtn) {
        iconBtn.textContent = icon;
    }
    // Clear custom input box
    const customInput = document.getElementById('custom-icon-input');
    if (customInput) {
        customInput.value = '';
    }
    // Close selector
    const picker = document.getElementById('group-icon-picker');
    if (picker) {
        picker.style.display = 'none';
    }
}

// Apply custom icon
function applyCustomIcon() {
    const customInput = document.getElementById('custom-icon-input');
    if (!customInput) return;
    
    const customIcon = customInput.value.trim();
    if (!customIcon) {
        return;
    }
    
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (iconBtn) {
        iconBtn.textContent = customIcon;
    }
    
    // Clear the input box and close the selector
    customInput.value = '';
    const picker = document.getElementById('group-icon-picker');
    if (picker) {
        picker.style.display = 'none';
    }
}

// Custom icon input box enter key processing
document.addEventListener('DOMContentLoaded', function() {
    const customInput = document.getElementById('custom-icon-input');
    if (customInput) {
        customInput.addEventListener('keydown', function(e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                applyCustomIcon();
            }
        });
    }
});

// Click outside to close the icon picker
document.addEventListener('click', function(event) {
    const picker = document.getElementById('group-icon-picker');
    const iconBtn = document.getElementById('create-group-icon-btn');
    if (picker && iconBtn) {
        // Close the selector if clicked other than the icon button and the selector itself
        if (!picker.contains(event.target) && !iconBtn.contains(event.target)) {
            picker.style.display = 'none';
        }
    }
});

// Create group
async function createGroup(event) {
    // Prevent events from bubbling up
    if (event) {
        event.preventDefault();
        event.stopPropagation();
    }

    const input = document.getElementById('create-group-name-input');
    if (!input) {
        console.error('Input box not found');
        return;
    }

    const name = input.value.trim();
    if (!name) {
        alert('Please enter a group name');
        return;
    }

    // Front-end validation: check if the name already exists
    try {
        let groups;
        if (Array.isArray(groupsCache) && groupsCache.length > 0) {
            groups = groupsCache;
        } else {
            const response = await apiFetch('/api/groups');
            groups = await response.json();
        }
        
        // Make sure groups is a valid array
        if (!Array.isArray(groups)) {
            groups = [];
        }
        
        const nameExists = groups.some(g => g.name === name);
        if (nameExists) {
            alert('Group name already exists, please use another name');
            return;
        }
    } catch (error) {
        console.error('Failed to check group name:', error);
    }

    // Get the selected icon
    const iconBtn = document.getElementById('create-group-icon-btn');
    const selectedIcon = iconBtn ? iconBtn.textContent.trim() : '📁';

    try {
        const response = await apiFetch('/api/groups', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                name: name,
                icon: selectedIcon,
            }),
        });

        if (!response.ok) {
            const error = await response.json();
            if (error.error && error.error.includes('Already exists')) {
                alert('Group name already exists, please use another name');
                return;
            }
            throw new Error(error.error || 'Creation failed');
        }

        const newGroup = await response.json();
        
        // Check if the "Move to Group" submenu is open
        const submenu = document.getElementById('move-to-group-submenu');
        const isSubmenuOpen = submenu && submenu.style.display !== 'none';

        await loadGroups();

        const modal = document.getElementById('create-group-modal');
        const shouldMove = modal && modal.dataset.moveConversation === 'true';
        
        closeCreateGroupModal();

        if (shouldMove && contextMenuConversationId) {
            moveConversationToGroup(contextMenuConversationId, newGroup.id);
        }

        // If the submenu is open, refresh it so that the newly created group appears immediately
        if (isSubmenuOpen) {
            await showMoveToGroupSubmenu();
        }
    } catch (error) {
        console.error('Failed to create group:', error);
        alert('Creation failed:' + (error.message || 'Unknown error'));
    }
}

// Enter group details
async function enterGroupDetail(groupId) {
    currentGroupId = groupId;
    // When entering the group details page, clear the group ID to which the current conversation belongs to avoid highlight conflicts.
    // Because the user is viewing the group details at this time, not a conversation in the group
    currentConversationGroupId = null;
    
    try {
        const response = await apiFetch(`/api/groups/${groupId}`);
        const group = await response.json();
        
        if (!group) {
            currentGroupId = null;
            return;
        }

        // Show the group details page, hide the conversation interface, but keep the sidebar visible
        const sidebar = document.querySelector('.conversation-sidebar');
        const groupDetailPage = document.getElementById('group-detail-page');
        const chatContainer = document.querySelector('.chat-container');
        const titleEl = document.getElementById('group-detail-title');

        // Keep sidebar visible
        if (sidebar) sidebar.style.display = 'flex';
        // Hide the conversation interface and display the group details page
        if (chatContainer) chatContainer.style.display = 'none';
        if (groupDetailPage) groupDetailPage.style.display = 'flex';
        if (titleEl) titleEl.textContent = group.name;

        // Refresh the group list to ensure that the current group is highlighted
        await loadGroups();

        // Load grouped conversations (use search query if there is one)
        loadGroupConversations(groupId, currentGroupSearchQuery);
    } catch (error) {
        console.error('Failed to load group:', error);
        currentGroupId = null;
    }
}

// Exit group details
function exitGroupDetail() {
    currentGroupId = null;
    currentGroupSearchQuery = ''; // Clear search status
    
    // Hide the search box and clear searches
    const searchContainer = document.getElementById('group-search-container');
    const searchInput = document.getElementById('group-search-input');
    if (searchContainer) searchContainer.style.display = 'none';
    if (searchInput) searchInput.value = '';
    
    const sidebar = document.querySelector('.conversation-sidebar');
    const groupDetailPage = document.getElementById('group-detail-page');
    const chatContainer = document.querySelector('.chat-container');

    // Keep sidebar visible
    if (sidebar) sidebar.style.display = 'flex';
    // Hide the group details page and display the conversation interface
    if (groupDetailPage) groupDetailPage.style.display = 'none';
    if (chatContainer) chatContainer.style.display = 'flex';

    loadConversationsWithGroups();
}

// Load conversations in a group
async function loadGroupConversations(groupId, searchQuery = '') {
    try {
        if (!groupId) {
            console.error('loadGroupConversations: groupId is null or undefined');
            return;
        }
        
        // Make sure the grouping map is loaded
        if (Object.keys(conversationGroupMappingCache).length === 0) {
            await loadConversationGroupMapping();
        }
        
        // Clear the list first to avoid showing old data
        const list = document.getElementById('group-conversations-list');
        if (!list) {
            console.error('group-conversations-list element not found');
            return;
        }
        
        // Show loading status
        if (searchQuery) {
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">Searching...</div>';
        } else {
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">Loading...</div>';
        }

        // Build the URL and add search parameters if there are search keywords
        let url = `/api/groups/${groupId}/conversations`;
        if (searchQuery && searchQuery.trim()) {
            url += '?search=' + encodeURIComponent(searchQuery.trim());
        }
        
        const response = await apiFetch(url);
        if (!response.ok) {
            console.error(`Failed to load conversations for group ${groupId}:`, response.statusText);
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">Loading failed, please try again</div>';
            return;
        }
        
        let groupConvs = await response.json();
        
        // Handle null or undefined cases as if they were empty arrays
        if (!groupConvs) {
            groupConvs = [];
        }
        
        // Verify returned data type
        if (!Array.isArray(groupConvs)) {
            console.error(`Invalid response for group ${groupId}:`, groupConvs);
            list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">Data format error</div>';
            return;
        }
        
        // Update the group mapping cache (only updates the conversations of the current group)
        // First clean up the mapping before the group (if any conversations are removed)
        Object.keys(conversationGroupMappingCache).forEach(convId => {
            if (conversationGroupMappingCache[convId] === groupId) {
                // If the conversation is not in the new list, it has been removed
                if (!groupConvs.find(c => c.id === convId)) {
                    delete conversationGroupMappingCache[convId];
                }
            }
        });
        
        // Update the current group's conversation map
        groupConvs.forEach(conv => {
            conversationGroupMappingCache[conv.id] = groupId;
        });

        // Clear the list again (clear the "Loading" prompt)
        list.innerHTML = '';

        if (groupConvs.length === 0) {
            if (searchQuery && searchQuery.trim()) {
                list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">No matching conversation found</div>';
            } else {
                list.innerHTML = '<div style="padding: 40px; text-align: center; color: var(--text-muted);">There are currently no conversations in this group</div>';
            }
            return;
        }

        // Load details of each conversation for messages
        for (const conv of groupConvs) {
            try {
                // Verify conversation ID exists
                if (!conv.id) {
                    console.warn('Conversation missing id:', conv);
                    continue;
                }
                
                const convResponse = await apiFetch(`/api/conversations/${conv.id}`);
                if (!convResponse.ok) {
                    console.error(`Failed to load conversation ${conv.id}:`, convResponse.statusText);
                    continue;
                }
                
                const fullConv = await convResponse.json();
                
                const item = document.createElement('div');
                item.className = 'group-conversation-item';
                item.dataset.conversationId = conv.id;
                // The active state is only displayed on the group details page and the conversation ID matches.
                // If you are not on the group details page, the active status should not be displayed.
                if (currentGroupId && conv.id === currentConversationId) {
                    item.classList.add('active');
                } else {
                    item.classList.remove('active');
                }

                // Create content wrapper
                const contentWrapper = document.createElement('div');
                contentWrapper.className = 'group-conversation-content-wrapper';

                const titleWrapper = document.createElement('div');
                titleWrapper.style.display = 'flex';
                titleWrapper.style.alignItems = 'center';
                titleWrapper.style.gap = '4px';

                const title = document.createElement('div');
                title.className = 'group-conversation-title';
                const titleText = fullConv.title || conv.title || 'Unnamed conversation';
                title.textContent = safeTruncateText(titleText, 60);
                title.title = titleText; // Set full title to view on hover
                titleWrapper.appendChild(title);

                // If the conversation is pinned in the group, show the pin icon
                if (conv.groupPinned) {
                    const pinIcon = document.createElement('span');
                    pinIcon.className = 'conversation-item-pinned';
                    pinIcon.innerHTML = '📌';
                    pinIcon.title = 'Pinned to the top of the group';
                    titleWrapper.appendChild(pinIcon);
                }

                contentWrapper.appendChild(titleWrapper);

                const timeWrapper = document.createElement('div');
                timeWrapper.className = 'group-conversation-time';
                const dateObj = fullConv.updatedAt ? new Date(fullConv.updatedAt) : new Date();
                timeWrapper.textContent = dateObj.toLocaleString('zh-CN', {
                    year: 'numeric',
                    month: 'long',
                    day: 'numeric',
                    hour: '2-digit',
                    minute: '2-digit'
                });

                contentWrapper.appendChild(timeWrapper);

                // If there is the first message, show content preview
                if (fullConv.messages && fullConv.messages.length > 0) {
                    const firstMsg = fullConv.messages.find(m => m.role === 'user' && m.content);
                    if (firstMsg && firstMsg.content) {
                        const content = document.createElement('div');
                        content.className = 'group-conversation-content';
                        let preview = firstMsg.content.substring(0, 200);
                        if (firstMsg.content.length > 200) {
                            preview += '...';
                        }
                        content.textContent = preview;
                        contentWrapper.appendChild(content);
                    }
                }

                item.appendChild(contentWrapper);

                // Add three dot menu button
                const menuBtn = document.createElement('button');
                menuBtn.className = 'conversation-item-menu';
                menuBtn.innerHTML = '⋯';
                menuBtn.onclick = (e) => {
                    e.stopPropagation();
                    contextMenuConversationId = conv.id;
                    showConversationContextMenu(e);
                };
                item.appendChild(menuBtn);

                item.onclick = (e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    // Switch to the conversation interface but keep the group details status
                    const groupDetailPage = document.getElementById('group-detail-page');
                    const chatContainer = document.querySelector('.chat-container');
                    if (groupDetailPage) groupDetailPage.style.display = 'none';
                    if (chatContainer) chatContainer.style.display = 'flex';
                    loadConversation(conv.id);
                };

                list.appendChild(item);
            } catch (err) {
                console.error(`加载对话 ${conv.id} 失败:`, err);
            }
        }
    } catch (error) {
        console.error('Failed to load group chat:', error);
    }
}

// Edit group
async function editGroup() {
    if (!currentGroupId) return;

    try {
        const response = await apiFetch(`/api/groups/${currentGroupId}`);
        const group = await response.json();
        if (!group) return;

        const newName = prompt('Please enter a new name:', group.name);
        if (newName === null || !newName.trim()) return;

        const trimmedName = newName.trim();
        
        // Front-end verification: Check whether the name already exists (exclude the current group)
        let groups;
        if (Array.isArray(groupsCache) && groupsCache.length > 0) {
            groups = groupsCache;
        } else {
            const response = await apiFetch('/api/groups');
            groups = await response.json();
        }
        
        // Make sure groups is a valid array
        if (!Array.isArray(groups)) {
            groups = [];
        }
        
        const nameExists = groups.some(g => g.name === trimmedName && g.id !== currentGroupId);
        if (nameExists) {
            alert('Group name already exists, please use another name');
            return;
        }

        const updateResponse = await apiFetch(`/api/groups/${currentGroupId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                name: trimmedName,
                icon: group.icon || '📁',
            }),
        });

        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            if (error.error && error.error.includes('Already exists')) {
                alert('Group name already exists, please use another name');
                return;
            }
            throw new Error(error.error || 'Update failed');
        }

        loadGroups();
        
        const titleEl = document.getElementById('group-detail-title');
        if (titleEl) {
            titleEl.textContent = trimmedName;
        }
    } catch (error) {
        console.error('Failed to edit group:', error);
        alert('Editing failed:' + (error.message || 'Unknown error'));
    }
}

// Delete group
async function deleteGroup() {
    if (!currentGroupId) return;

    if (!confirm('Are you sure you want to delete this group? Conversations in the group will not be deleted, but they will be removed from the group.')) {
        return;
    }

    try {
        await apiFetch(`/api/groups/${currentGroupId}`, {
            method: 'DELETE',
        });

        // Update cache
        groupsCache = groupsCache.filter(g => g.id !== currentGroupId);
        Object.keys(conversationGroupMappingCache).forEach(convId => {
            if (conversationGroupMappingCache[convId] === currentGroupId) {
                delete conversationGroupMappingCache[convId];
            }
        });

        // If the "Move to Group" submenu is open, refresh it
        const submenu = document.getElementById('move-to-group-submenu');
        if (submenu && submenu.style.display !== 'none') {
            // The submenu is open, reload the grouped list and refresh the submenu
            await loadGroups();
            await showMoveToGroupSubmenu();
        } else {
            exitGroupDetail();
            await loadGroups();
        }
        
        // Refresh the conversation list to ensure that previously grouped conversations are immediately visible
        await loadConversationsWithGroups();
    } catch (error) {
        console.error('Failed to delete group:', error);
        alert('Delete failed:' + (error.message || 'Unknown error'));
    }
}

// Rename grouping from context menu
async function renameGroupFromContext() {
    const groupId = contextMenuGroupId;
    if (!groupId) return;

    try {
        const response = await apiFetch(`/api/groups/${groupId}`);
        const group = await response.json();
        if (!group) return;

        const newName = prompt('Please enter a new name:', group.name);
        if (newName === null || !newName.trim()) {
            closeGroupContextMenu();
            return;
        }

        const trimmedName = newName.trim();
        
        // Front-end verification: Check whether the name already exists (exclude the current group)
        let groups;
        if (Array.isArray(groupsCache) && groupsCache.length > 0) {
            groups = groupsCache;
        } else {
            const response = await apiFetch('/api/groups');
            groups = await response.json();
        }
        
        // Make sure groups is a valid array
        if (!Array.isArray(groups)) {
            groups = [];
        }
        
        const nameExists = groups.some(g => g.name === trimmedName && g.id !== groupId);
        if (nameExists) {
            alert('Group name already exists, please use another name');
            return;
        }

        const updateResponse = await apiFetch(`/api/groups/${groupId}`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                name: trimmedName,
                icon: group.icon || '📁',
            }),
        });

        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            if (error.error && error.error.includes('Already exists')) {
                alert('Group name already exists, please use another name');
                return;
            }
            throw new Error(error.error || 'Update failed');
        }

        loadGroups();
        
        // If you are currently on the group details page, update the title
        if (currentGroupId === groupId) {
            const titleEl = document.getElementById('group-detail-title');
            if (titleEl) {
                titleEl.textContent = trimmedName;
            }
        }
    } catch (error) {
        console.error('Failed to rename group:', error);
        alert('Rename failed:' + (error.message || 'Unknown error'));
    }

    closeGroupContextMenu();
}

// Pin group from context menu
async function pinGroupFromContext() {
    const groupId = contextMenuGroupId;
    if (!groupId) return;

    try {
        // Get current group information
        const response = await apiFetch(`/api/groups/${groupId}`);
        const group = await response.json();
        if (!group) return;

        const newPinnedState = !group.pinned;

        // Call API to update pinned status
        const updateResponse = await apiFetch(`/api/groups/${groupId}/pinned`, {
            method: 'PUT',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({
                pinned: newPinnedState,
            }),
        });

        if (!updateResponse.ok) {
            const error = await updateResponse.json();
            throw new Error(error.error || 'Update failed');
        }

        // Reload the grouped list to update the display order
        loadGroups();
    } catch (error) {
        console.error('Pinned group failed:', error);
        alert('Pin failed:' + (error.message || 'Unknown error'));
    }

    closeGroupContextMenu();
}

// Remove grouping from context menu
async function deleteGroupFromContext() {
    const groupId = contextMenuGroupId;
    if (!groupId) return;

    if (!confirm('Are you sure you want to delete this group? Conversations in the group will not be deleted, but they will be removed from the group.')) {
        closeGroupContextMenu();
        return;
    }

    try {
        await apiFetch(`/api/groups/${groupId}`, {
            method: 'DELETE',
        });

        // Update cache
        groupsCache = groupsCache.filter(g => g.id !== groupId);
        Object.keys(conversationGroupMappingCache).forEach(convId => {
            if (conversationGroupMappingCache[convId] === groupId) {
                delete conversationGroupMappingCache[convId];
            }
        });

        // If the "Move to Group" submenu is open, refresh it
        const submenu = document.getElementById('move-to-group-submenu');
        if (submenu && submenu.style.display !== 'none') {
            // The submenu is open, reload the grouped list and refresh the submenu
            await loadGroups();
            await showMoveToGroupSubmenu();
        } else {
            // If you are currently on the group details page, exit the details page
            if (currentGroupId === groupId) {
                exitGroupDetail();
            }
            await loadGroups();
        }
        
        // Refresh the conversation list to ensure that previously grouped conversations are immediately visible
        await loadConversationsWithGroups();
    } catch (error) {
        console.error('Failed to delete group:', error);
        alert('Delete failed:' + (error.message || 'Unknown error'));
    }

    closeGroupContextMenu();
}

// Close group context menu
function closeGroupContextMenu() {
    const menu = document.getElementById('group-context-menu');
    if (menu) {
        menu.style.display = 'none';
    }
    contextMenuGroupId = null;
}


// Group search related variables
let groupSearchTimer = null;
let currentGroupSearchQuery = '';

// Toggle group search box display/hide
function toggleGroupSearch() {
    const searchContainer = document.getElementById('group-search-container');
    const searchInput = document.getElementById('group-search-input');
    
    if (!searchContainer || !searchInput) return;
    
    if (searchContainer.style.display === 'none') {
        searchContainer.style.display = 'block';
        searchInput.focus();
    } else {
        searchContainer.style.display = 'none';
        clearGroupSearch();
    }
}

// Handling grouped search input
function handleGroupSearchInput(event) {
    // Support enter key search
    if (event.key === 'Enter') {
        event.preventDefault();
        performGroupSearch();
        return;
    }
    
    // Support ESC key to close search
    if (event.key === 'Escape') {
        clearGroupSearch();
        toggleGroupSearch();
        return;
    }
    
    const searchInput = document.getElementById('group-search-input');
    const clearBtn = document.getElementById('group-search-clear-btn');
    
    if (!searchInput) return;
    
    const query = searchInput.value.trim();
    
    // Show/hide clear button
    if (clearBtn) {
        clearBtn.style.display = query ? 'block' : 'none';
    }
    
    // Anti-shake search
    if (groupSearchTimer) {
        clearTimeout(groupSearchTimer);
    }
    
    groupSearchTimer = setTimeout(() => {
        performGroupSearch();
    }, 300); // 300ms anti-shake
}

// Perform a grouped search
async function performGroupSearch() {
    const searchInput = document.getElementById('group-search-input');
    if (!searchInput || !currentGroupId) return;
    
    const query = searchInput.value.trim();
    currentGroupSearchQuery = query;
    
    // Load search results
    await loadGroupConversations(currentGroupId, query);
}

// Clear group search
function clearGroupSearch() {
    const searchInput = document.getElementById('group-search-input');
    const clearBtn = document.getElementById('group-search-clear-btn');
    
    if (searchInput) {
        searchInput.value = '';
    }
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    
    currentGroupSearchQuery = '';
    
    // Reload group conversations (without searching)
    if (currentGroupId) {
        loadGroupConversations(currentGroupId, '');
    }
}

// Load groups during initialization
document.addEventListener('DOMContentLoaded', async () => {
    await loadGroups();
    // Replace the original loadConversations call
    if (typeof loadConversations === 'function') {
        // Keep the original function, but use the new function
        const originalLoad = loadConversations;
        loadConversations = function(...args) {
            loadConversationsWithGroups(...args);
        };
    }
    await loadConversationsWithGroups();
    
    // Automatically refresh the conversation list when adding page focus
    // In this way, after creating a conversation through OpenAPI, you can automatically see the new conversation when you switch back to the page.
    let lastFocusTime = Date.now();
    const CONVERSATION_REFRESH_INTERVAL = 30000; // Refresh at most once in 30 seconds to avoid too frequent
    
    window.addEventListener('focus', () => {
        const now = Date.now();
        // The conversation list will only be refreshed if it has been more than 30 seconds since the last refresh.
        if (now - lastFocusTime > CONVERSATION_REFRESH_INTERVAL) {
            lastFocusTime = now;
            if (typeof loadConversationsWithGroups === 'function') {
                loadConversationsWithGroups();
            }
        }
    });
    
    // Listen for page visibility changes (when the user switches tabs and comes back)
    document.addEventListener('visibilitychange', () => {
        if (!document.hidden) {
            // When the page becomes visible, check if it needs to be refreshed
            const now = Date.now();
            if (now - lastFocusTime > CONVERSATION_REFRESH_INTERVAL) {
                lastFocusTime = now;
                if (typeof loadConversationsWithGroups === 'function') {
                    loadConversationsWithGroups();
                }
            }
        }
    });
});
