# Installing Skills for OpenCode

Enable Nexus skills in OpenCode via native skill discovery from `skills/`.

## Prerequisites

- [OpenCode.ai](https://opencode.ai) installed

## Installation

The published `nexus-skills` package installs the `nexus` plugin, which exposes the skills in `./skills/`.

Add to your OpenCode config:

```json
{
  "plugin": ["nexus-skills@git+https://github.com/sunprema/nexus-cli.git"]
}
```

To pin a specific version:

```json
{
  "plugin": ["nexus-skills@git+https://github.com/sunprema/nexus-cli.git#v0.1.0"]
}
```

Restart OpenCode. The plugin in `.opencode/plugins/nexus.js` automatically registers the skills directory — no additional configuration needed.

Verify by asking: "Use the `narrate` skill."
