# MCP

> [!NOTE]
> see: `https://github.com/<ORG>/<REPO>/settings/copilot/coding_agent`
>
> [Documentation:](https://docs.github.com/en/enterprise-cloud@latest/copilot/how-tos/copilot-on-github/customize-copilot/customize-cloud-agent/extend-cloud-agent-with-mcp)

```jsonl
 {
    // ~/.copilot/mcp-config.json || on github repo
   "mcpServers": {
     "github-mcp-server": {
       "type": "http",
       // Remove "/readonly" to enable wider access to all tools.
       // Then, use the "X-MCP-Toolsets" header to specify which toolsets you'd like to include.
       // Use the "tools" field to select individual tools from the toolsets.
       "url": "https://api.githubcopilot.com/mcp/readonly",
       "tools": ["*"],
       "headers": {
         "X-MCP-Toolsets": "repos,issues,users,pull_requests,code_security,secret_protection,actions,web_search"
       }
     },
        "context7": {
      "type": "http",
      "url": "https://mcp.context7.com/mcp",
      "headers": {
        "CONTEXT7_API_KEY": "COPILOT_MCP_CONTEXT7_API_KEY"
      },
      "tools": ["query-docs", "resolve-library-id"]
    }
   }
 }
```
