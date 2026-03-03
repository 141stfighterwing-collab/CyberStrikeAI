const progressTaskState = new Map();
let activeTaskInterval = null;
const ACTIVE_TASK_REFRESH_INTERVAL = 10000; // Check every 10 seconds
const TASK_FINAL_STATUSES = new Set(['failed', 'timeout', 'cancelled', 'completed']);

// Mapping of storage tool call IDs to DOM elements for updating execution status
const toolCallStatusMap = new Map();

const conversationExecutionTracker = {
    activeConversations: new Set(),
    update(tasks = []) {
        this.activeConversations.clear();
        tasks.forEach(task => {
            if (
                task &&
                task.conversationId &&
                !TASK_FINAL_STATUSES.has(task.status)
            ) {
                this.activeConversations.add(task.conversationId);
            }
        });
    },
    isRunning(conversationId) {
        return !!conversationId && this.activeConversations.has(conversationId);
    }
};

function isConversationTaskRunning(conversationId) {
    return conversationExecutionTracker.isRunning(conversationId);
}

function registerProgressTask(progressId, conversationId = null) {
    const state = progressTaskState.get(progressId) || {};
    state.conversationId = conversationId !== undefined && conversationId !== null
        ? conversationId
        : (state.conversationId ?? currentConversationId);
    state.cancelling = false;
    progressTaskState.set(progressId, state);

    const progressElement = document.getElementById(progressId);
    if (progressElement) {
        progressElement.dataset.conversationId = state.conversationId || '';
    }
}

function updateProgressConversation(progressId, conversationId) {
    if (!conversationId) {
        return;
    }
    registerProgressTask(progressId, conversationId);
}

function markProgressCancelling(progressId) {
    const state = progressTaskState.get(progressId);
    if (state) {
        state.cancelling = true;
    }
}

function finalizeProgressTask(progressId, finalLabel = 'Completed') {
    const stopBtn = document.getElementById(`${progressId}-stop-btn`);
    if (stopBtn) {
        stopBtn.disabled = true;
        stopBtn.textContent = finalLabel;
    }
    progressTaskState.delete(progressId);
}

async function requestCancel(conversationId) {
    const response = await apiFetch('/api/agent-loop/cancel', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json',
        },
        body: JSON.stringify({ conversationId }),
    });
    const result = await response.json().catch(() => ({}));
    if (!response.ok) {
        throw new Error(result.error || 'Cancellation failed');
    }
    return result;
}

function addProgressMessage() {
    const messagesDiv = document.getElementById('chat-messages');
    const messageDiv = document.createElement('div');
    messageCounter++;
    const id = 'progress-' + Date.now() + '-' + messageCounter;
    messageDiv.id = id;
    messageDiv.className = 'message system progress-message';
    
    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'message-content';
    
    const bubble = document.createElement('div');
    bubble.className = 'message-bubble progress-container';
    bubble.innerHTML = `
        <div class="progress-header">
            <span class="progress-title">🔍 Penetration testing in progress...</span>
            <div class="progress-actions">
                <button class="progress-stop" id="${id}-stop-btn" onclick="cancelProgressTask('${id}')">Stop task</button>
                <button class="progress-toggle" onclick="toggleProgressDetails('${id}')">Collapse details</button>
            </div>
        </div>
        <div class="progress-timeline expanded" id="${id}-timeline"></div>
    `;
    
    contentWrapper.appendChild(bubble);
    messageDiv.appendChild(contentWrapper);
    messageDiv.dataset.conversationId = currentConversationId || '';
    messagesDiv.appendChild(messageDiv);
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
    
    return id;
}

// Switch progress details display
function toggleProgressDetails(progressId) {
    const timeline = document.getElementById(progressId + '-timeline');
    const toggleBtn = document.querySelector(`#${progressId} .progress-toggle`);
    
    if (!timeline || !toggleBtn) return;
    
    if (timeline.classList.contains('expanded')) {
        timeline.classList.remove('expanded');
        toggleBtn.textContent = 'Expand details';
    } else {
        timeline.classList.add('expanded');
        toggleBtn.textContent = 'Collapse details';
    }
}

// Collapse all progress details
function collapseAllProgressDetails(assistantMessageId, progressId) {
    // Details of folding integration into MCP areas
    if (assistantMessageId) {
        const detailsId = 'process-details-' + assistantMessageId;
        const detailsContainer = document.getElementById(detailsId);
        if (detailsContainer) {
            const timeline = detailsContainer.querySelector('.progress-timeline');
            if (timeline) {
                // Make sure to remove expanded classes (whether included or not)
                timeline.classList.remove('expanded');
                const btn = document.querySelector(`#${assistantMessageId} .process-detail-btn`);
                if (btn) {
                    btn.innerHTML = '<span>Expand details</span>';
                }
            }
        }
    }
    
    // Collapse the independent details component (created through convertProgressToDetails)
    // Find all details components starting with details-
    const allDetails = document.querySelectorAll('[id^="details-"]');
    allDetails.forEach(detail => {
        const timeline = detail.querySelector('.progress-timeline');
        const toggleBtn = detail.querySelector('.progress-toggle');
        if (timeline) {
            timeline.classList.remove('expanded');
            if (toggleBtn) {
                toggleBtn.textContent = 'Expand details';
            }
        }
    });
    
    // Collapse the original progress message (if it still exists)
    if (progressId) {
        const progressTimeline = document.getElementById(progressId + '-timeline');
        const progressToggleBtn = document.querySelector(`#${progressId} .progress-toggle`);
        if (progressTimeline) {
            progressTimeline.classList.remove('expanded');
            if (progressToggleBtn) {
                progressToggleBtn.textContent = 'Expand details';
            }
        }
    }
}

// Get the current assistant message ID (for done event)
function getAssistantId() {
    // Get ID from recent assistant message
    const messages = document.querySelectorAll('.message.assistant');
    if (messages.length > 0) {
        return messages[messages.length - 1].id;
    }
    return null;
}

// Integrate progress details into tool call area
function integrateProgressToMCPSection(progressId, assistantMessageId) {
    const progressElement = document.getElementById(progressId);
    if (!progressElement) return;
    
    // Get timeline content
    const timeline = document.getElementById(progressId + '-timeline');
    let timelineHTML = '';
    if (timeline) {
        timelineHTML = timeline.innerHTML;
    }
    
    // Get the assistant message element
    const assistantElement = document.getElementById(assistantMessageId);
    if (!assistantElement) {
        removeMessage(progressId);
        return;
    }
    
    // Find MCP calling area
    const mcpSection = assistantElement.querySelector('.mcp-call-section');
    if (!mcpSection) {
        // If there is no MCP area, create a details component and place it below the message
        convertProgressToDetails(progressId, assistantMessageId);
        return;
    }
    
    // Get timeline content
    const hasContent = timelineHTML.trim().length > 0;
    
    // Check the timeline for incorrect items
    const hasError = timeline && timeline.querySelector('.timeline-item-error');
    
    // Make sure the button container exists
    let buttonsContainer = mcpSection.querySelector('.mcp-call-buttons');
    if (!buttonsContainer) {
        buttonsContainer = document.createElement('div');
        buttonsContainer.className = 'mcp-call-buttons';
        mcpSection.appendChild(buttonsContainer);
    }
    
    // Create a details container and place it under the MCP button area (unified structure)
    const detailsId = 'process-details-' + assistantMessageId;
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
    
    // Set detailed content (if there is an error, it will be collapsed by default; otherwise it will be collapsed by default)
    detailsContainer.innerHTML = `
        <div class="process-details-content">
            ${hasContent ? `<div class="progress-timeline" id="${detailsId}-timeline">${timelineHTML}</div>` : '<div class="progress-timeline-empty">No process details yet</div>'}
        </div>
    `;
    
    // Make sure the initial state is collapsed (collapsed by default, especially on error)
    if (hasContent) {
        const timeline = document.getElementById(detailsId + '-timeline');
        if (timeline) {
            // Make sure to collapse if there is an error; otherwise also collapse by default
            timeline.classList.remove('expanded');
        }
        
        // Update button text to "Expand details" (because it's collapsed by default)
        const processDetailBtn = buttonsContainer.querySelector('.process-detail-btn');
        if (processDetailBtn) {
            processDetailBtn.innerHTML = '<span>Expand details</span>';
        }
    }
    
    // Remove original progress message
    removeMessage(progressId);
}

// Switching process details display
function toggleProcessDetails(progressId, assistantMessageId) {
    const detailsId = 'process-details-' + assistantMessageId;
    const detailsContainer = document.getElementById(detailsId);
    if (!detailsContainer) return;
    
    const content = detailsContainer.querySelector('.process-details-content');
    const timeline = detailsContainer.querySelector('.progress-timeline');
    const btn = document.querySelector(`#${assistantMessageId} .process-detail-btn`);
    
    if (content && timeline) {
        if (timeline.classList.contains('expanded')) {
            timeline.classList.remove('expanded');
            if (btn) btn.innerHTML = '<span>Expand details</span>';
        } else {
            timeline.classList.add('expanded');
            if (btn) btn.innerHTML = '<span>Collapse details</span>';
        }
    } else if (timeline) {
        // If there is only timeline, switch directly
        if (timeline.classList.contains('expanded')) {
            timeline.classList.remove('expanded');
            if (btn) btn.innerHTML = '<span>Expand details</span>';
        } else {
            timeline.classList.add('expanded');
            if (btn) btn.innerHTML = '<span>Collapse details</span>';
        }
    }
    
    // Scroll to expanded details instead of scrolling to the bottom
    if (timeline && timeline.classList.contains('expanded')) {
        setTimeout(() => {
            // Use scrollIntoView to scroll to the details container position
            detailsContainer.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
        }, 100);
    }
}

// Stop the task corresponding to the current progress
async function cancelProgressTask(progressId) {
    const state = progressTaskState.get(progressId);
    const stopBtn = document.getElementById(`${progressId}-stop-btn`);

    if (!state || !state.conversationId) {
        if (stopBtn) {
            stopBtn.disabled = true;
            setTimeout(() => {
                stopBtn.disabled = false;
            }, 1500);
        }
        alert('The task information has not been synchronized yet, please try again later.');
        return;
    }

    if (state.cancelling) {
        return;
    }

    markProgressCancelling(progressId);
    if (stopBtn) {
        stopBtn.disabled = true;
        stopBtn.textContent = 'Canceling...';
    }

    try {
        await requestCancel(state.conversationId);
        loadActiveTasks();
    } catch (error) {
        console.error('Failed to cancel task:', error);
        alert('Failed to cancel task:' + error.message);
        if (stopBtn) {
            stopBtn.disabled = false;
            stopBtn.textContent = 'Stop task';
        }
        const currentState = progressTaskState.get(progressId);
        if (currentState) {
            currentState.cancelling = false;
        }
    }
}

// Convert progress message to collapsible details component
function convertProgressToDetails(progressId, assistantMessageId) {
    const progressElement = document.getElementById(progressId);
    if (!progressElement) return;
    
    // Get timeline content
    const timeline = document.getElementById(progressId + '-timeline');
    // Create details component even if timeline does not exist (displays empty state)
    let timelineHTML = '';
    if (timeline) {
        timelineHTML = timeline.innerHTML;
    }
    
    // Get the assistant message element
    const assistantElement = document.getElementById(assistantMessageId);
    if (!assistantElement) {
        removeMessage(progressId);
        return;
    }
    
    // Create details component
    const detailsId = 'details-' + Date.now() + '-' + messageCounter++;
    const detailsDiv = document.createElement('div');
    detailsDiv.id = detailsId;
    detailsDiv.className = 'message system progress-details';
    
    const contentWrapper = document.createElement('div');
    contentWrapper.className = 'message-content';
    
    const bubble = document.createElement('div');
    bubble.className = 'message-bubble progress-container completed';
    
    // Get timeline HTML content
    const hasContent = timelineHTML.trim().length > 0;
    
    // Check the timeline for incorrect items
    const hasError = timeline && timeline.querySelector('.timeline-item-error');
    
    // If there is an error, it will be collapsed by default; otherwise it will be expanded by default.
    const shouldExpand = !hasError;
    const expandedClass = shouldExpand ? 'expanded' : '';
    const toggleText = shouldExpand ? 'Collapse details' : 'Expand details';
    
    // Always show the details component, even if there is no content
    bubble.innerHTML = `
        <div class="progress-header">
            <span class="progress-title">📋 Penetration testing details</span>
            ${hasContent ? `<button class="progress-toggle" onclick="toggleProgressDetails('${detailsId}')">${toggleText}</button>` : ''}
        </div>
        ${hasContent ? `<div class="progress-timeline ${expandedClass}" id="${detailsId}-timeline">${timelineHTML}</div>` : '<div class="progress-timeline-empty">No process details yet (may be executed too fast or detailed events not triggered)</div>'}
    `;
    
    contentWrapper.appendChild(bubble);
    detailsDiv.appendChild(contentWrapper);
    
    // Insert the details component after the assistant message
    const messagesDiv = document.getElementById('chat-messages');
    // AssistantElement is a message div and needs to be inserted before its next sibling node
    if (assistantElement.nextSibling) {
        messagesDiv.insertBefore(detailsDiv, assistantElement.nextSibling);
    } else {
        // If there is no next sibling node, add it directly.
        messagesDiv.appendChild(detailsDiv);
    }
    
    // Remove original progress message
    removeMessage(progressId);
    
    // Scroll to bottom
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
}

// Handle streaming events
function handleStreamEvent(event, progressElement, progressId, 
                          getAssistantId, setAssistantId, getMcpIds, setMcpIds) {
    const timeline = document.getElementById(progressId + '-timeline');
    if (!timeline) return;
    
    switch (event.type) {
        case 'conversation':
            if (event.data && event.data.conversationId) {
                // Before updating, first obtain the original conversation ID corresponding to the task
                const taskState = progressTaskState.get(progressId);
                const originalConversationId = taskState?.conversationId;
                
                // Update task status
                updateProgressConversation(progressId, event.data.conversationId);
                
                // If the user has started a new conversation (currentConversationId is null),
                // And this conversation event comes from an old conversation, so currentConversationId will not be updated.
                if (currentConversationId === null && originalConversationId !== null) {
                    // The user has started a new conversation, ignoring the conversation event of the old conversation
                    // But still updates the task status so that the task information is displayed correctly
                    break;
                }
                
                // Update current conversation ID
                currentConversationId = event.data.conversationId;
                updateActiveConversation();
                addAttackChainButton(currentConversationId);
                loadActiveTasks();
                // Delayed refresh of conversation list to ensure user messages are saved and updated_at is updated
                // This will allow new conversations to appear correctly at the top of the recent conversations list
                // Use loadConversationsWithGroups to ensure that the group map cache is loaded correctly and can be displayed immediately regardless of whether there are groups or not.
                setTimeout(() => {
                    if (typeof loadConversationsWithGroups === 'function') {
                        loadConversationsWithGroups();
                    } else if (typeof loadConversations === 'function') {
                        loadConversations();
                    }
                }, 200);
            }
            break;
        case 'iteration':
            // Add iteration mark
            addTimelineItem(timeline, 'iteration', {
                title: `第 ${event.data?.iteration || 1} 轮迭代`,
                message: event.message,
                data: event.data
            });
            break;
            
        case 'thinking':
            // Show AI thinking content
            addTimelineItem(timeline, 'thinking', {
                title: '🤔 AI thinking',
                message: event.message,
                data: event.data
            });
            break;
            
        case 'tool_calls_detected':
            // Tool call detection
            addTimelineItem(timeline, 'tool_calls_detected', {
                title: `🔧 检测到 ${event.data?.count || 0} 个工具调用`,
                message: event.message,
                data: event.data
            });
            break;
            
        case 'tool_call':
            // Display tool call information
            const toolInfo = event.data || {};
            const toolName = toolInfo.toolName || 'Unknown tool';
            const index = toolInfo.index || 0;
            const total = toolInfo.total || 0;
            const toolCallId = toolInfo.toolCallId || null;
            
            // Add a tool call item and mark it as executing
            const toolCallItemId = addTimelineItem(timeline, 'tool_call', {
                title: `🔧 调用工具: ${escapeHtml(toolName)} (${index}/${total})`,
                message: event.message,
                data: toolInfo,
                expanded: false
            });
            
            // If there is a toolCallId, store the mapping relationship so that the status can be updated later.
            if (toolCallId && toolCallItemId) {
                toolCallStatusMap.set(toolCallId, {
                    itemId: toolCallItemId,
                    timeline: timeline
                });
                
                // Add executing status indicator
                updateToolCallStatus(toolCallId, 'running');
            }
            break;
            
        case 'tool_result':
            // Display tool execution results
            const resultInfo = event.data || {};
            const resultToolName = resultInfo.toolName || 'Unknown tool';
            const success = resultInfo.success !== false;
            const statusIcon = success ? '✅' : '❌';
            const resultToolCallId = resultInfo.toolCallId || null;
            
            // If there is an associated toolCallId, update the status of the tool call item
            if (resultToolCallId && toolCallStatusMap.has(resultToolCallId)) {
                updateToolCallStatus(resultToolCallId, success ? 'completed' : 'failed');
                // Remove from mapping (completed)
                toolCallStatusMap.delete(resultToolCallId);
            }
            
            addTimelineItem(timeline, 'tool_result', {
                title: `${statusIcon} 工具 ${escapeHtml(resultToolName)} 执行${success ? 'Finish' : 'Fail'}`,
                message: event.message,
                data: resultInfo,
                expanded: false
            });
            break;
            
        case 'progress':
            // Update progress status
            const progressTitle = document.querySelector(`#${progressId} .progress-title`);
            if (progressTitle) {
                progressTitle.textContent = '🔍 ' + event.message;
            }
            break;
        
        case 'cancelled':
            // Show error
            addTimelineItem(timeline, 'cancelled', {
                title: '⛔ Task canceled',
                message: event.message,
                data: event.data
            });
            
            // Update progress title to Canceled status
            const cancelTitle = document.querySelector(`#${progressId} .progress-title`);
            if (cancelTitle) {
                cancelTitle.textContent = '⛔ Task canceled';
            }
            
            // Update the progress container to the completed state (add completed class)
            const cancelProgressContainer = document.querySelector(`#${progressId} .progress-container`);
            if (cancelProgressContainer) {
                cancelProgressContainer.classList.add('completed');
            }
            
            // Complete progress task (marked as canceled)
            if (progressTaskState.has(progressId)) {
                finalizeProgressTask(progressId, 'Canceled');
            }
            
            // If the cancellation event contains messageId, it means there is an assistant message and the cancellation content needs to be displayed.
            if (event.data && event.data.messageId) {
                // Check if assistant message already exists
                let assistantId = event.data.messageId;
                let assistantElement = document.getElementById(assistantId);
                
                // If the helper message does not exist, create it
                if (!assistantElement) {
                    assistantId = addMessage('assistant', event.message, null, progressId);
                    setAssistantId(assistantId);
                    assistantElement = document.getElementById(assistantId);
                } else {
                    // If it already exists, update the content
                    const bubble = assistantElement.querySelector('.message-bubble');
                    if (bubble) {
                        bubble.innerHTML = escapeHtml(event.message).replace(/\n/g, '<br>');
                    }
                }
                
                // Integrate progress details into the tool invocation area (if not already there)
                if (assistantElement) {
                    const detailsId = 'process-details-' + assistantId;
                    if (!document.getElementById(detailsId)) {
                        integrateProgressToMCPSection(progressId, assistantId);
                    }
                    // Collapse details immediately (should collapse by default when canceling)
                    setTimeout(() => {
                        collapseAllProgressDetails(assistantId, progressId);
                    }, 100);
                }
            } else {
                // If there is no messageId, create the assistant message and integrate the details
                const assistantId = addMessage('assistant', event.message, null, progressId);
                setAssistantId(assistantId);
                
                // Integrate progress details into tool call area
                setTimeout(() => {
                    integrateProgressToMCPSection(progressId, assistantId);
                    // Make sure details are collapsed by default
                    collapseAllProgressDetails(assistantId, progressId);
                }, 100);
            }
            
            // Refresh task status immediately
            loadActiveTasks();
            break;
            
        case 'response':
            // Before updating, first obtain the original conversation ID corresponding to the task
            const responseTaskState = progressTaskState.get(progressId);
            const responseOriginalConversationId = responseTaskState?.conversationId;
            
            // Add assistant reply first
            const responseData = event.data || {};
            const mcpIds = responseData.mcpExecutionIds || [];
            setMcpIds(mcpIds);
            
            // Update conversation ID
            if (responseData.conversationId) {
                // If the user has started a new conversation (currentConversationId is null),
                // And this response event comes from an old conversation, so the currentConversationId will not be updated and no message will be added.
                if (currentConversationId === null && responseOriginalConversationId !== null) {
                    // The user has started a new conversation, ignoring the response event of the old conversation
                    // But still updates the task status so that the task information is displayed correctly
                    updateProgressConversation(progressId, responseData.conversationId);
                    break;
                }
                
                currentConversationId = responseData.conversationId;
                updateActiveConversation();
                addAttackChainButton(currentConversationId);
                updateProgressConversation(progressId, responseData.conversationId);
                loadActiveTasks();
            }
            
            // Add assistant reply and pass in progress ID to integrate details
            const assistantId = addMessage('assistant', event.message, mcpIds, progressId);
            setAssistantId(assistantId);
            
            // Integrate progress details into tool call area
            integrateProgressToMCPSection(progressId, assistantId);
            
            // Delay auto-collapse details (after 3 seconds)
            setTimeout(() => {
                collapseAllProgressDetails(assistantId, progressId);
            }, 3000);
            
            // Delay refreshing conversation list to ensure assistant messages are saved and updated_at is updated
            setTimeout(() => {
                loadConversations();
            }, 200);
            break;
            
        case 'error':
            // Show error
            addTimelineItem(timeline, 'error', {
                title: '❌ Error',
                message: event.message,
                data: event.data
            });
            
            // Update progress title to error status
            const errorTitle = document.querySelector(`#${progressId} .progress-title`);
            if (errorTitle) {
                errorTitle.textContent = '❌ Execution failed';
            }
            
            // Update the progress container to the completed state (add completed class)
            const progressContainer = document.querySelector(`#${progressId} .progress-container`);
            if (progressContainer) {
                progressContainer.classList.add('completed');
            }
            
            // Complete progress task (marked as failed)
            if (progressTaskState.has(progressId)) {
                finalizeProgressTask(progressId, 'Failed');
            }
            
            // If the error event contains messageId, it means there is an assistant message and the error content needs to be displayed.
            if (event.data && event.data.messageId) {
                // Check if assistant message already exists
                let assistantId = event.data.messageId;
                let assistantElement = document.getElementById(assistantId);
                
                // If the helper message does not exist, create it
                if (!assistantElement) {
                    assistantId = addMessage('assistant', event.message, null, progressId);
                    setAssistantId(assistantId);
                    assistantElement = document.getElementById(assistantId);
                } else {
                    // If it already exists, update the content
                    const bubble = assistantElement.querySelector('.message-bubble');
                    if (bubble) {
                        bubble.innerHTML = escapeHtml(event.message).replace(/\n/g, '<br>');
                    }
                }
                
                // Integrate progress details into the tool invocation area (if not already there)
                if (assistantElement) {
                    const detailsId = 'process-details-' + assistantId;
                    if (!document.getElementById(detailsId)) {
                        integrateProgressToMCPSection(progressId, assistantId);
                    }
                    // Collapse details immediately (should collapse by default on error)
                    setTimeout(() => {
                        collapseAllProgressDetails(assistantId, progressId);
                    }, 100);
                }
            } else {
                // If there is no messageId (such as an error when the task is already running), create a helper message and integrate the details
                const assistantId = addMessage('assistant', event.message, null, progressId);
                setAssistantId(assistantId);
                
                // Integrate progress details into tool call area
                setTimeout(() => {
                    integrateProgressToMCPSection(progressId, assistantId);
                    // Make sure details are collapsed by default
                    collapseAllProgressDetails(assistantId, progressId);
                }, 100);
            }
            
            // Refresh task status immediately (task status will be updated when execution fails)
            loadActiveTasks();
            break;
            
        case 'done':
            // Complete, update progress title (if progress message still exists)
            const doneTitle = document.querySelector(`#${progressId} .progress-title`);
            if (doneTitle) {
                doneTitle.textContent = '✅ Penetration test completed';
            }
            // Update conversation ID
            if (event.data && event.data.conversationId) {
                currentConversationId = event.data.conversationId;
                updateActiveConversation();
                addAttackChainButton(currentConversationId);
                updateProgressConversation(progressId, event.data.conversationId);
            }
            if (progressTaskState.has(progressId)) {
                finalizeProgressTask(progressId, 'Completed');
            }
            
            // Check the timeline for incorrect items
            const hasError = timeline && timeline.querySelector('.timeline-item-error');
            
            // Immediately refresh task status (ensure task status is synchronized)
            loadActiveTasks();
            
            // Delay refreshing the task status again (make sure the backend has completed the status update)
            setTimeout(() => {
                loadActiveTasks();
            }, 200);
            
            // Automatically collapse all details when completed (delay to ensure the response event has been processed)
            setTimeout(() => {
                const assistantIdFromDone = getAssistantId();
                if (assistantIdFromDone) {
                    collapseAllProgressDetails(assistantIdFromDone, progressId);
                } else {
                    // If unable to get assistant ID, try collapsing all details
                    collapseAllProgressDetails(null, progressId);
                }
                
                // If there is an error, make sure the details are collapsed (it should be collapsed by default on errors)
                if (hasError) {
                    // Make sure to collapse again (delay a little to make sure the DOM has been updated)
                    setTimeout(() => {
                        collapseAllProgressDetails(assistantIdFromDone || null, progressId);
                    }, 200);
                }
            }, 500);
            break;
    }
    
    // Automatically scroll to bottom
    const messagesDiv = document.getElementById('chat-messages');
    messagesDiv.scrollTop = messagesDiv.scrollHeight;
}

// Update tool call status
function updateToolCallStatus(toolCallId, status) {
    const mapping = toolCallStatusMap.get(toolCallId);
    if (!mapping) return;
    
    const item = document.getElementById(mapping.itemId);
    if (!item) return;
    
    const titleElement = item.querySelector('.timeline-item-title');
    if (!titleElement) return;
    
    // Remove previous state class
    item.classList.remove('tool-call-running', 'tool-call-completed', 'tool-call-failed');
    
    // Update styles and text based on status
    let statusText = '';
    if (status === 'running') {
        item.classList.add('tool-call-running');
        statusText = ' <span class="tool-status-badge tool-status-running">Executing...</span>';
    } else if (status === 'completed') {
        item.classList.add('tool-call-completed');
        statusText = ' <span class="tool-status-badge tool-status-completed">✅ Completed</span>';
    } else if (status === 'failed') {
        item.classList.add('tool-call-failed');
        statusText = ' <span class="tool-status-badge tool-status-failed">❌ Execution failed</span>';
    }
    
    // Update title (keep original text, append status)
    const originalText = titleElement.innerHTML;
    // Remove status flags that may have existed before
    const cleanText = originalText.replace(/\s*<span class="tool-status-badge[^>]*>.*?<\/span>/g, '');
    titleElement.innerHTML = cleanText + statusText;
}

// Add timeline item
function addTimelineItem(timeline, type, options) {
    const item = document.createElement('div');
    // Generate unique ID
    const itemId = 'timeline-item-' + Date.now() + '-' + Math.random().toString(36).substr(2, 9);
    item.id = itemId;
    item.className = `timeline-item timeline-item-${type}`;
    
    // Use the passed createdAt time, or the current time if none (backwards compatible)
    let eventTime;
    if (options.createdAt) {
        // Process strings or Date objects
        if (typeof options.createdAt === 'string') {
            eventTime = new Date(options.createdAt);
        } else if (options.createdAt instanceof Date) {
            eventTime = options.createdAt;
        } else {
            eventTime = new Date(options.createdAt);
        }
        // If parsing fails, use the current time
        if (isNaN(eventTime.getTime())) {
            eventTime = new Date();
        }
    } else {
        eventTime = new Date();
    }
    
    const time = eventTime.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    
    let content = `
        <div class="timeline-item-header">
            <span class="timeline-item-time">${time}</span>
            <span class="timeline-item-title">${escapeHtml(options.title || '')}</span>
        </div>
    `;
    
    // Add details based on type
    if (type === 'thinking' && options.message) {
        content += `<div class="timeline-item-content">${formatMarkdown(options.message)}</div>`;
    } else if (type === 'tool_call' && options.data) {
        const data = options.data;
        const args = data.argumentsObj || (data.arguments ? JSON.parse(data.arguments) : {});
        content += `
            <div class="timeline-item-content">
                <div class="tool-details">
                    <div class="tool-arg-section">
                        <strong>Parameter:</strong>
                        <pre class="tool-args">${escapeHtml(JSON.stringify(args, null, 2))}</pre>
                    </div>
                </div>
            </div>
        `;
    } else if (type === 'tool_result' && options.data) {
        const data = options.data;
        const isError = data.isError || !data.success;
        const result = data.result || data.error || 'No results';
        // Make sure result is a string
        const resultStr = typeof result === 'string' ? result : JSON.stringify(result);
        content += `
            <div class="timeline-item-content">
                <div class="tool-result-section ${isError ? 'error' : 'success'}">
                    <strong>Execution result:</strong>
                    <pre class="tool-result">${escapeHtml(resultStr)}</pre>
                    ${data.executionId ? `<div class="tool-execution-id">Execution ID:<code>${escapeHtml(data.executionId)}</code></div>` : ''}
                </div>
            </div>
        `;
    } else if (type === 'cancelled') {
        content += `
            <div class="timeline-item-content">
                ${escapeHtml(options.message || 'Task canceled')}
            </div>
        `;
    }
    
    item.innerHTML = content;
    timeline.appendChild(item);
    
    // Automatically expand details
    const expanded = timeline.classList.contains('expanded');
    if (!expanded && (type === 'tool_call' || type === 'tool_result')) {
        // For tool calls and results, a summary is shown by default
    }
    
    // Return item ID for subsequent updates
    return itemId;
}

// Load active task list
async function loadActiveTasks(showErrors = false) {
    const bar = document.getElementById('active-tasks-bar');
    try {
        const response = await apiFetch('/api/agent-loop/tasks');
        const result = await response.json().catch(() => ({}));

        if (!response.ok) {
            throw new Error(result.error || 'Failed to get active tasks');
        }

        renderActiveTasks(result.tasks || []);
    } catch (error) {
        console.error('Failed to get active tasks:', error);
        if (showErrors && bar) {
            bar.style.display = 'block';
            bar.innerHTML = `<div class="active-task-error">Unable to get task status: ${escapeHtml(error.message)}</div>`;
        }
    }
}

function renderActiveTasks(tasks) {
    const bar = document.getElementById('active-tasks-bar');
    if (!bar) return;

    const normalizedTasks = Array.isArray(tasks) ? tasks : [];
    conversationExecutionTracker.update(normalizedTasks);
    if (typeof updateAttackChainAvailability === 'function') {
        updateAttackChainAvailability();
    }

    if (normalizedTasks.length === 0) {
        bar.style.display = 'none';
        bar.innerHTML = '';
        return;
    }

    bar.style.display = 'flex';
    bar.innerHTML = '';

    normalizedTasks.forEach(task => {
        const item = document.createElement('div');
        item.className = 'active-task-item';

        const startedTime = task.startedAt ? new Date(task.startedAt) : null;
        const timeText = startedTime && !isNaN(startedTime.getTime())
            ? startedTime.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
            : '';

        // Display different text based on task status
        const statusMap = {
            'running': 'Executing',
            'cancelling': 'Canceling',
            'failed': 'Execution failed',
            'timeout': 'Execution timeout',
            'cancelled': 'Canceled',
            'completed': 'Completed'
        };
        const statusText = statusMap[task.status] || 'Executing';
        const isFinalStatus = ['failed', 'timeout', 'cancelled', 'completed'].includes(task.status);

        item.innerHTML = `
            <div class="active-task-info">
                <span class="active-task-status">${statusText}</span>
                <span class="active-task-message">${escapeHtml(task.message || 'Unnamed task')}</span>
            </div>
            <div class="active-task-actions">
                ${timeText ? `<span class="active-task-time">${timeText}</span>` : ''}
                ${!isFinalStatus ? '<button class="active-task-cancel">Stop task</button>' : ''}
            </div>
        `;

        // Only non-final tasks display the Stop button
        if (!isFinalStatus) {
            const cancelBtn = item.querySelector('.active-task-cancel');
            if (cancelBtn) {
                cancelBtn.onclick = () => cancelActiveTask(task.conversationId, cancelBtn);
                if (task.status === 'cancelling') {
                    cancelBtn.disabled = true;
                    cancelBtn.textContent = 'Canceling...';
                }
            }
        }

        bar.appendChild(item);
    });
}

async function cancelActiveTask(conversationId, button) {
    if (!conversationId) return;
    const originalText = button.textContent;
    button.disabled = true;
    button.textContent = 'Canceling...';

    try {
        await requestCancel(conversationId);
        loadActiveTasks();
    } catch (error) {
        console.error('Failed to cancel task:', error);
        alert('Failed to cancel task:' + error.message);
        button.disabled = false;
        button.textContent = originalText;
    }
}

// Monitor panel status
const monitorState = {
    executions: [],
    stats: {},
    lastFetchedAt: null,
    pagination: {
        page: 1,
        pageSize: (() => {
            // Read the number of saved displays per page from localStorage, the default is 20
            const saved = localStorage.getItem('monitorPageSize');
            return saved ? parseInt(saved, 10) : 20;
        })(),
        total: 0,
        totalPages: 0
    }
};

function openMonitorPanel() {
    // Switch to the MCP monitoring page
    if (typeof switchPage === 'function') {
        switchPage('mcp-monitor');
    }
    // Initialize the display quantity selector per page
    initializeMonitorPageSize();
}

// Initialize the display quantity selector per page
function initializeMonitorPageSize() {
    const pageSizeSelect = document.getElementById('monitor-page-size');
    if (pageSizeSelect) {
        pageSizeSelect.value = monitorState.pagination.pageSize;
    }
}

// Change the number displayed per page
function changeMonitorPageSize() {
    const pageSizeSelect = document.getElementById('monitor-page-size');
    if (!pageSizeSelect) {
        return;
    }
    
    const newPageSize = parseInt(pageSizeSelect.value, 10);
    if (isNaN(newPageSize) || newPageSize <= 0) {
        return;
    }
    
    // Save to localStorage
    localStorage.setItem('monitorPageSize', newPageSize.toString());
    
    // Update status
    monitorState.pagination.pageSize = newPageSize;
    monitorState.pagination.page = 1; // Reset to first page
    
    // Refresh data
    refreshMonitorPanel(1);
}

function closeMonitorPanel() {
    // No need to close functionality anymore since now it's the page instead of the modal
    // You can switch back to the conversation page if needed
    if (typeof switchPage === 'function') {
        switchPage('chat');
    }
}

async function refreshMonitorPanel(page = null) {
    const statsContainer = document.getElementById('monitor-stats');
    const execContainer = document.getElementById('monitor-executions');

    try {
        // If a page number is specified, use the specified page number, otherwise use the current page number
        const currentPage = page !== null ? page : monitorState.pagination.page;
        const pageSize = monitorState.pagination.pageSize;
        
        // Get current filter conditions
        const statusFilter = document.getElementById('monitor-status-filter');
        const toolFilter = document.getElementById('monitor-tool-filter');
        const currentStatusFilter = statusFilter ? statusFilter.value : 'all';
        const currentToolFilter = toolFilter ? (toolFilter.value.trim() || 'all') : 'all';
        
        // Build request URL
        let url = `/api/monitor?page=${currentPage}&page_size=${pageSize}`;
        if (currentStatusFilter && currentStatusFilter !== 'all') {
            url += `&status=${encodeURIComponent(currentStatusFilter)}`;
        }
        if (currentToolFilter && currentToolFilter !== 'all') {
            url += `&tool=${encodeURIComponent(currentToolFilter)}`;
        }
        
        const response = await apiFetch(url, { method: 'GET' });
        const result = await response.json().catch(() => ({}));
        if (!response.ok) {
            throw new Error(result.error || 'Failed to obtain monitoring data');
        }

        monitorState.executions = Array.isArray(result.executions) ? result.executions : [];
        monitorState.stats = result.stats || {};
        monitorState.lastFetchedAt = new Date();
        
        // Update pagination information
        if (result.total !== undefined) {
            monitorState.pagination = {
                page: result.page || currentPage,
                pageSize: result.page_size || pageSize,
                total: result.total || 0,
                totalPages: result.total_pages || 1
            };
        }

        renderMonitorStats(monitorState.stats, monitorState.lastFetchedAt);
        renderMonitorExecutions(monitorState.executions, currentStatusFilter);
        renderMonitorPagination();
        
        // Initialize the display quantity selector per page
        initializeMonitorPageSize();
    } catch (error) {
        console.error('Failed to refresh monitoring panel:', error);
        if (statsContainer) {
            statsContainer.innerHTML = `<div class="monitor-error">Unable to load statistics: ${escapeHtml(error.message)}</div>`;
        }
        if (execContainer) {
            execContainer.innerHTML = `<div class="monitor-error">Unable to load execution record: ${escapeHtml(error.message)}</div>`;
        }
    }
}

// Handling tool search input (anti-shake)
let toolFilterDebounceTimer = null;
function handleToolFilterInput() {
    // Clear previous timer
    if (toolFilterDebounceTimer) {
        clearTimeout(toolFilterDebounceTimer);
    }
    
    // Set a new timer and perform filtering after 500ms
    toolFilterDebounceTimer = setTimeout(() => {
        applyMonitorFilters();
    }, 500);
}

async function applyMonitorFilters() {
    const statusFilter = document.getElementById('monitor-status-filter');
    const toolFilter = document.getElementById('monitor-tool-filter');
    const status = statusFilter ? statusFilter.value : 'all';
    const tool = toolFilter ? (toolFilter.value.trim() || 'all') : 'all';
    // Re-fetch data from backend when filter conditions change
    await refreshMonitorPanelWithFilter(status, tool);
}

async function refreshMonitorPanelWithFilter(statusFilter = 'all', toolFilter = 'all') {
    const statsContainer = document.getElementById('monitor-stats');
    const execContainer = document.getElementById('monitor-executions');

    try {
        const currentPage = 1; // Reset to first page when filtering
        const pageSize = monitorState.pagination.pageSize;
        
        // Build request URL
        let url = `/api/monitor?page=${currentPage}&page_size=${pageSize}`;
        if (statusFilter && statusFilter !== 'all') {
            url += `&status=${encodeURIComponent(statusFilter)}`;
        }
        if (toolFilter && toolFilter !== 'all') {
            url += `&tool=${encodeURIComponent(toolFilter)}`;
        }
        
        const response = await apiFetch(url, { method: 'GET' });
        const result = await response.json().catch(() => ({}));
        if (!response.ok) {
            throw new Error(result.error || 'Failed to obtain monitoring data');
        }

        monitorState.executions = Array.isArray(result.executions) ? result.executions : [];
        monitorState.stats = result.stats || {};
        monitorState.lastFetchedAt = new Date();
        
        // Update pagination information
        if (result.total !== undefined) {
            monitorState.pagination = {
                page: result.page || currentPage,
                pageSize: result.page_size || pageSize,
                total: result.total || 0,
                totalPages: result.total_pages || 1
            };
        }

        renderMonitorStats(monitorState.stats, monitorState.lastFetchedAt);
        renderMonitorExecutions(monitorState.executions, statusFilter);
        renderMonitorPagination();
        
        // Initialize the display quantity selector per page
        initializeMonitorPageSize();
    } catch (error) {
        console.error('Failed to refresh monitoring panel:', error);
        if (statsContainer) {
            statsContainer.innerHTML = `<div class="monitor-error">Unable to load statistics: ${escapeHtml(error.message)}</div>`;
        }
        if (execContainer) {
            execContainer.innerHTML = `<div class="monitor-error">Unable to load execution record: ${escapeHtml(error.message)}</div>`;
        }
    }
}


function renderMonitorStats(statsMap = {}, lastFetchedAt = null) {
    const container = document.getElementById('monitor-stats');
    if (!container) {
        return;
    }

    const entries = Object.values(statsMap);
    if (entries.length === 0) {
        container.innerHTML = '<div class="monitor-empty">No statistics yet</div>';
        return;
    }

    // Calculate overall summary
    const totals = entries.reduce(
        (acc, item) => {
            acc.total += item.totalCalls || 0;
            acc.success += item.successCalls || 0;
            acc.failed += item.failedCalls || 0;
            const lastCall = item.lastCallTime ? new Date(item.lastCallTime) : null;
            if (lastCall && (!acc.lastCallTime || lastCall > acc.lastCallTime)) {
                acc.lastCallTime = lastCall;
            }
            return acc;
        },
        { total: 0, success: 0, failed: 0, lastCallTime: null }
    );

    const successRate = totals.total > 0 ? ((totals.success / totals.total) * 100).toFixed(1) : '0.0';
    const lastUpdatedText = lastFetchedAt ? lastFetchedAt.toLocaleString('zh-CN') : 'N/A';
    const lastCallText = totals.lastCallTime ? totals.lastCallTime.toLocaleString('zh-CN') : 'No calls yet';

    let html = `
        <div class="monitor-stat-card">
            <h4>Total calls</h4>
            <div class="monitor-stat-value">${totals.total}</div>
            <div class="monitor-stat-meta">Success ${totals.success} / Failure ${totals.failed}</div>
        </div>
        <div class="monitor-stat-card">
            <h4>Success rate</h4>
            <div class="monitor-stat-value">${successRate}%</div>
            <div class="monitor-stat-meta">Statistics are called from all tools</div>
        </div>
        <div class="monitor-stat-card">
            <h4>Last call</h4>
            <div class="monitor-stat-value" style="font-size:1rem;">${lastCallText}</div>
            <div class="monitor-stat-meta">Last refresh time: ${lastUpdatedText}</div>
        </div>
    `;

    // Show statistics for up to the top 4 tools (filter out tools with totalCalls of 0)
    const topTools = entries
        .filter(tool => (tool.totalCalls || 0) > 0)
        .slice()
        .sort((a, b) => (b.totalCalls || 0) - (a.totalCalls || 0))
        .slice(0, 4);

    topTools.forEach(tool => {
        const toolSuccessRate = tool.totalCalls > 0 ? ((tool.successCalls || 0) / tool.totalCalls * 100).toFixed(1) : '0.0';
        html += `
            <div class="monitor-stat-card">
                <h4>${escapeHtml(tool.toolName || 'Unknown tool')}</h4>
                <div class="monitor-stat-value">${tool.totalCalls || 0}</div>
                <div class="monitor-stat-meta">
                    成功 ${tool.successCalls || 0} / 失败 ${tool.failedCalls || 0} · 成功率 ${toolSuccessRate}%
                </div>
            </div>
        `;
    });

    container.innerHTML = `<div class="monitor-stats-grid">${html}</div>`;
}

function renderMonitorExecutions(executions = [], statusFilter = 'all') {
    const container = document.getElementById('monitor-executions');
    if (!container) {
        return;
    }

    if (!Array.isArray(executions) || executions.length === 0) {
        // Show different prompts based on whether there are filter conditions
        const toolFilter = document.getElementById('monitor-tool-filter');
        const currentToolFilter = toolFilter ? toolFilter.value : 'all';
        const hasFilter = (statusFilter && statusFilter !== 'all') || (currentToolFilter && currentToolFilter !== 'all');
        if (hasFilter) {
            container.innerHTML = '<div class="monitor-empty">There are no records under the current filter conditions.</div>';
        } else {
            container.innerHTML = '<div class="monitor-empty">No execution record yet</div>';
        }
        // Hide batch action bar
        const batchActions = document.getElementById('monitor-batch-actions');
        if (batchActions) {
            batchActions.style.display = 'none';
        }
        return;
    }

    // Since the filtering has been completed in the backend, all incoming execution records are used directly here.
    // There is no need for the front-end to filter again because the back-end has already returned the filtered data.
    const rows = executions
        .map(exec => {
            const status = (exec.status || 'unknown').toLowerCase();
            const statusClass = `monitor-status-chip ${status}`;
            const statusLabel = getStatusText(status);
            const startTime = exec.startTime ? new Date(exec.startTime).toLocaleString('zh-CN') : 'Unknown';
            const duration = formatExecutionDuration(exec.startTime, exec.endTime);
            const toolName = escapeHtml(exec.toolName || 'Unknown tool');
            const executionId = escapeHtml(exec.id || '');
            return `
                <tr>
                    <td>
                        <input type="checkbox" class="monitor-execution-checkbox" value="${executionId}" onchange="updateBatchActionsState()" />
                    </td>
                    <td>${toolName}</td>
                    <td><span class="${statusClass}">${statusLabel}</span></td>
                    <td>${startTime}</td>
                    <td>${duration}</td>
                    <td>
                        <div class="monitor-execution-actions">
                            <button class="btn-secondary" onclick="showMCPDetail('${executionId}')">Check the details</button>
                            <button class="btn-secondary btn-delete" onclick="deleteExecution('${executionId}')" title="Delete this execution record">Delete</button>
                        </div>
                    </td>
                </tr>
            `;
        })
        .join('');

    // First remove the old table container and loading prompt (keep the paging control)
    const oldTableContainer = container.querySelector('.monitor-table-container');
    if (oldTableContainer) {
        oldTableContainer.remove();
    }
    // Clear "Loading..." and other prompt messages
    const oldEmpty = container.querySelector('.monitor-empty');
    if (oldEmpty) {
        oldEmpty.remove();
    }
    
    // Create table container
    const tableContainer = document.createElement('div');
    tableContainer.className = 'monitor-table-container';
    tableContainer.innerHTML = `
        <table class="monitor-table">
            <thead>
                <tr>
                    <th style="width: 40px;">
                        <input type="checkbox" id="monitor-select-all" onchange="toggleSelectAll(this)" />
                    </th>
                    <th>Tool</th>
                    <th>State</th>
                    <th>Start time</th>
                    <th>Time consuming</th>
                    <th>Operate</th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>
    `;
    
    // Insert table before paging control (if paging control exists)
    const existingPagination = container.querySelector('.monitor-pagination');
    if (existingPagination) {
        container.insertBefore(tableContainer, existingPagination);
    } else {
        container.appendChild(tableContainer);
    }
    
    // Update batch operation status
    updateBatchActionsState();
}

// Render monitoring panel paging control
function renderMonitorPagination() {
    const container = document.getElementById('monitor-executions');
    if (!container) return;
    
    // Remove old paging controls
    const oldPagination = container.querySelector('.monitor-pagination');
    if (oldPagination) {
        oldPagination.remove();
    }
    
    const { page, totalPages, total, pageSize } = monitorState.pagination;
    
    // Always show paging controls
    const pagination = document.createElement('div');
    pagination.className = 'monitor-pagination';
    
    // Handle the case of no data
    const startItem = total === 0 ? 0 : (page - 1) * pageSize + 1;
    const endItem = total === 0 ? 0 : Math.min(page * pageSize, total);
    
    pagination.innerHTML = `
        <div class="pagination-info">
            <span>Display ${startItem}-${endItem} / total ${total} records</span>
            <label class="pagination-page-size">
Show per page
                <select id="monitor-page-size" onchange="changeMonitorPageSize()">
                    <option value="10" ${pageSize === 10 ? 'selected' : ''}>10</option>
                    <option value="20" ${pageSize === 20 ? 'selected' : ''}>20</option>
                    <option value="50" ${pageSize === 50 ? 'selected' : ''}>50</option>
                    <option value="100" ${pageSize === 100 ? 'selected' : ''}>100</option>
                </select>
            </label>
        </div>
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="refreshMonitorPanel(1)" ${page === 1 || total === 0 ? 'disabled' : ''}>Front page</button>
            <button class="btn-secondary" onclick="refreshMonitorPanel(${page - 1})" ${page === 1 || total === 0 ? 'disabled' : ''}>Previous page</button>
            <span class="pagination-page">Page ${page} / ${totalPages || 1}</span>
            <button class="btn-secondary" onclick="refreshMonitorPanel(${page + 1})" ${page >= totalPages || total === 0 ? 'disabled' : ''}>Next page</button>
            <button class="btn-secondary" onclick="refreshMonitorPanel(${totalPages || 1})" ${page >= totalPages || total === 0 ? 'disabled' : ''}>Last page</button>
        </div>
    `;
    
    container.appendChild(pagination);
    
    // Initialize the display quantity selector per page
    initializeMonitorPageSize();
}

// Delete execution record
async function deleteExecution(executionId) {
    if (!executionId) {
        return;
    }
    
    // Confirm deletion
    if (!confirm('Are you sure you want to delete this execution record? This operation is irreversible.')) {
        return;
    }
    
    try {
        const response = await apiFetch(`/api/monitor/execution/${executionId}`, {
            method: 'DELETE'
        });
        
        if (!response.ok) {
            const error = await response.json().catch(() => ({}));
            throw new Error(error.error || 'Failed to delete execution record');
        }
        
        // Refresh the current page after successful deletion
        const currentPage = monitorState.pagination.page;
        await refreshMonitorPanel(currentPage);
        
        alert('Execution record deleted');
    } catch (error) {
        console.error('Failed to delete execution record:', error);
        alert('Failed to delete execution record:' + error.message);
    }
}

// Update batch operation status
function updateBatchActionsState() {
    const checkboxes = document.querySelectorAll('.monitor-execution-checkbox:checked');
    const selectedCount = checkboxes.length;
    const batchActions = document.getElementById('monitor-batch-actions');
    const selectedCountSpan = document.getElementById('monitor-selected-count');
    
    if (selectedCount > 0) {
        if (batchActions) {
            batchActions.style.display = 'flex';
        }
        if (selectedCountSpan) {
            selectedCountSpan.textContent = `已选择 ${selectedCount} 项`;
        }
    } else {
        if (batchActions) {
            batchActions.style.display = 'none';
        }
    }
    
    // Update Select All checkbox status
    const selectAllCheckbox = document.getElementById('monitor-select-all');
    if (selectAllCheckbox) {
        const allCheckboxes = document.querySelectorAll('.monitor-execution-checkbox');
        const allChecked = allCheckboxes.length > 0 && Array.from(allCheckboxes).every(cb => cb.checked);
        selectAllCheckbox.checked = allChecked;
        selectAllCheckbox.indeterminate = selectedCount > 0 && selectedCount < allCheckboxes.length;
    }
}

// Toggle select all
function toggleSelectAll(checkbox) {
    const checkboxes = document.querySelectorAll('.monitor-execution-checkbox');
    checkboxes.forEach(cb => {
        cb.checked = checkbox.checked;
    });
    updateBatchActionsState();
}

// Select all
function selectAllExecutions() {
    const checkboxes = document.querySelectorAll('.monitor-execution-checkbox');
    checkboxes.forEach(cb => {
        cb.checked = true;
    });
    const selectAllCheckbox = document.getElementById('monitor-select-all');
    if (selectAllCheckbox) {
        selectAllCheckbox.checked = true;
        selectAllCheckbox.indeterminate = false;
    }
    updateBatchActionsState();
}

// Deselect all
function deselectAllExecutions() {
    const checkboxes = document.querySelectorAll('.monitor-execution-checkbox');
    checkboxes.forEach(cb => {
        cb.checked = false;
    });
    const selectAllCheckbox = document.getElementById('monitor-select-all');
    if (selectAllCheckbox) {
        selectAllCheckbox.checked = false;
        selectAllCheckbox.indeterminate = false;
    }
    updateBatchActionsState();
}

// Delete execution records in batches
async function batchDeleteExecutions() {
    const checkboxes = document.querySelectorAll('.monitor-execution-checkbox:checked');
    if (checkboxes.length === 0) {
        alert('Please select the execution record to be deleted first');
        return;
    }
    
    const ids = Array.from(checkboxes).map(cb => cb.value);
    const count = ids.length;
    
    // Confirm deletion
    if (!confirm(`确定要删除选中的 ${count} 条执行记录吗？此操作不可恢复。`)) {
        return;
    }
    
    try {
        const response = await apiFetch('/api/monitor/executions', {
            method: 'DELETE',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({ ids: ids })
        });
        
        if (!response.ok) {
            const error = await response.json().catch(() => ({}));
            throw new Error(error.error || 'Batch deletion of execution records failed');
        }
        
        const result = await response.json().catch(() => ({}));
        const deletedCount = result.deleted || count;
        
        // Refresh the current page after successful deletion
        const currentPage = monitorState.pagination.page;
        await refreshMonitorPanel(currentPage);
        
        alert(`成功删除 ${deletedCount} 条执行记录`);
    } catch (error) {
        console.error('Batch deletion of execution records failed:', error);
        alert('Batch deletion of execution records failed:' + error.message);
    }
}

function formatExecutionDuration(start, end) {
    if (!start) {
        return 'Unknown';
    }
    const startTime = new Date(start);
    const endTime = end ? new Date(end) : new Date();
    if (Number.isNaN(startTime.getTime()) || Number.isNaN(endTime.getTime())) {
        return 'Unknown';
    }
    const diffMs = Math.max(0, endTime - startTime);
    const seconds = Math.floor(diffMs / 1000);
    if (seconds < 60) {
        return `${seconds} 秒`;
    }
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) {
        const remain = seconds % 60;
        return remain > 0 ? `${minutes} 分 ${remain} 秒` : `${minutes} 分`;
    }
    const hours = Math.floor(minutes / 60);
    const remainMinutes = minutes % 60;
    return remainMinutes > 0 ? `${hours} 小时 ${remainMinutes} 分` : `${hours} 小时`;
}
