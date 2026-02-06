const API = '/api/v1';

const TYPE_COLORS = {
    vm: '#6e9ecf', node: '#6e9ecf', instance_group: '#6e9ecf',
    pod: '#5b9e8f', container: '#5b9e8f',
    service: '#b8a965', ingress: '#b8a965', load_balancer: '#b8a965', cdn: '#b8a965', backend_service: '#b8a965',
    database: '#8f7bb5', bucket: '#8f7bb5', disk: '#8f7bb5', queue: '#8f7bb5', pubsub: '#8f7bb5',
    network: '#5589a6', subnet: '#5589a6', ip_address: '#5589a6',
    dns_record: '#6aab8e', firewall_rule: '#c08a5a', health_check: '#c08a5a',
    certificate: '#b5706b', secret: '#b5706b', kms_key: '#b5706b',
    iam_binding: '#9478a7', iam_policy: '#9478a7', iam_group: '#9478a7', service_account: '#9478a7',
    monitor: '#6aab8e',
    namespace: '#7c8894',
    function: '#d4a76a',
    api_gateway: '#b8a965',
    nosql_database: '#8f7bb5',
};

const TYPE_SHAPES = {
    vm: 'rectangle', node: 'rectangle', instance_group: 'rectangle',
    pod: 'round-rectangle', container: 'round-rectangle',
    service: 'ellipse', ingress: 'ellipse', load_balancer: 'ellipse', backend_service: 'ellipse',
    network: 'hexagon', subnet: 'hexagon', ip_address: 'hexagon', cdn: 'hexagon',
    dns_record: 'triangle', firewall_rule: 'triangle', health_check: 'triangle',
    database: 'diamond', bucket: 'diamond', disk: 'diamond', queue: 'diamond', pubsub: 'diamond',
    certificate: 'octagon', secret: 'octagon', kms_key: 'octagon',
    iam_binding: 'pentagon', iam_policy: 'pentagon', iam_group: 'pentagon', service_account: 'pentagon',
    monitor: 'star',
    namespace: 'barrel',
    function: 'round-rectangle',
    api_gateway: 'ellipse',
    nosql_database: 'diamond',
};

const GROUP_COLORS = [
    '#1f3044', '#2d1f3d', '#1f3d2d', '#3d2d1f', '#1f2d3d',
    '#3d1f2d', '#2d3d1f', '#1f3d3d', '#3d3d1f', '#3d1f3d',
];

let cy;
let graphData;

function esc(str) {
    const d = document.createElement('div');
    d.appendChild(document.createTextNode(str == null ? '' : String(str)));
    return d.innerHTML;
}

async function fetchJSON(url) {
    const resp = await fetch(url);
    return resp.json();
}

async function init() {
    const [gd, stats] = await Promise.all([
        fetchJSON(`${API}/graph`),
        fetchJSON(`${API}/stats`),
    ]);
    graphData = gd;

    document.getElementById('stats').textContent =
        `${stats.nodes_total || 0} nodes | ${stats.edges_total || 0} edges | ${stats.expiring_certs || 0} expiring certs`;

    // Populate filter dropdowns
    const types = new Set((graphData.nodes || []).map(n => n.type));
    const sources = new Set((graphData.nodes || []).map(n => n.source));
    const typeSelect = document.getElementById('filter-type');
    const sourceSelect = document.getElementById('filter-source');
    types.forEach(t => { const o = document.createElement('option'); o.value = t; o.textContent = t; typeSelect.appendChild(o); });
    sources.forEach(s => { const o = document.createElement('option'); o.value = s; o.textContent = s; sourceSelect.appendChild(o); });

    // Build Cytoscape elements
    const elements = buildElements(graphData, '');

    cy = cytoscape({
        container: document.getElementById('cy'),
        elements,
        style: [
            {
                selector: 'node[!isGroup]',
                style: {
                    'label': 'data(label)',
                    'shape': el => TYPE_SHAPES[el.data('type')] || 'ellipse',
                    'background-color': el => TYPE_COLORS[el.data('type')] || '#D5D8DC',
                    'color': '#c9d1d9',
                    'font-size': '10px',
                    'text-valign': 'bottom',
                    'text-margin-y': 4,
                    'width': 34, 'height': 34,
                    'border-width': 2,
                    'border-color': '#30363d',
                },
            },
            {
                selector: '$node > node',
                style: {
                    'padding': '16px',
                    'background-opacity': 0.4,
                    'border-width': 1,
                    'border-color': '#30363d',
                    'label': 'data(label)',
                    'color': '#8b949e',
                    'font-size': '12px',
                    'text-valign': 'top',
                    'text-halign': 'center',
                    'text-margin-y': -8,
                },
            },
            {
                selector: 'edge',
                style: {
                    'label': 'data(label)',
                    'font-size': '8px',
                    'color': '#8b949e',
                    'line-color': '#30363d',
                    'target-arrow-color': '#30363d',
                    'target-arrow-shape': 'triangle',
                    'curve-style': 'bezier',
                    'width': 1.5,
                },
            },
            {
                selector: ':selected',
                style: { 'border-color': '#58a6ff', 'border-width': 3 },
            },
            {
                selector: '.highlighted',
                style: { 'border-color': '#f0883e', 'border-width': 3, 'background-color': '#f0883e' },
            },
            {
                selector: '.neighbor-highlight',
                style: { 'border-color': '#58a6ff', 'border-width': 3 },
            },
            {
                selector: '.dimmed',
                style: { 'opacity': 0.2 },
            },
        ],
        layout: {
            name: 'cose',
            animate: false,
            nodeRepulsion: () => 16000,
            idealEdgeLength: () => 140,
            nodeOverlap: 20,
            padding: 40,
            componentSpacing: 60,
            nestingFactor: 1.2,
            gravity: 0.3,
        },
    });

    // Node click -> show detail + impact
    cy.on('tap', 'node[!isGroup]', async (evt) => {
        const node = evt.target;
        const id = node.data('id');
        showDetail(id);
    });

    // Right-click context menu
    cy.on('cxttap', 'node[!isGroup]', (evt) => {
        evt.originalEvent.preventDefault();
        showContextMenu(evt.originalEvent, evt.target.data('id'));
    });

    // Hide context menu on click elsewhere
    cy.on('tap', () => hideContextMenu());
    document.addEventListener('click', () => hideContextMenu());

    // Search
    document.getElementById('search').addEventListener('input', (e) => {
        const q = e.target.value.toLowerCase();
        cy.nodes('[!isGroup]').forEach(n => {
            const match = !q || n.data('label').toLowerCase().includes(q) || n.data('id').toLowerCase().includes(q);
            n.toggleClass('dimmed', !match);
        });
    });

    // Filters
    const applyFilters = () => {
        const type = document.getElementById('filter-type').value;
        const source = document.getElementById('filter-source').value;
        cy.nodes('[!isGroup]').forEach(n => {
            const matchType = !type || n.data('type') === type;
            const matchSource = !source || n.data('source') === source;
            n.toggleClass('dimmed', !(matchType && matchSource));
        });
    };
    typeSelect.addEventListener('change', applyFilters);
    sourceSelect.addEventListener('change', applyFilters);

    // Group by
    document.getElementById('group-by').addEventListener('change', (e) => {
        rebuildGraph(e.target.value);
    });

    // Reset
    document.getElementById('btn-reset').addEventListener('click', resetView);

    // Close panel
    document.getElementById('close-panel').addEventListener('click', () => {
        document.getElementById('detail-panel').classList.add('hidden');
        cy.nodes().removeClass('highlighted dimmed neighbor-highlight');
    });

    // Legend toggle
    document.getElementById('btn-legend').addEventListener('click', () => {
        document.getElementById('legend').classList.toggle('hidden');
    });

    // Shortcuts toggle
    document.getElementById('btn-shortcuts').addEventListener('click', () => {
        document.getElementById('shortcuts-panel').classList.toggle('hidden');
    });

    // Export dropdown
    document.getElementById('btn-export').addEventListener('click', (e) => {
        e.stopPropagation();
        document.getElementById('export-menu').classList.toggle('show');
    });
    document.querySelectorAll('#export-menu a').forEach(a => {
        a.addEventListener('click', (e) => {
            e.preventDefault();
            document.getElementById('export-menu').classList.remove('show');
            handleExport(a.dataset.format);
        });
    });

    // Context menu actions
    document.querySelectorAll('#context-menu a').forEach(a => {
        a.addEventListener('click', (e) => {
            e.preventDefault();
            const nodeId = document.getElementById('context-menu').dataset.nodeId;
            hideContextMenu();
            handleContextAction(a.dataset.action, nodeId);
        });
    });

    // Scan button
    document.getElementById('btn-scan').addEventListener('click', triggerScan);
    checkScanRunning();

    // Keyboard shortcuts
    document.addEventListener('keydown', handleKeyboard);
}

// --- Export ---

function handleExport(format) {
    if (format === 'png') {
        const png = cy.png({ full: true, bg: '#0d1117', scale: 2 });
        const link = document.createElement('a');
        link.download = 'aib-graph.png';
        link.href = png;
        link.click();
        return;
    }
    const url = `${API}/export/${format}`;
    const link = document.createElement('a');
    link.href = url;
    link.download = '';
    link.click();
}

// --- Grouping ---

function buildElements(data, groupBy) {
    const elements = [];
    const groups = new Set();

    (data.nodes || []).forEach(n => {
        const nodeData = {
            id: n.id, label: n.name, type: n.type, source: n.source,
            provider: n.provider, expires_at: n.expires_at,
            namespace: (n.metadata && n.metadata.namespace) || '',
        };

        if (groupBy) {
            let groupVal = '';
            if (groupBy === 'source') groupVal = n.source || 'unknown';
            else if (groupBy === 'type') groupVal = n.type || 'unknown';
            else if (groupBy === 'namespace') groupVal = (n.metadata && n.metadata.namespace) || 'default';

            const groupId = `group:${groupBy}:${groupVal}`;
            nodeData.parent = groupId;
            groups.add(groupVal);
        }

        elements.push({ data: nodeData });
    });

    if (groupBy) {
        let colorIdx = 0;
        groups.forEach(g => {
            elements.push({
                data: {
                    id: `group:${groupBy}:${g}`,
                    label: g,
                    isGroup: true,
                    bgColor: GROUP_COLORS[colorIdx % GROUP_COLORS.length],
                },
            });
            colorIdx++;
        });
    }

    (data.edges || []).forEach(e => {
        elements.push({
            data: { id: e.id, source: e.from_id, target: e.to_id, label: e.type },
        });
    });

    return elements;
}

function rebuildGraph(groupBy) {
    const elements = buildElements(graphData, groupBy);
    cy.elements().remove();
    cy.add(elements);

    // Set group node background colors
    cy.nodes('[?isGroup]').forEach(n => {
        n.style('background-color', n.data('bgColor') || '#1f3044');
    });

    cy.layout({
        name: 'cose',
        animate: false,
        nodeRepulsion: () => 16000,
        idealEdgeLength: () => 140,
        nodeOverlap: 20,
        padding: 40,
        componentSpacing: 60,
        nestingFactor: 1.2,
        gravity: 0.3,
    }).run();
}

// --- Context Menu ---

function showContextMenu(event, nodeId) {
    const menu = document.getElementById('context-menu');
    menu.dataset.nodeId = nodeId;
    menu.style.left = event.clientX + 'px';
    menu.style.top = event.clientY + 'px';
    menu.classList.remove('hidden');
}

function hideContextMenu() {
    document.getElementById('context-menu').classList.add('hidden');
}

function handleContextAction(action, nodeId) {
    switch (action) {
        case 'impact':
            showDetail(nodeId);
            break;
        case 'neighbors':
            highlightNeighbors(nodeId);
            break;
        case 'copy-id':
            navigator.clipboard.writeText(nodeId);
            break;
    }
}

function highlightNeighbors(nodeId) {
    cy.nodes().removeClass('highlighted dimmed neighbor-highlight');
    cy.edges().removeClass('dimmed');

    const center = cy.getElementById(nodeId);
    if (!center.length) return;

    const neighborhood = center.neighborhood().add(center);
    cy.elements().addClass('dimmed');
    neighborhood.removeClass('dimmed');
    center.addClass('highlighted');
    neighborhood.nodes().not(center).addClass('neighbor-highlight');
}

// --- Keyboard Shortcuts ---

function handleKeyboard(e) {
    // Skip if user is typing in an input/select
    if (e.target.tagName === 'INPUT' || e.target.tagName === 'SELECT' || e.target.tagName === 'TEXTAREA') {
        if (e.key === 'Escape') {
            e.target.blur();
        }
        return;
    }

    switch (e.key) {
        case 'Escape':
            document.getElementById('detail-panel').classList.add('hidden');
            cy.nodes().removeClass('highlighted dimmed neighbor-highlight');
            cy.edges().removeClass('dimmed');
            hideContextMenu();
            break;
        case '/':
            e.preventDefault();
            document.getElementById('search').focus();
            break;
        case 'r':
            resetView();
            break;
        case 'l':
            document.getElementById('legend').classList.toggle('hidden');
            break;
        case '?':
            document.getElementById('shortcuts-panel').classList.toggle('hidden');
            break;
    }
}

function resetView() {
    cy.nodes().removeClass('dimmed highlighted neighbor-highlight');
    cy.edges().removeClass('dimmed');
    document.getElementById('search').value = '';
    document.getElementById('filter-type').value = '';
    document.getElementById('filter-source').value = '';
    document.getElementById('detail-panel').classList.add('hidden');
    cy.fit();
}

// --- Scan ---

async function triggerScan() {
    const btn = document.getElementById('btn-scan');
    const status = document.getElementById('scan-status');
    btn.disabled = true;
    status.className = 'scan-spinner';
    status.textContent = 'Scanning...';
    try {
        await fetch(`${API}/scan`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ source: 'all' }) });
        pollScanStatus();
    } catch {
        status.className = '';
        status.textContent = 'Scan failed';
        btn.disabled = false;
    }
}

function pollScanStatus() {
    const btn = document.getElementById('btn-scan');
    const status = document.getElementById('scan-status');
    const poll = setInterval(async () => {
        try {
            const data = await fetchJSON(`${API}/scan/status`);
            if (!data.running) {
                clearInterval(poll);
                status.className = '';
                status.textContent = 'Scan complete';
                btn.disabled = false;
                location.reload();
            }
        } catch {
            clearInterval(poll);
            status.className = '';
            status.textContent = '';
            btn.disabled = false;
        }
    }, 2000);
}

async function checkScanRunning() {
    try {
        const data = await fetchJSON(`${API}/scan/status`);
        if (data.running) {
            document.getElementById('btn-scan').disabled = true;
            document.getElementById('scan-status').className = 'scan-spinner';
            document.getElementById('scan-status').textContent = 'Scanning...';
            pollScanStatus();
        }
    } catch { /* ignore */ }
}

// --- Detail Panel ---

async function showDetail(nodeId) {
    const panel = document.getElementById('detail-panel');
    panel.classList.remove('hidden');

    const nodeData = (graphData.nodes || []).find(n => n.id === nodeId);
    if (!nodeData) return;

    document.getElementById('detail-name').textContent = `${nodeData.name} (${nodeData.type})`;

    let html = '<table class="meta-table">';
    html += `<tr><td>ID</td><td>${esc(nodeData.id)}</td></tr>`;
    html += `<tr><td>Type</td><td>${esc(nodeData.type)}</td></tr>`;
    html += `<tr><td>Source</td><td>${esc(nodeData.source)}</td></tr>`;
    html += `<tr><td>Provider</td><td>${esc(nodeData.provider || '-')}</td></tr>`;
    html += `<tr><td>Source File</td><td>${esc(nodeData.source_file || '-')}</td></tr>`;
    if (nodeData.expires_at) {
        const days = Math.ceil((new Date(nodeData.expires_at) - new Date()) / 86400000);
        const cls = days <= 0 ? 'expired' : days <= 7 ? 'critical' : days <= 30 ? 'warning' : 'ok';
        html += `<tr><td>Expires</td><td>${esc(nodeData.expires_at)} <span class="badge badge-${cls}">${days}d</span></td></tr>`;
    }
    if (nodeData.metadata) {
        for (const [k, v] of Object.entries(nodeData.metadata)) {
            html += `<tr><td>${esc(k)}</td><td>${esc(v)}</td></tr>`;
        }
    }
    html += '</table>';
    document.getElementById('detail-content').innerHTML = html;

    // Fetch impact
    try {
        const impact = await fetchJSON(`${API}/impact/${encodeURIComponent(nodeId)}`);
        let impactHtml = `<p>${parseInt(impact.affected_nodes) || 0} affected nodes</p>`;
        if (impact.impact_tree) {
            for (const [id, info] of Object.entries(impact.impact_tree)) {
                impactHtml += `<div class="impact-node"><span class="edge-type">[${esc(info.edge_type)}]</span> ${esc(id)}</div>`;
            }
        }
        document.getElementById('impact-content').innerHTML = impactHtml;

        // Highlight impacted nodes
        cy.nodes().removeClass('highlighted dimmed');
        const impactedIds = new Set(Object.keys(impact.impact_tree || {}));
        impactedIds.add(nodeId);
        cy.nodes('[!isGroup]').forEach(n => {
            if (impactedIds.has(n.data('id'))) {
                n.addClass('highlighted');
            } else {
                n.addClass('dimmed');
            }
        });
    } catch {
        document.getElementById('impact-content').innerHTML = '<p>Could not load impact data.</p>';
    }
}

init();
