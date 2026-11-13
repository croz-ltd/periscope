// How each MCP client is told about this hub. Every client gets a command and
// a config file, both built from the hub URL. The list is sorted by name.
//
// Where a client reads the token from the environment, the snippets use
// PERISCOPE_TOKEN, so a file that holds the configuration does not hold the
// secret and can be shared. Where it cannot, the snippet carries the token
// itself and says so.
//
// The syntax follows each client's own documentation as of November 2026.

export const TOKEN_ENV = 'PERISCOPE_TOKEN'

export interface Snippet {
  text: string
  // Where to run it or put it, and what to know about it.
  note: string
}

export interface McpClient {
  id: string
  name: string
  command: Snippet
  config: Snippet & { file: string }
}

const json = (v: unknown) => JSON.stringify(v, null, 2)

// clientsFor builds the snippets. withAuth is false for a hub running with the
// login off, which needs no header at all. literalToken is what goes where a
// client cannot read the environment: the real token right after it was
// issued, a placeholder otherwise.
export function clientsFor(url: string, withAuth: boolean, literalToken: string): McpClient[] {
  const bearerEnv = `Bearer $${TOKEN_ENV}`
  const header = withAuth ? ` \\\n  --header "Authorization: ${bearerEnv}"` : ''
  const headers = (value: string) => (withAuth ? { headers: { Authorization: value } } : {})

  const vsInline = JSON.stringify({
    name: 'periscope',
    type: 'http',
    url,
    ...headers(bearerEnv),
  }).replace(/"/g, '\\"')

  return [
    {
      id: 'claude-code',
      name: 'Claude Code',
      command: {
        text: `claude mcp add --transport http --scope user periscope ${url}${header}`,
        note: '--scope user adds Periscope to all your projects. Leave it out to add it to the current repository only.',
      },
      config: {
        file: '.mcp.json in the repository, or ~/.claude.json',
        text: json({ mcpServers: { periscope: { type: 'http', url, ...headers(`Bearer \${${TOKEN_ENV}}`) } } }),
        note: withAuth
          ? `Claude Code reads \${${TOKEN_ENV}} when it starts, so the file holds no secret and can be committed.`
          : '',
      },
    },
    {
      id: 'codex-cli',
      name: 'Codex CLI',
      command: {
        text: `codex mcp add periscope --url ${url}${withAuth ? ` \\\n  --bearer-token-env-var ${TOKEN_ENV}` : ''}`,
        note: withAuth
          ? `Codex reads ${TOKEN_ENV} every time it starts, so set it in your shell profile.`
          : '',
      },
      config: {
        file: '~/.codex/config.toml, or .codex/config.toml in a trusted project',
        text: `[mcp_servers.periscope]\nurl = "${url}"${withAuth ? `\nbearer_token_env_var = "${TOKEN_ENV}"` : ''}`,
        note: withAuth ? `The file names the variable, never the token.` : '',
      },
    },
    {
      id: 'copilot-cli',
      name: 'Copilot CLI',
      command: {
        text: `copilot mcp add --transport http periscope ${url}${header}`,
        note: 'Saves the server in ~/.copilot/mcp-config.json.',
      },
      config: {
        file: '~/.copilot/mcp-config.json',
        text: json({
          mcpServers: {
            periscope: { type: 'http', url, ...headers(`Bearer ${literalToken}`), tools: ['*'] },
          },
        }),
        note: withAuth ? 'This file holds the token itself. Keep it private and out of version control.' : '',
      },
    },
    {
      id: 'gemini-cli',
      name: 'Gemini CLI',
      command: {
        text: `gemini mcp add --scope user --transport http${withAuth ? ` \\\n  --header "Authorization: ${bearerEnv}"` : ''} \\\n  periscope ${url}`,
        note: '--scope user writes ~/.gemini/settings.json. Leave it out to write .gemini/settings.json in the project.',
      },
      config: {
        file: '~/.gemini/settings.json, or .gemini/settings.json in the project',
        text: json({ mcpServers: { periscope: { httpUrl: url, ...headers(bearerEnv) } } }),
        note: withAuth ? `Gemini CLI reads $${TOKEN_ENV} when it starts, so the file holds no secret.` : '',
      },
    },
    {
      id: 'vs-code',
      name: 'VS Code',
      command: {
        text: `code --add-mcp "${vsInline}"`,
        note: withAuth
          ? 'Adds Periscope to your user profile. The shell puts the token in, so the profile holds it.'
          : 'Adds Periscope to your user profile.',
      },
      config: {
        file: '.vscode/mcp.json in the workspace',
        text: json(
          withAuth
            ? {
                servers: {
                  periscope: {
                    type: 'http',
                    url,
                    headers: { Authorization: 'Bearer ${input:periscope-token}' },
                  },
                },
                inputs: [
                  {
                    type: 'promptString',
                    id: 'periscope-token',
                    description: 'Periscope MCP token',
                    password: true,
                  },
                ],
              }
            : { servers: { periscope: { type: 'http', url } } },
        ),
        note: withAuth
          ? 'VS Code asks for the token the first time and keeps it in its secret storage, so the file can be committed.'
          : '',
      },
    },
  ]
}
