package kubernetes

import (
	"bytes"
	"fmt"
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
	Data       interface{} `yaml:"data"`
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
}

type k8sContainer struct {
	Name    string       `yaml:"name"`
	Image   string       `yaml:"image"`
	Ports   []k8sPort    `yaml:"ports"`
	EnvFrom []k8sEnvFrom `yaml:"envFrom"`
	Env     []k8sEnv     `yaml:"env"`
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
	ValueFrom *k8sValueFrom `yaml:"valueFrom"`
}

type k8sValueFrom struct {
	SecretKeyRef    *k8sKeyRef `yaml:"secretKeyRef"`
	ConfigMapKeyRef *k8sKeyRef `yaml:"configMapKeyRef"`
}

type k8sKeyRef struct {
	Name string `yaml:"name"`
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
		resources = append(resources, res)
	}

	// First pass: create all nodes so we can resolve references.
	nodeMap := make(map[string]models.Node)    // nodeID → node
	workloadLabels := make(map[string]map[string]string) // nodeID → pod template labels

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
				Type:       models.AssetSecret, // ConfigMaps are config, closest type
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "kubernetes",
				Metadata:   meta,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
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

			node := models.Node{
				ID:         nodeID,
				Name:       res.Metadata.Name,
				Type:       models.AssetCertificate,
				Source:     "kubernetes",
				SourceFile: sourceFile,
				Provider:   "cert-manager",
				Metadata:   meta,
				LastSeen:   now,
				FirstSeen:  now,
			}
			nodeMap[nodeID] = node
			result.Nodes = append(result.Nodes, node)

		default:
			result.Warnings = append(result.Warnings, fmt.Sprintf("skipping unsupported kind: %s/%s", res.Kind, res.Metadata.Name))
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
			}

		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
			wlID := k8sNodeID("pod", ns, res.Metadata.Name)
			seen := make(map[string]bool)

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
					}
				}
				for _, env := range c.Env {
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
					}
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
		}
	}

	return result, nil
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
