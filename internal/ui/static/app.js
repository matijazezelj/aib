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

const SENSITIVE_TYPES = new Set(['secret', 'certificate', 'kms_key']);
const ZOOM_LABEL_THRESHOLD = 0.6;

let cy;
let graphData;
let auditData = null;
let auditByNode = {};  // nodeId → { severity, findings[] }
let privacyMode = localStorage.getItem('privacyMode') === 'true';
let selectedNodeId = null;
let disabledEdgeTypes = new Set();
let declutterMode = localStorage.getItem('declutterMode') === 'true';
let focusedResourceId = '';
let focusedResourceNodes = null;
let focusedSourceKey = '';
let evidenceFilterMode = 'all';
let onlineIconsEnabled = false;

const ICON_CDN_BASE = 'https://cdn.simpleicons.org';
const iconDataCache = new Map();

/* ---------- Built-in AWS service icons (Simple Icons removed all Amazon icons) ---------- */
const _bi = (svg) => 'data:image/svg+xml;base64,' + btoa(svg);
const BUILTIN_ICONS = {
    /* generic AWS logo – stylised arrow/smile */
    _aws: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M2 16c4-2 8-2 10 0s6 2 10 0"/><path d="M6 4l6 8 6-8"/></svg>'),
    /* EC2 – server / compute instance */
    _aws_vm: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><line x1="4" y1="9" x2="20" y2="9"/><line x1="4" y1="15" x2="20" y2="15"/><circle cx="7" cy="6.5" r=".5" fill="#c9d1d9"/><circle cx="7" cy="12" r=".5" fill="#c9d1d9"/><circle cx="7" cy="17.5" r=".5" fill="#c9d1d9"/></svg>'),
    /* S3 – bucket */
    _aws_bucket: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M4 5v14c0 1.66 3.58 3 8 3s8-1.34 8-3V5"/><path d="M4 12c0 1.66 3.58 3 8 3s8-1.34 8-3"/></svg>'),
    /* Lambda – function */
    _aws_function: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20h4l4-8 4 8h4"/><path d="M12 12l4-8"/></svg>'),
    /* RDS – database */
    _aws_database: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><ellipse cx="12" cy="5" rx="8" ry="3"/><path d="M20 5v14c0 1.66-3.58 3-8 3s-8-1.34-8-3V5"/><path d="M4 9c0 1.66 3.58 3 8 3s8-1.34 8-3"/><path d="M4 14c0 1.66 3.58 3 8 3s8-1.34 8-3"/></svg>'),
    /* SQS – queue */
    _aws_queue: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="6" width="6" height="4" rx="1"/><rect x="9" y="6" width="6" height="4" rx="1"/><rect x="16" y="6" width="6" height="4" rx="1"/><path d="M5 14v2h14v-2"/><path d="M12 16v4"/><path d="M9 20h6"/></svg>'),
    /* SNS – pub/sub notification */
    _aws_pubsub: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/><path d="M13.73 21a2 2 0 0 1-3.46 0"/></svg>'),
    /* ELB – load balancer */
    _aws_load_balancer: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="5" r="3"/><circle cx="5" cy="19" r="3"/><circle cx="19" cy="19" r="3"/><line x1="12" y1="8" x2="5" y2="16"/><line x1="12" y1="8" x2="19" y2="16"/></svg>'),
    /* Route 53 – DNS */
    _aws_dns_record: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10A15.3 15.3 0 0 1 12 2z"/></svg>'),
    /* ECS – container */
    _aws_container: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/><polyline points="3.27 6.96 12 12.01 20.73 6.96"/><line x1="12" y1="22.08" x2="12" y2="12"/></svg>'),
    /* IAM – service account / role */
    _aws_service_account: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><circle cx="12" cy="10" r="3"/><path d="M12 13v3"/></svg>'),
    /* VPC – network */
    _aws_network: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="2" y="2" width="20" height="20" rx="3"/><circle cx="8" cy="8" r="2"/><circle cx="16" cy="8" r="2"/><circle cx="8" cy="16" r="2"/><circle cx="16" cy="16" r="2"/><line x1="10" y1="8" x2="14" y2="8"/><line x1="8" y1="10" x2="8" y2="14"/><line x1="16" y1="10" x2="16" y2="14"/><line x1="10" y1="16" x2="14" y2="16"/></svg>'),
    /* Subnet */
    _aws_subnet: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="3" width="18" height="18" rx="2" stroke-dasharray="4 2"/><line x1="3" y1="12" x2="21" y2="12"/><line x1="12" y1="3" x2="12" y2="21"/></svg>'),
    /* Security Group – firewall */
    _aws_firewall_rule: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><line x1="9" y1="12" x2="15" y2="12"/></svg>'),
    /* KMS – key */
    _aws_kms_key: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="10" r="5"/><path d="M12.5 12.5L21 21"/><path d="M17 17l2-2"/><path d="M19.5 14.5l2-2"/></svg>'),
    /* Secrets Manager – secret / lock */
    _aws_secret: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><rect x="5" y="11" width="14" height="10" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/><circle cx="12" cy="16" r="1" fill="#c9d1d9"/></svg>'),
    /* Generic AWS service */
    _aws_service: _bi('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="#c9d1d9" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="3"/><path d="M12 2v4"/><path d="M12 18v4"/><path d="M4.93 4.93l2.83 2.83"/><path d="M16.24 16.24l2.83 2.83"/><path d="M2 12h4"/><path d="M18 12h4"/><path d="M4.93 19.07l2.83-2.83"/><path d="M16.24 7.76l2.83-2.83"/></svg>'),
};

const DETAIL_PANEL_WIDTH_KEY = 'detailPanelWidth';
const RESOURCE_PANEL_WIDTH_KEY = 'resourcePanelWidth';
const PANEL_DEFAULT_RATIO = 0.10;
const DETAIL_PANEL_MIN_WIDTH = 140;
const RESOURCE_PANEL_MIN_WIDTH = 140;

function getPanelDefaultWidth() {
    return Math.floor(window.innerWidth * PANEL_DEFAULT_RATIO);
}

function getResourcePanelMaxWidth() {
    return Math.max(RESOURCE_PANEL_MIN_WIDTH, Math.floor(window.innerWidth * 0.45));
}

function clampResourcePanelWidth(width) {
    return Math.min(getResourcePanelMaxWidth(), Math.max(RESOURCE_PANEL_MIN_WIDTH, width));
}

function applyResourcePanelWidth(width) {
    const panel = document.getElementById('resource-panel');
    if (!panel) return;
    panel.style.width = `${clampResourcePanelWidth(width)}px`;
}

function getDetailPanelMaxWidth() {
    return Math.max(DETAIL_PANEL_MIN_WIDTH, Math.floor(window.innerWidth * 0.45));
}

function clampDetailPanelWidth(width) {
    return Math.min(getDetailPanelMaxWidth(), Math.max(DETAIL_PANEL_MIN_WIDTH, width));
}

function applyDetailPanelWidth(width) {
    const panel = document.getElementById('detail-panel');
    if (!panel) return;
    panel.style.width = `${clampDetailPanelWidth(width)}px`;
}

function initDetailPanelResizer() {
    const panel = document.getElementById('detail-panel');
    const handle = document.getElementById('detail-resizer');
    if (!panel || !handle) return;

    const saved = parseInt(localStorage.getItem(DETAIL_PANEL_WIDTH_KEY) || String(getPanelDefaultWidth()), 10);
    if (!Number.isNaN(saved)) {
        applyDetailPanelWidth(saved);
    }

    let dragging = false;

    handle.addEventListener('mousedown', (evt) => {
        evt.preventDefault();
        dragging = true;
        document.body.classList.add('resizing-panel');
    });

    window.addEventListener('mousemove', (evt) => {
        if (!dragging) return;
        const viewportWidth = window.innerWidth;
        const width = viewportWidth - evt.clientX;
        const clamped = clampDetailPanelWidth(width);
        panel.style.width = `${clamped}px`;
    });

    const stopDragging = () => {
        if (!dragging) return;
        dragging = false;
        document.body.classList.remove('resizing-panel');
        const width = parseInt(panel.style.width || '360', 10);
        if (!Number.isNaN(width)) {
            localStorage.setItem(DETAIL_PANEL_WIDTH_KEY, String(clampDetailPanelWidth(width)));
        }
    };

    window.addEventListener('mouseup', stopDragging);
    window.addEventListener('blur', stopDragging);
    window.addEventListener('resize', () => {
        const width = parseInt(panel.style.width || '360', 10);
        if (!Number.isNaN(width)) applyDetailPanelWidth(width);
    });
}

function initResourcePanelResizer() {
    const panel = document.getElementById('resource-panel');
    const handle = document.getElementById('resource-resizer');
    if (!panel || !handle) return;

    const saved = parseInt(localStorage.getItem(RESOURCE_PANEL_WIDTH_KEY) || String(getPanelDefaultWidth()), 10);
    if (!Number.isNaN(saved)) {
        applyResourcePanelWidth(saved);
    }

    let dragging = false;

    handle.addEventListener('mousedown', (evt) => {
        evt.preventDefault();
        dragging = true;
        document.body.classList.add('resizing-panel');
    });

    window.addEventListener('mousemove', (evt) => {
        if (!dragging) return;
        const width = evt.clientX;
        const clamped = clampResourcePanelWidth(width);
        panel.style.width = `${clamped}px`;
    });

    const stopDragging = () => {
        if (!dragging) return;
        dragging = false;
        document.body.classList.remove('resizing-panel');
        const width = parseInt(panel.style.width || String(getPanelDefaultWidth()), 10);
        if (!Number.isNaN(width)) {
            localStorage.setItem(RESOURCE_PANEL_WIDTH_KEY, String(clampResourcePanelWidth(width)));
        }
    };

    window.addEventListener('mouseup', stopDragging);
    window.addEventListener('blur', stopDragging);
    window.addEventListener('resize', () => {
        const width = parseInt(panel.style.width || String(getPanelDefaultWidth()), 10);
        if (!Number.isNaN(width)) applyResourcePanelWidth(width);
    });
}

const SOURCE_ORDER = ['k8s', 'terraform', 'terraform-plan', 'cloudformation', 'pulumi', 'compose', 'ansible'];

function normalizeSourceKey(src) {
    if (!src) return 'unknown';
    if (src === 'kubernetes' || src === 'kubernetes-live') return 'k8s';
    return src;
}

const LAYOUTS = {
    readable: {
        name: 'cose',
        animate: false,
        nodeRepulsion: () => 28000,
        idealEdgeLength: () => 200,
        nodeOverlap: 28,
        padding: 64,
        componentSpacing: 200,
        nestingFactor: 1.3,
        gravity: 0.25,
    },
    balanced: {
        name: 'cose',
        animate: false,
        nodeRepulsion: () => 20000,
        idealEdgeLength: () => 160,
        nodeOverlap: 24,
        padding: 48,
        componentSpacing: 140,
        nestingFactor: 1.25,
        gravity: 0.28,
    },
    compact: {
        name: 'cose',
        animate: false,
        nodeRepulsion: () => 14000,
        idealEdgeLength: () => 120,
        nodeOverlap: 18,
        padding: 32,
        componentSpacing: 80,
        nestingFactor: 1.15,
        gravity: 0.32,
    },
};

function esc(str) {
    const d = document.createElement('div');
    d.appendChild(document.createTextNode(str == null ? '' : String(str)));
    return d.innerHTML;
}

function maskSensitive(str) {
    if (!str) return '';
    return str.replace(/./g, '*').slice(0, 8) + '\u2026';
}

async function fetchJSON(url) {
    const resp = await fetch(url);
    if (!resp.ok) {
        throw new Error(`HTTP ${resp.status}: ${resp.statusText}`);
    }
    return resp.json();
}

// --- Visible/Total Counter ---

function updateVisibleCount() {
    const visibleNodes = cy.nodes('[!isGroup]').filter(':visible').not('.dimmed').length;
    const totalNodes = cy.nodes('[!isGroup]').length;
    const visibleEdges = cy.edges().filter(':visible').not('.dimmed').length;
    const totalEdges = cy.edges().length;
    document.getElementById('stat-nodes-visible').textContent = visibleNodes;
    document.getElementById('stat-edges-visible').textContent = visibleEdges;
    document.getElementById('stat-nodes').textContent = totalNodes;
    document.getElementById('stat-edges').textContent = totalEdges;
}

// --- Edge Type Filter Pills ---

function buildEdgeFilterPills() {
    const container = document.getElementById('edge-filters');
    container.innerHTML = '';
    const edgeTypes = new Set();
    (graphData.edges || []).forEach(e => edgeTypes.add(e.type));
    if (edgeTypes.size === 0) return;

    const sorted = Array.from(edgeTypes).sort();
    sorted.forEach(type => {
        const pill = document.createElement('button');
        pill.className = 'edge-pill';
        pill.textContent = type;
        pill.dataset.edgeType = type;
        if (disabledEdgeTypes.has(type)) pill.classList.add('inactive');
        pill.addEventListener('click', () => {
            pill.classList.toggle('inactive');
            if (pill.classList.contains('inactive')) {
                disabledEdgeTypes.add(type);
            } else {
                disabledEdgeTypes.delete(type);
            }
            applyNodeVisibilityFilters();
        });
        container.appendChild(pill);
    });
}

function applyEdgeTypeFilter() {
    cy.edges().forEach(e => {
        e.toggleClass('dimmed', disabledEdgeTypes.has(e.data('label')));
    });
}

function computeConnectedResourceSet(startId) {
    const visited = new Set([startId]);
    const queue = [startId];

    while (queue.length > 0) {
        const current = queue.shift();
        (graphData.edges || []).forEach(e => {
            let next = null;
            if (e.from_id === current) next = e.to_id;
            else if (e.to_id === current) next = e.from_id;
            if (next && !visited.has(next)) {
                visited.add(next);
                queue.push(next);
            }
        });
    }

    return visited;
}

function setFocusedResource(nodeId) {
    focusedResourceId = nodeId || '';
    focusedResourceNodes = focusedResourceId ? computeConnectedResourceSet(focusedResourceId) : null;

    document.querySelectorAll('#resource-panel-list .group-item').forEach(row => {
        row.classList.toggle('active', row.dataset.nodeId === focusedResourceId);
    });

    applyNodeVisibilityFilters();
}

function setFocusedSource(sourceKey) {
    focusedSourceKey = sourceKey || '';
    if (focusedSourceKey) {
        setFocusedResource('');
    } else {
        document.querySelectorAll('#resource-panel-list .resource-section').forEach(section => {
            section.classList.remove('active-source');
        });
        applyNodeVisibilityFilters();
    }

    document.querySelectorAll('#resource-panel-list .resource-section').forEach(section => {
        section.classList.toggle('active-source', section.dataset.source === focusedSourceKey);
    });
}

function renderResourcePanel() {
    const list = document.getElementById('resource-panel-list');
    list.innerHTML = '';

    const sourceBuckets = new Map();
    (graphData.nodes || []).forEach(node => {
        const sourceKey = normalizeSourceKey(node.source);
        if (!sourceBuckets.has(sourceKey)) sourceBuckets.set(sourceKey, []);
        sourceBuckets.get(sourceKey).push(node);
    });

    const sourceKeys = Array.from(sourceBuckets.keys()).sort((a, b) => {
        const ai = SOURCE_ORDER.indexOf(a);
        const bi = SOURCE_ORDER.indexOf(b);
        if (ai !== -1 && bi !== -1) return ai - bi;
        if (ai !== -1) return -1;
        if (bi !== -1) return 1;
        return a.localeCompare(b);
    });

    sourceKeys.forEach(sourceKey => {
        const section = document.createElement('div');
        section.className = 'resource-section';
        section.dataset.source = sourceKey;

        const sectionTitle = document.createElement('div');
        sectionTitle.className = 'resource-section-title';
        sectionTitle.textContent = `${sourceKey} (${sourceBuckets.get(sourceKey).length})`;
        sectionTitle.title = `Show only ${sourceKey}`;
        sectionTitle.addEventListener('click', () => {
            if (focusedSourceKey === sourceKey) setFocusedSource('');
            else setFocusedSource(sourceKey);
        });
        section.appendChild(sectionTitle);

        const sectionList = document.createElement('div');
        sectionList.className = 'resource-section-list';

        const nodes = sourceBuckets.get(sourceKey)
            .slice()
            .sort((a, b) => (a.name || '').localeCompare(b.name || '') || a.id.localeCompare(b.id));

        nodes.forEach(node => {
            const row = document.createElement('div');
            row.className = 'group-item';
            row.dataset.nodeId = node.id;
            row.dataset.source = sourceKey;

            const label = document.createElement('label');
            label.style.cursor = 'pointer';

            const name = document.createElement('span');
            name.className = 'group-name';
            name.textContent = node.name || node.id;

            const meta = document.createElement('span');
            meta.className = 'group-count';
            meta.textContent = node.type || '';

            label.appendChild(name);
            row.appendChild(label);
            row.appendChild(meta);

            row.addEventListener('click', () => {
                if (focusedResourceId === node.id) setFocusedResource('');
                else setFocusedResource(node.id);
            });

            sectionList.appendChild(row);
        });

        section.appendChild(sectionList);
        list.appendChild(section);
    });

    document.querySelectorAll('#resource-panel-list .resource-section').forEach(section => {
        section.classList.toggle('active-source', section.dataset.source === focusedSourceKey);
    });
}

function applyResourceSearchFilter() {
    const q = (document.getElementById('resource-search').value || '').toLowerCase();
    document.querySelectorAll('#resource-panel-list .group-item').forEach(row => {
        const text = row.textContent.toLowerCase();
        row.style.display = (!q || text.includes(q)) ? '' : 'none';
    });

    document.querySelectorAll('#resource-panel-list .resource-section').forEach(section => {
        const visibleRows = section.querySelectorAll('.group-item:not([style*="display: none"])').length;
        section.style.display = visibleRows > 0 ? '' : 'none';
    });
}

function applyNodeVisibilityFilters() {
    if (!cy) return;

    const q = (document.getElementById('search').value || '').toLowerCase();
    const type = document.getElementById('filter-type').value;
    const source = document.getElementById('filter-source').value;

    cy.nodes('[!isGroup]').forEach(n => {
        const id = (n.data('id') || '').toLowerCase();
        const label = (n.data('label') || '').toLowerCase();
        const matchQuery = !q || label.includes(q) || id.includes(q);
        const matchType = !type || n.data('assetType') === type;
        const matchSource = !source || n.data('source') === source;
        const matchFocused = !focusedResourceNodes || focusedResourceNodes.has(n.data('id'));
        const nodeRaw = (graphData.nodes || []).find(x => x.id === n.data('id'));
        const normalizedSource = nodeRaw ? normalizeSourceKey(nodeRaw.source) : 'unknown';
        const matchFocusedSource = !focusedSourceKey || normalizedSource === focusedSourceKey;

        n.toggleClass('dimmed', !(matchQuery && matchType && matchSource && matchFocused && matchFocusedSource));
    });

    cy.edges().forEach(e => {
        const sourceNode = cy.getElementById(e.data('source'));
        const targetNode = cy.getElementById(e.data('target'));
        const endpointHidden = (sourceNode && sourceNode.hasClass('dimmed')) || (targetNode && targetNode.hasClass('dimmed'));
        const edgeTypeDisabled = disabledEdgeTypes.has(e.data('label'));
        e.toggleClass('dimmed', endpointHidden || edgeTypeDisabled);
    });

    updateVisibleCount();
}

function getLayoutMode() {
    const sel = document.getElementById('layout-mode');
    if (!sel) return 'readable';
    return sel.value || 'readable';
}

function runCurrentLayout() {
    const mode = getLayoutMode();
    const cfg = LAYOUTS[mode] || LAYOUTS.readable;
    cy.layout(cfg).run();
    tileComponents();
}

/* ---------- Connected-component tiling ---------- */
// After CoSE finishes, repack disconnected components into a tidy grid
// so they don't overlap or drift into each other.
function tileComponents() {
    if (!cy) return;
    // Skip tiling when compound groups are active — CoSE handles them
    if (cy.nodes('[?isGroup]').length > 0) return;

    const components = cy.elements().components();
    if (components.length <= 1) return;  // nothing to tile

    // Measure bounding box of each component
    const boxes = components.map(comp => {
        const bb = comp.boundingBox();
        return { comp, w: bb.w, h: bb.h, cx: (bb.x1 + bb.x2) / 2, cy: (bb.y1 + bb.y2) / 2 };
    });

    // Sort largest-area first for nicer packing
    boxes.sort((a, b) => (b.w * b.h) - (a.w * a.h));

    const gap = 120;  // px between components
    const cols = Math.ceil(Math.sqrt(boxes.length));

    // Compute column widths and row heights
    const colWidths = new Array(cols).fill(0);
    const rows = Math.ceil(boxes.length / cols);
    const rowHeights = new Array(rows).fill(0);
    boxes.forEach((b, i) => {
        const col = i % cols;
        const row = Math.floor(i / cols);
        colWidths[col] = Math.max(colWidths[col], b.w);
        rowHeights[row] = Math.max(rowHeights[row], b.h);
    });

    // Place each component in its grid cell center
    cy.batch(() => {
        boxes.forEach((b, i) => {
            const col = i % cols;
            const row = Math.floor(i / cols);

            // Target cell center
            let tx = 0;
            for (let c = 0; c < col; c++) tx += colWidths[c] + gap;
            tx += colWidths[col] / 2;

            let ty = 0;
            for (let r = 0; r < row; r++) ty += rowHeights[r] + gap;
            ty += rowHeights[row] / 2;

            const dx = tx - b.cx;
            const dy = ty - b.cy;

            // Shift all nodes in this component
            b.comp.nodes().forEach(n => {
                const pos = n.position();
                n.position({ x: pos.x + dx, y: pos.y + dy });
            });
        });
    });

    cy.fit(undefined, 40);
}

function applyDeclutterMode() {
    if (!cy) return;

    cy.batch(() => {
        cy.nodes('[!isGroup]').removeClass('decluttered-isolated');
        cy.edges().removeClass('decluttered-edge');
        if (!declutterMode) return;

        cy.nodes('[!isGroup]').forEach(n => {
            if (n.connectedEdges().length === 0) {
                n.addClass('decluttered-isolated');
            }
        });
        cy.edges().addClass('decluttered-edge');
    });

    const btn = document.getElementById('btn-declutter');
    if (btn) btn.classList.toggle('active', declutterMode);
    localStorage.setItem('declutterMode', String(declutterMode));
    updateVisibleCount();
}

// --- Privacy Mode ---

function getNodeLabel(ele) {
    if (privacyMode && SENSITIVE_TYPES.has(ele.data('assetType'))) {
        return maskSensitive(ele.data('label'));
    }
    return ele.data('label');
}

function truncateLabel(str) {
    if (!str) return '';
    return str.length > 18 ? str.slice(0, 16) + '…' : str;
}

function togglePrivacyMode() {
    privacyMode = !privacyMode;
    localStorage.setItem('privacyMode', privacyMode);
    document.getElementById('btn-privacy').classList.toggle('active', privacyMode);
    document.getElementById('btn-declutter').classList.toggle('active', declutterMode);
    document.getElementById('layout-mode').value = localStorage.getItem('layoutMode') || 'readable';
    cy.style().update();
    // Re-render detail panel if open
    if (selectedNodeId) showDetail(selectedNodeId);
}

function maskDetailValue(key, value) {
    if (!privacyMode) return esc(value);
    const sensitiveKeys = new Set(['id', 'name', 'arn', 'dns_name', 'hostname', 'ip', 'endpoint']);
    if (sensitiveKeys.has(key.toLowerCase())) return esc(maskSensitive(String(value)));
    return esc(value);
}

function iconURL(slug) {
    return `${ICON_CDN_BASE}/${slug}/c9d1d9`;
}

function resolveNodeIconSlug(nodeData) {
    const type = String(nodeData.assetType || nodeData.type || '').toLowerCase();
    const source = String(nodeData.source || '').toLowerCase();
    const provider = String(nodeData.provider || '').toLowerCase();
    const label = String(nodeData.label || nodeData.name || '').toLowerCase();

    if (label.includes('redis') || provider.includes('redis')) return 'redis';
    if (label.includes('postgres') || provider.includes('postgres')) return 'postgresql';
    if (label.includes('mysql') || provider.includes('mysql')) return 'mysql';
    if (label.includes('mongo') || provider.includes('mongo')) return 'mongodb';
    if (label.includes('nginx')) return 'nginx';

    if (source.includes('kubernetes') || source === 'k8s' || provider.includes('kubernetes')) return 'kubernetes';
    if (source.includes('terraform') || provider.includes('terraform')) return 'terraform';
    if (source.includes('pulumi') || provider.includes('pulumi')) return 'pulumi';
    if (source.includes('ansible') || provider.includes('ansible')) return 'ansible';
    if (source.includes('compose') || provider.includes('docker') || type === 'container') return 'docker';
    if (source.includes('cloudformation') || provider.includes('aws')) {
        const awsKey = '_aws_' + type;
        return BUILTIN_ICONS[awsKey] ? awsKey : '_aws';
    }

    if (provider.includes('gcp') || provider.includes('google')) return 'googlecloud';
    if (provider.includes('azure')) return '';

    if (type === 'database') return 'postgresql';
    if (type === 'service') return 'kubernetes';

    return '';
}

function getNodeIconURL(nodeData) {
    const slug = resolveNodeIconSlug(nodeData);
    if (!slug) return '';
    return slug;
}

async function fetchIconDataURI(slug) {
    if (!slug) return '';
    if (iconDataCache.has(slug)) return iconDataCache.get(slug);

    // Built-in icons (e.g. AWS) – no network request needed
    if (BUILTIN_ICONS[slug]) {
        iconDataCache.set(slug, BUILTIN_ICONS[slug]);
        return BUILTIN_ICONS[slug];
    }

    try {
        const resp = await fetch(iconURL(slug));
        if (!resp.ok) {
            iconDataCache.set(slug, '');
            return '';
        }
        const svg = await resp.text();
        const dataURI = 'data:image/svg+xml;base64,' + btoa(unescape(encodeURIComponent(svg)));
        iconDataCache.set(slug, dataURI);
        return dataURI;
    } catch {
        iconDataCache.set(slug, '');
        return '';
    }
}

async function applyOnlineIcons() {
    if (!cy) return;

    const btn = document.getElementById('btn-online-icons');

    if (!onlineIconsEnabled) {
        // Remove per-node style overrides so stylesheet defaults take over
        cy.nodes('[!isGroup]').forEach((node) => {
            node.removeStyle('background-image background-image-opacity');
        });
        if (btn) btn.classList.remove('active');
        return;
    }

    // Collect unique slugs
    const slugs = new Set();
    cy.nodes('[!isGroup]').forEach((node) => {
        const slug = getNodeIconURL(node.data());
        if (slug) slugs.add(slug);
    });
    // Fetch all icons in parallel
    await Promise.all(Array.from(slugs).map((slug) => fetchIconDataURI(slug)));

    // Apply cached data URIs as direct style overrides
    cy.batch(() => {
        cy.nodes('[!isGroup]').forEach((node) => {
            const slug = getNodeIconURL(node.data());
            const dataURI = slug ? (iconDataCache.get(slug) || '') : '';
            if (dataURI) {
                node.style({
                    'background-image': dataURI,
                    'background-image-opacity': 1,
                    'background-image-containment': 'inside',
                    'background-fit': 'contain',
                    'background-width': '62%',
                    'background-height': '62%',
                });
            }
        });
    });

    if (btn) btn.classList.toggle('active', onlineIconsEnabled);
}

function isConnectionStringKey(key) {
    const lowered = String(key || '').toLowerCase();
    return (
        lowered === 'connection_string' ||
        lowered === 'database_url' ||
        lowered === 'db_url' ||
        lowered === 'redis_url' ||
        lowered.endsWith('_url') ||
        lowered.endsWith('_dsn')
    );
}

function looksLikeConnectionString(value) {
    const raw = String(value == null ? '' : value).trim();
    if (!raw) return false;
    return /^[a-z][a-z0-9+.-]*:\/\//i.test(raw);
}

async function copyTextToClipboard(text) {
    const raw = String(text == null ? '' : text);
    if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(raw);
        return;
    }

    const temp = document.createElement('textarea');
    temp.value = raw;
    temp.style.position = 'fixed';
    temp.style.left = '-9999px';
    document.body.appendChild(temp);
    temp.focus();
    temp.select();
    document.execCommand('copy');
    document.body.removeChild(temp);
}

// --- Init ---

async function init() {
    const [gd, stats, audit] = await Promise.all([
        fetchJSON(`${API}/graph`),
        fetchJSON(`${API}/stats`),
        fetchJSON(`${API}/graph/analysis/audit`).catch(() => null),
    ]);
    graphData = gd;

    // Index audit findings by node ID, keeping the worst severity per node
    auditData = audit;
    auditByNode = {};
    if (audit && audit.findings) {
        const sevOrder = { critical: 3, warning: 2, info: 1 };
        for (const f of audit.findings) {
            if (!auditByNode[f.resource_id]) {
                auditByNode[f.resource_id] = { severity: f.severity, findings: [] };
            }
            auditByNode[f.resource_id].findings.push(f);
            if ((sevOrder[f.severity] || 0) > (sevOrder[auditByNode[f.resource_id].severity] || 0)) {
                auditByNode[f.resource_id].severity = f.severity;
            }
        }
    }

    // Stats header
    document.getElementById('stat-nodes').textContent = stats.nodes_total || 0;
    document.getElementById('stat-edges').textContent = stats.edges_total || 0;
    document.getElementById('stat-certs').textContent = `${stats.expiring_certs || 0} expiring certs`;

    // Privacy button initial state
    document.getElementById('btn-privacy').classList.toggle('active', privacyMode);
    document.getElementById('btn-online-icons').classList.toggle('active', onlineIconsEnabled);

    initResourcePanelResizer();
    initDetailPanelResizer();

    // Populate filter dropdowns
    const types = new Set((graphData.nodes || []).map(n => n.type));
    const sources = new Set((graphData.nodes || []).map(n => n.source));
    const typeSelect = document.getElementById('filter-type');
    const sourceSelect = document.getElementById('filter-source');
    types.forEach(t => { const o = document.createElement('option'); o.value = t; o.textContent = t; typeSelect.appendChild(o); });
    sources.forEach(s => { const o = document.createElement('option'); o.value = s; o.textContent = s; sourceSelect.appendChild(o); });

    // Build edge filter pills
    buildEdgeFilterPills();
    renderResourcePanel();
    applyResourceSearchFilter();

    // Build Cytoscape elements
    // Default to source grouping for a tidier view
    const defaultGroupBy = '';
    document.getElementById('group-by').value = defaultGroupBy;
    const elements = buildElements(graphData, defaultGroupBy);

    cy = cytoscape({
        container: document.getElementById('cy'),
        elements,
        style: [
            {
                selector: 'node[!isGroup]',
                style: {
                    'label': function(ele) { return truncateLabel(getNodeLabel(ele)); },
                    'shape': el => TYPE_SHAPES[el.data('assetType')] || 'ellipse',
                    'background-color': el => TYPE_COLORS[el.data('assetType')] || '#D5D8DC',
                    'color': '#c9d1d9',
                    'font-size': '14px',
                    'text-background-color': '#0d1117',
                    'text-background-opacity': 0.72,
                    'text-background-padding': 2,
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
                    'font-size': '14px',
                    'text-valign': 'top',
                    'text-halign': 'center',
                    'text-margin-y': -8,
                },
            },
            {
                selector: 'edge',
                style: {
                    'label': 'data(label)',
                    'font-size': '11px',
                    'color': '#8b949e',
                    'text-opacity': 0,
                    'opacity': 0.55,
                    'line-color': '#30363d',
                    'target-arrow-color': '#30363d',
                    'target-arrow-shape': 'triangle',
                    'curve-style': function(ele) { return ele.data('curveStyle') || 'bezier'; },
                    'control-point-distances': function(ele) { return ele.data('cpDist') || 0; },
                    'width': 1.5,
                },
            },
            {
                selector: 'edge:selected',
                style: { 'text-opacity': 1, 'line-color': '#58a6ff', 'target-arrow-color': '#58a6ff', 'width': 2.5 },
            },
            {
                selector: 'edge.edge-hover',
                style: { 'text-opacity': 1, 'line-color': '#58a6ff', 'target-arrow-color': '#58a6ff', 'width': 2 },
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
                selector: 'node[securityRisk = "critical"]',
                style: { 'border-color': '#da3633', 'border-width': 3.5 },
            },
            {
                selector: 'node[securityRisk = "warning"]',
                style: { 'border-color': '#d29922', 'border-width': 3 },
            },
            {
                selector: '.dimmed',
                style: { 'opacity': 0.2 },
            },
            {
                selector: '.labels-hidden',
                style: { 'label': '' },
            },
            {
                selector: '.decluttered-edge',
                style: {
                    'opacity': 0.22,
                    'text-opacity': 0,
                },
            },
            {
                selector: '.decluttered-isolated',
                style: {
                    'opacity': 0.16,
                    'label': '',
                },
            },
        ],
        layout: LAYOUTS[getLayoutMode()] || LAYOUTS.readable,
    });

    // Tile disconnected components into a grid after initial layout
    tileComponents();

    // Set group node background colors
    cy.nodes('[?isGroup]').forEach(n => {
        n.style('background-color', n.data('bgColor') || '#1f3044');
    });

    applyDeclutterMode();

    // Update visible count after initial build
    updateVisibleCount();

    // Edge hover for labels
    cy.on('mouseover', 'edge', (evt) => { evt.target.addClass('edge-hover'); });
    cy.on('mouseout', 'edge', (evt) => { evt.target.removeClass('edge-hover'); });

    // Zoom-based label hiding
    cy.on('zoom', () => {
        const zoom = cy.zoom();
        cy.batch(() => {
            if (zoom < ZOOM_LABEL_THRESHOLD) {
                cy.nodes('[!isGroup]').addClass('labels-hidden');
            } else {
                cy.nodes('[!isGroup]').removeClass('labels-hidden');
            }
        });
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
    document.getElementById('search').addEventListener('input', applyNodeVisibilityFilters);

    // Filters
    typeSelect.addEventListener('change', applyNodeVisibilityFilters);
    sourceSelect.addEventListener('change', applyNodeVisibilityFilters);

    // Resource panel controls
    document.getElementById('btn-resource-all').addEventListener('click', () => {
        focusedSourceKey = '';
        setFocusedResource('');
        document.querySelectorAll('#resource-panel-list .resource-section').forEach(section => {
            section.classList.remove('active-source');
        });
        applyNodeVisibilityFilters();
    });
    document.getElementById('resource-search').addEventListener('input', applyResourceSearchFilter);

    document.getElementById('layout-mode').addEventListener('change', (e) => {
        localStorage.setItem('layoutMode', e.target.value || 'readable');
        runCurrentLayout();
    });

    document.getElementById('btn-declutter').addEventListener('click', () => {
        declutterMode = !declutterMode;
        applyDeclutterMode();
    });

    // Group by
    document.getElementById('group-by').addEventListener('change', (e) => {
        rebuildGraph(e.target.value);
        applyNodeVisibilityFilters();
    });

    // Reset
    document.getElementById('btn-reset').addEventListener('click', resetView);

    // Quick filter: certificates
    document.getElementById('btn-certs').addEventListener('click', () => {
        document.getElementById('filter-type').value = 'certificate';
        applyNodeVisibilityFilters();
    });

    // Close panel
    document.getElementById('close-panel').addEventListener('click', () => {
        document.getElementById('detail-panel').classList.add('hidden');
        selectedNodeId = null;
        cy.nodes().removeClass('highlighted dimmed neighbor-highlight');
        cy.edges().removeClass('dimmed');
        updateVisibleCount();
    });

    // Legend toggle
    document.getElementById('btn-legend').addEventListener('click', () => {
        document.getElementById('legend').classList.toggle('hidden');
    });

    // Shortcuts toggle
    document.getElementById('btn-shortcuts').addEventListener('click', () => {
        document.getElementById('shortcuts-panel').classList.toggle('hidden');
    });

    // Privacy toggle
    document.getElementById('btn-privacy').addEventListener('click', togglePrivacyMode);

    // Online icons (explicit user consent)
    document.getElementById('btn-online-icons').addEventListener('click', () => {
        if (!onlineIconsEnabled) {
            const allowed = window.confirm(
                'Enable online icons?\n\nAIB will fetch service/provider icons from a public icon CDN (cdn.simpleicons.org) in your browser.\n\nNo icon requests are made unless you enable this.'
            );
            if (!allowed) return;
        }

        onlineIconsEnabled = !onlineIconsEnabled;
        applyOnlineIcons();
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

    // Focus mode: upstream depth buttons
    document.querySelectorAll('#upstream-depth button').forEach(btn => {
        btn.addEventListener('click', () => {
            if (!selectedNodeId) return;
            document.querySelectorAll('#upstream-depth button').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            focusUpstream(selectedNodeId, parseInt(btn.dataset.depth));
        });
    });

    // Focus mode: downstream depth buttons
    document.querySelectorAll('#downstream-depth button').forEach(btn => {
        btn.addEventListener('click', () => {
            if (!selectedNodeId) return;
            document.querySelectorAll('#downstream-depth button').forEach(b => b.classList.remove('active'));
            btn.classList.add('active');
            focusDownstream(selectedNodeId, parseInt(btn.dataset.depth));
        });
    });

    // Focus mode: secrets touched
    document.getElementById('btn-secrets-touched').addEventListener('click', () => {
        if (!selectedNodeId) return;
        focusSecretsTouched(selectedNodeId);
    });

    // Focus mode: external exposure
    document.getElementById('btn-external-exposure').addEventListener('click', () => {
        if (!selectedNodeId) return;
        focusExternalExposure(selectedNodeId);
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
        const audit = auditByNode[n.id];
        const nodeData = {
            id: n.id, label: n.name, assetType: n.type, type: n.type, source: n.source,
            provider: n.provider, expires_at: n.expires_at,
            namespace: (n.metadata && n.metadata.namespace) || '',
            securityRisk: audit ? audit.severity : 'none',
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

    // Build edges with parallel edge detection
    const edgePairCount = {};
    const edgePairIndex = {};
    (data.edges || []).forEach(e => {
        const pairKey = [e.from_id, e.to_id].sort().join('|');
        edgePairCount[pairKey] = (edgePairCount[pairKey] || 0) + 1;
    });

    (data.edges || []).forEach(e => {
        const pairKey = [e.from_id, e.to_id].sort().join('|');
        const count = edgePairCount[pairKey];
        const edgeData = {
            id: e.id, source: e.from_id, target: e.to_id, label: e.type,
            curveStyle: 'bezier',
            cpDist: 0,
        };

        if (count > 1) {
            if (!edgePairIndex[pairKey]) edgePairIndex[pairKey] = 0;
            const idx = edgePairIndex[pairKey]++;
            edgeData.curveStyle = 'unbundled-bezier';
            const distances = [30, -30, 60, -60, 90, -90];
            edgeData.cpDist = distances[idx % distances.length];
        }

        elements.push({ data: edgeData });
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

    runCurrentLayout();
    applyOnlineIcons();

    applyEdgeTypeFilter();
    applyDeclutterMode();
    applyNodeVisibilityFilters();
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
    updateVisibleCount();
}

// --- Focus Mode ---

async function focusUpstream(nodeId, depth) {
    clearFocusMessage();
    try {
        const impact = await fetchJSON(`${API}/impact/${encodeURIComponent(nodeId)}`);
        const matchIds = new Set([nodeId]);
        if (impact.impact_tree) {
            for (const [id, info] of Object.entries(impact.impact_tree)) {
                if (info.depth <= depth) matchIds.add(id);
            }
        }
        highlightNodeSet(matchIds);
    } catch {
        setFocusMessage('Could not load upstream data.');
    }
}

async function focusDownstream(nodeId, depth) {
    clearFocusMessage();
    try {
        const result = await fetchJSON(`${API}/graph/dependency-chain/${encodeURIComponent(nodeId)}?depth=${depth}`);
        const matchIds = new Set([nodeId]);
        (result.nodes || []).forEach(n => matchIds.add(n.id));
        highlightNodeSet(matchIds);
    } catch {
        setFocusMessage('Could not load downstream data.');
    }
}

function focusSecretsTouched(nodeId) {
    clearFocusMessage();
    const secretNodes = new Set();
    const visited = new Set();
    const queue = [nodeId];
    visited.add(nodeId);

    // BFS through all edges from the selected node, collect nodes connected via mounts_secret edges
    while (queue.length > 0) {
        const current = queue.shift();
        (graphData.edges || []).forEach(e => {
            let neighbor = null;
            if (e.from_id === current) neighbor = e.to_id;
            else if (e.to_id === current) neighbor = e.from_id;
            if (neighbor && !visited.has(neighbor)) {
                visited.add(neighbor);
                queue.push(neighbor);
                if (e.type === 'mounts_secret') {
                    secretNodes.add(neighbor);
                    secretNodes.add(current);
                }
            }
        });
    }

    if (secretNodes.size === 0) {
        setFocusMessage('No secrets touched.');
        return;
    }
    secretNodes.add(nodeId);
    highlightNodeSet(secretNodes);
}

function focusExternalExposure(nodeId) {
    clearFocusMessage();
    const externalTypes = new Set(['ingress', 'load_balancer']);

    // Build adjacency for reverse BFS (upstream)
    const parentMap = {};
    (graphData.edges || []).forEach(e => {
        if (!parentMap[e.to_id]) parentMap[e.to_id] = [];
        parentMap[e.to_id].push(e.from_id);
    });

    // BFS upstream from nodeId
    const visited = new Set();
    const prev = {};
    const queue = [nodeId];
    visited.add(nodeId);
    let exposedNode = null;

    while (queue.length > 0 && !exposedNode) {
        const current = queue.shift();
        const nodeInfo = (graphData.nodes || []).find(n => n.id === current);
        if (nodeInfo && externalTypes.has(nodeInfo.type) && current !== nodeId) {
            exposedNode = current;
            break;
        }
        (parentMap[current] || []).forEach(parent => {
            if (!visited.has(parent)) {
                visited.add(parent);
                prev[parent] = current;
                queue.push(parent);
            }
        });
    }

    if (!exposedNode) {
        setFocusMessage('Not externally exposed.');
        return;
    }

    // Reconstruct path
    const pathNodes = new Set([nodeId]);
    let cur = exposedNode;
    while (cur && cur !== nodeId) {
        pathNodes.add(cur);
        cur = prev[cur];
    }
    pathNodes.add(exposedNode);
    highlightNodeSet(pathNodes);
    setFocusMessage(`Exposed via ${exposedNode}`);
}

function highlightNodeSet(ids) {
    cy.nodes().removeClass('highlighted dimmed neighbor-highlight');
    cy.edges().removeClass('dimmed');
    cy.nodes('[!isGroup]').forEach(n => {
        if (ids.has(n.data('id'))) {
            n.addClass('highlighted');
        } else {
            n.addClass('dimmed');
        }
    });
    cy.edges().forEach(e => {
        if (!ids.has(e.data('source')) || !ids.has(e.data('target'))) {
            e.addClass('dimmed');
        }
    });
    updateVisibleCount();
}

function setFocusMessage(msg) {
    document.getElementById('focus-message').textContent = msg;
}
function clearFocusMessage() {
    document.getElementById('focus-message').textContent = '';
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
            selectedNodeId = null;
            cy.nodes().removeClass('highlighted dimmed neighbor-highlight');
            cy.edges().removeClass('dimmed');
            hideContextMenu();
            updateVisibleCount();
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
        case 'p':
            togglePrivacyMode();
            break;
    }
}

function resetView() {
    cy.nodes().removeClass('dimmed highlighted neighbor-highlight labels-hidden');
    cy.edges().removeClass('dimmed edge-hover decluttered-edge');
    cy.nodes().removeClass('decluttered-isolated');
    disabledEdgeTypes.clear();
    buildEdgeFilterPills();
    document.getElementById('search').value = '';
    document.getElementById('filter-type').value = '';
    document.getElementById('filter-source').value = '';
    declutterMode = false;
    localStorage.setItem('declutterMode', 'false');
    document.getElementById('btn-declutter').classList.remove('active');
    document.getElementById('group-by').value = '';
    focusedSourceKey = '';
    setFocusedResource('');
    document.getElementById('resource-search').value = '';
    applyResourceSearchFilter();
    rebuildGraph('');
    document.getElementById('detail-panel').classList.add('hidden');
    selectedNodeId = null;
    // Clear focus depth button states
    document.querySelectorAll('.depth-buttons button').forEach(b => b.classList.remove('active'));
    clearFocusMessage();
    cy.fit();
    applyNodeVisibilityFilters();
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

function buildConnectionEvidenceRows(nodeId) {
    const evidenceRows = [];
    (graphData.edges || []).forEach((edge) => {
        if (edge.from_id !== nodeId && edge.to_id !== nodeId) return;
        if (evidenceFilterMode === 'connects_to' && edge.type !== 'connects_to') return;

        const metadata = edge.metadata || {};
        if (Object.keys(metadata).length === 0) return;

        const isOutgoing = edge.from_id === nodeId;
        const peerID = isOutgoing ? edge.to_id : edge.from_id;
        const peerNode = (graphData.nodes || []).find(n => n.id === peerID);
        const peerName = peerNode ? (peerNode.name || peerID) : peerID;
        const direction = isOutgoing ? '→' : '←';

        const metadataParts = Object.entries(metadata)
            .filter(([, value]) => String(value || '').trim() !== '')
            .map(([key, value]) => `${esc(key)}=${esc(String(value))}`)
            .join(', ');

        if (!metadataParts) return;

        evidenceRows.push(`<tr><td>${esc(edge.type)}</td><td><span class="evidence-target">${direction} ${esc(peerName)}</span><br><span class="evidence-meta">${metadataParts}</span></td></tr>`);
    });

    return evidenceRows;
}

// --- Detail Panel ---

async function showDetail(nodeId) {
    selectedNodeId = nodeId;
    const panel = document.getElementById('detail-panel');
    panel.classList.remove('hidden');

    const nodeData = (graphData.nodes || []).find(n => n.id === nodeId);
    if (!nodeData) return;

    const isSensitive = privacyMode && SENSITIVE_TYPES.has(nodeData.type);
    const displayName = isSensitive ? maskSensitive(nodeData.name) : nodeData.name;

    document.getElementById('detail-name').textContent = `${displayName} (${nodeData.type})`;

    // Security findings banner
    const nodeAudit = auditByNode[nodeId];
    let html = '';
    if (nodeAudit && nodeAudit.findings.length > 0) {
        const worstSev = nodeAudit.severity;
        const sevClass = worstSev === 'critical' ? 'security-banner-critical' : worstSev === 'warning' ? 'security-banner-warning' : 'security-banner-info';
        html += `<div class="security-banner ${sevClass}">`;
        html += `<strong>Security Findings (${nodeAudit.findings.length})</strong>`;
        html += '<ul>';
        for (const f of nodeAudit.findings) {
            html += `<li><span class="badge badge-${f.severity === 'critical' ? 'critical' : f.severity === 'warning' ? 'warning' : 'ok'}">${esc(f.severity)}</span> ${esc(f.description)}</li>`;
        }
        html += '</ul></div>';
    }

    html += '<table class="meta-table">';
    html += `<tr><td>ID</td><td>${maskDetailValue('id', nodeData.id)}</td></tr>`;
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
        const metadataEntries = Object.entries(nodeData.metadata).sort((a, b) => {
            const aConn = isConnectionStringKey(a[0]) || looksLikeConnectionString(a[1]);
            const bConn = isConnectionStringKey(b[0]) || looksLikeConnectionString(b[1]);
            if (aConn !== bConn) return aConn ? -1 : 1;
            return a[0].localeCompare(b[0]);
        });

        for (const [k, v] of metadataEntries) {
            const renderedValue = isSensitive ? maskDetailValue(k, v) : esc(v);
            const isConn = isConnectionStringKey(k) || looksLikeConnectionString(v);
            const valueClass = isConn ? 'meta-value connection-string' : 'meta-value';
            const copyButton = isConn
                ? `<button type="button" class="copy-value-btn" data-copy="${encodeURIComponent(String(v == null ? '' : v))}">Copy</button>`
                : '';
            html += `<tr><td>${esc(k)}</td><td><span class="meta-cell"><span class="${valueClass}">${renderedValue}</span>${copyButton}</span></td></tr>`;
        }
    }
    html += '</table>';

    const evidenceRows = buildConnectionEvidenceRows(nodeId);

    if (evidenceRows.length > 0) {
        html += '<div class="evidence-header">';
        html += '<h4>Connection Evidence</h4>';
        html += '<div class="evidence-controls">';
        html += `<button type="button" class="evidence-filter-btn${evidenceFilterMode === 'all' ? ' active' : ''}" data-evidence-filter="all">All</button>`;
        html += `<button type="button" class="evidence-filter-btn${evidenceFilterMode === 'connects_to' ? ' active' : ''}" data-evidence-filter="connects_to">connects_to</button>`;
        html += '</div>';
        html += '</div>';
        html += '<table class="meta-table evidence-table">';
        html += evidenceRows.join('');
        html += '</table>';
    }

    document.getElementById('detail-content').innerHTML = html;

    document.querySelectorAll('.copy-value-btn').forEach((btn) => {
        btn.addEventListener('click', async () => {
            const raw = decodeURIComponent(btn.getAttribute('data-copy') || '');
            const originalLabel = btn.textContent;
            try {
                await copyTextToClipboard(raw);
                btn.textContent = 'Copied';
            } catch {
                btn.textContent = 'Failed';
            }
            setTimeout(() => {
                btn.textContent = originalLabel;
            }, 1200);
        });
    });

    document.querySelectorAll('.evidence-filter-btn').forEach((btn) => {
        btn.addEventListener('click', async () => {
            const nextMode = btn.getAttribute('data-evidence-filter') || 'all';
            if (nextMode === evidenceFilterMode) return;
            evidenceFilterMode = nextMode;
            await showDetail(nodeId);
        });
    });

    // Reset focus controls
    document.querySelectorAll('.depth-buttons button').forEach(b => b.classList.remove('active'));
    clearFocusMessage();

    // Fetch impact
    try {
        const impact = await fetchJSON(`${API}/impact/${encodeURIComponent(nodeId)}`);
        let impactHtml = `<p>${parseInt(impact.affected_nodes) || 0} affected nodes</p>`;
        if (impact.impact_tree) {
            for (const [id, info] of Object.entries(impact.impact_tree)) {
                const displayId = (privacyMode && isSensitive) ? maskSensitive(id) : esc(id);
                impactHtml += `<div class="impact-node"><span class="edge-type">[${esc(info.edge_type)}]</span> ${displayId}</div>`;
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
        updateVisibleCount();
    } catch {
        document.getElementById('impact-content').innerHTML = '<p>Could not load impact data.</p>';
    }
}

init();

// --- Auto-refresh ---
let autoRefreshTimer = null;

function startAutoRefresh(intervalMs) {
    stopAutoRefresh();
    autoRefreshTimer = setInterval(async () => {
        try {
            const gd = await fetchJSON(`${API}/graph`);
            const prevNodeCount = (graphData.nodes || []).length;
            const prevEdgeCount = (graphData.edges || []).length;
            if (gd.nodes?.length !== prevNodeCount || gd.edges?.length !== prevEdgeCount) {
                graphData = gd;
                const groupBy = document.getElementById('group-by').value;
                const elements = buildElements(graphData, groupBy);
                cy.json({ elements });
                cy.layout(LAYOUTS[document.getElementById('layout-mode')?.value || 'balanced']).run();
                renderResourcePanel();
                buildEdgeFilterPills();
                applyNodeVisibilityFilters();
            }
        } catch { /* ignore transient failures */ }
    }, intervalMs);
}

function stopAutoRefresh() {
    if (autoRefreshTimer) {
        clearInterval(autoRefreshTimer);
        autoRefreshTimer = null;
    }
}

// Expose for optional use via browser console: startAutoRefresh(30000)
window.startAutoRefresh = startAutoRefresh;
window.stopAutoRefresh = stopAutoRefresh;
