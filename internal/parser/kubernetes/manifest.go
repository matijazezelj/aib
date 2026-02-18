package kubernetes

import (
	"bytes"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"encoding/pem"
	"strings"
	"time"

	"github.com/matijazezelj/aib/internal/parser"
	"github.com/matijazezelj/aib/pkg/models"
	"go.yaml.in/yaml/v3"
)

// k8sResource is a lightweight representation of a Kubernetes resource,
// parsed without importing the full k8s API types.
type k8sResource struct {
	APIVersion string      `yaml:"apiVersion"`
	Kind       string      `yaml:"kind"`
	Metadata   k8sMeta     `yaml:"metadata"`
	Spec       k8sSpec     `yaml:"spec"`
	Type       string      `yaml:"type"`
	Data       map[string]string `yaml:"data"`
	Status     k8sStatus   `yaml:"status"`

	// RBAC: RoleBinding/ClusterRoleBinding top-level fields
	RoleRef  *k8sRoleRef  `yaml:"roleRef"`
	Subjects []k8sSubject `yaml:"subjects"`
}

type k8sStatus struct {
	NotAfter string `yaml:"notAfter"`
}

type k8sMeta struct {
	Name        string            `yaml:"name"`
	Namespace   string            `yaml:"namespace"`
	Labels      map[string]string `yaml:"labels"`
	Annotations map[string]string `yaml:"annotations"`
}

type k8sSpec struct {
	// Service selector (flat map) or Deployment selector (matchLabels)
	Selector k8sSelector `yaml:"selector"`
	Type     string      `yaml:"type"`
	Ports    []k8sServicePort `yaml:"ports"`

	// Ingress
	Rules        []k8sIngressRule `yaml:"rules"`
	TLS          []k8sIngressTLS  `yaml:"tls"`
	IngressClass string           `yaml:"ingressClassName"`

	// Workload (Deployment/StatefulSet/DaemonSet)
	Replicas *int       `yaml:"replicas"`
	Template k8sPodSpec `yaml:"template"`

	// cert-manager Certificate
	SecretName string   `yaml:"secretName"`
	DNSNames   []string `yaml:"dnsNames"`

	// Job / CronJob
	Schedule    string      `yaml:"schedule"`
	JobTemplate k8sJobTmpl  `yaml:"jobTemplate"`

	// HPA
	ScaleTargetRef *k8sScaleTargetRef `yaml:"scaleTargetRef"`
	MinReplicas    *int               `yaml:"minReplicas"`
	MaxReplicas    int                `yaml:"maxReplicas"`

	// NetworkPolicy
	PodSelector *k8sPodSelector  `yaml:"podSelector"`
	PolicyTypes []string         `yaml:"policyTypes"`
}

// k8sSelector handles both Service selector (flat map) and Deployment selector ({matchLabels}).
type k8sSelector struct {
	MatchLabels map[string]string `yaml:"matchLabels"`
	Labels      map[string]string // flat selector for Services
}

func (s *k8sSelector) UnmarshalYAML(value *yaml.Node) error {
	// Try as flat map first (Service style: selector: {app: foo})
	var flat map[string]string
	if err := value.Decode(&flat); err == nil {
		s.Labels = flat
		return nil
	}
	// Try as structured selector (Deployment style: selector: {matchLabels: {app: foo}})
	type structured struct {
		MatchLabels map[string]string `yaml:"matchLabels"`
	}
	var st structured
	if err := value.Decode(&st); err == nil {
		s.MatchLabels = st.MatchLabels
		return nil
	}
	return nil // silently ignore unparseable selectors
}

// GetLabels returns the effective label map regardless of format.
func (s k8sSelector) GetLabels() map[string]string {
	if len(s.Labels) > 0 {
		return s.Labels
	}
	return s.MatchLabels
}

type k8sServicePort struct {
	Name       string `yaml:"name"`
	Port       int    `yaml:"port"`
	TargetPort interface{} `yaml:"targetPort"`
	Protocol   string `yaml:"protocol"`
}

type k8sIngressRule struct {
	Host string        `yaml:"host"`
	HTTP *k8sHTTPRule  `yaml:"http"`
}

type k8sHTTPRule struct {
	Paths []k8sHTTPPath `yaml:"paths"`
}

type k8sHTTPPath struct {
	Path    string         `yaml:"path"`
	Backend k8sBackend     `yaml:"backend"`
}

type k8sBackend struct {
	Service *k8sBackendSvc `yaml:"service"`
	// legacy format
	ServiceName string `yaml:"serviceName"`
}

type k8sBackendSvc struct {
	Name string `yaml:"name"`
}

type k8sIngressTLS struct {
	Hosts      []string `yaml:"hosts"`
	SecretName string   `yaml:"secretName"`
}

type k8sPodSpec struct {
	Metadata k8sMeta         `yaml:"metadata"`
	Spec     k8sContainerSpec `yaml:"spec"`
}

type k8sContainerSpec struct {
	Containers     []k8sContainer `yaml:"containers"`
	InitContainers []k8sContainer `yaml:"initContainers"`
	Volumes        []k8sVolume    `yaml:"volumes"`
	HostNetwork    bool           `yaml:"hostNetwork"`
	HostPID        bool           `yaml:"hostPID"`
	HostIPC        bool           `yaml:"hostIPC"`
	ServiceAccountName string     `yaml:"serviceAccountName"`
}

type k8sSecurityContext struct {
	Privileged               *bool `yaml:"privileged"`
	RunAsNonRoot             *bool `yaml:"runAsNonRoot"`
	ReadOnlyRootFilesystem   *bool `yaml:"readOnlyRootFilesystem"`
	AllowPrivilegeEscalation *bool `yaml:"allowPrivilegeEscalation"`
	RunAsUser                *int  `yaml:"runAsUser"`
}

type k8sContainer struct {
	Name            string              `yaml:"name"`
	Image           string              `yaml:"image"`
	Ports           []k8sPort           `yaml:"ports"`
	EnvFrom         []k8sEnvFrom        `yaml:"envFrom"`
	Env             []k8sEnv            `yaml:"env"`
	SecurityContext *k8sSecurityContext  `yaml:"securityContext"`
}

type k8sPort struct {
	ContainerPort int    `yaml:"containerPort"`
	Protocol      string `yaml:"protocol"`
}

type k8sEnvFrom struct {
	SecretRef    *k8sRef `yaml:"secretRef"`
	ConfigMapRef *k8sRef `yaml:"configMapRef"`
}

type k8sEnv struct {
	Name      string       `yaml:"name"`
	Value     string       `yaml:"value"`
	ValueFrom *k8sValueFrom `yaml:"valueFrom"`
}

type k8sValueFrom struct {
	SecretKeyRef    *k8sKeyRef `yaml:"secretKeyRef"`
	ConfigMapKeyRef *k8sKeyRef `yaml:"configMapKeyRef"`
}

type k8sKeyRef struct {
	Name string `yaml:"name"`
	Key  string `yaml:"key"`
}

type k8sRef struct {
	Name string `yaml:"name"`
}

type k8sVolume struct {
	Name      string          `yaml:"name"`
	Secret    *k8sVolSecret   `yaml:"secret"`
	ConfigMap *k8sVolConfigMap `yaml:"configMap"`
}

type k8sVolSecret struct {
	SecretName string `yaml:"secretName"`
}

type k8sVolConfigMap struct {
	Name string `yaml:"name"`
}

// RBAC types
type k8sRoleRef struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

type k8sSubject struct {
	Kind      string `yaml:"kind"`
	Name      string `yaml:"name"`
	Namespace string `yaml:"namespace"`
}

// NetworkPolicy pod selector
type k8sPodSelector struct {
	MatchLabels map[string]string `yaml:"matchLabels"`
}

// HPA scale target
type k8sScaleTargetRef struct {
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

// Job template (for CronJob)
type k8sJobTmpl struct {
	Spec k8sJobTmplSpec `yaml:"spec"`
}

type k8sJobTmplSpec struct {
	Template k8sPodSpec `yaml:"template"`
}

// k8sList wraps a Kubernetes List response (from kubectl get -o yaml).
type k8sList struct {
	Kind  string        `yaml:"kind"`
	Items []k8sResource `yaml:"items"`
}

// parseManifests parses multi-document YAML into nodes and edges.
func parseManifests(data []byte, sourceFile string, now time.Time) (*parser.ParseResult, error) {
	result := &parser.ParseResult{}

	// Split on document separator and unmarshal each document individually.
	// Using yaml.NewDecoder on a stream can hang on malformed flow mappings,
	// so we isolate each document instead.
	var resources []k8sResource
	docs := splitYAMLDocuments(data)
	for _, doc := range docs {
		doc = bytes.TrimSpace(doc)
		if len(doc) == 0 {
			continue
		}
		var res k8sResource
		if err := yaml.Unmarshal(doc, &res); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipping invalid YAML document in %s: %v", sourceFile, err))
			continue
		}
		if res.Kind == "" {
			continue
		}
		// Handle kubectl "List" wrapper (e.g. from kubectl get -o yaml)
		if res.Kind == "List" || res.Kind == "DeploymentList" || res.Kind == "ServiceList" ||
			strings.HasSuffix(res.Kind, "List") {
			var list k8sList
			if err := yaml.Unmarshal(doc, &list); err == nil {
				resources = append(resources, list.Items...)
				continue
			}
		}
		resources = append(resources, res)
	}

	// First pass: create all nodes so we can resolve references.
	nodeMap := make(map[string]models.Node)    // nodeID → node
	workloadLabels := make(map[string]map[string]string) // nodeID → pod template labels
	serviceIDs := make(map[string]bool)
	configMapData := make(map[string]map[string]string)

	for _, res := range resources {
		ns := res.Metadata.Namespace
		if ns == "" {
			ns = "default"
		}

		switch res.Kind {
		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
			nodeID := k8sNodeID("pod", ns, res.Metadata.Name)
			meta := map[string]string{
				"kind":      res.Kind,
				"namespace": ns,
			}
			if res.Spec.Replicas != nil {
				meta["replicas"] = fmt.Sprintf("%d", *res.Spec.Replicas)
			}
			// Collect container images
			var images []string
			for _, c := range res.Spec.Template.Spec.Containers {
				if c.Image != "" {
					images = append(images, c.Image)
				}
			}
			if len(images) > 0 {
				meta["images"] = strings.Join(images, ",")
			}
			for k, v := range res.Metadata.Labels {
				meta["label:"+k] = v
			}

			// Security context extraction
			podSpec := res.Spec.Template.Spec
			if podSpec.HostNetwork {
				meta["security.host_network"] = "true"
			}
			if podSpec.HostPID {
				meta["security.host_pid"] = "true"
			}
			if podSpec.HostIPC {
				meta["security.host_ipc"] = "true"
			}
			if podSpec.ServiceAccountName != "" {
				meta["service_account"] = podSpec.ServiceAccountName
			}
			allContainers := append(podSpec.Containers, podSpec.InitContainers...)
			for _, c := range allContainers {
				if c.SecurityContext != nil {
					prefix := "security." + c.Name + "."
					sc := c.SecurityContext
					if sc.Privileged != nil && *sc.Privileged {
						meta[prefix+"privileged"] = "true"
					}
					if sc.RunAsNonRoot != nil {
						meta[prefix+"run_as_non_root"] = fmt.Sprintf("%t", *sc.RunAsNonRoot)
					}
					if sc.ReadOnlyRootFilesystem != nil {
						meta[prefix+"read_only_root_fs"] = fmt.Sprintf("%t", *sc.ReadOnlyRootFilesystem)
					}
					if sc.AllowPrivilegeEscalation != nil {
						meta[prefix+"allow_privilege_escalation"] = fmt.Sprintf("%t", *sc.AllowPrivilegeEscalation)
					}
					if sc.RunAsUser != nil {
						meta[prefix+"run_as_user"] = fmt.Sprintf("%d", *sc.RunAsUser)
					}
				}
			}

			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetPod,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "kubernetes",
				Metadata:   meta,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

			// Store pod template labels for service selector matching
			if res.Spec.Template.Metadata.Labels != nil {
				workloadLabels[nodeID] = res.Spec.Template.Metadata.Labels
			}

		case "Service":
			nodeID := k8sNodeID("service", ns, res.Metadata.Name)
			meta := map[string]string{
				"namespace":    ns,
				"service_type": res.Spec.Type,
			}
			var ports []string
			for _, p := range res.Spec.Ports {
				ports = append(ports, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
			}
			if len(ports) > 0 {
				meta["ports"] = strings.Join(ports, ",")
			}
			for k, v := range res.Metadata.Labels {
				meta["label:"+k] = v
			}

			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetService,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "kubernetes",
				Metadata:   meta,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			serviceIDs[nodeID] = true
			result.Nodes = append(result.Nodes, node)

		case "Ingress":
			nodeID := k8sNodeID("ingress", ns, res.Metadata.Name)
			meta := map[string]string{
				"namespace": ns,
			}
			var hosts []string
			for _, rule := range res.Spec.Rules {
				if rule.Host != "" {
					hosts = append(hosts, rule.Host)
				}
			}
			if len(hosts) > 0 {
				meta["hosts"] = strings.Join(hosts, ",")
			}
			if res.Spec.IngressClass != "" {
				meta["ingress_class"] = res.Spec.IngressClass
			}
			for k, v := range res.Metadata.Labels {
				meta["label:"+k] = v
			}

			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetIngress,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "kubernetes",
				Metadata:   meta,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "Secret":
			nodeID := k8sNodeID("secret", ns, res.Metadata.Name)
			meta := map[string]string{
				"namespace": ns,
			}
			if strings.TrimSpace(res.Type) != "" {
				meta["type"] = strings.TrimSpace(res.Type)
			}
			for k, v := range res.Metadata.Labels {
				meta["label:"+k] = v
			}

			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetSecret,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "kubernetes",
				Metadata:   meta,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

			// For Kubernetes TLS secrets, derive a certificate node so certs are
			// visible in the graph even without cert-manager Certificate CRDs.
			if strings.EqualFold(strings.TrimSpace(res.Type), "kubernetes.io/tls") {
				expiresAt, _ := extractTLSSecretExpiry(res.Data)
				certID := k8sNodeID("certificate", ns, res.Metadata.Name)
				if _, exists := nodeMap[certID]; !exists {
					certNode := models.Node{
						ID:         certID,
						Name:       res.Metadata.Name,
						Type:       models.AssetCertificate,
						Source:     "kubernetes",
						SourceFile: sourceFile,
						Provider:   "kubernetes",
						Metadata: map[string]string{
							"namespace":            ns,
							"derived_from_secret":  "true",
							"secret_name":          res.Metadata.Name,
						},
						ExpiresAt:  expiresAt,
						LastSeen:   now,
						FirstSeen:  now,
					}
					if expiresAt != nil {
						certNode.Metadata["not_after"] = expiresAt.UTC().Format(time.RFC3339)
					}
					nodeMap[certID] = certNode
					result.Nodes = append(result.Nodes, certNode)
				}
				edgeID := fmt.Sprintf("%s->depends_on->%s", nodeID, certID)
				result.Edges = append(result.Edges, models.Edge{
					ID:       edgeID,
					FromID:   nodeID,
					ToID:     certID,
					Type:     models.EdgeDependsOn,
					Metadata: map[string]string{"via": "tls_secret"},
				})
			}

		case "ConfigMap":
			nodeID := k8sNodeID("configmap", ns, res.Metadata.Name)
			meta := map[string]string{
				"namespace": ns,
			}
			for k, v := range res.Metadata.Labels {
				meta["label:"+k] = v
			}

			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetConfigMap,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "kubernetes",
				Metadata:   meta,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			if len(res.Data) > 0 {
				configMapData[nodeID] = res.Data
			}
			result.Nodes = append(result.Nodes, node)

		case "Namespace":
			nodeID := fmt.Sprintf("k8s:namespace:%s", res.Metadata.Name) // namespaces are not namespace-scoped
			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetNamespace,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "kubernetes",
				Metadata:   map[string]string{},
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "Certificate":
			// cert-manager Certificate CRD
			nodeID := k8sNodeID("certificate", ns, res.Metadata.Name)
			meta := map[string]string{
				"namespace": ns,
			}
			if len(res.Spec.DNSNames) > 0 {
				meta["dns_names"] = strings.Join(res.Spec.DNSNames, ",")
			}
			if res.Spec.SecretName != "" {
				meta["secret_name"] = res.Spec.SecretName
			}
			for k, v := range res.Metadata.Labels {
				meta["label:"+k] = v
			}

			var expiresAt *time.Time
			if strings.TrimSpace(res.Status.NotAfter) != "" {
				if ts, err := time.Parse(time.RFC3339, strings.TrimSpace(res.Status.NotAfter)); err == nil {
					expiresAt = &ts
					meta["not_after"] = ts.UTC().Format(time.RFC3339)
				}
			}

			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetCertificate,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "cert-manager",
				Metadata:   meta,
				ExpiresAt:  expiresAt,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "ServiceAccount":
			nodeID := k8sNodeID("serviceaccount", ns, res.Metadata.Name)
			meta := map[string]string{"namespace": ns}
			for k, v := range res.Metadata.Labels {
				meta["label:"+k] = v
			}
			node := models.Node{
				ID: nodeID, Name: res.Metadata.Name, Type: models.AssetServiceAccount,
				Source: "kubernetes", SourceFile: sourceFile, Provider: "kubernetes",
				Metadata: meta, LastSeen: now, FirstSeen: now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "Role", "ClusterRole":
			nodeID := k8sNodeID("role", ns, res.Metadata.Name)
			if res.Kind == "ClusterRole" {
				nodeID = fmt.Sprintf("k8s:clusterrole:%s", res.Metadata.Name)
			}
			meta := map[string]string{"kind": res.Kind, "namespace": ns}
			node := models.Node{
				ID: nodeID, Name: res.Metadata.Name, Type: models.AssetIAMPolicy,
				Source: "kubernetes", SourceFile: sourceFile, Provider: "kubernetes",
				Metadata: meta, LastSeen: now, FirstSeen: now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "RoleBinding", "ClusterRoleBinding":
			nodeID := k8sNodeID("rolebinding", ns, res.Metadata.Name)
			if res.Kind == "ClusterRoleBinding" {
				nodeID = fmt.Sprintf("k8s:clusterrolebinding:%s", res.Metadata.Name)
			}
			meta := map[string]string{"kind": res.Kind, "namespace": ns}
			node := models.Node{
				ID: nodeID, Name: res.Metadata.Name, Type: models.AssetIAMBinding,
				Source: "kubernetes", SourceFile: sourceFile, Provider: "kubernetes",
				Metadata: meta, LastSeen: now, FirstSeen: now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "NetworkPolicy":
			nodeID := k8sNodeID("networkpolicy", ns, res.Metadata.Name)
			meta := map[string]string{"namespace": ns}
			if len(res.Spec.PolicyTypes) > 0 {
				meta["policy_types"] = strings.Join(res.Spec.PolicyTypes, ",")
			}
			node := models.Node{
				ID: nodeID, Name: res.Metadata.Name, Type: models.AssetFirewallRule,
				Source: "kubernetes", SourceFile: sourceFile, Provider: "kubernetes",
				Metadata: meta, LastSeen: now, FirstSeen: now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "Job":
			nodeID := k8sNodeID("job", ns, res.Metadata.Name)
			meta := map[string]string{"kind": "Job", "namespace": ns}
			node := models.Node{
				ID: nodeID, Name: res.Metadata.Name, Type: models.AssetPod,
				Source: "kubernetes", SourceFile: sourceFile, Provider: "kubernetes",
				Metadata: meta, LastSeen: now, FirstSeen: now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "CronJob":
			nodeID := k8sNodeID("cronjob", ns, res.Metadata.Name)
			meta := map[string]string{"kind": "CronJob", "namespace": ns}
			if res.Spec.Schedule != "" {
				meta["schedule"] = res.Spec.Schedule
			}
			node := models.Node{
				ID: nodeID, Name: res.Metadata.Name, Type: models.AssetPod,
				Source: "kubernetes", SourceFile: sourceFile, Provider: "kubernetes",
				Metadata: meta, LastSeen: now, FirstSeen: now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		case "HorizontalPodAutoscaler":
			nodeID := k8sNodeID("hpa", ns, res.Metadata.Name)
			meta := map[string]string{"namespace": ns}
			if res.Spec.MinReplicas != nil {
				meta["min_replicas"] = fmt.Sprintf("%d", *res.Spec.MinReplicas)
			}
			if res.Spec.MaxReplicas > 0 {
				meta["max_replicas"] = fmt.Sprintf("%d", res.Spec.MaxReplicas)
			}
			node := models.Node{
				ID: nodeID, Name: res.Metadata.Name, Type: models.AssetMonitor,
				Source: "kubernetes", SourceFile: sourceFile, Provider: "kubernetes",
				Metadata: meta, LastSeen: now, FirstSeen: now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		default:
			// Only warn for non-well-known Kubernetes kinds to reduce noise.
			wellKnown := map[string]bool{
				"Endpoints": true, "EndpointSlice": true, "Event": true,
				"LimitRange": true, "ResourceQuota": true, "PodDisruptionBudget": true,
				"StorageClass": true, "CSIDriver": true, "CSINode": true,
				"VolumeAttachment": true, "PriorityClass": true,
				"MutatingWebhookConfiguration": true, "ValidatingWebhookConfiguration": true,
				"CustomResourceDefinition": true, "APIService": true,
				"List": true, "ComponentStatus": true, "Node": true,
			}
			if !wellKnown[res.Kind] {
				result.Warnings = append(result.Warnings, fmt.Sprintf("skipping unsupported kind: %s/%s", res.Kind, res.Metadata.Name))
			}
		}
	}

	// Second pass: create edges.
	for _, res := range resources {
		ns := res.Metadata.Namespace
		if ns == "" {
			ns = "default"
		}

		switch res.Kind {
		case "Service":
			svcID := k8sNodeID("service", ns, res.Metadata.Name)
			// Match service selector to workload pod template labels
			svcSelector := res.Spec.Selector.GetLabels()
			if len(svcSelector) > 0 {
				for wlID, labels := range workloadLabels {
					if labelsMatch(svcSelector, labels) {
						edgeID := fmt.Sprintf("%s->member_of->%s", wlID, svcID)
						result.Edges = append(result.Edges, models.Edge{
							ID:       edgeID,
							FromID:   wlID,
							ToID:     svcID,
							Type:     models.EdgeMemberOf,
							Metadata: map[string]string{"via": "label_selector"},
						})
					}
				}
			}

		case "Ingress":
			ingressID := k8sNodeID("ingress", ns, res.Metadata.Name)

			// Ingress → Service via backend rules
			for _, rule := range res.Spec.Rules {
				if rule.HTTP == nil {
					continue
				}
				for _, path := range rule.HTTP.Paths {
					svcName := ""
					if path.Backend.Service != nil {
						svcName = path.Backend.Service.Name
					} else if path.Backend.ServiceName != "" {
						svcName = path.Backend.ServiceName
					}
					if svcName == "" {
						continue
					}
					svcID := k8sNodeID("service", ns, svcName)
					edgeID := fmt.Sprintf("%s->routes_to->%s", ingressID, svcID)
					result.Edges = append(result.Edges, models.Edge{
						ID:       edgeID,
						FromID:   ingressID,
						ToID:     svcID,
						Type:     models.EdgeRoutesTo,
						Metadata: map[string]string{"host": rule.Host, "path": path.Path},
					})
				}
			}

			// Ingress → TLS Secret
			for _, tls := range res.Spec.TLS {
				if tls.SecretName == "" {
					continue
				}
				secretID := k8sNodeID("secret", ns, tls.SecretName)
				edgeID := fmt.Sprintf("%s->terminates_tls->%s", ingressID, secretID)
				result.Edges = append(result.Edges, models.Edge{
					ID:       edgeID,
					FromID:   ingressID,
					ToID:     secretID,
					Type:     models.EdgeTerminatesTLS,
					Metadata: map[string]string{"hosts": strings.Join(tls.Hosts, ",")},
				})
				// Auto-create the TLS secret node if not already present
				if _, exists := nodeMap[secretID]; !exists {
					node := models.Node{
						ID:         secretID,
						Name:       tls.SecretName,
						Type:       models.AssetSecret,
						Source:     "kubernetes",
						SourceFile: sourceFile,
						Provider:   "kubernetes",
						Metadata:   map[string]string{"type": "kubernetes.io/tls"},
						LastSeen:   now,
						FirstSeen:  now,
					}
					nodeMap[secretID] = node
					result.Nodes = append(result.Nodes, node)
				}

				// Also expose the TLS cert as a first-class certificate node.
				certID := k8sNodeID("certificate", ns, tls.SecretName)
				if _, exists := nodeMap[certID]; !exists {
					certMeta := map[string]string{
						"namespace":           ns,
						"derived_from_secret": "true",
						"secret_name":         tls.SecretName,
					}
					if len(tls.Hosts) > 0 {
						certMeta["dns_names"] = strings.Join(tls.Hosts, ",")
					}
					certNode := models.Node{
						ID:         certID,
						Name:       tls.SecretName,
						Type:       models.AssetCertificate,
						Source:     "kubernetes",
						SourceFile: sourceFile,
						Provider:   "kubernetes",
						Metadata:   certMeta,
						LastSeen:   now,
						FirstSeen:  now,
					}
					nodeMap[certID] = certNode
					result.Nodes = append(result.Nodes, certNode)
				}

				secretToCertEdge := fmt.Sprintf("%s->depends_on->%s", secretID, certID)
				result.Edges = append(result.Edges, models.Edge{
					ID:       secretToCertEdge,
					FromID:   secretID,
					ToID:     certID,
					Type:     models.EdgeDependsOn,
					Metadata: map[string]string{"via": "tls_secret"},
				})

				ingressToCertEdge := fmt.Sprintf("%s->terminates_tls->%s", ingressID, certID)
				result.Edges = append(result.Edges, models.Edge{
					ID:     ingressToCertEdge,
					FromID: ingressID,
					ToID:   certID,
					Type:   models.EdgeTerminatesTLS,
					Metadata: map[string]string{
						"hosts": strings.Join(tls.Hosts, ","),
					},
				})
			}

		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
			wlID := k8sNodeID("pod", ns, res.Metadata.Name)
			seen := make(map[string]bool)
			connectivityValues := make(map[string]string)

			// Volume mounts → Secrets and ConfigMaps
			for _, vol := range res.Spec.Template.Spec.Volumes {
				if vol.Secret != nil && vol.Secret.SecretName != "" {
					secretID := k8sNodeID("secret", ns, vol.Secret.SecretName)
					eid := fmt.Sprintf("%s->mounts_secret->%s", wlID, secretID)
					if !seen[eid] {
						seen[eid] = true
						result.Edges = append(result.Edges, models.Edge{
							ID:       eid,
							FromID:   wlID,
							ToID:     secretID,
							Type:     models.EdgeMountsSecret,
							Metadata: map[string]string{"via": "volume"},
						})
					}
				ensureNode(nodeMap, result, secretID, vol.Secret.SecretName, models.AssetSecret, ns, sourceFile, now)
				}
				if vol.ConfigMap != nil && vol.ConfigMap.Name != "" {
					cmID := k8sNodeID("configmap", ns, vol.ConfigMap.Name)
					eid := fmt.Sprintf("%s->depends_on->%s", wlID, cmID)
					if !seen[eid] {
						seen[eid] = true
						result.Edges = append(result.Edges, models.Edge{
							ID:       eid,
							FromID:   wlID,
							ToID:     cmID,
							Type:     models.EdgeDependsOn,
							Metadata: map[string]string{"via": "volume"},
						})
					}
				ensureNode(nodeMap, result, cmID, vol.ConfigMap.Name, models.AssetConfigMap, ns, sourceFile, now)
				}
			}

			// envFrom and env valueFrom references
			allContainers := append(res.Spec.Template.Spec.Containers, res.Spec.Template.Spec.InitContainers...)
			for _, c := range allContainers {
				for _, ef := range c.EnvFrom {
					if ef.SecretRef != nil && ef.SecretRef.Name != "" {
						secretID := k8sNodeID("secret", ns, ef.SecretRef.Name)
						eid := fmt.Sprintf("%s->mounts_secret->%s", wlID, secretID)
						if !seen[eid] {
							seen[eid] = true
							result.Edges = append(result.Edges, models.Edge{
								ID:       eid,
								FromID:   wlID,
								ToID:     secretID,
								Type:     models.EdgeMountsSecret,
								Metadata: map[string]string{"via": "envFrom"},
							})
						}
					ensureNode(nodeMap, result, secretID, ef.SecretRef.Name, models.AssetSecret, ns, sourceFile, now)
					}
					if ef.ConfigMapRef != nil && ef.ConfigMapRef.Name != "" {
						cmID := k8sNodeID("configmap", ns, ef.ConfigMapRef.Name)
						eid := fmt.Sprintf("%s->depends_on->%s", wlID, cmID)
						if !seen[eid] {
							seen[eid] = true
							result.Edges = append(result.Edges, models.Edge{
								ID:       eid,
								FromID:   wlID,
								ToID:     cmID,
								Type:     models.EdgeDependsOn,
								Metadata: map[string]string{"via": "envFrom"},
							})
						}
					ensureNode(nodeMap, result, cmID, ef.ConfigMapRef.Name, models.AssetConfigMap, ns, sourceFile, now)
						for key, value := range configMapData[cmID] {
							connectivityValues["configmap:"+ef.ConfigMapRef.Name+":"+key] = value
						}
					}
				}
				for _, env := range c.Env {
					if strings.TrimSpace(env.Value) != "" {
						connectivityValues["env:"+env.Name] = env.Value
					}
					if env.ValueFrom == nil {
						continue
					}
					if env.ValueFrom.SecretKeyRef != nil && env.ValueFrom.SecretKeyRef.Name != "" {
						secretID := k8sNodeID("secret", ns, env.ValueFrom.SecretKeyRef.Name)
						eid := fmt.Sprintf("%s->mounts_secret->%s", wlID, secretID)
						if !seen[eid] {
							seen[eid] = true
							result.Edges = append(result.Edges, models.Edge{
								ID:       eid,
								FromID:   wlID,
								ToID:     secretID,
								Type:     models.EdgeMountsSecret,
								Metadata: map[string]string{"via": "env"},
							})
						}
					ensureNode(nodeMap, result, secretID, env.ValueFrom.SecretKeyRef.Name, models.AssetSecret, ns, sourceFile, now)
					}
					if env.ValueFrom.ConfigMapKeyRef != nil && env.ValueFrom.ConfigMapKeyRef.Name != "" {
						cmID := k8sNodeID("configmap", ns, env.ValueFrom.ConfigMapKeyRef.Name)
						eid := fmt.Sprintf("%s->depends_on->%s", wlID, cmID)
						if !seen[eid] {
							seen[eid] = true
							result.Edges = append(result.Edges, models.Edge{
								ID:       eid,
								FromID:   wlID,
								ToID:     cmID,
								Type:     models.EdgeDependsOn,
								Metadata: map[string]string{"via": "env"},
							})
						}
					ensureNode(nodeMap, result, cmID, env.ValueFrom.ConfigMapKeyRef.Name, models.AssetConfigMap, ns, sourceFile, now)
						if env.ValueFrom.ConfigMapKeyRef.Key != "" {
							if cmValues, ok := configMapData[cmID]; ok {
								if value, exists := cmValues[env.ValueFrom.ConfigMapKeyRef.Key]; exists {
									connectivityValues["configmap_key:"+env.ValueFrom.ConfigMapKeyRef.Name+":"+env.ValueFrom.ConfigMapKeyRef.Key] = value
								}
							}
						}
					}
				}
			}

			for source, value := range connectivityValues {
				for _, svcID := range inferServiceTargets(value, ns, serviceIDs) {
					eid := fmt.Sprintf("%s->connects_to->%s", wlID, svcID)
					if seen[eid] {
						continue
					}
					seen[eid] = true
					result.Edges = append(result.Edges, models.Edge{
						ID:     eid,
						FromID: wlID,
						ToID:   svcID,
						Type:   models.EdgeConnectsTo,
						Metadata: map[string]string{
							"via":      source,
							"raw_value": value,
						},
					})
				}
			}

		case "Certificate":
			// cert-manager Certificate → Secret it writes to
			if res.Spec.SecretName != "" {
				certID := k8sNodeID("certificate", ns, res.Metadata.Name)
				secretID := k8sNodeID("secret", ns, res.Spec.SecretName)
				edgeID := fmt.Sprintf("%s->depends_on->%s", certID, secretID)
				result.Edges = append(result.Edges, models.Edge{
					ID:       edgeID,
					FromID:   certID,
					ToID:     secretID,
					Type:     models.EdgeDependsOn,
					Metadata: map[string]string{"via": "cert-manager"},
				})
				// Auto-create secret node
				if _, exists := nodeMap[secretID]; !exists {
					node := models.Node{
						ID:         secretID,
						Name:       res.Spec.SecretName,
						Type:       models.AssetSecret,
						Source:     "kubernetes",
						SourceFile: sourceFile,
						Provider:   "cert-manager",
						Metadata:   map[string]string{"type": "kubernetes.io/tls"},
						LastSeen:   now,
						FirstSeen:  now,
					}
					nodeMap[secretID] = node
					result.Nodes = append(result.Nodes, node)
				}
			}

		case "RoleBinding", "ClusterRoleBinding":
			bindingID := k8sNodeID("rolebinding", ns, res.Metadata.Name)
			if res.Kind == "ClusterRoleBinding" {
				bindingID = fmt.Sprintf("k8s:clusterrolebinding:%s", res.Metadata.Name)
			}
			// Binding → Role/ClusterRole
			if res.RoleRef != nil {
				var roleID string
				if res.RoleRef.Kind == "ClusterRole" {
					roleID = fmt.Sprintf("k8s:clusterrole:%s", res.RoleRef.Name)
				} else {
					roleID = k8sNodeID("role", ns, res.RoleRef.Name)
				}
				eid := fmt.Sprintf("%s->depends_on->%s", bindingID, roleID)
				result.Edges = append(result.Edges, models.Edge{
					ID: eid, FromID: bindingID, ToID: roleID,
					Type: models.EdgeDependsOn, Metadata: map[string]string{"via": "roleRef"},
				})
			}
			// Binding → Subjects (ServiceAccounts)
			for _, subj := range res.Subjects {
				if subj.Kind == "ServiceAccount" {
					subjNs := subj.Namespace
					if subjNs == "" {
						subjNs = ns
					}
					saID := k8sNodeID("serviceaccount", subjNs, subj.Name)
					eid := fmt.Sprintf("%s->managed_by->%s", saID, bindingID)
					result.Edges = append(result.Edges, models.Edge{
						ID: eid, FromID: saID, ToID: bindingID,
						Type: models.EdgeManagedBy, Metadata: map[string]string{"via": "subject"},
					})
				}
			}

		case "NetworkPolicy":
			npID := k8sNodeID("networkpolicy", ns, res.Metadata.Name)
			// NetworkPolicy → Pods via podSelector
			if res.Spec.PodSelector != nil && len(res.Spec.PodSelector.MatchLabels) > 0 {
				for wlID, labels := range workloadLabels {
					if labelsMatch(res.Spec.PodSelector.MatchLabels, labels) {
						eid := fmt.Sprintf("%s->managed_by->%s", wlID, npID)
						result.Edges = append(result.Edges, models.Edge{
							ID: eid, FromID: wlID, ToID: npID,
							Type: models.EdgeManagedBy, Metadata: map[string]string{"via": "podSelector"},
						})
					}
				}
			}

		case "HorizontalPodAutoscaler":
			hpaID := k8sNodeID("hpa", ns, res.Metadata.Name)
			if res.Spec.ScaleTargetRef != nil {
				targetKind := strings.ToLower(res.Spec.ScaleTargetRef.Kind)
				var targetID string
				if targetKind == "deployment" || targetKind == "statefulset" || targetKind == "replicaset" {
					targetID = k8sNodeID("pod", ns, res.Spec.ScaleTargetRef.Name)
				}
				if targetID != "" {
					eid := fmt.Sprintf("%s->managed_by->%s", targetID, hpaID)
					result.Edges = append(result.Edges, models.Edge{
						ID: eid, FromID: targetID, ToID: hpaID,
						Type: models.EdgeManagedBy, Metadata: map[string]string{"via": "scaleTargetRef"},
					})
				}
			}
		}
	}

	return result, nil
}

// ensureNode auto-creates a node if it doesn't already exist in nodeMap.
// This prevents FK constraint violations when edges reference secrets or
// configmaps that aren't defined as explicit resources in the manifest.
func ensureNode(nodeMap map[string]models.Node, result *parser.ParseResult, id, name string, assetType models.AssetType, ns, sourceFile string, now time.Time) {
	if _, exists := nodeMap[id]; !exists {
		node := models.Node{
			ID:         id,
			Name:       name,
			Type:       assetType,
			Source:     "kubernetes",
			SourceFile: sourceFile,
			Provider:   "kubernetes",
			Metadata:   map[string]string{"namespace": ns, "auto_created": "true"},
			LastSeen:   now,
			FirstSeen:  now,
		}
		nodeMap[id] = node
		result.Nodes = append(result.Nodes, node)
	}
}

// k8sNodeID builds a namespace-scoped node ID for Kubernetes resources.
func k8sNodeID(kind, namespace, name string) string {
	return fmt.Sprintf("k8s:%s:%s/%s", kind, namespace, name)
}

// splitYAMLDocuments splits multi-document YAML on "---" separators.
func splitYAMLDocuments(data []byte) [][]byte {
	return bytes.Split(data, []byte("\n---"))
}

// labelsMatch returns true if all selector labels are present in the target labels.
func labelsMatch(selector, target map[string]string) bool {
	for k, v := range selector {
		if target[k] != v {
			return false
		}
	}
	return true
}

func inferServiceTargets(value, defaultNamespace string, serviceIDs map[string]bool) []string {
	hosts := extractPotentialHosts(value)
	seen := make(map[string]bool)
	var targets []string

	for _, host := range hosts {
		for _, candidate := range serviceIDCandidatesForHost(host, defaultNamespace) {
			if serviceIDs[candidate] && !seen[candidate] {
				seen[candidate] = true
				targets = append(targets, candidate)
			}
		}
	}

	return targets
}

func extractPotentialHosts(value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	seen := make(map[string]bool)
	var hosts []string
	addHost := func(raw string) {
		raw = strings.TrimSpace(strings.Trim(raw, "\"'()[]{}<>"))
		if raw == "" {
			return
		}
		if strings.Contains(raw, "/") {
			raw = strings.SplitN(raw, "/", 2)[0]
		}
		if strings.Contains(raw, "@") {
			raw = strings.SplitN(raw, "@", 2)[1]
		}
		if strings.HasPrefix(raw, "[") && strings.Contains(raw, "]") {
			h, p, err := net.SplitHostPort(raw)
			if err == nil && h != "" {
				raw = h
			} else if p == "" {
				raw = strings.Trim(raw, "[]")
			}
		} else if strings.Count(raw, ":") == 1 {
			if h, _, err := net.SplitHostPort(raw); err == nil && h != "" {
				raw = h
			} else {
				raw = strings.SplitN(raw, ":", 2)[0]
			}
		}
		raw = strings.Trim(strings.TrimSpace(raw), ".")
		if raw == "" {
			return
		}
		if !seen[raw] {
			seen[raw] = true
			hosts = append(hosts, raw)
		}
	}

	if parsed, err := url.Parse(value); err == nil && parsed.Host != "" {
		if h := parsed.Hostname(); h != "" {
			addHost(h)
		}
	}

	tokens := strings.FieldsFunc(value, func(r rune) bool {
		switch r {
		case ' ', '\t', '\n', '\r', ',', ';':
			return true
		default:
			return false
		}
	})
	for _, token := range tokens {
		if strings.Contains(token, "://") {
			if parsed, err := url.Parse(token); err == nil && parsed.Host != "" {
				if h := parsed.Hostname(); h != "" {
					addHost(h)
				}
			}
			continue
		}
		addHost(token)
	}

	return hosts
}

func serviceIDCandidatesForHost(host, defaultNamespace string) []string {
	host = strings.Trim(strings.ToLower(host), ".")
	if host == "" {
		return nil
	}

	parts := strings.Split(host, ".")
	serviceName := parts[0]
	if serviceName == "" {
		return nil
	}

	candidates := []string{k8sNodeID("service", defaultNamespace, serviceName)}
	if len(parts) > 1 && parts[1] != "svc" && parts[1] != "cluster" && parts[1] != "local" {
		candidates = append(candidates, k8sNodeID("service", parts[1], serviceName))
	}
	return candidates
}

func extractTLSSecretExpiry(data map[string]string) (*time.Time, error) {
	if len(data) == 0 {
		return nil, nil
	}

	rawCRT, ok := data["tls.crt"]
	if !ok || strings.TrimSpace(rawCRT) == "" {
		return nil, nil
	}

	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(rawCRT))
	if err != nil {
		return nil, fmt.Errorf("decode tls.crt: %w", err)
	}

	var cert *x509.Certificate
	if block, _ := pem.Decode(decoded); block != nil {
		cert, err = x509.ParseCertificate(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse pem certificate: %w", err)
		}
	} else {
		cert, err = x509.ParseCertificate(decoded)
		if err != nil {
			return nil, fmt.Errorf("parse der certificate: %w", err)
		}
	}

	if cert == nil {
		return nil, nil
	}

	ts := cert.NotAfter.UTC()
	return &ts, nil
}
