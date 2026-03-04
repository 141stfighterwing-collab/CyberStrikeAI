// Role management related functions
let currentRole = localStorage.getItem('currentRole') || '';
let roles = [];
let rolesSearchKeyword = ''; // Role search keywords
let rolesSearchTimeout = null; // Search for anti-shake timer
let allRoleTools = []; // Stores a list of all tools (used for character tool selection)
let roleToolsPagination = {
    page: 1,
    pageSize: 20,
    total: 0,
    totalPages: 1
};
let roleToolsSearchKeyword = ''; // Tool search keywords
let roleToolStateMap = new Map(); // Tool status mapping: toolKey -> { enabled: boolean, ... }
let roleUsesAllTools = false; // Mark whether the role uses all tools (when tools are not configured)
let totalEnabledToolsInMCP = 0; // Total number of enabled tools (obtained from MCP management, obtained from API response)
let roleConfiguredTools = new Set(); // Tool list for role configuration (used to determine which tools should be selected)

// Skills related
let allRoleSkills = []; // Store a list of all skills
let roleSkillsSearchKeyword = ''; // Skills search keywords
let roleSelectedSkills = new Set(); // Selected skills collection

// Sort the role list: default role first, others by name
function sortRoles(rolesArray) {
    const sortedRoles = [...rolesArray];
    // Separate the "default" role
    const defaultRole = sortedRoles.find(r => r.name === 'Default');
    const otherRoles = sortedRoles.filter(r => r.name !== 'Default');
    
    // Other roles are sorted by name, keeping a fixed order
    otherRoles.sort((a, b) => {
        const nameA = a.name || '';
        const nameB = b.name || '';
        return nameA.localeCompare(nameB, 'zh-CN');
    });
    
    // Put the "default" role first, with other roles following in sorted order
    const result = defaultRole ? [defaultRole, ...otherRoles] : otherRoles;
    return result;
}

// Load all characters
async function loadRoles() {
    try {
        const response = await apiFetch('/api/roles');
        if (!response.ok) {
            throw new Error('Failed to load character');
        }
        const data = await response.json();
        roles = data.roles || [];
        updateRoleSelectorDisplay();
        renderRoleSelectionSidebar(); // Render sidebar role list
        return roles;
    } catch (error) {
        console.error('Failed to load character:', error);
        showNotification('Failed to load character:' + error.message, 'error');
        return [];
    }
}

// Handle role changes
function handleRoleChange(roleName) {
    const oldRole = currentRole;
    currentRole = roleName || '';
    localStorage.setItem('currentRole', currentRole);
    updateRoleSelectorDisplay();
    renderRoleSelectionSidebar(); // Update sidebar selection status
    
    // When switching roles, if the tool list is already loaded, mark it as needing to be reloaded
    // This way the tool list will be reloaded with the new role the next time @tool suggestions are triggered.
    if (oldRole !== currentRole && typeof window !== 'undefined') {
        // Notify chat.js that the tool list needs to be reloaded by setting a flag
        window._mentionToolsRoleChanged = true;
    }
}

// Update character selector display
function updateRoleSelectorDisplay() {
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    const roleSelectorIcon = document.getElementById('role-selector-icon');
    const roleSelectorText = document.getElementById('role-selector-text');
    
    if (!roleSelectorBtn || !roleSelectorIcon || !roleSelectorText) return;

    let selectedRole;
    if (currentRole && currentRole !== 'Default') {
        selectedRole = roles.find(r => r.name === currentRole);
    } else {
        selectedRole = roles.find(r => r.name === 'Default');
    }

    if (selectedRole) {
        // Use the icon from the configuration, or the default icon if there is none
        let icon = selectedRole.icon || '🔵';
        // If the icon is in Unicode escape format (\U0001F3C6), it needs to be converted to emoji
        if (icon && typeof icon === 'string') {
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If conversion fails, use default icon
                    console.warn('Converting icon to Unicode escaping failed:', icon, e);
                    icon = '🔵';
                }
            }
        }
        roleSelectorIcon.textContent = icon;
        roleSelectorText.textContent = selectedRole.name || 'Default';
    } else {
        // Default role
        roleSelectorIcon.textContent = '🔵';
        roleSelectorText.textContent = 'Default';
    }
}

// Render the main content area role selection list
function renderRoleSelectionSidebar() {
    const roleList = document.getElementById('role-selection-list');
    if (!roleList) return;

    // Clear list
    roleList.innerHTML = '';

    // Get the icon according to the role configuration, if there is no configuration, use the default icon
    function getRoleIcon(role) {
        if (role.icon) {
            // If the icon is in Unicode escape format (\U0001F3C6), it needs to be converted to emoji
            let icon = role.icon;
            // Check if it is a Unicode escape format (may contain quotes)
            const unicodeMatch = icon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    icon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If the conversion fails, the original value is used
                    console.warn('Converting icon to Unicode escaping failed:', icon, e);
                }
            }
            return icon;
        }
        // If no icon is configured, a default icon is generated based on the first character of the role name.
        // Use some common default icons
        return '👤';
    }
    
    // Sort roles: Default role first, others sorted by name
    const sortedRoles = sortRoles(roles);
    
    // Show only enabled roles
    const enabledSortedRoles = sortedRoles.filter(r => r.enabled !== false);
    
    enabledSortedRoles.forEach(role => {
        const isDefaultRole = role.name === 'Default';
        const isSelected = isDefaultRole ? (currentRole === '' || currentRole === 'Default') : (currentRole === role.name);
        const roleItem = document.createElement('div');
        roleItem.className = 'role-selection-item-main' + (isSelected ? ' selected' : '');
        roleItem.onclick = () => {
            selectRole(role.name);
            closeRoleSelectionPanel(); // Automatically close panel after selection
        };
        const icon = getRoleIcon(role);
        
        // Handle the description of the default role
        let description = role.description || 'No description yet';
        if (isDefaultRole && !role.description) {
            description = 'The default role does not carry additional user prompt words and uses the default MCP.';
        }
        
        roleItem.innerHTML = `
            <div class="role-selection-item-icon-main">${icon}</div>
            <div class="role-selection-item-content-main">
                <div class="role-selection-item-name-main">${escapeHtml(role.name)}</div>
                <div class="role-selection-item-description-main">${escapeHtml(description)}</div>
            </div>
            ${isSelected ? '<div class="role-selection-checkmark-main">✓</div>' : ''}
        `;
        roleList.appendChild(roleItem);
    });
}

// Select role
function selectRole(roleName) {
    // Map "default" to an empty string (representing the default role)
    if (roleName === 'Default') {
        roleName = '';
    }
    handleRoleChange(roleName);
    renderRoleSelectionSidebar(); // Re-render to update selected state
}

// Toggle display/hide of character selection panel
function toggleRoleSelectionPanel() {
    const panel = document.getElementById('role-selection-panel');
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    if (!panel) return;
    
    const isHidden = panel.style.display === 'none' || !panel.style.display;
    
    if (isHidden) {
        panel.style.display = 'flex'; // Use flex layout
        // Add visual feedback of open status
        if (roleSelectorBtn) {
            roleSelectorBtn.classList.add('active');
        }
        
        // Make sure the panel is rendered before checking the position
        setTimeout(() => {
            const wrapper = document.querySelector('.role-selector-wrapper');
            if (wrapper) {
                const rect = wrapper.getBoundingClientRect();
                const panelHeight = panel.offsetHeight || 400;
                const viewportHeight = window.innerHeight;
                
                // If the top of the panel exceeds the viewport, scroll to the appropriate position
                if (rect.top - panelHeight < 0) {
                    const scrollY = window.scrollY + rect.top - panelHeight - 20;
                    window.scrollTo({ top: Math.max(0, scrollY), behavior: 'smooth' });
                }
            }
        }, 10);
    } else {
        panel.style.display = 'none';
        // Remove visual feedback of open state
        if (roleSelectorBtn) {
            roleSelectorBtn.classList.remove('active');
        }
    }
}

// Close the character selection panel (automatically called after selecting a character)
function closeRoleSelectionPanel() {
    const panel = document.getElementById('role-selection-panel');
    const roleSelectorBtn = document.getElementById('role-selector-btn');
    if (panel) {
        panel.style.display = 'none';
    }
    if (roleSelectorBtn) {
        roleSelectorBtn.classList.remove('active');
    }
}

// Escape HTML
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// Refresh role list
async function refreshRoles() {
    await loadRoles();
    // Check whether the current page is a role management page
    const currentPage = typeof window.currentPage === 'function' ? window.currentPage() : (window.currentPage || 'chat');
    if (currentPage === 'roles-management') {
        renderRolesList();
    }
    // Always update sidebar role selection list
    renderRoleSelectionSidebar();
    showNotification('Refreshed', 'success');
}

// Render role list
function renderRolesList() {
    const rolesList = document.getElementById('roles-list');
    if (!rolesList) return;

    // Filter roles (based on search keywords)
    let filteredRoles = roles;
    if (rolesSearchKeyword) {
        const keyword = rolesSearchKeyword.toLowerCase();
        filteredRoles = roles.filter(role => 
            role.name.toLowerCase().includes(keyword) ||
            (role.description && role.description.toLowerCase().includes(keyword))
        );
    }

    if (filteredRoles.length === 0) {
        rolesList.innerHTML = '<div class="empty-state">' + 
            (rolesSearchKeyword ? 'No matching role found' : 'No role yet') +
            '</div>';
        return;
    }

    // Sort roles: Default role first, others sorted by name
    const sortedRoles = sortRoles(filteredRoles);
    
    rolesList.innerHTML = sortedRoles.map(role => {
        // Get the character icon, if it is in Unicode escape format, convert it to emoji
        let roleIcon = role.icon || '👤';
        if (roleIcon && typeof roleIcon === 'string') {
            // Check if it is a Unicode escape format (may contain quotes)
            const unicodeMatch = roleIcon.match(/^"?\\U([0-9A-F]{8})"?$/i);
            if (unicodeMatch) {
                try {
                    const codePoint = parseInt(unicodeMatch[1], 16);
                    roleIcon = String.fromCodePoint(codePoint);
                } catch (e) {
                    // If conversion fails, use default icon
                    console.warn('Converting icon to Unicode escaping failed:', roleIcon, e);
                    roleIcon = '👤';
                }
            }
        }

        // Get tool list display
        let toolsDisplay = '';
        let toolsCount = 0;
        if (role.name === 'Default') {
            toolsDisplay = 'Use all tools';
        } else if (role.tools && role.tools.length > 0) {
            toolsCount = role.tools.length;
            // Show top 5 tool names
            const toolNames = role.tools.slice(0, 5).map(tool => {
                // If it is an external tool, the format is external_mcp::tool_name, and only the tool name is displayed.
                const toolName = tool.includes('::') ? tool.split('::')[1] : tool;
                return escapeHtml(toolName);
            });
            if (toolsCount <= 5) {
                toolsDisplay = toolNames.join(', ');
            } else {
                toolsDisplay = toolNames.join(', ') + ` 等 ${toolsCount} 个`;
            }
        } else if (role.mcps && role.mcps.length > 0) {
            toolsCount = role.mcps.length;
            toolsDisplay = `等 ${toolsCount} 个`;
        } else {
            toolsDisplay = 'Use all tools';
        }

        return `
        <div class="role-card">
            <div class="role-card-header">
                <h3 class="role-card-title">
                    <span class="role-card-icon">${roleIcon}</span>
                    ${escapeHtml(role.name)}
                </h3>
                <span class="role-card-badge ${role.enabled !== false ? 'enabled' : 'disabled'}">
                    ${role.enabled !== false ? 'Enabled' : 'Disabled'}
                </span>
            </div>
            <div class="role-card-description">${escapeHtml(role.description || 'No description')}</div>
            <div class="role-card-tools">
                <span class="role-card-tools-label">Tool:</span>
                <span class="role-card-tools-value">${toolsDisplay}</span>
            </div>
            <div class="role-card-actions">
                <button class="btn-secondary btn-small" onclick="editRole('${escapeHtml(role.name)}')">Edit</button>
                ${role.name !== 'Default' ? `<button class="btn-secondary btn-small btn-danger" onclick="deleteRole('${escapeHtml(role.name)}')">Delete</button>` : ''}
            </div>
        </div>
    `;
    }).join('');
}

// Handling role search input
function handleRolesSearchInput() {
    clearTimeout(rolesSearchTimeout);
    rolesSearchTimeout = setTimeout(() => {
        searchRoles();
    }, 300);
}

// Search roles
function searchRoles() {
    const searchInput = document.getElementById('roles-search');
    if (!searchInput) return;
    
    rolesSearchKeyword = searchInput.value.trim();
    const clearBtn = document.getElementById('roles-search-clear');
    if (clearBtn) {
        clearBtn.style.display = rolesSearchKeyword ? 'block' : 'none';
    }
    
    renderRolesList();
}

// Clear role search
function clearRolesSearch() {
    const searchInput = document.getElementById('roles-search');
    if (searchInput) {
        searchInput.value = '';
    }
    rolesSearchKeyword = '';
    const clearBtn = document.getElementById('roles-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    renderRolesList();
}

// Generate tool unique identifier (consistent with getToolKey in settings.js)
function getToolKey(tool) {
    // If it is an external tool, use external_mcp::tool.name as the unique identifier
    if (tool.is_external && tool.external_mcp) {
        return `${tool.external_mcp}::${tool.name}`;
    }
    // Built-in tools use the tool name directly
    return tool.name;
}

// Save the tool state of the current page to the global map
function saveCurrentRolePageToolStates() {
    document.querySelectorAll('#role-tools-list .role-tool-item').forEach(item => {
        const toolKey = item.dataset.toolKey;
        const checkbox = item.querySelector('input[type="checkbox"]');
        if (toolKey && checkbox) {
            const toolName = item.dataset.toolName;
            const isExternal = item.dataset.isExternal === 'true';
            const externalMcp = item.dataset.externalMcp || '';
            const existingState = roleToolStateMap.get(toolKey);
            roleToolStateMap.set(toolKey, {
                enabled: checkbox.checked,
                is_external: isExternal,
                external_mcp: externalMcp,
                name: toolName,
                mcpEnabled: existingState ? existingState.mcpEnabled : true // Keep MCP enabled
            });
        }
    });
}

// Load all tools list (for character tool selection)
async function loadRoleTools(page = 1, searchKeyword = '') {
    try {
        // Before loading a new page, save the state of the current page to the global map
        saveCurrentRolePageToolStates();
        
        const pageSize = roleToolsPagination.pageSize;
        let url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
        if (searchKeyword) {
            url += `&search=${encodeURIComponent(searchKeyword)}`;
        }
        
        const response = await apiFetch(url);
        if (!response.ok) {
            throw new Error('Failed to get tool list');
        }
        
        const result = await response.json();
        allRoleTools = result.tools || [];
        roleToolsPagination = {
            page: result.page || page,
            pageSize: result.page_size || pageSize,
            total: result.total || 0,
            totalPages: result.total_pages || 1
        };
        
        // Updates the total number of enabled tools (obtained from API response)
        if (result.total_enabled !== undefined) {
            totalEnabledToolsInMCP = result.total_enabled;
        }
        
        // Initialize the tool status map (if the tool is not in the map, use the status returned by the server)
        // But be careful: if the tool is already in the mapping (such as a pre-set selected tool when editing a character), the state in the mapping is retained.
        allRoleTools.forEach(tool => {
            const toolKey = getToolKey(tool);
            if (!roleToolStateMap.has(toolKey)) {
                // Tool is not in the map
                let enabled = false;
                if (roleUsesAllTools) {
                    // Marked if all tools are used and the tools are enabled in MCP Management
                    enabled = tool.enabled ? true : false;
                } else {
                    // If not all tools are used, only tools are marked selected in the tool list of the role configuration
                    enabled = roleConfiguredTools.has(toolKey);
                }
                roleToolStateMap.set(toolKey, {
                    enabled: enabled,
                    is_external: tool.is_external || false,
                    external_mcp: tool.external_mcp || '',
                    name: tool.name,
                    mcpEnabled: tool.enabled // Save original enabled state in MCP management
                });
            } else {
                // The tool is already in the map (may be a preset selected tool or manually selected by the user), retaining the state in the map
                // Note: Do not force overwriting of user-cancelled tool selections, even if all tools are used
                const state = roleToolStateMap.get(toolKey);
                // If using all tools and the tools are enabled in MCP Management, make sure the mark is checked
                if (roleUsesAllTools && tool.enabled) {
                    // When using all tools, make sure all enabled tools are selected
                    state.enabled = true;
                }
                // If not using all tools, keep the state in the map (don't overwrite as the state is already set correctly on initialization)
                state.is_external = tool.is_external || false;
                state.external_mcp = tool.external_mcp || '';
                state.mcpEnabled = tool.enabled; // Update original enabled status in MCP management
                if (!state.name || state.name === toolKey.split('::').pop()) {
                    state.name = tool.name; // Update tool name
                }
            }
        });
        
        renderRoleToolsList();
        renderRoleToolsPagination();
        updateRoleToolsStats();
    } catch (error) {
        console.error('Failed to load tool list:', error);
        const toolsList = document.getElementById('role-tools-list');
        if (toolsList) {
            toolsList.innerHTML = `<div class="tools-error">Failed to load tool list: ${escapeHtml(error.message)}</div>`;
        }
    }
}

// Render character tool selection list
function renderRoleToolsList() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Clear loading prompts and old content
    toolsList.innerHTML = '';
    
    const listContainer = document.createElement('div');
    listContainer.className = 'role-tools-list-items';
    listContainer.innerHTML = '';
    
    if (allRoleTools.length === 0) {
        listContainer.innerHTML = '<div class="tools-empty">No tools yet</div>';
        toolsList.appendChild(listContainer);
        return;
    }
    
    allRoleTools.forEach(tool => {
        const toolKey = getToolKey(tool);
        const toolItem = document.createElement('div');
        toolItem.className = 'role-tool-item';
        toolItem.dataset.toolKey = toolKey;
        toolItem.dataset.toolName = tool.name;
        toolItem.dataset.isExternal = tool.is_external ? 'true' : 'false';
        toolItem.dataset.externalMcp = tool.external_mcp || '';
        
        // Get tool status from status map
        const toolState = roleToolStateMap.get(toolKey) || {
            enabled: tool.enabled,
            is_external: tool.is_external || false,
            external_mcp: tool.external_mcp || ''
        };
        
        // External tool label
        let externalBadge = '';
        if (toolState.is_external || tool.is_external) {
            const externalMcpName = toolState.external_mcp || tool.external_mcp || '';
            const badgeText = externalMcpName ? `外部 (${escapeHtml(externalMcpName)})` : 'External';
            const badgeTitle = externalMcpName ? `外部MCP工具 - 来源：${escapeHtml(externalMcpName)}` : 'External MCP tools';
            externalBadge = `<span class="external-tool-badge" title="${badgeTitle}">${badgeText}</span>`;
        }
        
        // Generate unique checkbox id
        const checkboxId = `role-tool-${escapeHtml(toolKey).replace(/::/g, '--')}`;
        
        toolItem.innerHTML = `
            <input type="checkbox" id="${checkboxId}" ${toolState.enabled ? 'checked' : ''} 
                   onchange="handleRoleToolCheckboxChange('${escapeHtml(toolKey)}', this.checked)" />
            <div class="role-tool-item-info">
                <div class="role-tool-item-name">
                    ${escapeHtml(tool.name)}
                    ${externalBadge}
                </div>
                <div class="role-tool-item-desc">${escapeHtml(tool.description || 'No description')}</div>
            </div>
        `;
        listContainer.appendChild(toolItem);
    });
    
    toolsList.appendChild(listContainer);
}

// Rendering tool list paging control
function renderRoleToolsPagination() {
    const toolsList = document.getElementById('role-tools-list');
    if (!toolsList) return;
    
    // Remove old paging controls
    const oldPagination = toolsList.querySelector('.role-tools-pagination');
    if (oldPagination) {
        oldPagination.remove();
    }
    
    // If there is only one page or no data, pagination will not be displayed
    if (roleToolsPagination.totalPages <= 1) {
        return;
    }
    
    const pagination = document.createElement('div');
    pagination.className = 'role-tools-pagination';
    
    const { page, totalPages, total } = roleToolsPagination;
    const startItem = (page - 1) * roleToolsPagination.pageSize + 1;
    const endItem = Math.min(page * roleToolsPagination.pageSize, total);
    
    pagination.innerHTML = `
        <div class="pagination-info">
            显示 ${startItem}-${endItem} / 共 ${total} 个工具${roleToolsSearchKeyword ? ` (搜索: "${escapeHtml(roleToolsSearchKeyword)}")` : ''}
        </div>
        <div class="pagination-controls">
            <button class="btn-secondary" onclick="loadRoleTools(1, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === 1 ? 'disabled' : ''}>Front page</button>
            <button class="btn-secondary" onclick="loadRoleTools(${page - 1}, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === 1 ? 'disabled' : ''}>Previous page</button>
            <span class="pagination-page">Page ${page} / ${totalPages}</span>
            <button class="btn-secondary" onclick="loadRoleTools(${page + 1}, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === totalPages ? 'disabled' : ''}>Next page</button>
            <button class="btn-secondary" onclick="loadRoleTools(${totalPages}, '${escapeHtml(roleToolsSearchKeyword)}')" ${page === totalPages ? 'disabled' : ''}>Last page</button>
        </div>
    `;
    
    toolsList.appendChild(pagination);
}

// Handling tool checkbox status changes
function handleRoleToolCheckboxChange(toolKey, enabled) {
    const toolItem = document.querySelector(`.role-tool-item[data-tool-key="${toolKey}"]`);
    if (toolItem) {
        const toolName = toolItem.dataset.toolName;
        const isExternal = toolItem.dataset.isExternal === 'true';
        const externalMcp = toolItem.dataset.externalMcp || '';
        const existingState = roleToolStateMap.get(toolKey);
        roleToolStateMap.set(toolKey, {
            enabled: enabled,
            is_external: isExternal,
            external_mcp: externalMcp,
            name: toolName,
            mcpEnabled: existingState ? existingState.mcpEnabled : true // Keep MCP enabled
        });
    }
    updateRoleToolsStats();
}

// Select all tool
function selectAllRoleTools() {
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                const existingState = roleToolStateMap.get(toolKey);
                // Only select tools that are enabled in MCP management
                const shouldEnable = existingState && existingState.mcpEnabled !== false;
                checkbox.checked = shouldEnable;
                roleToolStateMap.set(toolKey, {
                    enabled: shouldEnable,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName,
                    mcpEnabled: existingState ? existingState.mcpEnabled : true
                });
            }
        }
    });
    updateRoleToolsStats();
}

// Do not select any tools
function deselectAllRoleTools() {
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        checkbox.checked = false;
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const toolName = toolItem.dataset.toolName;
            const isExternal = toolItem.dataset.isExternal === 'true';
            const externalMcp = toolItem.dataset.externalMcp || '';
            if (toolKey) {
                const existingState = roleToolStateMap.get(toolKey);
                roleToolStateMap.set(toolKey, {
                    enabled: false,
                    is_external: isExternal,
                    external_mcp: externalMcp,
                    name: toolName,
                    mcpEnabled: existingState ? existingState.mcpEnabled : true // Keep MCP enabled
                });
            }
        }
    });
    updateRoleToolsStats();
}

// Search tool
function searchRoleTools(keyword) {
    roleToolsSearchKeyword = keyword;
    const clearBtn = document.getElementById('role-tools-search-clear');
    if (clearBtn) {
        clearBtn.style.display = keyword ? 'block' : 'none';
    }
    loadRoleTools(1, keyword);
}

// Clear search
function clearRoleToolsSearch() {
    document.getElementById('role-tools-search').value = '';
    searchRoleTools('');
}

// Update tool statistics
function updateRoleToolsStats() {
    const statsEl = document.getElementById('role-tools-stats');
    if (!statsEl) return;
    
    // Count the number of tools selected on the current page
    const currentPageEnabled = Array.from(document.querySelectorAll('#role-tools-list input[type="checkbox"]:checked')).length;
    
    // Count the number of enabled tools on the current page (enabled tools in MCP management)
    // Get it from the state map first, if not, get it from the tool data
    let currentPageEnabledInMCP = 0;
    allRoleTools.forEach(tool => {
        const toolKey = getToolKey(tool);
        const state = roleToolStateMap.get(toolKey);
        // If the tool is enabled in MCP management (obtained from status mapping or tool data), it is included in the number of enabled tools on the current page.
        const mcpEnabled = state ? (state.mcpEnabled !== false) : (tool.enabled !== false);
        if (mcpEnabled) {
            currentPageEnabledInMCP++;
        }
    });
    
    // If using all tools, use the total number of enabled tools obtained from the API
    if (roleUsesAllTools) {
        // Use the total number of enabled tools obtained from the API response
        const totalEnabled = totalEnabledToolsInMCP || 0;
        // The denominator of the current page should be the total number of tools on the current page (20 per page), not the number of enabled tools on the current page.
        const currentPageTotal = document.querySelectorAll('#role-tools-list input[type="checkbox"]').length;
        // Total tools (all tools, enabled and inactive)
        const totalTools = roleToolsPagination.total || 0;
        statsEl.innerHTML = `
            <span title="Number of tools selected on the current page">✅The current page is selected:<strong>${currentPageEnabled}</strong> / ${currentPageTotal}</span>
            <span title="Total number of tools selected among all enabled tools (based on MCP management)">📊 Total selected:<strong>${totalEnabled}</strong> / ${totalTools} <em>(Use all enabled tools)</em></span>
        `;
        return;
    }
    
    // Count the number of tools actually selected by the role (only the tools that have been enabled in MCP management are counted)
    let totalSelected = 0;
    roleToolStateMap.forEach(state => {
        // Only tools that are enabled in MCP management and selected by the role are counted.
        if (state.enabled && state.mcpEnabled !== false) {
            totalSelected++;
        }
    });
    
    // If the current page has unsaved status, it needs to be combined and calculated.
    document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
        const toolItem = checkbox.closest('.role-tool-item');
        if (toolItem) {
            const toolKey = toolItem.dataset.toolKey;
            const savedState = roleToolStateMap.get(toolKey);
            if (savedState && savedState.enabled !== checkbox.checked && savedState.mcpEnabled !== false) {
                // If the status is inconsistent, checkbox status is used (but only the tools enabled in MCP management are counted)
                if (checkbox.checked && !savedState.enabled) {
                    totalSelected++;
                } else if (!checkbox.checked && savedState.enabled) {
                    totalSelected--;
                }
            }
        }
    });
    
    // The total number of all enabled tools that the role can select (should be based on the total in MCP management, not status mapping)
    // Because the character can select any enabled tool, the total should be the total of all enabled tools
    let totalEnabledForRole = totalEnabledToolsInMCP || 0;
    
    // If the total returned by the API is 0 or not set, try counting from the state map (as an alternative)
    if (totalEnabledForRole === 0) {
        roleToolStateMap.forEach(state => {
            // Only count tools that have been enabled in MCP management
            if (state.mcpEnabled !== false) { // McpEnabled is true or undefined (defaults to enabled when not set)
                totalEnabledForRole++;
            }
        });
    }
    
    // The denominator of the current page should be the total number of tools on the current page (20 per page), not the number of enabled tools on the current page.
    const currentPageTotal = document.querySelectorAll('#role-tools-list input[type="checkbox"]').length;
    // Total tools (all tools, enabled and inactive)
    const totalTools = roleToolsPagination.total || 0;
    
    statsEl.innerHTML = `
        <span title="Number of tools selected on the current page (only enabled tools are counted)">✅The current page is selected:<strong>${currentPageEnabled}</strong> / ${currentPageTotal}</span>
        <span title="The total number of tools associated with the role (based on the actual configuration of the role)">📊 Total selected:<strong>${totalSelected}</strong> / ${totalTools}</span>
    `;
}

// Get the selected tool list (return toolKey array)
async function getSelectedRoleTools() {
    // Save the state of the current page first
    saveCurrentRolePageToolStates();
    
    // If there are no search keywords, you need to load the tool for all pages to ensure that the status mapping is complete.
    // But for performance we can just get the selected tool from the state map
    // The problem is: if the user only selects a tool on some pages, the tool status of other pages may not be in the map
    
    // If the total number of tools is greater than the number of loaded tools, we need to make sure that all tools that are not loading the page are also considered
    // But for role tool selection, we only need to get the tools that the user has explicitly selected
    // So just get the selected tool directly from the status map
    
    // Get all selected tools from status map (only tools that are enabled in MCP management are returned)
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        // Only tools that are enabled in MCP management and selected by the role are returned
        if (state.enabled && state.mcpEnabled !== false) {
            selectedTools.push(toolKey);
        }
    });
    
    // If the user may have selected a tool on another page, we need to ensure that the state of the current page is also saved
    // But the state map should already contain the state of all visited pages
    
    return selectedTools;
}

// Set the selected tool (when used to edit a character)
function setSelectedRoleTools(selectedToolKeys) {
    const selectedSet = new Set(selectedToolKeys || []);
    
    // Update status map
    roleToolStateMap.forEach((state, toolKey) => {
        state.enabled = selectedSet.has(toolKey);
    });
    
    // Update the checkbox status of the current page
    document.querySelectorAll('#role-tools-list .role-tool-item').forEach(item => {
        const toolKey = item.dataset.toolKey;
        const checkbox = item.querySelector('input[type="checkbox"]');
        if (toolKey && checkbox) {
            checkbox.checked = selectedSet.has(toolKey);
        }
    });
    
    updateRoleToolsStats();
}

// Show add role modal box
async function showAddRoleModal() {
    const modal = document.getElementById('role-modal');
    if (!modal) return;

    document.getElementById('role-modal-title').textContent = 'Add role';
    document.getElementById('role-name').value = '';
    document.getElementById('role-name').disabled = false;
    document.getElementById('role-description').value = '';
    document.getElementById('role-icon').value = '';
    document.getElementById('role-user-prompt').value = '';
    document.getElementById('role-enabled').checked = true;

    // When adding a role: Show the tool selection interface and hide the default role prompts
    const toolsSection = document.getElementById('role-tools-section');
    const defaultHint = document.getElementById('role-tools-default-hint');
    const toolsControls = document.querySelector('.role-tools-controls');
    const toolsList = document.getElementById('role-tools-list');
    const formHint = toolsSection ? toolsSection.querySelector('.form-hint') : null;
    
    if (defaultHint) {
        defaultHint.style.display = 'none';
    }
    if (toolsControls) {
        toolsControls.style.display = 'block';
    }
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    if (formHint) {
        formHint.style.display = 'block';
    }

    // Reset tool status
    roleToolStateMap.clear();
    roleConfiguredTools.clear(); // Clear the role configuration tool list
    roleUsesAllTools = false; // All tools are not used by default when adding a character
    roleToolsSearchKeyword = '';
    const searchInput = document.getElementById('role-tools-search');
    if (searchInput) {
        searchInput.value = '';
    }
    const clearBtn = document.getElementById('role-tools-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    
    // Clear the tool list DOM to avoid saveCurrentRolePageToolStates in loadRoleTools from reading old states
    if (toolsList) {
        toolsList.innerHTML = '';
    }

    // Reset skills status
    roleSelectedSkills.clear();
    roleSkillsSearchKeyword = '';
    const skillsSearchInput = document.getElementById('role-skills-search');
    if (skillsSearchInput) {
        skillsSearchInput.value = '';
    }
    const skillsClearBtn = document.getElementById('role-skills-search-clear');
    if (skillsClearBtn) {
        skillsClearBtn.style.display = 'none';
    }

    // Load and render the tool list
    await loadRoleTools(1, '');
    
    // Make sure the tool list is showing
    if (toolsList) {
        toolsList.style.display = 'block';
    }
    
    // Make sure statistics are updated correctly (showing 0/108)
    updateRoleToolsStats();

    // Load and render the skills list
    await loadRoleSkills();

    modal.style.display = 'flex';
}

// Edit role
async function editRole(roleName) {
    const role = roles.find(r => r.name === roleName);
    if (!role) {
        showNotification('Role does not exist', 'error');
        return;
    }

    const modal = document.getElementById('role-modal');
    if (!modal) return;

    document.getElementById('role-modal-title').textContent = 'Edit role';
    document.getElementById('role-name').value = role.name;
    document.getElementById('role-name').disabled = true; // Name modification is not allowed during editing
    document.getElementById('role-description').value = role.description || '';
    // Process the icon field: if it is a Unicode escape format, convert it to emoji; otherwise, use it directly
    let iconValue = role.icon || '';
    if (iconValue && iconValue.startsWith('\\U')) {
        // Convert Unicode escape format (such as \U0001F3C6) to emoji
        try {
            const codePoint = parseInt(iconValue.substring(2), 16);
            iconValue = String.fromCodePoint(codePoint);
        } catch (e) {
            // If the conversion fails, the original value is used
        }
    }
    document.getElementById('role-icon').value = iconValue;
    document.getElementById('role-user-prompt').value = role.user_prompt || '';
    document.getElementById('role-enabled').checked = role.enabled !== false;

    // Check if it is the default role
    const isDefaultRole = roleName === 'Default';
    const toolsSection = document.getElementById('role-tools-section');
    const defaultHint = document.getElementById('role-tools-default-hint');
    const toolsControls = document.querySelector('.role-tools-controls');
    const toolsList = document.getElementById('role-tools-list');
    const formHint = toolsSection ? toolsSection.querySelector('.form-hint') : null;
    
    if (isDefaultRole) {
        // Default role: Hide tool selection interface and display prompt information
        if (defaultHint) {
            defaultHint.style.display = 'block';
        }
        if (toolsControls) {
            toolsControls.style.display = 'none';
        }
        if (toolsList) {
            toolsList.style.display = 'none';
        }
        if (formHint) {
            formHint.style.display = 'none';
        }
    } else {
        // Non-default role: display the tool selection interface and hide the prompt information
        if (defaultHint) {
            defaultHint.style.display = 'none';
        }
        if (toolsControls) {
            toolsControls.style.display = 'block';
        }
        if (toolsList) {
            toolsList.style.display = 'block';
        }
        if (formHint) {
            formHint.style.display = 'block';
        }

        // Reset tool status
        roleToolStateMap.clear();
        roleConfiguredTools.clear(); // Clear the role configuration tool list
        roleToolsSearchKeyword = '';
        const searchInput = document.getElementById('role-tools-search');
        if (searchInput) {
            searchInput.value = '';
        }
        const clearBtn = document.getElementById('role-tools-search-clear');
        if (clearBtn) {
            clearBtn.style.display = 'none';
        }

        // The tools field is used first, if not, the mcps field is used (backward compatibility)
        const selectedTools = role.tools || (role.mcps && role.mcps.length > 0 ? role.mcps : []);
        
        // Determine whether to use all tools: if tools are not configured (or tools is an empty array), it means that all tools are used
        roleUsesAllTools = !role.tools || role.tools.length === 0;
        
        // List of tools for saving role configurations
        if (selectedTools.length > 0) {
            selectedTools.forEach(toolKey => {
                roleConfiguredTools.add(toolKey);
            });
        }
        
        // If there is a selected tool, initialize the state mapping first
        if (selectedTools.length > 0) {
            roleUsesAllTools = false; // There are configuration tools, not all tools are used
            // Add selected tool to state map (marked as selected)
            selectedTools.forEach(toolKey => {
                // If there is no such tool in the mapping, first create a default state (enabled is true)
                if (!roleToolStateMap.has(toolKey)) {
                    roleToolStateMap.set(toolKey, {
                        enabled: true,
                        is_external: false,
                        external_mcp: '',
                        name: toolKey.split('::').pop() || toolKey // Extract tool name from toolKey
                    });
                } else {
                    // If it already exists, update it to selected state
                    const state = roleToolStateMap.get(toolKey);
                    state.enabled = true;
                }
            });
        }

        // Load tool list (first page)
        await loadRoleTools(1, '');
        
        // If using all tools, marks all enabled tools on the current page as selected
        if (roleUsesAllTools) {
            // Mark all tools enabled in MCP management on the current page as selected
            document.querySelectorAll('#role-tools-list input[type="checkbox"]').forEach(checkbox => {
                const toolItem = checkbox.closest('.role-tool-item');
                if (toolItem) {
                    const toolKey = toolItem.dataset.toolKey;
                    const toolName = toolItem.dataset.toolName;
                    const isExternal = toolItem.dataset.isExternal === 'true';
                    const externalMcp = toolItem.dataset.externalMcp || '';
                    if (toolKey) {
                        const state = roleToolStateMap.get(toolKey);
                        // Only select tools that are enabled in MCP management
                        // If the state exists, use mcpEnabled in the state; otherwise assume it is enabled (since loadRoleTools should have initialized all tools)
                        const shouldEnable = state ? (state.mcpEnabled !== false) : true;
                        checkbox.checked = shouldEnable;
                        if (state) {
                            state.enabled = shouldEnable;
                        } else {
                            // If the state does not exist, create a new state (this should not happen since loadRoleTools should have already been initialized)
                            roleToolStateMap.set(toolKey, {
                                enabled: shouldEnable,
                                is_external: isExternal,
                                external_mcp: externalMcp,
                                name: toolName,
                                mcpEnabled: true // Assuming it is enabled, the actual value is updated in loadRoleTools
                            });
                        }
                    }
                }
            });
            // Update statistics to ensure correct number of selections are shown
            updateRoleToolsStats();
        } else if (selectedTools.length > 0) {
            // After loading is complete, set the selected state again (make sure the tools on the current page are also set correctly)
            setSelectedRoleTools(selectedTools);
        }
    }

    // Load and set skills
    await loadRoleSkills();
    // Set role configuration skills
    const selectedSkills = role.skills || [];
    roleSelectedSkills.clear();
    selectedSkills.forEach(skill => {
        roleSelectedSkills.add(skill);
    });
    renderRoleSkills();

    modal.style.display = 'flex';
}

// Close role modal box
function closeRoleModal() {
    const modal = document.getElementById('role-modal');
    if (modal) {
        modal.style.display = 'none';
    }
}

// Get all selected tools (including tools not enabled in MCP management)
function getAllSelectedRoleTools() {
    // Save the state of the current page first
    saveCurrentRolePageToolStates();
    
    // Get all selected tools from status map (regardless of whether enabled in MCP management)
    const selectedTools = [];
    roleToolStateMap.forEach((state, toolKey) => {
        if (state.enabled) {
            selectedTools.push({
                key: toolKey,
                name: state.name || toolKey.split('::').pop() || toolKey,
                mcpEnabled: state.mcpEnabled !== false // When mcpEnabled is false, it is not enabled. Otherwise, it is considered enabled.
            });
        }
    });
    
    return selectedTools;
}

// Check and get tools not enabled in MCP management
function getDisabledTools(selectedTools) {
    return selectedTools.filter(tool => {
        const state = roleToolStateMap.get(tool.key);
        // If mcpEnabled is explicitly false, it is considered not enabled
        return state && state.mcpEnabled === false;
    });
}

// Load all tools into the state map (used when switching from using all tools to some tools)
async function loadAllToolsToStateMap() {
    try {
        const pageSize = 100; // Use a larger page size to reduce the number of requests
        let page = 1;
        let hasMore = true;
        
        // Traverse all pages to get all tools
        while (hasMore) {
            const url = `/api/config/tools?page=${page}&page_size=${pageSize}`;
            const response = await apiFetch(url);
            if (!response.ok) {
                throw new Error('Failed to get tool list');
            }
            
            const result = await response.json();
            
            // Add all tools to state map
            result.tools.forEach(tool => {
                const toolKey = getToolKey(tool);
                if (!roleToolStateMap.has(toolKey)) {
                    // Tool is not in the mapping and is initialized according to the current mode
                    let enabled = false;
                    if (roleUsesAllTools) {
                        // Marked if all tools are used and the tools are enabled in MCP Management
                        enabled = tool.enabled ? true : false;
                    } else {
                        // If not all tools are used, only tools are marked selected in the tool list of the role configuration
                        enabled = roleConfiguredTools.has(toolKey);
                    }
                    roleToolStateMap.set(toolKey, {
                        enabled: enabled,
                        is_external: tool.is_external || false,
                        external_mcp: tool.external_mcp || '',
                        name: tool.name,
                        mcpEnabled: tool.enabled // Save original enabled state in MCP management
                    });
                } else {
                    // Tool is already in the map, update other properties but leave enabled state
                    const state = roleToolStateMap.get(toolKey);
                    state.is_external = tool.is_external || false;
                    state.external_mcp = tool.external_mcp || '';
                    state.mcpEnabled = tool.enabled; // Update original enabled status in MCP management
                    if (!state.name || state.name === toolKey.split('::').pop()) {
                        state.name = tool.name; // Update tool name
                    }
                }
            });
            
            // Check if there are more pages
            if (page >= result.total_pages) {
                hasMore = false;
            } else {
                page++;
            }
        }
    } catch (error) {
        console.error('Failed to load all tools to state map:', error);
        throw error;
    }
}

// Save character
async function saveRole() {
    const name = document.getElementById('role-name').value.trim();
    if (!name) {
        showNotification('Role name cannot be empty', 'error');
        return;
    }

    const description = document.getElementById('role-description').value.trim();
    let icon = document.getElementById('role-icon').value.trim();
    // Convert emoji to Unicode escaped format to match YAML format (such as \U0001F3C6)
    if (icon) {
        // Get the Unicode code point of the first character (handling the case where the emoji may be multiple characters)
        const codePoint = icon.codePointAt(0);
        if (codePoint && codePoint > 0x7F) {
            // Convert to 8-digit hexadecimal format (\U0001F3C6)
            icon = '\\U' + codePoint.toString(16).toUpperCase().padStart(8, '0');
        }
    }
    const userPrompt = document.getElementById('role-user-prompt').value.trim();
    const enabled = document.getElementById('role-enabled').checked;

    const isEdit = document.getElementById('role-name').disabled;
    
    // Check if it is the default role
    const isDefaultRole = name === 'Default';
    
    // Check if the role is being added for the first time (after excluding the default role, there are no user-created roles)
    const isFirstUserRole = !isEdit && !isDefaultRole && roles.filter(r => r.name !== 'Default').length === 0;
    
    // Default role does not save tools field (use all tools)
    // Non-default role: If all tools are used (roleUsesAllTools is true), the tools field is not saved either
    let tools = [];
    let disabledTools = []; // Storing tools not enabled in MCP Management
    
    if (!isDefaultRole) {
        // Save the state of the current page
        saveCurrentRolePageToolStates();
        
        // Collect all selected tools (including those not enabled in MCP management)
        let allSelectedTools = getAllSelectedRoleTools();
        
        // If this is the first time you add a character and no tools are selected, all tools will be used by default.
        if (isFirstUserRole && allSelectedTools.length === 0) {
            roleUsesAllTools = true;
            showNotification('It is detected that this is the first time adding a character and no tools are selected, all tools will be used by default', 'info');
        } else if (roleUsesAllTools) {
            // If all tools are currently in use, need to check if the user canceled some tools
            // Check if there are any unchecked enabled tools in the status map
            let hasUnselectedTools = false;
            roleToolStateMap.forEach((state) => {
                // If the tool is enabled but not selected in MCP management, the user canceled the tool
                if (state.mcpEnabled !== false && !state.enabled) {
                    hasUnselectedTools = true;
                }
            });
            
            // If the user cancels some enabled tools, switch to partial tools mode
            if (hasUnselectedTools) {
                // Before switching, all tools need to be loaded into the state map
                // This way we correctly save the state of all tools (except those canceled by the user)
                await loadAllToolsToStateMap();
                
                // Mark all enabled tools as checked (except those that the user has deselected)
                // Tools that have been canceled by the user are enabled to false in the state map and remain unchanged
                roleToolStateMap.forEach((state, toolKey) => {
                    // If the tool is enabled in MCP Management and is not explicitly marked as unchecked in the status map (i.e. enabled is not false)
                    // Then mark as selected
                    if (state.mcpEnabled !== false && state.enabled !== false) {
                        state.enabled = true;
                    }
                });
                
                roleUsesAllTools = false;
            } else {
                // Even if all tools are used, all tools need to be loaded into the state map in order to check if any unenabled tools are selected
                // This can detect if the user has manually selected some tools that are not enabled
                await loadAllToolsToStateMap();
                
                // Check if any unenabled tools are manually selected (enabled is true but mcpEnabled is false)
                let hasDisabledToolsSelected = false;
                roleToolStateMap.forEach((state) => {
                    if (state.enabled && state.mcpEnabled === false) {
                        hasDisabledToolsSelected = true;
                    }
                });
                
                // If no unenabled tools are selected, mark all enabled tools as selected (this is the default behavior with all tools)
                if (!hasDisabledToolsSelected) {
                    roleToolStateMap.forEach((state) => {
                        if (state.mcpEnabled !== false) {
                            state.enabled = true;
                        }
                    });
                }
                
                // Updated allSelectedTools as all tools are now included in the state map
                allSelectedTools = getAllSelectedRoleTools();
            }
        }
        
        // Check which tools are not enabled in MCP management (check whether all tools are used or not)
        disabledTools = getDisabledTools(allSelectedTools);
        
        // If there are unenabled tools, prompt the user
        if (disabledTools.length > 0) {
            const toolNames = disabledTools.map(t => t.name).join('、');
            const message = `以下 ${disabledTools.length} 个工具未在MCP管理中启用，无法在角色中配置：\n\n${toolNames}\n\n请先在"MCP management"中启用这些工具，然后再在角色中配置。\n\n是否继续保存？（将只保存已启用的工具）`;
            
            if (!confirm(message)) {
                return; // User cancels save
            }
        }
        
        // If using all tools, there is no need to get a list of tools
        if (!roleUsesAllTools) {
            // Get the selected tool list (only tools enabled in MCP management)
            tools = await getSelectedRoleTools();
        }
    }

    // Get selected skills
    const skills = Array.from(roleSelectedSkills);

    const roleData = {
        name: name,
        description: description,
        icon: icon || undefined, // If it is an empty string, the field is not sent
        user_prompt: userPrompt,
        tools: tools, // The default role is an empty array, indicating that all tools are used
        skills: skills, // Skills list
        enabled: enabled
    };
    const url = isEdit ? `/api/roles/${encodeURIComponent(name)}` : '/api/roles';
    const method = isEdit ? 'PUT' : 'POST';

    try {
        const response = await apiFetch(url, {
            method: method,
            headers: {
                'Content-Type': 'application/json'
            },
            body: JSON.stringify(roleData)
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to save character');
        }

        // If any unenabled tools are filtered out, prompt the user
        if (disabledTools.length > 0) {
            let toolNames = disabledTools.map(t => t.name).join('、');
            // If list of tool names is too long, display is truncated
            if (toolNames.length > 100) {
                toolNames = toolNames.substring(0, 100) + '...';
            }
            showNotification(
                `${isEdit ? 'Role updated' : 'Role created'}，但已过滤 ${disabledTools.length} 个未在MCP管理中启用的工具：${toolNames}。请先在"MCP management"中启用这些工具，然后再在角色中配置。`,
                'warning'
            );
        } else {
            showNotification(isEdit ? 'Role updated' : 'Role created', 'success');
        }
        
        closeRoleModal();
        await refreshRoles();
    } catch (error) {
        console.error('Failed to save character:', error);
        showNotification('Failed to save character:' + error.message, 'error');
    }
}

// Delete role
async function deleteRole(roleName) {
    if (roleName === 'Default') {
        showNotification('Cannot delete default role', 'error');
        return;
    }

    if (!confirm(`确定要删除角色"${roleName}"吗？此操作不可撤销。`)) {
        return;
    }

    try {
        const response = await apiFetch(`/api/roles/${encodeURIComponent(roleName)}`, {
            method: 'DELETE'
        });

        if (!response.ok) {
            const error = await response.json();
            throw new Error(error.error || 'Failed to delete role');
        }

        showNotification('Role deleted', 'success');
        
        // If the currently selected role is deleted, switch to the default role
        if (currentRole === roleName) {
            handleRoleChange('');
        }

        await refreshRoles();
    } catch (error) {
        console.error('Failed to delete role:', error);
        showNotification('Failed to delete role:' + error.message, 'error');
    }
}

// Initialize the role list when switching pages
if (typeof switchPage === 'function') {
    const originalSwitchPage = switchPage;
    switchPage = function(page) {
        originalSwitchPage(page);
        if (page === 'roles-management') {
            loadRoles().then(() => renderRolesList());
        }
    };
}

// Click outside the modal box to close it
document.addEventListener('click', (e) => {
    const roleSelectModal = document.getElementById('role-select-modal');
    if (roleSelectModal && e.target === roleSelectModal) {
        closeRoleSelectModal();
    }

    const roleModal = document.getElementById('role-modal');
    if (roleModal && e.target === roleModal) {
        closeRoleModal();
    }

    // Click outside the character select panel to close the panel (but not the character select button and the panel itself)
    const roleSelectionPanel = document.getElementById('role-selection-panel');
    const roleSelectorWrapper = document.querySelector('.role-selector-wrapper');
    if (roleSelectionPanel && roleSelectionPanel.style.display !== 'none' && roleSelectionPanel.style.display) {
        // Check if click is on panel or wrapper
        if (!roleSelectorWrapper?.contains(e.target)) {
            closeRoleSelectionPanel();
        }
    }
});

// Initialized when page loads
document.addEventListener('DOMContentLoaded', () => {
    loadRoles();
    updateRoleSelectorDisplay();
});

// Get the currently selected role (for use by chat.js)
function getCurrentRole() {
    return currentRole || '';
}

// Expose functions to the global scope
if (typeof window !== 'undefined') {
    window.getCurrentRole = getCurrentRole;
    window.toggleRoleSelectionPanel = toggleRoleSelectionPanel;
    window.closeRoleSelectionPanel = closeRoleSelectionPanel;
    window.currentSelectedRole = getCurrentRole();
    
    // Monitor role changes and update global variables
    const originalHandleRoleChange = handleRoleChange;
    handleRoleChange = function(roleName) {
        originalHandleRoleChange(roleName);
        if (typeof window !== 'undefined') {
            window.currentSelectedRole = getCurrentRole();
        }
    };
}

// ==================== Skills related functions ====================

// Load skills list
async function loadRoleSkills() {
    try {
        const response = await apiFetch('/api/roles/skills/list');
        if (!response.ok) {
            throw new Error('Failed to load skills list');
        }
        const data = await response.json();
        allRoleSkills = data.skills || [];
        renderRoleSkills();
    } catch (error) {
        console.error('Failed to load skills list:', error);
        allRoleSkills = [];
        const skillsList = document.getElementById('role-skills-list');
        if (skillsList) {
            skillsList.innerHTML = '<div class="skills-error">Failed to load skills list: ' + error.message + '</div>';
        }
    }
}

// Render skills list
function renderRoleSkills() {
    const skillsList = document.getElementById('role-skills-list');
    if (!skillsList) return;

    // Filter skills
    let filteredSkills = allRoleSkills;
    if (roleSkillsSearchKeyword) {
        const keyword = roleSkillsSearchKeyword.toLowerCase();
        filteredSkills = allRoleSkills.filter(skill => 
            skill.toLowerCase().includes(keyword)
        );
    }

    if (filteredSkills.length === 0) {
        skillsList.innerHTML = '<div class="skills-empty">' + 
            (roleSkillsSearchKeyword ? 'No matching skills found' : 'No skills available yet') +
            '</div>';
        updateRoleSkillsStats();
        return;
    }

    // Render skills list
    skillsList.innerHTML = filteredSkills.map(skill => {
        const isSelected = roleSelectedSkills.has(skill);
        return `
            <div class="role-skill-item" data-skill="${skill}">
                <label class="checkbox-label">
                    <input type="checkbox" class="modern-checkbox" 
                           ${isSelected ? 'checked' : ''} 
                           onchange="toggleRoleSkill('${skill}', this.checked)" />
                    <span class="checkbox-custom"></span>
                    <span class="checkbox-text">${escapeHtml(skill)}</span>
                </label>
            </div>
        `;
    }).join('');

    updateRoleSkillsStats();
}

// Switch skill selected state
function toggleRoleSkill(skill, checked) {
    if (checked) {
        roleSelectedSkills.add(skill);
    } else {
        roleSelectedSkills.delete(skill);
    }
    updateRoleSkillsStats();
}

// Select all skills
function selectAllRoleSkills() {
    let filteredSkills = allRoleSkills;
    if (roleSkillsSearchKeyword) {
        const keyword = roleSkillsSearchKeyword.toLowerCase();
        filteredSkills = allRoleSkills.filter(skill => 
            skill.toLowerCase().includes(keyword)
        );
    }
    filteredSkills.forEach(skill => {
        roleSelectedSkills.add(skill);
    });
    renderRoleSkills();
}

// Do not select any skills
function deselectAllRoleSkills() {
    let filteredSkills = allRoleSkills;
    if (roleSkillsSearchKeyword) {
        const keyword = roleSkillsSearchKeyword.toLowerCase();
        filteredSkills = allRoleSkills.filter(skill => 
            skill.toLowerCase().includes(keyword)
        );
    }
    filteredSkills.forEach(skill => {
        roleSelectedSkills.delete(skill);
    });
    renderRoleSkills();
}

// Search skills
function searchRoleSkills(keyword) {
    roleSkillsSearchKeyword = keyword;
    const clearBtn = document.getElementById('role-skills-search-clear');
    if (clearBtn) {
        clearBtn.style.display = keyword ? 'block' : 'none';
    }
    renderRoleSkills();
}

// Clear skills search
function clearRoleSkillsSearch() {
    const searchInput = document.getElementById('role-skills-search');
    if (searchInput) {
        searchInput.value = '';
    }
    roleSkillsSearchKeyword = '';
    const clearBtn = document.getElementById('role-skills-search-clear');
    if (clearBtn) {
        clearBtn.style.display = 'none';
    }
    renderRoleSkills();
}

// Update skills statistics
function updateRoleSkillsStats() {
    const statsEl = document.getElementById('role-skills-stats');
    if (!statsEl) return;

    let filteredSkills = allRoleSkills;
    if (roleSkillsSearchKeyword) {
        const keyword = roleSkillsSearchKeyword.toLowerCase();
        filteredSkills = allRoleSkills.filter(skill => 
            skill.toLowerCase().includes(keyword)
        );
    }

    const selectedCount = Array.from(roleSelectedSkills).filter(skill => 
        filteredSkills.includes(skill)
    ).length;

    statsEl.textContent = `已选择 ${selectedCount} / ${filteredSkills.length}`;
}

// HTML escape function
function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}
