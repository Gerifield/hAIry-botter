package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"os"
	"path/filepath"

	"hairy-botter/internal/config"
)

// Node represents a node in the visualization graph.
type Node struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Group   string `json:"group"`   // e.g., "bot", "skill", "mcp"
	Content string `json:"content"` // file content
}

// Edge represents a directed edge in the visualization graph.
type Edge struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Label string `json:"label,omitempty"`
}

// Graph holds the nodes and edges.
type Graph struct {
	Nodes []Node `json:"nodes"`
	Edges []Edge `json:"edges"`
}

var graph Graph
var nodeIDCounter int

func nextID() string {
	nodeIDCounter++
	return fmt.Sprintf("n%d", nodeIDCounter)
}

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	outPath := flag.String("out", "visualization.html", "path to output HTML file")
	flag.Parse()

	log.Printf("Starting visualization tool. Config: %s, Output: %s", *configPath, *outPath)

	err := processConfig(*configPath, "")
	if err != nil {
		log.Fatalf("Error processing config: %v", err)
	}

	err = generateHTML(*outPath)
	if err != nil {
		log.Fatalf("Error generating HTML: %v", err)
	}

	log.Println("Visualization generated successfully.")
}

func processConfig(configPath string, parentID string) error {
	log.Printf("Processing config: %s", configPath)

	// Read raw config for display
	contentBytes, err := os.ReadFile(configPath)
	content := ""
	if err == nil {
		content = string(contentBytes)
	} else {
		content = fmt.Sprintf("Failed to read file: %v", err)
	}

	// Load config struct
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config %s: %w", configPath, err)
	}

	// Create Bot Node
	botID := nextID()
	agentName := cfg.AgentConfig.AgentName
	if agentName == "" {
		agentName = "Agent"
	}
	label := fmt.Sprintf("Bot: %s\n(%s)", agentName, filepath.Base(configPath))

	graph.Nodes = append(graph.Nodes, Node{
		ID:      botID,
		Label:   label,
		Group:   "bot",
		Content: content,
	})

	// Link to parent if it exists
	if parentID != "" {
		graph.Edges = append(graph.Edges, Edge{
			From:  parentID,
			To:    botID,
			Label: "sub-agent",
		})
	}

	// Process Skills (Static Inject)
	injectedFiles := append([]string{}, cfg.Context.StaticInject...)

	for _, skillFile := range injectedFiles {
		skillContentBytes, err := os.ReadFile(skillFile)
		skillContent := ""
		if err == nil {
			skillContent = string(skillContentBytes)
		} else {
			skillContent = fmt.Sprintf("Failed to read file: %v", err)
		}

		skillID := nextID()
		graph.Nodes = append(graph.Nodes, Node{
			ID:      skillID,
			Label:   fmt.Sprintf("Skill (Static): %s", filepath.Base(skillFile)),
			Group:   "skill",
			Content: skillContent,
		})

		graph.Edges = append(graph.Edges, Edge{
			From:  botID,
			To:    skillID,
			Label: "loads",
		})
	}

	// Process Dynamic Data
	for _, dynData := range cfg.Context.DynamicData {
		dynID := nextID()
		graph.Nodes = append(graph.Nodes, Node{
			ID:      dynID,
			Label:   fmt.Sprintf("Dynamic Data:\n%s", dynData.Name),
			Group:   "skill",
			Content: fmt.Sprintf("Name: %s\nCommand: %s\nArgs: %v", dynData.Name, dynData.Command, dynData.Args),
		})

		graph.Edges = append(graph.Edges, Edge{
			From:  botID,
			To:    dynID,
			Label: "executes",
		})
	}

	// Process MCP Servers
	for _, mcpSrv := range cfg.Capabilities.MCPServers {
		mcpID := nextID()

		// Determine if it's a sub-agent bot
		isSubAgent := false
		subConfigPath := ""

		if mcpSrv.Type == "cli" {
			// Check if the path or any arg indicates it's the server-bot
			isBotCommand := false
			if mcpSrv.Path == "server-bot" || filepath.Base(mcpSrv.Path) == "server-bot" {
				isBotCommand = true
			}
			for _, arg := range mcpSrv.Args {
				if arg == "cmd/server-bot/main.go" || arg == "server-bot" {
					isBotCommand = true
				}
			}

			if isBotCommand {
				// look for --config or -config
				for i, arg := range mcpSrv.Args {
					if (arg == "--config" || arg == "-config") && i+1 < len(mcpSrv.Args) {
						subConfigPath = mcpSrv.Args[i+1]
						isSubAgent = true
						break
					}
				}
			}
		}

		if isSubAgent {
			// It's a sub-agent, process recursively
			err := processConfig(subConfigPath, botID)
			if err != nil {
				log.Printf("Warning: failed to process sub-agent config %s: %v", subConfigPath, err)
			}
		} else {
			// Normal MCP Server
			var mcpLabel string
			if mcpSrv.Type == "http" {
				mcpLabel = fmt.Sprintf("MCP (HTTP)\n%s", mcpSrv.Path)
			} else {
				mcpLabel = fmt.Sprintf("MCP (CLI)\n%s", mcpSrv.Path)
			}

			// Format details for content
			mcpContent := fmt.Sprintf("Type: %s\nPath: %s\nArgs: %v\nEnv: %v", mcpSrv.Type, mcpSrv.Path, mcpSrv.Args, mcpSrv.Env)

			graph.Nodes = append(graph.Nodes, Node{
				ID:      mcpID,
				Label:   mcpLabel,
				Group:   "mcp",
				Content: mcpContent,
			})

			graph.Edges = append(graph.Edges, Edge{
				From:  botID,
				To:    mcpID,
				Label: "connects",
			})
		}
	}

	return nil
}

const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Bot Setup Visualization</title>
    <script type="text/javascript" src="https://unpkg.com/vis-network/standalone/umd/vis-network.min.js"></script>
    <style type="text/css">
        body {
            font-family: sans-serif;
            margin: 0;
            padding: 0;
            display: flex;
            height: 100vh;
        }
        #mynetwork {
            flex: 1;
            border-right: 1px solid #ccc;
        }
        #sidebar {
            width: 400px;
            padding: 20px;
            overflow-y: auto;
            background-color: #f9f9f9;
        }
        pre {
            white-space: pre-wrap;
            word-wrap: break-word;
            background-color: #eee;
            padding: 10px;
            border-radius: 5px;
        }
    </style>
</head>
<body>

<div id="mynetwork"></div>
<div id="sidebar">
    <h2>Details</h2>
    <div id="content">Click a node to see its contents here.</div>
</div>

<script type="text/javascript">
    // create an array with nodes
    var nodesArray = {{.NodesJSON}};
    var edgesArray = {{.EdgesJSON}};

    var nodes = new vis.DataSet(nodesArray);
    var edges = new vis.DataSet(edgesArray);

    var container = document.getElementById('mynetwork');
    var data = {
        nodes: nodes,
        edges: edges
    };
    var options = {
        nodes: {
            shape: 'box',
            font: {
                size: 14
            }
        },
        groups: {
            bot: { color: { background: '#97c2fc', border: '#2b7ce9' } },
            skill: { color: { background: '#ffffcc', border: '#cccc00' } },
            mcp: { color: { background: '#ffcc99', border: '#ff9933' } }
        },
        layout: {
            hierarchical: {
                direction: 'UD',
                sortMethod: 'directed'
            }
        },
        physics: {
            enabled: false
        }
    };
    var network = new vis.Network(container, data, options);

    var contentDiv = document.getElementById('content');

    network.on("click", function (params) {
        if (params.nodes.length > 0) {
            var nodeId = params.nodes[0];
            var node = nodes.get(nodeId);
            if (node && node.content) {
                contentDiv.innerHTML = "<h3>" + (node.label.replace(/\n/g, '<br>')) + "</h3><pre>" + escapeHtml(node.content) + "</pre>";
            } else {
                contentDiv.innerHTML = "No content available.";
            }
        }
    });

    function escapeHtml(unsafe) {
        return unsafe
             .replace(/&/g, "&amp;")
             .replace(/</g, "&lt;")
             .replace(/>/g, "&gt;")
             .replace(/"/g, "&quot;")
             .replace(/'/g, "&#039;");
    }
</script>
</body>
</html>
`

func generateHTML(outPath string) error {
	nodesJSON, err := json.Marshal(graph.Nodes)
	if err != nil {
		return err
	}
	edgesJSON, err := json.Marshal(graph.Edges)
	if err != nil {
		return err
	}

	tmpl, err := template.New("index").Parse(htmlTemplate)
	if err != nil {
		return err
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()

	data := struct {
		NodesJSON template.JS
		EdgesJSON template.JS
	}{
		NodesJSON: template.JS(nodesJSON),
		EdgesJSON: template.JS(edgesJSON),
	}

	return tmpl.Execute(f, data)
}
