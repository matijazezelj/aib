package compose

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// portList accepts both the short syntax ("8080:80/tcp") and the long syntax
// that `docker compose config` emits:
//
//   - target: 80
//     published: "8080"
//     protocol: tcp
//     host_ip: 127.0.0.1
//
// Every entry is normalised to the short form, so downstream code only ever
// sees strings.
type portList []string

func (p *portList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: ports must be a list", node.Line)
	}
	for _, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			*p = append(*p, item.Value)
		case yaml.MappingNode:
			var lp struct {
				Target    any    `yaml:"target"`
				Published any    `yaml:"published"`
				Protocol  string `yaml:"protocol"`
				HostIP    string `yaml:"host_ip"`
			}
			if err := item.Decode(&lp); err != nil {
				return err
			}
			*p = append(*p, formatLongPort(lp.HostIP, lp.Published, lp.Target, lp.Protocol))
		default:
			return fmt.Errorf("line %d: unsupported ports entry", item.Line)
		}
	}
	return nil
}

func formatLongPort(hostIP string, published, target any, protocol string) string {
	var b strings.Builder
	if hostIP != "" {
		b.WriteString(hostIP + ":")
	}
	if pub := scalarString(published); pub != "" {
		b.WriteString(pub + ":")
	}
	b.WriteString(scalarString(target))
	if protocol != "" && protocol != "tcp" {
		b.WriteString("/" + protocol)
	}
	return b.String()
}

// volumeList accepts the short syntax ("name:/path[:ro]") and the long
// syntax (type/source/target/read_only), normalised to the short form.
type volumeList []string

func (v *volumeList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: volumes must be a list", node.Line)
	}
	for _, item := range node.Content {
		switch item.Kind {
		case yaml.ScalarNode:
			*v = append(*v, item.Value)
		case yaml.MappingNode:
			var lv struct {
				Source   string `yaml:"source"`
				Target   string `yaml:"target"`
				ReadOnly bool   `yaml:"read_only"`
			}
			if err := item.Decode(&lv); err != nil {
				return err
			}
			s := lv.Target
			if lv.Source != "" {
				s = lv.Source + ":" + lv.Target
			}
			if lv.ReadOnly {
				s += ":ro"
			}
			*v = append(*v, s)
		default:
			return fmt.Errorf("line %d: unsupported volumes entry", item.Line)
		}
	}
	return nil
}

// scalarString renders a YAML scalar that may be an int or a string.
func scalarString(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

// includeEntry is one element of a top-level `include:` list. Compose allows a
// bare path or a mapping whose `path` is a string or a list of strings.
type includeEntry struct {
	Paths []string
}

type includeList []includeEntry

func (l *includeList) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return fmt.Errorf("line %d: include must be a list", node.Line)
	}
	for _, item := range node.Content {
		var e includeEntry
		switch item.Kind {
		case yaml.ScalarNode:
			e.Paths = []string{item.Value}
		case yaml.MappingNode:
			var m struct {
				Path yaml.Node `yaml:"path"`
			}
			if err := item.Decode(&m); err != nil {
				return err
			}
			switch m.Path.Kind {
			case yaml.ScalarNode:
				e.Paths = []string{m.Path.Value}
			case yaml.SequenceNode:
				// Only the first file is the base; later ones are overrides of the same project.
				for _, p := range m.Path.Content {
					e.Paths = append(e.Paths, p.Value)
				}
			}
		default:
			return fmt.Errorf("line %d: unsupported include entry", item.Line)
		}
		*l = append(*l, e)
	}
	return nil
}
