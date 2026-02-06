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
};

let cy;

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
    const [graphData, stats] = await Promise.all([
        fetchJSON(`${API}/graph`),
        fetchJSON(`${API}/stats`),
    ]);

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
    const elements = [];
    (graphData.nodes || []).forEach(n => {
        elements.push({
            data: {
                id: n.id, label: n.name, type: n.type, source: n.source,
                provider: n.provider, expires_at: n.expires_at,
            },
        });
    });
    (graphData.edges || []).forEach(e => {
        elements.push({
            data: { id: e.id, source: e.from_id, target: e.to_id, label: e.type },
        });
    });

    cy = cytoscape({
        container: document.getElementById('cy'),
        elements,
        style: [
            {
                selector: 'node',
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

    // Node click → show detail + impact
    cy.on('tap', 'node', async (evt) => {
        const node = evt.target;
        const id = node.data('id');
        showDetail(id, graphData);
    });

    // Search
    document.getElementById('search').addEventListener('input', (e) => {
        const q = e.target.value.toLowerCase();
        cy.nodes().forEach(n => {
            const match = !q || n.data('label').toLowerCase().includes(q) || n.data('id').toLowerCase().includes(q);
            n.toggleClass('dimmed', !match);
        });
    });

    // Filters
    const applyFilters = () => {
        const type = document.getElementById('filter-type').value;
        const source = document.getElementById('filter-source').value;
        cy.nodes().forEach(n => {
            const matchType = !type || n.data('type') === type;
            const matchSource = !source || n.data('source') === source;
            n.toggleClass('dimmed', !(matchType && matchSource));
        });
    };
    typeSelect.addEventListener('change', applyFilters);
    sourceSelect.addEventListener('change', applyFilters);

    // Reset
    document.getElementById('btn-reset').addEventListener('click', () => {
        cy.nodes().removeClass('dimmed highlighted');
        cy.edges().removeClass('dimmed');
        document.getElementById('search').value = '';
        typeSelect.value = '';
        sourceSelect.value = '';
        document.getElementById('detail-panel').classList.add('hidden');
        cy.fit();
    });

    // Close panel
    document.getElementById('close-panel').addEventListener('click', () => {
        document.getElementById('detail-panel').classList.add('hidden');
        cy.nodes().removeClass('highlighted dimmed');
    });

    // Legend toggle
    document.getElementById('btn-legend').addEventListener('click', () => {
        document.getElementById('legend').classList.toggle('hidden');
    });

    // Scan button
    document.getElementById('btn-scan').addEventListener('click', triggerScan);
    checkScanRunning();
}

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

async function showDetail(nodeId, graphData) {
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
        cy.nodes().forEach(n => {
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
