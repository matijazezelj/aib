const API = '/api/v1';

const TYPE_COLORS = {
    vm: '#AED6F1', node: '#AED6F1',
    pod: '#A3E4D7', container: '#A3E4D7',
    service: '#F9E79F',
    ingress: '#F5CBA7', load_balancer: '#F5CBA7',
    database: '#D7BDE2',
    certificate: '#F1948A',
    secret: '#E74C3C',
    network: '#85C1E9', subnet: '#85C1E9',
    dns_record: '#82E0AA',
    firewall_rule: '#F0B27A',
    bucket: '#AED6F1',
    ip_address: '#85C1E9',
    namespace: '#D5D8DC',
    queue: '#BB8FCE', pubsub: '#BB8FCE',
};

let cy;

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
                    'background-color': el => TYPE_COLORS[el.data('type')] || '#D5D8DC',
                    'color': '#c9d1d9',
                    'font-size': '10px',
                    'text-valign': 'bottom',
                    'text-margin-y': 4,
                    'width': 30, 'height': 30,
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
        layout: { name: 'cose', animate: false, nodeRepulsion: () => 8000, idealEdgeLength: () => 120 },
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
}

async function showDetail(nodeId, graphData) {
    const panel = document.getElementById('detail-panel');
    panel.classList.remove('hidden');

    const nodeData = (graphData.nodes || []).find(n => n.id === nodeId);
    if (!nodeData) return;

    document.getElementById('detail-name').textContent = `${nodeData.name} (${nodeData.type})`;

    let html = '<table class="meta-table">';
    html += `<tr><td>ID</td><td>${nodeData.id}</td></tr>`;
    html += `<tr><td>Type</td><td>${nodeData.type}</td></tr>`;
    html += `<tr><td>Source</td><td>${nodeData.source}</td></tr>`;
    html += `<tr><td>Provider</td><td>${nodeData.provider || '-'}</td></tr>`;
    html += `<tr><td>Source File</td><td>${nodeData.source_file || '-'}</td></tr>`;
    if (nodeData.expires_at) {
        const days = Math.ceil((new Date(nodeData.expires_at) - new Date()) / 86400000);
        const cls = days <= 0 ? 'expired' : days <= 7 ? 'critical' : days <= 30 ? 'warning' : 'ok';
        html += `<tr><td>Expires</td><td>${nodeData.expires_at} <span class="badge badge-${cls}">${days}d</span></td></tr>`;
    }
    if (nodeData.metadata) {
        for (const [k, v] of Object.entries(nodeData.metadata)) {
            html += `<tr><td>${k}</td><td>${v}</td></tr>`;
        }
    }
    html += '</table>';
    document.getElementById('detail-content').innerHTML = html;

    // Fetch impact
    try {
        const impact = await fetchJSON(`${API}/impact/${encodeURIComponent(nodeId)}`);
        let impactHtml = `<p>${impact.affected_nodes || 0} affected nodes</p>`;
        if (impact.impact_tree) {
            for (const [id, info] of Object.entries(impact.impact_tree)) {
                impactHtml += `<div class="impact-node"><span class="edge-type">[${info.edge_type}]</span> ${id}</div>`;
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
